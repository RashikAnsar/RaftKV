package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/RashikAnsar/raftkv/internal/storage"
)

// FIFOQueue implements a first-in-first-out queue using RaftKV storage
type FIFOQueue struct {
	mu     sync.RWMutex
	config *QueueConfig
	store  storage.Store
	stats  *QueueStats

	// In-flight messages (visibility timeout tracking)
	inFlight    map[string]*inflightMessage
	inFlightMu  sync.RWMutex

	// Counters
	deliveredCount   int64
	acknowledgedCount int64
	failedCount      int64

	ctx    context.Context
	cancel context.CancelFunc
}

type inflightMessage struct {
	MessageID      string
	VisibleAt      time.Time
	DeliveryCount  int
}

// NewFIFOQueue creates a new FIFO queue
func NewFIFOQueue(config *QueueConfig, store storage.Store) (*FIFOQueue, error) {
	if config == nil {
		return nil, fmt.Errorf("queue config cannot be nil")
	}
	if store == nil {
		return nil, fmt.Errorf("store cannot be nil")
	}

	ctx, cancel := context.WithCancel(context.Background())

	q := &FIFOQueue{
		config:   config,
		store:    store,
		inFlight: make(map[string]*inflightMessage),
		ctx:      ctx,
		cancel:   cancel,
		stats: &QueueStats{
			Name:      config.Name,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	// Start background cleanup for visibility timeouts
	go q.visibilityTimeoutLoop()

	return q, nil
}

// Enqueue adds a message to the queue
func (q *FIFOQueue) Enqueue(msg *Message) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Check max length
	if q.config.MaxLength > 0 {
		length := q.lenUnlocked()
		if length >= q.config.MaxLength {
			return fmt.Errorf("queue is full (max length: %d)", q.config.MaxLength)
		}
	}

	// Set queue name and creation time
	msg.QueueName = q.config.Name
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}

	// Set expiration if configured
	if q.config.MessageTTL > 0 && msg.ExpiresAt == nil {
		expiresAt := time.Now().Add(q.config.MessageTTL)
		msg.ExpiresAt = &expiresAt
	}

	// Store message
	key := q.messageKey(msg.ID)
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	if err := q.store.Put(q.ctx, key, data); err != nil {
		return fmt.Errorf("failed to store message: %w", err)
	}

	// Add to queue index (include timestamp in key for FIFO ordering)
	indexKey := q.queueIndexKey(msg.ID, msg.CreatedAt)
	indexValue := fmt.Sprintf("%d:%s", msg.CreatedAt.UnixNano(), msg.ID)
	if err := q.store.Put(q.ctx, indexKey, []byte(indexValue)); err != nil {
		// Cleanup message if index fails
		q.store.Delete(q.ctx, key)
		return fmt.Errorf("failed to update queue index: %w", err)
	}

	q.stats.Length++
	q.stats.UpdatedAt = time.Now()

	return nil
}

// Dequeue retrieves and removes the next message from the queue
func (q *FIFOQueue) Dequeue() (*Message, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Get next message ID from index
	messageID, err := q.getNextMessageIDUnlocked()
	if err != nil {
		return nil, err
	}
	if messageID == "" {
		return nil, fmt.Errorf("queue is empty")
	}

	// Retrieve message
	msg, err := q.getUnlocked(messageID)
	if err != nil {
		return nil, err
	}

	// Check if expired
	if msg.IsExpired() {
		// Delete expired message
		q.deleteUnlocked(messageID)
		return nil, fmt.Errorf("message expired")
	}

	// Check if scheduled
	if msg.IsScheduled() {
		return nil, fmt.Errorf("no messages ready (next scheduled for %s)", msg.ScheduledAt.Format(time.RFC3339))
	}

	// Mark as in-flight (visibility timeout)
	msg.IncrementDeliveryCount()
	q.inFlightMu.Lock()
	q.inFlight[messageID] = &inflightMessage{
		MessageID:     messageID,
		VisibleAt:     time.Now().Add(q.config.VisibilityTimeout),
		DeliveryCount: msg.DeliveryCount,
	}
	q.inFlightMu.Unlock()

	// Update message delivery count in storage
	key := q.messageKey(messageID)
	data, _ := json.Marshal(msg)
	q.store.Put(q.ctx, key, data)

	q.deliveredCount++
	q.stats.UpdatedAt = time.Now()

	return msg, nil
}

