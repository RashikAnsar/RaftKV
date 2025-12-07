package sharding

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ShardStore is an interface for reading/writing data to a shard
// This will be implemented by the actual shard storage backend
type ShardStore interface {
	// Scan returns an iterator over all keys in the shard
	Scan(ctx context.Context) (Iterator, error)

	// Get retrieves a value for a key
	Get(ctx context.Context, key string) ([]byte, error)

	// Put writes a key-value pair
	Put(ctx context.Context, key string, value []byte) error

	// Delete removes a key
	Delete(ctx context.Context, key string) error

	// Count returns the number of keys in the shard
	Count(ctx context.Context) (int64, error)
}

// Iterator is an interface for iterating over key-value pairs
type Iterator interface {
	// Next advances to the next key-value pair
	Next() bool

	// Key returns the current key
	Key() string

	// Value returns the current value
	Value() []byte

	// Error returns any error that occurred during iteration
	Error() error

	// Close releases resources
	Close() error
}

// MigratorConfig contains configuration for the migrator
type MigratorConfig struct {
	// BatchSize is the number of keys to copy in each batch
	BatchSize int

	// ProgressUpdateInterval is how often to report progress
	ProgressUpdateInterval time.Duration

	// MaxRetries is the maximum number of retries for failed operations
	MaxRetries int

	// RetryDelay is the delay between retries
	RetryDelay time.Duration
}

// DefaultMigratorConfig returns a default configuration
func DefaultMigratorConfig() *MigratorConfig {
	return &MigratorConfig{
		BatchSize:              1000,
		ProgressUpdateInterval: 5 * time.Second,
		MaxRetries:             3,
		RetryDelay:             1 * time.Second,
	}
}

// Migrator handles data migration between shards
type Migrator struct {
	config  *MigratorConfig
	manager *Manager
	logger  *zap.Logger

	// Active migrations
	mu         sync.RWMutex
	migrations map[string]*migrationWorker
}

// NewMigrator creates a new migrator
func NewMigrator(config *MigratorConfig, manager *Manager, logger *zap.Logger) *Migrator {
	return &Migrator{
		config:     config,
		manager:    manager,
		logger:     logger,
		migrations: make(map[string]*migrationWorker),
	}
}

// StartMigration starts a migration process
func (m *Migrator) StartMigration(ctx context.Context, migrationID string, sourceStore, targetStore ShardStore) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already running
	if _, exists := m.migrations[migrationID]; exists {
		return fmt.Errorf("migration %s is already running", migrationID)
	}

	// Create worker
	worker := &migrationWorker{
		migrationID: migrationID,
		config:      m.config,
		manager:     m.manager,
		sourceStore: sourceStore,
		targetStore: targetStore,
		logger:      m.logger.With(zap.String("migration_id", migrationID)),
		ctx:         ctx,
		cancel:      func() {},
	}

	// Start worker
	m.migrations[migrationID] = worker
	go worker.run()

	m.logger.Info("Started migration worker",
		zap.String("migration_id", migrationID))

	return nil
}

// StopMigration stops a migration process
func (m *Migrator) StopMigration(migrationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	worker, exists := m.migrations[migrationID]
	if !exists {
		return fmt.Errorf("migration %s not found", migrationID)
	}

	worker.cancel()
	delete(m.migrations, migrationID)

	m.logger.Info("Stopped migration worker",
		zap.String("migration_id", migrationID))

	return nil
}

// GetMigrationStatus returns the status of a migration
func (m *Migrator) GetMigrationStatus(migrationID string) (*MigrationStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	worker, exists := m.migrations[migrationID]
	if !exists {
		return nil, fmt.Errorf("migration %s not found", migrationID)
	}

	return worker.getStatus(), nil
}

// MigrationStatus represents the current status of a migration
type MigrationStatus struct {
	MigrationID string
	State       MigrationState
	Progress    float64
	KeysCopied  int64
	TotalKeys   int64
	ErrorCount  int
	LastError   string
}

// migrationWorker handles the actual migration work
type migrationWorker struct {
	migrationID string
	config      *MigratorConfig
	manager     *Manager
	sourceStore ShardStore
	targetStore ShardStore
	logger      *zap.Logger

	ctx    context.Context
	cancel context.CancelFunc

	// Progress tracking
	mu          sync.RWMutex
	keysCopied  int64
	totalKeys   int64
	errorCount  int
	lastError   string
}

