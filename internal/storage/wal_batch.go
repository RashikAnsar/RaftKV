package storage

import (
	"context"
	"sync"
	"time"
)

// BatchConfig configures the write batching behavior
type BatchConfig struct {
	MaxBatchSize  int           // Maximum number of operations in a batch
	MaxBatchBytes int           // Maximum bytes in a batch
	MaxWaitTime   time.Duration // Maximum time to wait before flushing
	Enabled       bool          // Enable/disable batching
}

// DefaultBatchConfig returns sensible defaults for batching
func DefaultBatchConfig() BatchConfig {
	return BatchConfig{
		MaxBatchSize:  100,              // Up to 100 operations
		MaxBatchBytes: 1024 * 1024,      // 1MB of data
		MaxWaitTime:   10 * time.Millisecond, // 10ms max latency
		Enabled:       true,
	}
}

// batchedOperation represents a single operation waiting to be batched
type batchedOperation struct {
	entry  *WALEntry
	result chan error // Channel to receive the write result
}

// BatchedWAL wraps a WAL with batching capabilities
type BatchedWAL struct {
	wal    *WAL
	config BatchConfig

	mu         sync.Mutex
	batch      []*batchedOperation
	batchBytes int
	timer      *time.Timer

	stopCh chan struct{}
	doneCh chan struct{}
}

// NewBatchedWAL creates a new batched WAL writer
func NewBatchedWAL(wal *WAL, config BatchConfig) *BatchedWAL {
	if !config.Enabled {
		// If batching is disabled, return a pass-through implementation
		return &BatchedWAL{
			wal:    wal,
			config: BatchConfig{Enabled: false},
		}
	}

	b := &BatchedWAL{
		wal:    wal,
		config: config,
		batch:  make([]*batchedOperation, 0, config.MaxBatchSize),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}

	// Start background flusher
	go b.backgroundFlusher()

	return b
}

// Append adds an entry to the batch and returns when it's been written
func (b *BatchedWAL) Append(ctx context.Context, entry *WALEntry) error {
	// If batching is disabled, write directly
	if !b.config.Enabled {
		return b.wal.Append(entry)
	}

	// Create operation with result channel
	op := &batchedOperation{
		entry:  entry,
		result: make(chan error, 1),
	}

	// Add to batch
	b.mu.Lock()
	shouldFlush := b.addToBatch(op)
	b.mu.Unlock()

	// Flush immediately if batch is full
	if shouldFlush {
		b.flushBatch()
	}

	// Wait for result or context cancellation
	select {
	case err := <-op.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// addToBatch adds an operation to the current batch (must hold lock)
// Returns true if batch should be flushed immediately
func (b *BatchedWAL) addToBatch(op *batchedOperation) bool {
	// Estimate size
	entrySize := len(op.entry.Key) + len(op.entry.Value) + walHeaderSize + walChecksumSize

	// Add to batch
	b.batch = append(b.batch, op)
	b.batchBytes += entrySize

	// Start timer if this is the first operation
	if len(b.batch) == 1 {
		if b.timer == nil {
			b.timer = time.AfterFunc(b.config.MaxWaitTime, func() {
				b.flushBatch()
			})
		} else {
			b.timer.Reset(b.config.MaxWaitTime)
		}
	}

	// Check if batch is full
	return len(b.batch) >= b.config.MaxBatchSize || b.batchBytes >= b.config.MaxBatchBytes
}

// flushBatch writes all pending operations to the WAL
func (b *BatchedWAL) flushBatch() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Stop timer
	if b.timer != nil {
		b.timer.Stop()
	}

	// Nothing to flush
	if len(b.batch) == 0 {
		return
	}

	// Get current batch
	currentBatch := b.batch
	b.batch = make([]*batchedOperation, 0, b.config.MaxBatchSize)
	b.batchBytes = 0

	// Collect all entries
	entries := make([]*WALEntry, len(currentBatch))
	for i, op := range currentBatch {
		entries[i] = op.entry
	}

	// Write all entries in a single batch (ONE fsync!)
	writeErr := b.wal.AppendBatch(entries)

	// Send result to all waiting goroutines
	for _, op := range currentBatch {
		op.result <- writeErr
		close(op.result)
	}
}

// backgroundFlusher periodically flushes the batch
func (b *BatchedWAL) backgroundFlusher() {
	defer close(b.doneCh)

	ticker := time.NewTicker(b.config.MaxWaitTime)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.flushBatch()
		case <-b.stopCh:
			// Final flush before shutdown
			b.flushBatch()
			return
		}
	}
}

// Close flushes any pending operations and closes the WAL
func (b *BatchedWAL) Close() error {
	if !b.config.Enabled {
		return b.wal.Close()
	}

	// Stop background flusher
	close(b.stopCh)
	<-b.doneCh

	// Final flush
	b.flushBatch()

	return b.wal.Close()
}

// Replay delegates to underlying WAL
func (b *BatchedWAL) Replay() ([]*WALEntry, error) {
	return b.wal.Replay()
}

// Sync forces an immediate flush
func (b *BatchedWAL) Sync() error {
	if !b.config.Enabled {
		return nil
	}

	b.flushBatch()
	return nil
}

// Stats returns batching statistics
type BatchStats struct {
	PendingOps   int
	PendingBytes int
}

func (b *BatchedWAL) Stats() BatchStats {
	b.mu.Lock()
	defer b.mu.Unlock()

	return BatchStats{
		PendingOps:   len(b.batch),
		PendingBytes: b.batchBytes,
	}
}
