package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/RashikAnsar/raftkv/internal/storage"
	"go.uber.org/zap"
)

// QueueManager manages multiple queues
type QueueManager struct {
	mu     sync.RWMutex
	queues map[string]Queue
	store  storage.Store
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc
}

// NewQueueManager creates a new queue manager
func NewQueueManager(store storage.Store, logger *zap.Logger) *QueueManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &QueueManager{
		queues: make(map[string]Queue),
		store:  store,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
}

// CreateQueue creates a new queue with the given configuration
func (qm *QueueManager) CreateQueue(config *QueueConfig) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	if config == nil {
		return fmt.Errorf("queue config cannot be nil")
	}

	if config.Name == "" {
		return fmt.Errorf("queue name cannot be empty")
	}

	// Check if queue already exists
	if _, exists := qm.queues[config.Name]; exists {
		return fmt.Errorf("queue already exists: %s", config.Name)
	}

	// Create the appropriate queue type
	var q Queue
	var err error

	switch config.Type {
	case "fifo", "":
		q, err = NewFIFOQueue(config, qm.store)
	case "priority":
		q, err = NewPriorityQueue(config, qm.store)
	default:
		return fmt.Errorf("unsupported queue type: %s", config.Type)
	}

	if err != nil {
		return fmt.Errorf("failed to create queue: %w", err)
	}

	qm.queues[config.Name] = q

	// Store queue configuration
	if err := qm.storeQueueConfig(config); err != nil {
		qm.logger.Error("Failed to store queue config",
			zap.String("queue", config.Name),
			zap.Error(err),
		)
		// Don't fail - queue is already created in memory
	}

	qm.logger.Info("Queue created",
		zap.String("name", config.Name),
		zap.String("type", config.Type),
	)

	return nil
}

// DeleteQueue deletes a queue
func (qm *QueueManager) DeleteQueue(name string) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	q, exists := qm.queues[name]
	if !exists {
		return fmt.Errorf("queue not found: %s", name)
	}

	// Purge all messages
	if err := q.Purge(); err != nil {
		return fmt.Errorf("failed to purge queue: %w", err)
	}

	// Close the queue
	if err := q.Close(); err != nil {
		qm.logger.Warn("Error closing queue",
			zap.String("queue", name),
			zap.Error(err),
		)
	}

	// Remove from map
	delete(qm.queues, name)

	// Delete queue configuration
	if err := qm.deleteQueueConfig(name); err != nil {
		qm.logger.Error("Failed to delete queue config",
			zap.String("queue", name),
			zap.Error(err),
		)
	}

	qm.logger.Info("Queue deleted", zap.String("name", name))

	return nil
}

// GetQueue returns a queue by name
func (qm *QueueManager) GetQueue(name string) (Queue, error) {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	q, exists := qm.queues[name]
	if !exists {
		return nil, fmt.Errorf("queue not found: %s", name)
	}

	return q, nil
}

// ListQueues returns a list of all queue names
func (qm *QueueManager) ListQueues() []string {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	names := make([]string, 0, len(qm.queues))
	for name := range qm.queues {
		names = append(names, name)
	}

	return names
}

// GetQueueStats returns statistics for all queues
func (qm *QueueManager) GetQueueStats() map[string]*QueueStats {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	stats := make(map[string]*QueueStats, len(qm.queues))
	for name, q := range qm.queues {
		stats[name] = q.Stats()
	}

	return stats
}

// Enqueue adds a message to a queue
func (qm *QueueManager) Enqueue(queueName string, msg *Message) error {
	q, err := qm.GetQueue(queueName)
	if err != nil {
		return err
	}

	return q.Enqueue(msg)
}

// Dequeue retrieves and removes the next message from a queue
func (qm *QueueManager) Dequeue(queueName string) (*Message, error) {
	q, err := qm.GetQueue(queueName)
	if err != nil {
		return nil, err
	}

	return q.Dequeue()
}

// Peek retrieves the next message without removing it
func (qm *QueueManager) Peek(queueName string) (*Message, error) {
	q, err := qm.GetQueue(queueName)
	if err != nil {
		return nil, err
	}

	return q.Peek()
}

// Acknowledge marks a message as processed
func (qm *QueueManager) Acknowledge(queueName, messageID string) error {
	q, err := qm.GetQueue(queueName)
	if err != nil {
		return err
	}

	return q.Acknowledge(messageID)
}

// Nack negatively acknowledges a message
func (qm *QueueManager) Nack(queueName, messageID string, requeue bool) error {
	q, err := qm.GetQueue(queueName)
	if err != nil {
		return err
	}

	return q.Nack(messageID, requeue)
}

// Delete removes a specific message from a queue
func (qm *QueueManager) Delete(queueName, messageID string) error {
	q, err := qm.GetQueue(queueName)
	if err != nil {
		return err
	}

	return q.Delete(messageID)
}

// Get retrieves a specific message by ID
func (qm *QueueManager) Get(queueName, messageID string) (*Message, error) {
	q, err := qm.GetQueue(queueName)
	if err != nil {
		return nil, err
	}

	return q.Get(messageID)
}

// Purge removes all messages from a queue
func (qm *QueueManager) Purge(queueName string) error {
	q, err := qm.GetQueue(queueName)
	if err != nil {
		return err
	}

	return q.Purge()
}