// Peek retrieves the next message without removing it
func (q *FIFOQueue) Peek() (*Message, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	messageID, err := q.getNextMessageIDUnlocked()
	if err != nil {
		return nil, err
	}
	if messageID == "" {
		return nil, fmt.Errorf("queue is empty")
	}

	return q.getUnlocked(messageID)
}

// Acknowledge marks a message as processed and removes it from the queue
func (q *FIFOQueue) Acknowledge(messageID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Remove from in-flight tracking
	q.inFlightMu.Lock()
	delete(q.inFlight, messageID)
	q.inFlightMu.Unlock()

	// Delete message
	if err := q.deleteUnlocked(messageID); err != nil {
		return err
	}

	q.acknowledgedCount++
	q.stats.UpdatedAt = time.Now()

	return nil
}

// Nack negatively acknowledges a message (requeue or send to DLQ)
func (q *FIFOQueue) Nack(messageID string, requeue bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Get message
	msg, err := q.getUnlocked(messageID)
	if err != nil {
		return err
	}

	// Remove from in-flight tracking
	q.inFlightMu.Lock()
	delete(q.inFlight, messageID)
	q.inFlightMu.Unlock()

	// Check max retries
	if q.config.MaxRetries > 0 && msg.DeliveryCount >= q.config.MaxRetries {
		// Send to DLQ
		if q.config.DLQEnabled {
			msg.DLQReason = fmt.Sprintf("max retries exceeded (%d)", q.config.MaxRetries)
			if err := q.sendToDLQ(msg); err != nil {
				return fmt.Errorf("failed to send to DLQ: %w", err)
			}
		}
		// Delete from main queue
		q.deleteUnlocked(messageID)
		q.failedCount++
		return nil
	}

	if !requeue {
		// Delete message
		q.deleteUnlocked(messageID)
		q.failedCount++
		return nil
	}

	// Requeue - message stays in queue, just remove from in-flight
	// It will be available for delivery again
	return nil
}

// Delete removes a specific message from the queue
func (q *FIFOQueue) Delete(messageID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.deleteUnlocked(messageID)
}

// Get retrieves a specific message by ID
func (q *FIFOQueue) Get(messageID string) (*Message, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.getUnlocked(messageID)
}

// Len returns the number of messages in the queue
func (q *FIFOQueue) Len() int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.lenUnlocked()
}

// Stats returns queue statistics
func (q *FIFOQueue) Stats() *QueueStats {
	q.mu.RLock()
	defer q.mu.RUnlock()

	stats := *q.stats
	stats.Length = q.lenUnlocked()
	stats.DeliveredCount = q.deliveredCount
	stats.AcknowledgedCount = q.acknowledgedCount
	stats.FailedCount = q.failedCount

	// Count ready vs scheduled messages
	stats.Ready = 0
	stats.Scheduled = 0
	prefix := q.queueIndexPrefix()
	keys, _ := q.store.List(q.ctx, prefix, 0)
	for _, key := range keys {
		// Extract message ID from index key
		// Key format: queue:index:fifo:{queueName}:{timestamp}:{messageID}
		msgID := extractMessageIDFromKey(key, prefix)
		if msg, err := q.getUnlocked(msgID); err == nil {
			if msg.IsScheduled() {
				stats.Scheduled++
			} else {
				stats.Ready++
			}
		}
	}

	return &stats
}

// Purge removes all messages from the queue
func (q *FIFOQueue) Purge() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Get all message IDs
	prefix := q.queueIndexPrefix()
	keys, err := q.store.List(q.ctx, prefix, 0)
	if err != nil {
		return fmt.Errorf("failed to list queue messages: %w", err)
	}

	// Delete all messages and index entries
	for _, key := range keys {
		msgID := extractMessageIDFromKey(key, prefix)
		q.deleteUnlocked(msgID)
	}

	// Clear in-flight tracking
	q.inFlightMu.Lock()
	q.inFlight = make(map[string]*inflightMessage)
	q.inFlightMu.Unlock()

	q.stats.Length = 0
	q.stats.UpdatedAt = time.Now()

	return nil
}

// Close closes the queue
func (q *FIFOQueue) Close() error {
	q.cancel()
	return nil
}

// Internal helper methods

func (q *FIFOQueue) messageKey(messageID string) string {
	return fmt.Sprintf("queue:msg:%s:%s", q.config.Name, messageID)
}

