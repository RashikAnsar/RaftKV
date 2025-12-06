package cdc

import (
	"context"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
)

// Publisher publishes change events to subscribers
// Thread-safe implementation using channels and mutexes
type Publisher struct {
	mu          sync.RWMutex
	subscribers map[string]*Subscriber
	logger      *zap.Logger
	sequenceNum atomic.Uint64 // Monotonically increasing sequence number
	dcID        string         // Datacenter ID for event tagging
	closed      atomic.Bool
}

// NewPublisher creates a new CDC publisher
func NewPublisher(datacenterID string, logger *zap.Logger) *Publisher {
	return &Publisher{
		subscribers: make(map[string]*Subscriber),
		logger:      logger,
		dcID:        datacenterID,
	}
}

// Publish sends a change event to all subscribers
// This is called from FSM.Apply() after a change is successfully applied
func (p *Publisher) Publish(ctx context.Context, event *ChangeEvent) error {
	if p.closed.Load() {
		return ErrPublisherClosed
	}

	// Enrich event with metadata
	event.DatacenterID = p.dcID
	event.SequenceNum = p.sequenceNum.Add(1)

	p.mu.RLock()
	defer p.mu.RUnlock()

	// Publish to all subscribers
	// Note: We don't wait for subscribers to process, this is async
	publishedCount := 0
	for id, sub := range p.subscribers {
		// Apply subscriber filter if set
		if sub.filter != nil && !sub.filter(event) {
			continue
		}

		// Non-blocking send to subscriber
		select {
		case sub.events <- event.Clone(): // Clone to prevent data races
			publishedCount++
		default:
			// Channel full, subscriber is slow
			p.logger.Warn("Subscriber channel full, dropping event",
				zap.String("subscriber_id", id),
				zap.Uint64("raft_index", event.RaftIndex),
			)
			sub.metricsDropped.Add(1)
		}
	}

	p.logger.Debug("Published change event",
		zap.Uint64("raft_index", event.RaftIndex),
		zap.String("operation", event.Operation),
		zap.String("key", event.Key),
		zap.Int("subscribers", publishedCount),
	)

	return nil
}

// Subscribe adds a new subscriber and returns it
func (p *Publisher) Subscribe(id string, bufferSize int, filter EventFilter) *Subscriber {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if subscriber already exists
	if sub, exists := p.subscribers[id]; exists {
		p.logger.Warn("Subscriber already exists, returning existing", zap.String("id", id))
		return sub
	}

	sub := newSubscriber(id, bufferSize, filter, p.logger)
	p.subscribers[id] = sub

	p.logger.Info("Subscriber added",
		zap.String("subscriber_id", id),
		zap.Int("buffer_size", bufferSize),
	)

	return sub
}

// Unsubscribe removes a subscriber
func (p *Publisher) Unsubscribe(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if sub, exists := p.subscribers[id]; exists {
		sub.Close()
		delete(p.subscribers, id)
		p.logger.Info("Subscriber removed", zap.String("subscriber_id", id))
	}
}

// GetSubscriber returns a subscriber by ID
func (p *Publisher) GetSubscriber(id string) (*Subscriber, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	sub, exists := p.subscribers[id]
	return sub, exists
}

// SubscriberCount returns the number of active subscribers
func (p *Publisher) SubscriberCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.subscribers)
}

// Close closes the publisher and all subscribers
func (p *Publisher) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Close all subscribers
	for id, sub := range p.subscribers {
		sub.Close()
		delete(p.subscribers, id)
	}

	p.logger.Info("CDC publisher closed")
	return nil
}

// Stats returns publisher statistics
func (p *Publisher) Stats() PublisherStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := PublisherStats{
		SubscriberCount: len(p.subscribers),
		SequenceNum:     p.sequenceNum.Load(),
		DatacenterID:    p.dcID,
	}

	return stats
}

// PublisherStats contains publisher statistics
type PublisherStats struct {
	SubscriberCount int
	SequenceNum     uint64
	DatacenterID    string
}