// GetQueueConfig retrieves the configuration for a queue
func (qm *QueueManager) GetQueueConfig(name string) (*QueueConfig, error) {
	key := qm.configKey(name)
	data, err := qm.store.Get(qm.ctx, key)
	if err != nil {
		if err == storage.ErrKeyNotFound {
			return nil, fmt.Errorf("queue config not found: %s", name)
		}
		return nil, fmt.Errorf("failed to get queue config: %w", err)
	}

	var config QueueConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal queue config: %w", err)
	}

	return &config, nil
}

// LoadQueues loads all queue configurations from storage and recreates them
func (qm *QueueManager) LoadQueues() error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	prefix := "queue:config:"
	keys, err := qm.store.List(qm.ctx, prefix, 0)
	if err != nil {
		return fmt.Errorf("failed to list queue configs: %w", err)
	}

	for _, key := range keys {
		data, err := qm.store.Get(qm.ctx, key)
		if err != nil {
			qm.logger.Warn("Failed to load queue config",
				zap.String("key", key),
				zap.Error(err),
			)
			continue
		}

		var config QueueConfig
		if err := json.Unmarshal(data, &config); err != nil {
			qm.logger.Warn("Failed to unmarshal queue config",
				zap.String("key", key),
				zap.Error(err),
			)
			continue
		}

		// Skip if already loaded
		if _, exists := qm.queues[config.Name]; exists {
			continue
		}

		// Create the queue
		var q Queue
		switch config.Type {
		case "fifo", "":
			q, err = NewFIFOQueue(&config, qm.store)
		case "priority":
			q, err = NewPriorityQueue(&config, qm.store)
		default:
			qm.logger.Warn("Unsupported queue type",
				zap.String("queue", config.Name),
				zap.String("type", config.Type),
			)
			continue
		}

		if err != nil {
			qm.logger.Error("Failed to create queue",
				zap.String("queue", config.Name),
				zap.Error(err),
			)
			continue
		}

		qm.queues[config.Name] = q
		qm.logger.Info("Queue loaded",
			zap.String("name", config.Name),
			zap.String("type", config.Type),
		)
	}

	return nil
}

// Close closes all queues
func (qm *QueueManager) Close() error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	qm.cancel()

	for name, q := range qm.queues {
		if err := q.Close(); err != nil {
			qm.logger.Warn("Error closing queue",
				zap.String("queue", name),
				zap.Error(err),
			)
		}
	}

	qm.queues = make(map[string]Queue)

	return nil
}

// Batch operations

// BatchEnqueue enqueues multiple messages to a queue
func (qm *QueueManager) BatchEnqueue(queueName string, messages []*Message) ([]error, error) {
	q, err := qm.GetQueue(queueName)
	if err != nil {
		return nil, err
	}

	errors := make([]error, len(messages))
	for i, msg := range messages {
		errors[i] = q.Enqueue(msg)
	}

	return errors, nil
}

// BatchDequeue dequeues multiple messages from a queue
func (qm *QueueManager) BatchDequeue(queueName string, count int) ([]*Message, error) {
	q, err := qm.GetQueue(queueName)
	if err != nil {
		return nil, err
	}

	messages := make([]*Message, 0, count)
	for i := 0; i < count; i++ {
		msg, err := q.Dequeue()
		if err != nil {
			// Stop on first error (likely empty queue)
			break
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// BatchAcknowledge acknowledges multiple messages
func (qm *QueueManager) BatchAcknowledge(queueName string, messageIDs []string) ([]error, error) {
	q, err := qm.GetQueue(queueName)
	if err != nil {
		return nil, err
	}

	errors := make([]error, len(messageIDs))
	for i, msgID := range messageIDs {
		errors[i] = q.Acknowledge(msgID)
	}

	return errors, nil
}

// Scheduled message processing

// ProcessScheduledMessages processes scheduled messages that are now ready
func (qm *QueueManager) ProcessScheduledMessages() error {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	for name, q := range qm.queues {
		stats := q.Stats()
		if stats.Scheduled > 0 {
			qm.logger.Debug("Processing scheduled messages",
				zap.String("queue", name),
				zap.Int("scheduled", stats.Scheduled),
			)
		}
	}

	return nil
}

// StartScheduledMessageProcessor starts a background goroutine to process scheduled messages
func (qm *QueueManager) StartScheduledMessageProcessor(interval time.Duration) chan struct{} {
	stopCh := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-qm.ctx.Done():
				return
			case <-ticker.C:
				if err := qm.ProcessScheduledMessages(); err != nil {
					qm.logger.Error("Failed to process scheduled messages",
						zap.Error(err),
					)
				}
			}
		}
	}()

	return stopCh
}

// Internal helper methods

func (qm *QueueManager) configKey(queueName string) string {
	return fmt.Sprintf("queue:config:%s", queueName)
}

func (qm *QueueManager) storeQueueConfig(config *QueueConfig) error {
	key := qm.configKey(config.Name)
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return qm.store.Put(qm.ctx, key, data)
}

func (qm *QueueManager) deleteQueueConfig(queueName string) error {
	key := qm.configKey(queueName)
	return qm.store.Delete(qm.ctx, key)
}
