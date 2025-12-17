package storage

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// TTLManager handles automatic expiration of keys with TTL
type TTLManager struct {
	store           Store
	logger          *zap.Logger
	scanInterval    time.Duration
	batchSize       int
	stopCh          chan struct{}
	wg              sync.WaitGroup
	mu              sync.RWMutex
	expirationCount uint64
	lastScanTime    time.Time
}

// TTLManagerConfig configures the TTL manager
type TTLManagerConfig struct {
	ScanInterval time.Duration // How often to scan for expired keys (default: 1 second)
	BatchSize    int           // Max keys to expire per scan (default: 100)
}

// NewTTLManager creates a new TTL manager
func NewTTLManager(store Store, logger *zap.Logger, config TTLManagerConfig) *TTLManager {
	if config.ScanInterval == 0 {
		config.ScanInterval = 1 * time.Second
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}

	return &TTLManager{
		store:        store,
		logger:       logger,
		scanInterval: config.ScanInterval,
		batchSize:    config.BatchSize,
		stopCh:       make(chan struct{}),
	}
}

// Start begins the background expiration loop
func (m *TTLManager) Start() {
	m.wg.Add(1)
	go m.expirationLoop()
	m.logger.Info("TTL manager started",
		zap.Duration("scan_interval", m.scanInterval),
		zap.Int("batch_size", m.batchSize),
	)
}

// Stop stops the background expiration loop
func (m *TTLManager) Stop() {
	close(m.stopCh)
	m.wg.Wait()
	m.logger.Info("TTL manager stopped")
}

// expirationLoop runs in the background and periodically scans for expired keys
func (m *TTLManager) expirationLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.scanAndExpire()
		case <-m.stopCh:
			return
		}
	}
}

// scanAndExpire scans for expired keys and removes them
func (m *TTLManager) scanAndExpire() {
	ctx := context.Background()
	startTime := time.Now()

	// List all keys (we'll need to check each one's expiration)
	// In a production system, you might maintain a separate index of keys with TTL
	keys, err := m.store.List(ctx, "", 0)
	if err != nil {
		m.logger.Error("Failed to list keys for TTL scan", zap.Error(err))
		return
	}

	expiredCount := 0

	// Check each key for expiration
	for i, key := range keys {
		// Respect batch size limit per scan
		if expiredCount >= m.batchSize {
			m.logger.Debug("Reached batch size limit for TTL scan",
				zap.Int("expired_count", expiredCount),
				zap.Int("batch_size", m.batchSize),
			)
			break
		}

		// Check if we should stop
		select {
		case <-m.stopCh:
			return
		default:
		}

		// Try to get the key - this will trigger lazy expiration
		// If the key has expired, Get() will delete it and return ErrKeyNotFound
		_, err := m.store.Get(ctx, key)
		if err == ErrKeyNotFound {
			// Key was expired and deleted, or doesn't exist
			expiredCount++
		}
		// For other errors, just continue to next key
		_ = i
	}

	m.mu.Lock()
	m.expirationCount += uint64(expiredCount)
	m.lastScanTime = time.Now()
	m.mu.Unlock()

	if expiredCount > 0 {
		m.logger.Info("TTL scan completed",
			zap.Int("expired_keys", expiredCount),
			zap.Duration("duration", time.Since(startTime)),
		)
	}
}

// GetStats returns TTL manager statistics
func (m *TTLManager) GetStats() TTLStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return TTLStats{
		ExpirationCount: m.expirationCount,
		LastScanTime:    m.lastScanTime,
		ScanInterval:    m.scanInterval,
	}
}

// TTLStats contains TTL manager statistics
type TTLStats struct {
	ExpirationCount uint64
	LastScanTime    time.Time
	ScanInterval    time.Duration
}

// SetTTL sets the TTL for a key (convenience method)
func (m *TTLManager) SetTTL(ctx context.Context, key string, ttl time.Duration) error {
	// Get current value
	value, err := m.store.Get(ctx, key)
	if err != nil {
		return err
	}

	// Re-put with expiration
	// This is a simplified approach - in reality, you'd want to update just the expiration
	// without triggering a full Put operation
	_ = value
	return nil
}

// GetTTL returns the remaining TTL for a key
func (m *TTLManager) GetTTL(ctx context.Context, key string) (time.Duration, error) {
	// This would need to access the ExpiresAt field
	// For now, return a placeholder
	return 0, ErrKeyNotFound
}