func (q *FIFOQueue) queueIndexKey(messageID string, createdAt time.Time) string {
	// Include timestamp in key for proper FIFO ordering when keys are sorted
	return fmt.Sprintf("queue:index:fifo:%s:%d:%s", q.config.Name, createdAt.UnixNano(), messageID)
}

func (q *FIFOQueue) queueIndexPrefix() string {
	return fmt.Sprintf("queue:index:fifo:%s:", q.config.Name)
}

func (q *FIFOQueue) getNextMessageIDUnlocked() (string, error) {
	prefix := q.queueIndexPrefix()
	// Get all messages to allow skipping in-flight ones
	keys, err := q.store.List(q.ctx, prefix, 0)
	if err != nil {
		return "", fmt.Errorf("failed to get next message: %w", err)
	}

	if len(keys) == 0 {
		return "", nil
	}

	// Iterate through keys to find first non-in-flight message
	for _, key := range keys {
		// Extract message ID from index key
		// Key format: queue:index:fifo:{queueName}:{timestamp}:{messageID}
		// After removing prefix, we have: {timestamp}:{messageID}
		remainder := key[len(prefix):]

		// Find the last colon to extract message ID
		lastColon := -1
		for i := len(remainder) - 1; i >= 0; i-- {
			if remainder[i] == ':' {
				lastColon = i
				break
			}
		}

		var messageID string
		if lastColon != -1 {
			messageID = remainder[lastColon+1:]
		} else {
			messageID = remainder
		}

		// Check if message is in-flight
		q.inFlightMu.RLock()
		inFlight, exists := q.inFlight[messageID]
		q.inFlightMu.RUnlock()

		if exists && time.Now().Before(inFlight.VisibleAt) {
			// Message is in-flight and not yet visible, skip to next
			continue
		}

		// Found an available message
		return messageID, nil
	}

	// All messages are in-flight
	return "", nil
}

func (q *FIFOQueue) getUnlocked(messageID string) (*Message, error) {
	key := q.messageKey(messageID)
	data, err := q.store.Get(q.ctx, key)
	if err != nil {
		if err == storage.ErrKeyNotFound {
			return nil, fmt.Errorf("message not found: %s", messageID)
		}
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	return &msg, nil
}

func (q *FIFOQueue) deleteUnlocked(messageID string) error {
	// Get message first to retrieve timestamp for index key
	msg, err := q.getUnlocked(messageID)
	if err != nil {
		return err
	}

	// Delete message
	key := q.messageKey(messageID)
	if err := q.store.Delete(q.ctx, key); err != nil {
		return fmt.Errorf("failed to delete message: %w", err)
	}

	// Delete index entry (requires timestamp from message)
	indexKey := q.queueIndexKey(messageID, msg.CreatedAt)
	if err := q.store.Delete(q.ctx, indexKey); err != nil {
		return fmt.Errorf("failed to delete index entry: %w", err)
	}

	q.stats.Length--
	q.stats.UpdatedAt = time.Now()

	return nil
}

func (q *FIFOQueue) lenUnlocked() int {
	prefix := q.queueIndexPrefix()
	keys, err := q.store.List(q.ctx, prefix, 0)
	if err != nil {
		return 0
	}
	return len(keys)
}

func (q *FIFOQueue) sendToDLQ(msg *Message) error {
	// TODO: Implement DLQ support
	// For now, just log the message
	return nil
}

// visibilityTimeoutLoop handles visibility timeout for in-flight messages
func (q *FIFOQueue) visibilityTimeoutLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-q.ctx.Done():
			return
		case <-ticker.C:
			q.checkVisibilityTimeouts()
		}
	}
}

func (q *FIFOQueue) checkVisibilityTimeouts() {
	q.inFlightMu.Lock()
	defer q.inFlightMu.Unlock()

	now := time.Now()
	for messageID, inFlight := range q.inFlight {
		if now.After(inFlight.VisibleAt) {
			// Message visibility timeout expired, make it available again
			delete(q.inFlight, messageID)
		}
	}
}

// Helper function to extract message ID from index key
// Key format: queue:index:fifo:{queueName}:{timestamp}:{messageID}
func extractMessageIDFromKey(key, prefix string) string {
	remainder := key[len(prefix):]

	// Find the last colon to extract message ID
	lastColon := -1
	for i := len(remainder) - 1; i >= 0; i-- {
		if remainder[i] == ':' {
			lastColon = i
			break
		}
	}

	if lastColon != -1 {
		return remainder[lastColon+1:]
	}
	return remainder
}
