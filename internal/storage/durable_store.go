package storage

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type DurableStore struct {
	mu              sync.Mutex // Protects walIndex, opsSinceSnapshot, and Raft tracking
	memory          *MemoryStore
	wal             *WAL
	snapshotManager *SnapshotManager
	config          DurableStoreConfig // Store config for compaction settings

	snapshotEvery    int         // Take snapshot every N operations (0 = disabled)
	opsSinceSnapshot int         // Operations since last snapshot
	walIndex         uint64      // Current WAL index
	snapshotting     atomic.Bool // True if snapshot in progress

	// Raft tracking
	lastRaftIndex uint64 // Last applied Raft log index
	lastRaftTerm  uint64 // Last applied Raft term
}

type DurableStoreConfig struct {
	DataDir       string
	SyncOnWrite   bool // true = durable but slower, false = faster but risk data loss
	SnapshotEvery int  // Take snapshot every N operations (0 = disabled)

	// Compaction settings
	CompactionEnabled bool   // Enable WAL compaction after snapshots
	CompactionMargin  uint64 // Safety margin: keep entries before (snapshot index - margin)
	MaxWALSegments    int    // Force compaction if WAL segments exceed this (0 = disabled)
}

func NewDurableStore(config DurableStoreConfig) (*DurableStore, error) {
	// Default: snapshot every 10,000 operations
	if config.SnapshotEvery == 0 {
		config.SnapshotEvery = 10000
	}

	// Default compaction settings
	if config.CompactionMargin == 0 {
		config.CompactionMargin = 100 // Keep 100 entries before snapshot
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
		config:          config, // Store config for compaction
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
		s.lastRaftIndex = snapshot.RaftIndex
		s.lastRaftTerm = snapshot.RaftTerm

		fmt.Printf("Recovered from snapshot at index %d (Raft: %d/%d, %d keys)\n",
			snapshot.Index, snapshot.RaftIndex, snapshot.RaftTerm, snapshot.KeyCount)
	}

	// Step 2: Replay WAL entries after snapshot
	entries, err := s.wal.Replay()
	if err != nil {
		return fmt.Errorf("failed to replay WAL: %w", err)
	}

	// Apply entries after snapshot index
	appliedCount := 0
	for _, entry := range entries {
		// Skip entries already in snapshot (if we have snapshot metadata)
		if snapshot != nil && entry.RaftIndex > 0 && entry.RaftIndex <= snapshot.RaftIndex {
			continue
		}

		switch entry.Operation {
		case OpPut:
			if err := s.memory.Put(ctx, entry.Key, entry.Value); err != nil {
				return fmt.Errorf("failed to apply PUT during recovery: %w", err)
			}
			s.walIndex++
			appliedCount++

			// Update Raft tracking from WAL entries
			if entry.RaftIndex > s.lastRaftIndex {
				s.lastRaftIndex = entry.RaftIndex
				s.lastRaftTerm = entry.RaftTerm
			}

		case OpDelete:
			if err := s.memory.Delete(ctx, entry.Key); err != nil {
				return fmt.Errorf("failed to apply DELETE during recovery: %w", err)
			}
			s.walIndex++
			appliedCount++

			// Update Raft tracking from WAL entries
			if entry.RaftIndex > s.lastRaftIndex {
				s.lastRaftIndex = entry.RaftIndex
				s.lastRaftTerm = entry.RaftTerm
			}

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
	raftIndex := s.lastRaftIndex
	raftTerm := s.lastRaftTerm
	s.mu.Unlock()

	if err := s.snapshotManager.CreateWithRaftMetadata(currentIndex, data, raftIndex, raftTerm); err != nil {
		return "", fmt.Errorf("failed to create snapshot: %w", err)
	}

	s.mu.Lock()
	s.opsSinceSnapshot = 0
	s.mu.Unlock()

	// Clean up old snapshots (keep last 3)
	if err := s.snapshotManager.DeleteOldSnapshots(3); err != nil {
		fmt.Printf("WARNING: Failed to delete old snapshots: %v\n", err)
	}

	// Compact WAL after snapshot (if enabled)
	if s.config.CompactionEnabled && raftIndex > 0 {
		// Keep a safety margin before the snapshot
		compactBefore := raftIndex
		if raftIndex > s.config.CompactionMargin {
			compactBefore = raftIndex - s.config.CompactionMargin
		}

		deletedCount, err := s.wal.CompactSegmentsBefore(compactBefore)
		if err != nil {
			fmt.Printf("WARNING: Failed to compact WAL: %v\n", err)
		} else if deletedCount > 0 {
			fmt.Printf("WAL compacted: deleted %d segments (before RaftIndex %d)\n", deletedCount, compactBefore)
		}
	}

	// Check if we need forced compaction due to too many segments
	if s.config.MaxWALSegments > 0 {
		segmentCount, _ := s.wal.GetSegmentCount()
		if segmentCount > s.config.MaxWALSegments {
			fmt.Printf("WARNING: WAL has %d segments (max: %d). Consider triggering snapshot.\n",
				segmentCount, s.config.MaxWALSegments)
		}
	}

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

	s.mu.Lock()
	s.walIndex = snapshot.Index
	s.lastRaftIndex = snapshot.RaftIndex
	s.lastRaftTerm = snapshot.RaftTerm
	s.opsSinceSnapshot = 0
	s.mu.Unlock()

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

// ApplyRaftEntry applies a Raft log entry to the store with Raft metadata
// This is the Raft-aware version of Put/Delete
func (s *DurableStore) ApplyRaftEntry(ctx context.Context, index, term uint64, op string, key string, value []byte) error {
	// Determine operation type
	var operation byte
	switch op {
	case "put":
		operation = OpPut
	case "delete":
		operation = OpDelete
	default:
		return fmt.Errorf("unknown operation: %s", op)
	}

	// Create WAL entry with Raft metadata
	entry := &WALEntry{
		RaftIndex: index,
		RaftTerm:  term,
		Operation: operation,
		Timestamp: time.Now(),
		Key:       key,
		Value:     value,
	}

	// 1. Write to WAL first
	if err := s.wal.Append(entry); err != nil {
		return fmt.Errorf("failed to write to WAL: %w", err)
	}

	// 2. Apply to memory
	switch operation {
	case OpPut:
		if err := s.memory.Put(ctx, key, value); err != nil {
			return fmt.Errorf("failed to apply PUT to memory: %w", err)
		}
	case OpDelete:
		if err := s.memory.Delete(ctx, key); err != nil {
			return fmt.Errorf("failed to apply DELETE to memory: %w", err)
		}
	}

	// 3. Update counters and Raft tracking
	s.mu.Lock()
	s.walIndex++
	s.opsSinceSnapshot++
	s.lastRaftIndex = index
	s.lastRaftTerm = term
	shouldSnapshot := s.snapshotEvery > 0 &&
		s.opsSinceSnapshot >= s.snapshotEvery &&
		!s.snapshotting.Load()
	s.mu.Unlock()

	// 4. Trigger snapshot if needed
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

// LastAppliedIndex returns the last applied Raft index and term
func (s *DurableStore) LastAppliedIndex() (index, term uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRaftIndex, s.lastRaftTerm
}

// SetLastAppliedIndex sets the last applied Raft index (used during restore)
func (s *DurableStore) SetLastAppliedIndex(index, term uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastRaftIndex = index
	s.lastRaftTerm = term
}
