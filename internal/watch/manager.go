package watch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/RashikAnsar/raftkv/internal/cdc"
	"github.com/RashikAnsar/raftkv/internal/storage"
	"go.uber.org/zap"
)

var (
	// ErrWatcherNotFound is returned when trying to cancel a non-existent watcher
	ErrWatcherNotFound = errors.New("watcher not found")

	// ErrTooManyWatchers is returned when the maximum number of watchers is reached
	ErrTooManyWatchers = errors.New("too many active watchers")

	// ErrManagerClosed is returned when operating on a closed manager
	ErrManagerClosed = errors.New("watch manager is closed")
)

// WatchConfig holds configuration for the watch manager
type WatchConfig struct {
	MaxWatchers         int           // Maximum number of concurrent watchers
	BufferSize          int           // Event buffer size per watcher
	InitialValueTimeout time.Duration // Timeout for fetching initial values
}

// DefaultWatchConfig returns the default watch configuration
func DefaultWatchConfig() WatchConfig {
	return WatchConfig{
		MaxWatchers:         1000,
		BufferSize:          1000,
		InitialValueTimeout: 5 * time.Second,
	}
}

// WatchManager manages all active watch subscriptions
type WatchManager struct {
	mu           sync.RWMutex
	watchers     map[string]*Watcher // watchID -> Watcher
	cdcPublisher *cdc.Publisher
	store        storage.Store
	logger       *zap.Logger
	config       WatchConfig
	closed       bool

	// Metrics
	totalWatchers   uint64 // Total watchers created
	eventsDelivered uint64 // Total events delivered
}

// NewWatchManager creates a new watch manager
func NewWatchManager(cdcPublisher *cdc.Publisher, store storage.Store, logger *zap.Logger, config WatchConfig) *WatchManager {
	return &WatchManager{
		watchers:     make(map[string]*Watcher),
		cdcPublisher: cdcPublisher,
		store:        store,
		logger:       logger,
		config:       config,
	}
}

// CreateWatch creates a new watch subscription
func (wm *WatchManager) CreateWatch(ctx context.Context, req *WatchRequest) (*Watcher, error) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	// Check if manager is closed
	if wm.closed {
		return nil, ErrManagerClosed
	}

	// Check if we've reached the maximum number of watchers
	if len(wm.watchers) >= wm.config.MaxWatchers {
		wm.logger.Warn("Maximum number of watchers reached",
			zap.Int("max_watchers", wm.config.MaxWatchers),
			zap.Int("active_watchers", len(wm.watchers)),
		)
		return nil, ErrTooManyWatchers
	}

	// Generate unique watch ID
	watchID := fmt.Sprintf("watch-%d-%d", time.Now().UnixNano(), wm.totalWatchers)
	wm.totalWatchers++

	// Create CDC event filter based on watch request
	var filter cdc.EventFilter
	if req.KeyPrefix != "" || len(req.Operations) > 0 {
		filters := []cdc.EventFilter{}

		// Add key prefix filter if specified
		if req.KeyPrefix != "" {
			filters = append(filters, cdc.KeyPrefixFilter(req.KeyPrefix))
		}

		// Add operation filters if specified
		if len(req.Operations) > 0 {
			// Create a composite filter for all operations (OR logic)
			opFilters := make([]cdc.EventFilter, 0, len(req.Operations))
			for _, op := range req.Operations {
				opFilters = append(opFilters, cdc.OperationFilter(op))
			}
			// For multiple operations, we need OR logic, not AND
			// So we create a custom filter
			filters = append(filters, func(e *cdc.ChangeEvent) bool {
				for _, op := range req.Operations {
					if e.Operation == op {
						return true
					}
				}
				return false
			})
		}

		if len(filters) > 0 {
			filter = cdc.CompositeFilter(filters...)
		}
	}

	// Subscribe to CDC publisher
	subscriber := wm.cdcPublisher.Subscribe(watchID, wm.config.BufferSize, filter)

	// Create watcher
	watcher := &Watcher{
		id:         watchID,
		subscriber: subscriber,
		store:      wm.store,
		logger:     wm.logger,
		config:     wm.config,
		keyPrefix:  req.KeyPrefix,
		operations: req.Operations,
		createdAt:  time.Now(),
	}

	// Fetch and send initial value if requested
	if req.SendInitialValue && req.KeyPrefix != "" {
		watcher.initialValue = true
		watcher.initialValueSent = false
	}

	// Register watcher
	wm.watchers[watchID] = watcher

	wm.logger.Info("Watch created",
		zap.String("watch_id", watchID),
		zap.String("key_prefix", req.KeyPrefix),
		zap.Strings("operations", req.Operations),
		zap.Bool("send_initial_value", req.SendInitialValue),
	)

	return watcher, nil
}

// CancelWatch cancels and removes a watch subscription
func (wm *WatchManager) CancelWatch(watchID string) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	watcher, exists := wm.watchers[watchID]
	if !exists {
		return ErrWatcherNotFound
	}

	// Unsubscribe from CDC
	wm.cdcPublisher.Unsubscribe(watchID)

	// Close watcher
	watcher.Close()

	// Remove from active watchers
	delete(wm.watchers, watchID)

	wm.logger.Info("Watch canceled",
		zap.String("watch_id", watchID),
		zap.Duration("duration", time.Since(watcher.createdAt)),
	)

	return nil
}

// GetWatcher retrieves a watcher by ID
func (wm *WatchManager) GetWatcher(watchID string) (*Watcher, bool) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	watcher, exists := wm.watchers[watchID]
	return watcher, exists
}

// ListWatchers returns all active watcher IDs
func (wm *WatchManager) ListWatchers() []string {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	watcherIDs := make([]string, 0, len(wm.watchers))
	for id := range wm.watchers {
		watcherIDs = append(watcherIDs, id)
	}

	return watcherIDs
}

// Stats returns watch manager statistics
type WatchStats struct {
	ActiveWatchers  int
	TotalWatchers   uint64
	EventsDelivered uint64
}

// GetStats returns current watch manager statistics
func (wm *WatchManager) GetStats() WatchStats {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	stats := WatchStats{
		ActiveWatchers:  len(wm.watchers),
		TotalWatchers:   wm.totalWatchers,
		EventsDelivered: wm.eventsDelivered,
	}

	// Aggregate events delivered from all active watchers
	for _, watcher := range wm.watchers {
		stats.EventsDelivered += watcher.GetEventsDelivered()
	}

	return stats
}

// Close closes the watch manager and all active watchers
func (wm *WatchManager) Close() error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	if wm.closed {
		return nil
	}

	wm.closed = true

	// Close all active watchers
	for id, watcher := range wm.watchers {
		wm.cdcPublisher.Unsubscribe(id)
		watcher.Close()
	}

	// Clear watchers map
	wm.watchers = make(map[string]*Watcher)

	wm.logger.Info("Watch manager closed")

	return nil
}
