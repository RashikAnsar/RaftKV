package watch

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/RashikAnsar/raftkv/internal/cdc"
	"github.com/RashikAnsar/raftkv/internal/storage"
	"go.uber.org/zap"
)

// WatchRequest represents a watch subscription request
type WatchRequest struct {
	KeyPrefix        string
	Operations       []string
	StartRevision    int64
	SendInitialValue bool
}

// WatchEvent represents a key-value change event for watchers
type WatchEvent struct {
	Key       string
	Value     []byte
	Operation string
	RaftIndex uint64
	RaftTerm  uint64
	Timestamp int64
	Version   uint64
}

// Watcher represents a single watch subscription
type Watcher struct {
	id         string
	subscriber *cdc.Subscriber
	store      storage.Store
	logger     *zap.Logger
	config     WatchConfig
	keyPrefix  string
	operations []string
	createdAt  time.Time

	// Initial value handling
	initialValue     bool
	initialValueSent bool

	// Metrics
	mu              sync.RWMutex
	eventsDelivered uint64
	closed          bool
}

// GetID returns the watcher ID
func (w *Watcher) GetID() string {
	return w.id
}

// GetEventsDelivered returns the number of events delivered
func (w *Watcher) GetEventsDelivered() uint64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.eventsDelivered
}

// FetchInitialValues fetches and returns initial values for keys matching the prefix
func (w *Watcher) FetchInitialValues(ctx context.Context) ([]*WatchEvent, error) {
	if !w.initialValue || w.initialValueSent {
		return nil, nil
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, w.config.InitialValueTimeout)
	defer cancel()

	// List all keys with the prefix
	keys, err := w.store.List(ctx, w.keyPrefix, 0)
	if err != nil {
		w.logger.Error("Failed to list keys for initial values",
			zap.String("watch_id", w.id),
			zap.String("key_prefix", w.keyPrefix),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}

	events := make([]*WatchEvent, 0, len(keys))

	// Fetch value for each key
	for _, key := range keys {
		value, err := w.store.Get(ctx, key)
		if err == storage.ErrKeyNotFound {
			// Key was deleted between list and get, skip it
			continue
		}
		if err != nil {
			w.logger.Warn("Failed to get key for initial value",
				zap.String("watch_id", w.id),
				zap.String("key", key),
				zap.Error(err),
			)
			continue
		}

		// Create watch event for this key
		events = append(events, &WatchEvent{
			Key:       key,
			Value:     value,
			Operation: "initial",
			Version:   0, // Version not available without GetWithVersion
			Timestamp: time.Now().UnixNano(),
		})
	}

	w.mu.Lock()
	w.initialValueSent = true
	w.eventsDelivered += uint64(len(events))
	w.mu.Unlock()

	w.logger.Debug("Sent initial values",
		zap.String("watch_id", w.id),
		zap.Int("count", len(events)),
	)

	return events, nil
}

// Recv receives the next watch event, blocking until one is available
func (w *Watcher) Recv(ctx context.Context) (*WatchEvent, error) {
	w.mu.RLock()
	if w.closed {
		w.mu.RUnlock()
		return nil, fmt.Errorf("watcher closed")
	}
	w.mu.RUnlock()

	// Receive CDC event
	cdcEvent, err := w.subscriber.Recv(ctx)
	if err != nil {
		return nil, err
	}

	// Transform CDC event to Watch event
	watchEvent := w.transformEvent(cdcEvent)

	// Increment delivered count
	w.mu.Lock()
	w.eventsDelivered++
	w.mu.Unlock()

	return watchEvent, nil
}

// RecvWithTimeout receives the next watch event with a timeout
func (w *Watcher) RecvWithTimeout(timeout time.Duration) (*WatchEvent, error) {
	w.mu.RLock()
	if w.closed {
		w.mu.RUnlock()
		return nil, fmt.Errorf("watcher closed")
	}
	w.mu.RUnlock()

	// Receive CDC event with timeout
	cdcEvent, err := w.subscriber.RecvWithTimeout(timeout)
	if err != nil {
		return nil, err
	}

	// Transform CDC event to Watch event
	watchEvent := w.transformEvent(cdcEvent)

	// Increment delivered count
	w.mu.Lock()
	w.eventsDelivered++
	w.mu.Unlock()

	return watchEvent, nil
}

// TryRecv attempts to receive a watch event without blocking
func (w *Watcher) TryRecv() (*WatchEvent, bool) {
	w.mu.RLock()
	if w.closed {
		w.mu.RUnlock()
		return nil, false
	}
	w.mu.RUnlock()

	// Try to receive CDC event
	cdcEvent := w.subscriber.TryRecv()
	if cdcEvent == nil {
		return nil, false
	}

	// Transform CDC event to Watch event
	watchEvent := w.transformEvent(cdcEvent)

	// Increment delivered count
	w.mu.Lock()
	w.eventsDelivered++
	w.mu.Unlock()

	return watchEvent, true
}

// transformEvent transforms a CDC event to a Watch event
func (w *Watcher) transformEvent(cdcEvent *cdc.ChangeEvent) *WatchEvent {
	return &WatchEvent{
		Key:       cdcEvent.Key,
		Value:     cdcEvent.Value,
		Operation: cdcEvent.Operation,
		RaftIndex: cdcEvent.RaftIndex,
		RaftTerm:  cdcEvent.RaftTerm,
		Timestamp: cdcEvent.Timestamp.UnixNano(),
		Version:   0, // Version not available in CDC event, would need to be added
	}
}

// Close closes the watcher
func (w *Watcher) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return
	}

	w.closed = true

	w.logger.Debug("Watcher closed",
		zap.String("watch_id", w.id),
		zap.Duration("duration", time.Since(w.createdAt)),
		zap.Uint64("events_delivered", w.eventsDelivered),
	)
}

// IsClosed returns whether the watcher is closed
func (w *Watcher) IsClosed() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.closed
}
