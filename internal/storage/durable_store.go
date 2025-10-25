package storage

import (
	"context"
	"fmt"
	"time"
)

type DurableStore struct {
	memory *MemoryStore
	wal    *WAL
}

type DurableStoreConfig struct {
	DataDir     string
	SyncOnWrite bool // true = durable but slower, false = faster but risk data loss
}

func NewDurableStore(config DurableStoreConfig) (*DurableStore, error) {
	wal, err := NewWAL(WALConfig{
		Dir:         config.DataDir,
		SyncOnWrite: config.SyncOnWrite,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create WAL: %w", err)
	}

	memory := NewMemoryStore()

	store := &DurableStore{
		memory: memory,
		wal:    wal,
	}

	// Replay WAL to restore state
	if err := store.recover(); err != nil {
		wal.Close()
		return nil, fmt.Errorf("failed to recover from WAL: %w", err)
	}

	return store, nil
}

func (s *DurableStore) recover() error {
	entries, err := s.wal.Replay()
	if err != nil {
		return err
	}

	ctx := context.Background()

	for _, entry := range entries {
		switch entry.Operation {
		case OpPut:
			if err := s.memory.Put(ctx, entry.Key, entry.Value); err != nil {
				return fmt.Errorf("failed to apply PUT during recovery: %w", err)
			}

		case OpDelete:
			if err := s.memory.Delete(ctx, entry.Key); err != nil {
				return fmt.Errorf("failed to apply DELETE during recovery: %w", err)
			}

		default:
			return fmt.Errorf("unknown operation type: %d", entry.Operation)
		}
	}

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

	return nil
}

func (s *DurableStore) Delete(ctx context.Context, key string) error {
	entry := &WALEntry{
		Operation: OpDelete,
		Timestamp: time.Now(),
		Key:       key,
	}

	if err := s.wal.Append(entry); err != nil {
		return fmt.Errorf("failed to write to WAL: %w", err)
	}

	if err := s.memory.Delete(ctx, key); err != nil {
		return fmt.Errorf("failed to apply to memory after WAL write: %w", err)
	}

	return nil
}

func (s *DurableStore) List(ctx context.Context, prefix string, limit int) ([]string, error) {
	return s.memory.List(ctx, prefix, limit)
}

func (s *DurableStore) Snapshot(ctx context.Context) (string, error) {
	return "", nil
}

func (s *DurableStore) Restore(ctx context.Context, snapshotPath string) error {
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