// run executes the migration process
func (w *migrationWorker) run() {
	w.logger.Info("Migration worker started")

	// Update to preparing state
	if err := w.manager.UpdateMigrationState(w.migrationID, MigrationStatePreparing, 0.0, 0); err != nil {
		w.logger.Error("Failed to update migration state", zap.Error(err))
		return
	}

	// Count total keys
	totalKeys, err := w.sourceStore.Count(w.ctx)
	if err != nil {
		w.setError(err)
		w.manager.UpdateMigrationState(w.migrationID, MigrationStateFailed, 0.0, 0)
		return
	}

	w.mu.Lock()
	w.totalKeys = totalKeys
	w.mu.Unlock()

	w.logger.Info("Starting migration", zap.Int64("total_keys", totalKeys))

	// Update to copying state
	if err := w.manager.UpdateMigrationState(w.migrationID, MigrationStateCopying, 0.0, 0); err != nil {
		w.logger.Error("Failed to update migration state", zap.Error(err))
		return
	}

	// Copy data
	if err := w.copyData(); err != nil {
		w.setError(err)
		w.manager.UpdateMigrationState(w.migrationID, MigrationStateFailed, w.getProgress(), w.keysCopied)
		return
	}

	// Update to syncing state
	if err := w.manager.UpdateMigrationState(w.migrationID, MigrationStateSyncing, 0.95, w.keysCopied); err != nil {
		w.logger.Error("Failed to update migration state", zap.Error(err))
		return
	}

	// Final sync (catch any updates during copy)
	if err := w.finalSync(); err != nil {
		w.setError(err)
		w.manager.UpdateMigrationState(w.migrationID, MigrationStateFailed, 0.95, w.keysCopied)
		return
	}

	// Complete migration
	if err := w.manager.CompleteMigration(w.migrationID); err != nil {
		w.logger.Error("Failed to complete migration", zap.Error(err))
		return
	}

	w.logger.Info("Migration completed successfully",
		zap.Int64("keys_copied", w.keysCopied))
}

// copyData copies all data from source to target
func (w *migrationWorker) copyData() error {
	iter, err := w.sourceStore.Scan(w.ctx)
	if err != nil {
		return fmt.Errorf("failed to scan source: %w", err)
	}
	defer iter.Close()

	batch := make(map[string][]byte)
	lastProgressUpdate := time.Now()

	for iter.Next() {
		select {
		case <-w.ctx.Done():
			return w.ctx.Err()
		default:
		}

		key := iter.Key()
		value := iter.Value()

		batch[key] = value

		// Copy batch when full
		if len(batch) >= w.config.BatchSize {
			if err := w.copyBatch(batch); err != nil {
				return err
			}
			batch = make(map[string][]byte)
		}

		// Update progress periodically
		if time.Since(lastProgressUpdate) >= w.config.ProgressUpdateInterval {
			progress := w.getProgress()
			w.manager.UpdateMigrationState(w.migrationID, MigrationStateCopying, progress, w.keysCopied)
			lastProgressUpdate = time.Now()
		}
	}

	// Copy remaining batch
	if len(batch) > 0 {
		if err := w.copyBatch(batch); err != nil {
			return err
		}
	}

	if err := iter.Error(); err != nil {
		return fmt.Errorf("iteration error: %w", err)
	}

	return nil
}

// copyBatch copies a batch of key-value pairs to the target
func (w *migrationWorker) copyBatch(batch map[string][]byte) error {
	for key, value := range batch {
		if err := w.copyKeyWithRetry(key, value); err != nil {
			return err
		}

		w.mu.Lock()
		w.keysCopied++
		w.mu.Unlock()
	}

	return nil
}

// copyKeyWithRetry copies a single key with retry logic
func (w *migrationWorker) copyKeyWithRetry(key string, value []byte) error {
	var lastErr error

	for attempt := 0; attempt < w.config.MaxRetries; attempt++ {
		if err := w.targetStore.Put(w.ctx, key, value); err != nil {
			lastErr = err
			w.logger.Warn("Failed to copy key, retrying",
				zap.String("key", key),
				zap.Int("attempt", attempt+1),
				zap.Error(err))

			time.Sleep(w.config.RetryDelay)
			continue
		}

		return nil
	}

	return fmt.Errorf("failed to copy key %s after %d attempts: %w", key, w.config.MaxRetries, lastErr)
}

// finalSync performs a final synchronization to catch any updates during the copy
func (w *migrationWorker) finalSync() error {
	// In a real implementation, this would use change data capture or
	// compare timestamps to identify keys that changed during the copy phase
	w.logger.Info("Performing final sync")

	// For now, this is a placeholder that would be implemented based on
	// the specific storage backend's capabilities

	return nil
}

// getStatus returns the current migration status
func (w *migrationWorker) getStatus() *MigrationStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return &MigrationStatus{
		MigrationID: w.migrationID,
		State:       MigrationStateCopying, // Would track actual state
		Progress:    w.getProgress(),
		KeysCopied:  w.keysCopied,
		TotalKeys:   w.totalKeys,
		ErrorCount:  w.errorCount,
		LastError:   w.lastError,
	}
}

// getProgress calculates the current progress percentage
func (w *migrationWorker) getProgress() float64 {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.totalKeys == 0 {
		return 0.0
	}

	return float64(w.keysCopied) / float64(w.totalKeys)
}

// setError records an error
func (w *migrationWorker) setError(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.errorCount++
	w.lastError = err.Error()

	w.logger.Error("Migration error",
		zap.Error(err),
		zap.Int("error_count", w.errorCount))
}
