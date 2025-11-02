package storage

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type DurableStore struct {
	mu              sync.Mutex // Protects walIndex and opsSinceSnapshot
	memory          *MemoryStore
	wal             *WAL
	snapshotManager *SnapshotManager

	snapshotEvery    int         // Take snapshot every N operations (0 = disabled)
	opsSinceSnapshot int         // Operations since last snapshot
	walIndex         uint64      // Current WAL index
	snapshotting     atomic.Bool // True if snapshot in progress
}

type DurableStoreConfig struct {
	DataDir       string
	SyncOnWrite   bool // true = durable but slower, false = faster but risk data loss
	SnapshotEvery int  // Take snapshot every N operations (0 = disabled)
}

func NewDurableStore(config DurableStoreConfig) (*DurableStore, error) {
	// Default: snapshot every 10,000 operations
	if config.SnapshotEvery == 0 {
		config.SnapshotEvery = 10000
	}

	// Create WAL
	wal, err := NewWAL(WALConfig{
		Dir:         config.DataDir,
		SyncOnWrite: config.SyncOnWrite,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create WAL: %w", err)
	}

	// Create snapshot manager
	snapshotManager, err := NewSnapshotManager(config.DataDir)
	if err != nil {
		wal.Close()
		return nil, fmt.Errorf("failed to create snapshot manager: %w", err)
	}

	// Create memory store
	memory := NewMemoryStore()

	store := &DurableStore{
		memory:          memory,
		wal:             wal,
		snapshotManager: snapshotManager,
		snapshotEvery:   config.SnapshotEvery,
	}

	// Recover from snapshot + WAL
	if err := store.recover(); err != nil {
		wal.Close()
		return nil, fmt.Errorf("failed to recover: %w", err)
	}

	return store, nil
}

// recover replays snapshot + WAL to restore in-memory state
func (s *DurableStore) recover() error {
	ctx := context.Background()

	// Step 1: Try to load latest snapshot
	snapshot, err := s.snapshotManager.Restore()
	if err != nil {
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}

	if snapshot != nil {
		// Load snapshot data into memory
		for key, value := range snapshot.Data {
			if err := s.memory.Put(ctx, key, value); err != nil {
				return fmt.Errorf("failed to apply snapshot data: %w", err)
			}
		}

		s.walIndex = snapshot.Index

		fmt.Printf("Recovered from snapshot at index %d (%d keys)\n",
			snapshot.Index, snapshot.KeyCount)
	}

	// Step 2: Replay WAL entries after snapshot
	entries, err := s.wal.Replay()
	if err != nil {
		return fmt.Errorf("failed to replay WAL: %w", err)
	}

	// Apply entries after snapshot index
	appliedCount := 0
	for _, entry := range entries {
		// Skip entries already in snapshot
		// (We don't have entry indices in WAL, so we apply all)

		switch entry.Operation {
		case OpPut:
			if err := s.memory.Put(ctx, entry.Key, entry.Value); err != nil {
				return fmt.Errorf("failed to apply PUT during recovery: %w", err)
			}
			s.walIndex++
			appliedCount++

		case OpDelete:
			if err := s.memory.Delete(ctx, entry.Key); err != nil {
				return fmt.Errorf("failed to apply DELETE during recovery: %w", err)
			}
			s.walIndex++
			appliedCount++

		default:
			return fmt.Errorf("unknown operation type: %d", entry.Operation)
		}
	}

	if appliedCount > 0 {
		fmt.Printf("Replayed %d WAL entries after snapshot\n", appliedCount)
	}

	s.opsSinceSnapshot = 0

	return nil
}

func (s *DurableStore) Get(ctx context.Context, key string) ([]byte, error) {
	return s.memory.Get(ctx, key)
}

func (s *DurableStore) Put(ctx context.Context, key string, value []byte) error {
	entry := &WALEntry{
		Operation: OpPut,
		Timestamp: time.Now(),
		Key:       key,
		Value:     value,
	}

	if err := s.wal.Append(entry); err != nil {
		return fmt.Errorf("failed to write to WAL: %w", err)
	}

	if err := s.memory.Put(ctx, key, value); err != nil {
		// This is a critical error - WAL and memory are out of sync
		// TODO: in production handle it as per requirements
		return fmt.Errorf("failed to apply to memory after WAL write: %w", err)
	}

	s.mu.Lock()
	s.walIndex++
	s.opsSinceSnapshot++
	shouldSnapshot := s.snapshotEvery > 0 &&
		s.opsSinceSnapshot >= s.snapshotEvery &&
		!s.snapshotting.Load()
	s.mu.Unlock()

	if shouldSnapshot {
		if s.snapshotting.CompareAndSwap(false, true) {
			// Take snapshot in background (non-blocking)
			go func() {
				s.takeSnapshot()
				s.snapshotting.Store(false)
			}()
		}
	}

	return nil
}

func (s *DurableStore) Delete(ctx context.Context, key string) error {
	// 1. Write to WAL first
	entry := &WALEntry{
		Operation: OpDelete,
		Timestamp: time.Now(),
		Key:       key,
	}

	if err := s.wal.Append(entry); err != nil {
		return fmt.Errorf("failed to write to WAL: %w", err)
	}

	// 2. Then apply to memory
	if err := s.memory.Delete(ctx, key); err != nil {
		return fmt.Errorf("failed to apply to memory after WAL write: %w", err)
	}

	// 3. Update counters
	s.mu.Lock()
	s.walIndex++
	s.opsSinceSnapshot++
	shouldSnapshot := s.snapshotEvery > 0 &&
		s.opsSinceSnapshot >= s.snapshotEvery &&
		!s.snapshotting.Load()
	s.mu.Unlock()

	if shouldSnapshot {
		if s.snapshotting.CompareAndSwap(false, true) {
			go func() {
				s.takeSnapshot()
				s.snapshotting.Store(false)
			}()
		}
	}

	return nil
}

func (s *DurableStore) List(ctx context.Context, prefix string, limit int) ([]string, error) {
	return s.memory.List(ctx, prefix, limit)
}

func (s *DurableStore) Snapshot(ctx context.Context) (string, error) {
	return s.takeSnapshotSync()
}

func (s *DurableStore) takeSnapshot() {
	path, err := s.takeSnapshotSync()
	if err != nil {
		fmt.Printf("ERROR: Failed to create snapshot: %v\n", err)
		return
	}
	fmt.Printf("Snapshot created: %s\n", path)
}

func (s *DurableStore) takeSnapshotSync() (string, error) {
	ctx := context.Background()

	keys, err := s.memory.List(ctx, "", 0)
	if err != nil {
		return "", fmt.Errorf("failed to list keys: %w", err)
	}

	data := make(map[string][]byte, len(keys))
	for _, key := range keys {
		value, err := s.memory.Get(ctx, key)
		if err != nil {
			// Key might have been deleted, skip it
			continue
		}
		data[key] = value
	}

	s.mu.Lock()
	currentIndex := s.walIndex
	s.mu.Unlock()

	if err := s.snapshotManager.Create(currentIndex, data); err != nil {
		return "", fmt.Errorf("failed to create snapshot: %w", err)
	}

	s.mu.Lock()
	s.opsSinceSnapshot = 0
	s.mu.Unlock()

	// Clean up old snapshots (keep last 3)
	if err := s.snapshotManager.DeleteOldSnapshots(3); err != nil {
		fmt.Printf("WARNING: Failed to delete old snapshots: %v\n", err)
	}

	// Clean up old WAL segments (keep segments after latest snapshot)
	// Note: In production, we might need to handle as per requirements
	// For now, we don't delete WAL segments to be safe

	path := fmt.Sprintf("snapshot-%09d.gob", currentIndex)
	return path, nil
}

func (s *DurableStore) Restore(ctx context.Context, snapshotPath string) error {
	var index uint64
	_, err := fmt.Sscanf(snapshotPath, "snapshot-%d.gob", &index)
	if err != nil {
		return fmt.Errorf("invalid snapshot path: %w", err)
	}

	snapshot, err := s.snapshotManager.RestoreFromIndex(index)
	if err != nil {
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}

	s.memory.Reset()

	for key, value := range snapshot.Data {
		if err := s.memory.Put(ctx, key, value); err != nil {
			return fmt.Errorf("failed to restore key %s: %w", key, err)
		}
	}

	s.walIndex = snapshot.Index
	s.opsSinceSnapshot = 0

	return nil
}

func (s *DurableStore) Stats() Stats {
	return s.memory.Stats()
}

func (s *DurableStore) Close() error {
	if err := s.wal.Close(); err != nil {
		return fmt.Errorf("failed to close WAL: %w", err)
	}

	if err := s.memory.Close(); err != nil {
		return fmt.Errorf("failed to close memory store: %w", err)
	}

	return nil
}

func (s *DurableStore) Sync() error {
	return s.wal.Sync()
}

// Reset clears all data from the store (used during FSM restore)
func (s *DurableStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.memory.Reset()
	s.opsSinceSnapshot = 0
	// Note: We don't reset walIndex as WAL is append-only
}
