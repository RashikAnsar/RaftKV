package storage

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// KeyValue represents a versioned key-value pair
type KeyValue struct {
	Key       string
	Value     []byte
	Version   uint64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type MemoryStore struct {
	mu     sync.RWMutex
	data   map[string]*KeyValue
	closed atomic.Bool

	statsGets    atomic.Int64
	statsPuts    atomic.Int64
	statsDeletes atomic.Int64
	statsCAS     atomic.Int64 // CAS operations counter
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data: make(map[string]*KeyValue),
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
	kv, exists := s.data[key]
	s.mu.RUnlock()

	s.statsGets.Add(1)

	if !exists {
		return nil, ErrKeyNotFound
	}

	result := make([]byte, len(kv.Value))
	copy(result, kv.Value)

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

	now := time.Now()
	s.mu.Lock()
	existing, exists := s.data[key]
	if exists {
		// Update existing key - increment version
		s.data[key] = &KeyValue{
			Key:       key,
			Value:     valueCopy,
			Version:   existing.Version + 1,
			CreatedAt: existing.CreatedAt,
			UpdatedAt: now,
		}
	} else {
		// New key - version starts at 1
		s.data[key] = &KeyValue{
			Key:       key,
			Value:     valueCopy,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}
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

// ListWithOptions performs a filtered and paginated list operation
func (s *MemoryStore) ListWithOptions(ctx context.Context, opts ListOptions) (*ListResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s.closed.Load() {
		return nil, ErrStoreClosed
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Collect all keys matching the filters
	allKeys := make([]string, 0)
	for key := range s.data {
		// Check context periodically for large datasets
		if len(allKeys)%1000 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		// Apply prefix filter
		if opts.Prefix != "" && !strings.HasPrefix(key, opts.Prefix) {
			continue
		}

		// Apply range filter (Start/End)
		if opts.Start != "" && key < opts.Start {
			continue
		}
		if opts.End != "" && key > opts.End {
			continue
		}

		allKeys = append(allKeys, key)
	}

	// Sort keys
	if opts.Reverse {
		// Sort in descending order
		sort.Slice(allKeys, func(i, j int) bool {
			return allKeys[i] > allKeys[j]
		})
	} else {
		// Sort in ascending order
		sort.Strings(allKeys)
	}

	// Apply cursor (skip keys up to and including cursor)
	startIdx := 0
	if opts.Cursor != "" {
		for i, key := range allKeys {
			if key == opts.Cursor {
				startIdx = i + 1
				break
			}
		}
	}

	// Apply limit and determine if there are more results
	endIdx := len(allKeys)
	hasMore := false
	if opts.Limit > 0 && startIdx+opts.Limit < len(allKeys) {
		endIdx = startIdx + opts.Limit
		hasMore = true
	}

	// Extract the page of keys
	var pageKeys []string
	if startIdx < len(allKeys) {
		pageKeys = allKeys[startIdx:endIdx]
	} else {
		pageKeys = []string{}
	}

	// Determine next cursor
	nextCursor := ""
	if hasMore && len(pageKeys) > 0 {
		nextCursor = pageKeys[len(pageKeys)-1]
	}

	return &ListResult{
		Keys:       pageKeys,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

func (s *MemoryStore) Snapshot(ctx context.Context) (string, error) {
	// Not implemented for memory store (no persistence)
	return "", errors.New("snapshot not supported by memory store")
}

func (s *MemoryStore) Restore(ctx context.Context, snapshotPath string) error {
	// Not implemented for memory store (no persistence)
	return errors.New("restore not supported by memory store")
}

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
	s.data = make(map[string]*KeyValue)
	s.mu.Unlock()

	s.statsGets.Store(0)
	s.statsPuts.Store(0)
	s.statsDeletes.Store(0)
	s.statsCAS.Store(0)
}

// RestoreKeyValue directly restores a KeyValue (used for snapshot recovery)
func (s *MemoryStore) RestoreKeyValue(ctx context.Context, key string, kv *KeyValue) error {
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
	defer s.mu.Unlock()

	// Directly set the KeyValue to preserve version information
	s.data[key] = kv

	return nil
}

// GetWithVersion retrieves both the value and version for a key
func (s *MemoryStore) GetWithVersion(ctx context.Context, key string) ([]byte, uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}

	if key == "" {
		return nil, 0, ErrInvalidKey
	}

	if s.closed.Load() {
		return nil, 0, ErrStoreClosed
	}

	s.mu.RLock()
	kv, exists := s.data[key]
	s.mu.RUnlock()

	s.statsGets.Add(1)

	if !exists {
		return nil, 0, ErrKeyNotFound
	}

	result := make([]byte, len(kv.Value))
	copy(result, kv.Value)

	return result, kv.Version, nil
}

// CompareAndSwap atomically updates a key if the current version matches expectedVersion
// Returns the current version and whether the swap succeeded
func (s *MemoryStore) CompareAndSwap(ctx context.Context, key string, expectedVersion uint64, newValue []byte) (uint64, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}

	if key == "" {
		return 0, false, ErrInvalidKey
	}

	if s.closed.Load() {
		return 0, false, ErrStoreClosed
	}

	valueCopy := make([]byte, len(newValue))
	copy(valueCopy, newValue)

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.data[key]
	if !exists {
		// Key doesn't exist - CAS fails
		s.statsCAS.Add(1)
		return 0, false, nil
	}

	if existing.Version != expectedVersion {
		// Version mismatch - CAS fails
		s.statsCAS.Add(1)
		return existing.Version, false, nil
	}

	// Version matches - perform the swap
	s.data[key] = &KeyValue{
		Key:       key,
		Value:     valueCopy,
		Version:   existing.Version + 1,
		CreatedAt: existing.CreatedAt,
		UpdatedAt: now,
	}

	s.statsCAS.Add(1)
	return existing.Version + 1, true, nil
}

// SetIfNotExists atomically creates a key only if it doesn't exist
// Returns the version and whether the key was created
func (s *MemoryStore) SetIfNotExists(ctx context.Context, key string, value []byte) (uint64, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}

	if key == "" {
		return 0, false, ErrInvalidKey
	}

	if s.closed.Load() {
		return 0, false, ErrStoreClosed
	}

	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, exists := s.data[key]; exists {
		// Key already exists - operation fails, return existing version
		s.statsCAS.Add(1)
		return existing.Version, false, nil
	}

	// Key doesn't exist - create it
	s.data[key] = &KeyValue{
		Key:       key,
		Value:     valueCopy,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.statsCAS.Add(1)
	s.statsPuts.Add(1)
	return 1, true, nil
}
