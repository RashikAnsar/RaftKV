package cdc

import (
	"context"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Subscriber receives change events from a publisher
type Subscriber struct {
	id             string
	events         chan *ChangeEvent
	filter         EventFilter
	logger         *zap.Logger
	closed         atomic.Bool
	metricsDropped atomic.Uint64 // Count of dropped events
}

// newSubscriber creates a new subscriber (internal use)
func newSubscriber(id string, bufferSize int, filter EventFilter, logger *zap.Logger) *Subscriber {
	return &Subscriber{
		id:     id,
		events: make(chan *ChangeEvent, bufferSize),
		filter: filter,
		logger: logger,
	}
}

// ID returns the subscriber ID
func (s *Subscriber) ID() string {
	return s.id
}

// Events returns the event channel for consumption
// Consumer should read from this channel in a loop
func (s *Subscriber) Events() <-chan *ChangeEvent {
	return s.events
}

// Recv receives the next event with context support
// Blocks until an event is available or context is cancelled
func (s *Subscriber) Recv(ctx context.Context) (*ChangeEvent, error) {
	if s.closed.Load() {
		return nil, ErrSubscriberClosed
	}

	select {
	case event, ok := <-s.events:
		if !ok {
			return nil, ErrSubscriberClosed
		}
		return event, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// RecvWithTimeout receives the next event with a timeout
func (s *Subscriber) RecvWithTimeout(timeout time.Duration) (*ChangeEvent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.Recv(ctx)
}

// TryRecv attempts to receive an event without blocking
// Returns nil if no event is available
func (s *Subscriber) TryRecv() *ChangeEvent {
	select {
	case event := <-s.events:
		return event
	default:
		return nil
	}
}

// Close closes the subscriber
func (s *Subscriber) Close() {
	if !s.closed.CompareAndSwap(false, true) {
		return // Already closed
	}

	close(s.events)
	s.logger.Debug("Subscriber closed", zap.String("id", s.id))
}

// IsClosed returns true if the subscriber is closed
func (s *Subscriber) IsClosed() bool {
	return s.closed.Load()
}

// Stats returns subscriber statistics
func (s *Subscriber) Stats() SubscriberStats {
	return SubscriberStats{
		ID:           s.id,
		QueueLength:  len(s.events),
		QueueCap:     cap(s.events),
		DroppedCount: s.metricsDropped.Load(),
		Closed:       s.closed.Load(),
	}
}

// SubscriberStats contains subscriber statistics
type SubscriberStats struct {
	ID           string
	QueueLength  int
	QueueCap     int
	DroppedCount uint64
	Closed       bool
}
