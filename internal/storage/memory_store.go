package storage

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
)

type MemoryStore struct {
	mu     sync.RWMutex
	data   map[string][]byte
	closed atomic.Bool

	statsGets    atomic.Int64
	statsPuts    atomic.Int64
	statsDeletes atomic.Int64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data: make(map[string][]byte),
	}
}

func (s *MemoryStore) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if key == "" {
		return nil, ErrInvalidKey
	}

	if s.closed.Load() {
		return nil, ErrStoreClosed
	}

	s.mu.RLock()
	value, exists := s.data[key]
	s.mu.RUnlock()

	s.statsGets.Add(1)

	if !exists {
		return nil, ErrKeyNotFound
	}

	result := make([]byte, len(value))
	copy(result, value)

	return result, nil
}

func (s *MemoryStore) Put(ctx context.Context, key string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if key == "" {
		return ErrInvalidKey
	}

	if s.closed.Load() {
		return ErrStoreClosed
	}

	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	s.mu.Lock()
	s.data[key] = valueCopy
	s.mu.Unlock()

	s.statsPuts.Add(1)

	return nil
}

func (s *MemoryStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if key == "" {
		return ErrInvalidKey
	}

	if s.closed.Load() {
		return ErrStoreClosed
	}

	s.mu.Lock()
	delete(s.data, key)
	s.mu.Unlock()

	s.statsDeletes.Add(1)

	return nil
}

func (s *MemoryStore) List(ctx context.Context, prefix string, limit int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s.closed.Load() {
		return nil, ErrStoreClosed
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]string, 0)
	for key := range s.data {
		if len(keys)%1000 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
			if limit > 0 && len(keys) >= limit {
				break
			}
		}
	}

	return keys, nil
}

// NOTE: Snapshot() and Restore() removed from MemoryStore.
// MemoryStore is now only used internally by DurableStore.
// DurableStore handles all snapshot/restore operations.

func (s *MemoryStore) Stats() Stats {
	s.mu.RLock()
	keyCount := len(s.data)
	s.mu.RUnlock()

	return Stats{
		Gets:     s.statsGets.Load(),
		Puts:     s.statsPuts.Load(),
		Deletes:  s.statsDeletes.Load(),
		KeyCount: int64(keyCount),
	}
}

func (s *MemoryStore) Close() error {
	if s.closed.Swap(true) {
		return ErrStoreClosed
	}

	s.mu.Lock()
	s.data = nil
	s.mu.Unlock()

	return nil
}

func (s *MemoryStore) Reset() {
	s.mu.Lock()
	s.data = make(map[string][]byte)
	s.mu.Unlock()

	s.statsGets.Store(0)
	s.statsPuts.Store(0)
	s.statsDeletes.Store(0)
}
