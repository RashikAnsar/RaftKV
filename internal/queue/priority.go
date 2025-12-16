package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/RashikAnsar/raftkv/internal/storage"
)

// PriorityQueue implements a priority queue using RaftKV storage
// Higher priority values are dequeued first
// Within the same priority level, FIFO ordering is used
type PriorityQueue struct {
	mu     sync.RWMutex
	config *QueueConfig
	store  storage.Store
	stats  *QueueStats

	// In-flight messages (visibility timeout tracking)
	inFlight   map[string]*inflightMessage
	inFlightMu sync.RWMutex

	// Counters
	deliveredCount    int64
	acknowledgedCount int64
	failedCount       int64

	ctx    context.Context
	cancel context.CancelFunc
}

// NewPriorityQueue creates a new priority queue
func NewPriorityQueue(config *QueueConfig, store storage.Store) (*PriorityQueue, error) {
	if config == nil {
		return nil, fmt.Errorf("queue config cannot be nil")
	}
	if store == nil {
		return nil, fmt.Errorf("store cannot be nil")
	}

	ctx, cancel := context.WithCancel(context.Background())

	q := &PriorityQueue{
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
func (q *PriorityQueue) Enqueue(msg *Message) error {
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

	// Add to priority queue index
	// Key format: queue:index:priority:{queueName}:{priority}:{timestamp}:{messageID}
	// This ensures higher priority messages come first, and within same priority, FIFO order
	indexKey := q.queueIndexKey(msg.ID, msg.Priority, msg.CreatedAt)
	indexValue := fmt.Sprintf("%d:%d:%s", msg.Priority, msg.CreatedAt.UnixNano(), msg.ID)
	if err := q.store.Put(q.ctx, indexKey, []byte(indexValue)); err != nil {
		// Cleanup message if index fails
		q.store.Delete(q.ctx, key)
		return fmt.Errorf("failed to update queue index: %w", err)
	}

	q.stats.Length++
	q.stats.UpdatedAt = time.Now()

	return nil
}

// Dequeue retrieves and removes the next highest priority message from the queue
func (q *PriorityQueue) Dequeue() (*Message, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Get next message ID from index (highest priority first)
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

// Peek retrieves the next highest priority message without removing it
func (q *PriorityQueue) Peek() (*Message, error) {
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
func (q *PriorityQueue) Acknowledge(messageID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Remove from in-flight tracking
	q.inFlightMu.Lock()
	delete(q.inFlight, messageID)
	q.inFlightMu.Unlock()

	// Get message to retrieve priority for index deletion
	msg, err := q.getUnlocked(messageID)
	if err != nil {
		return err
	}

	// Delete message
	if err := q.deleteUnlocked(messageID); err != nil {
		return err
	}

	// Delete priority index entry
	indexKey := q.queueIndexKey(msg.ID, msg.Priority, msg.CreatedAt)
	q.store.Delete(q.ctx, indexKey)

	q.acknowledgedCount++
	q.stats.UpdatedAt = time.Now()

	return nil
}

// Nack negatively acknowledges a message (requeue or send to DLQ)
func (q *PriorityQueue) Nack(messageID string, requeue bool) error {
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
func (q *PriorityQueue) Delete(messageID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.deleteUnlocked(messageID)
}

// Get retrieves a specific message by ID
func (q *PriorityQueue) Get(messageID string) (*Message, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.getUnlocked(messageID)
}

// Len returns the number of messages in the queue
func (q *PriorityQueue) Len() int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.lenUnlocked()
}

// Stats returns queue statistics
func (q *PriorityQueue) Stats() *QueueStats {
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
		msgID := extractMessageIDFromPriorityKey(key, prefix)
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
func (q *PriorityQueue) Purge() error {
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
		msgID := extractMessageIDFromPriorityKey(key, prefix)
		q.deleteUnlocked(msgID)
		// Delete index entry
		q.store.Delete(q.ctx, key)
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
func (q *PriorityQueue) Close() error {
	q.cancel()
	return nil
}

// Internal helper methods

func (q *PriorityQueue) messageKey(messageID string) string {
	return fmt.Sprintf("queue:msg:%s:%s", q.config.Name, messageID)
}

func (q *PriorityQueue) queueIndexKey(messageID string, priority int, createdAt time.Time) string {
	// Use negative priority for sorting (higher priority first)
	// Format: queue:index:priority:{queueName}:{-priority}:{timestamp}:{messageID}
	return fmt.Sprintf("queue:index:priority:%s:%010d:%d:%s",
		q.config.Name,
		999999999-priority, // Invert priority so higher values come first lexicographically
		createdAt.UnixNano(),
		messageID,
	)
}

func (q *PriorityQueue) queueIndexPrefix() string {
	return fmt.Sprintf("queue:index:priority:%s:", q.config.Name)
}

func (q *PriorityQueue) getNextMessageIDUnlocked() (string, error) {
	prefix := q.queueIndexPrefix()
	keys, err := q.store.List(q.ctx, prefix, 0)
	if err != nil {
		return "", fmt.Errorf("failed to get next message: %w", err)
	}

	if len(keys) == 0 {
		return "", nil
	}

	// Sort keys to get highest priority first (lowest numerical value due to inversion)
	sort.Strings(keys)

	// Check each message in priority order
	for _, key := range keys {
		messageID := extractMessageIDFromPriorityKey(key, prefix)

		// Check if message is in-flight
		q.inFlightMu.RLock()
		inFlight, exists := q.inFlight[messageID]
		q.inFlightMu.RUnlock()

		if exists && time.Now().Before(inFlight.VisibleAt) {
			// Message is in-flight and not yet visible, try next
			continue
		}

		// Check if message is scheduled or expired
		msg, err := q.getUnlocked(messageID)
		if err != nil {
			continue
		}

		if msg.IsExpired() {
			// Delete expired message and continue
			q.deleteUnlocked(messageID)
			continue
		}

		if msg.IsScheduled() {
			// Message not ready yet, continue
			continue
		}

		// Found a valid message
		return messageID, nil
	}

	// No available messages
	return "", nil
}

func (q *PriorityQueue) getUnlocked(messageID string) (*Message, error) {
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

func (q *PriorityQueue) deleteUnlocked(messageID string) error {
	// Get message first to get priority for index deletion
	msg, err := q.getUnlocked(messageID)
	if err != nil {
		return err
	}

	// Delete message
	key := q.messageKey(messageID)
	if err := q.store.Delete(q.ctx, key); err != nil {
		return fmt.Errorf("failed to delete message: %w", err)
	}

	// Delete index entry
	indexKey := q.queueIndexKey(msg.ID, msg.Priority, msg.CreatedAt)
	if err := q.store.Delete(q.ctx, indexKey); err != nil {
		return fmt.Errorf("failed to delete index entry: %w", err)
	}

	q.stats.Length--
	q.stats.UpdatedAt = time.Now()

	return nil
}

func (q *PriorityQueue) lenUnlocked() int {
	prefix := q.queueIndexPrefix()
	keys, err := q.store.List(q.ctx, prefix, 0)
	if err != nil {
		return 0
	}
	return len(keys)
}

func (q *PriorityQueue) sendToDLQ(msg *Message) error {
	// TODO: Implement DLQ support
	// For now, just log the message
	return nil
}

// visibilityTimeoutLoop handles visibility timeout for in-flight messages
func (q *PriorityQueue) visibilityTimeoutLoop() {
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

func (q *PriorityQueue) checkVisibilityTimeouts() {
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

// Helper function to extract message ID from priority queue index key
// Format: queue:index:priority:{queueName}:{priority}:{timestamp}:{messageID}
func extractMessageIDFromPriorityKey(key, prefix string) string {
	// Remove prefix
	remainder := key[len(prefix):]

	// Find the last colon - everything after it is the message ID
	lastColon := -1
	for i := len(remainder) - 1; i >= 0; i-- {
		if remainder[i] == ':' {
			lastColon = i
			break
		}
	}

	if lastColon == -1 {
		return remainder
	}

	return remainder[lastColon+1:]
}
