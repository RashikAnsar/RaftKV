package storage

import (
	"context"
	"time"
)

// CachedStore wraps a Store with an LRU cache for read optimization
type CachedStore struct {
	store Store
	cache *LRUCache
}

// NewCachedStore creates a new cached store wrapper
func NewCachedStore(store Store, config CacheConfig) *CachedStore {
	return &CachedStore{
		store: store,
		cache: NewLRUCache(config),
	}
}

// Get retrieves a value, checking cache first
func (cs *CachedStore) Get(ctx context.Context, key string) ([]byte, error) {
	// Check cache first
	if value, hit := cs.cache.Get(key); hit {
		return value, nil
	}

	// Cache miss - get from underlying store
	value, err := cs.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	// Add to cache
	cs.cache.Put(key, value)

	return value, nil
}

// Put stores a value and updates the cache
func (cs *CachedStore) Put(ctx context.Context, key string, value []byte) error {
	// Write to underlying store first
	if err := cs.store.Put(ctx, key, value); err != nil {
		return err
	}

	// Update cache (write-through)
	cs.cache.Put(key, value)

	return nil
}

// PutWithTTL stores a value with TTL and updates the cache
func (cs *CachedStore) PutWithTTL(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	// Write to underlying store first
	if err := cs.store.PutWithTTL(ctx, key, value, ttl); err != nil {
		return err
	}

	// Update cache (write-through)
	// Note: Cache has its own global TTL; per-key TTL is managed by underlying store
	cs.cache.Put(key, value)

	return nil
}

// GetTTL returns the remaining TTL for a key
func (cs *CachedStore) GetTTL(ctx context.Context, key string) (time.Duration, error) {
	// Delegate to underlying store
	// Note: We don't cache TTL values as they change over time
	return cs.store.GetTTL(ctx, key)
}

// SetTTL updates the TTL for an existing key
func (cs *CachedStore) SetTTL(ctx context.Context, key string, ttl time.Duration) error {
	// Update in underlying store
	if err := cs.store.SetTTL(ctx, key, ttl); err != nil {
		return err
	}

	// No cache update needed - cache TTL is managed separately
	return nil
}

// Delete removes a value and invalidates cache
func (cs *CachedStore) Delete(ctx context.Context, key string) error {
	// Delete from underlying store
	if err := cs.store.Delete(ctx, key); err != nil {
		return err
	}

	// Invalidate cache entry
	cs.cache.Delete(key)

	return nil
}

// List returns all keys with the given prefix (bypasses cache)
func (cs *CachedStore) List(ctx context.Context, prefix string, limit int) ([]string, error) {
	// List operations bypass cache (typically infrequent)
	return cs.store.List(ctx, prefix, limit)
}

// ListWithOptions performs a filtered and paginated list operation (bypasses cache)
func (cs *CachedStore) ListWithOptions(ctx context.Context, opts ListOptions) (*ListResult, error) {
	// List operations bypass cache (typically infrequent)
	return cs.store.ListWithOptions(ctx, opts)
}

// Snapshot creates a snapshot of the underlying store
func (cs *CachedStore) Snapshot(ctx context.Context) (string, error) {
	return cs.store.Snapshot(ctx)
}

// Restore restores from a snapshot
func (cs *CachedStore) Restore(ctx context.Context, snapshotPath string) error {
	// Clear cache when restoring
	cs.cache.Clear()
	return cs.store.Restore(ctx, snapshotPath)
}

// Stats returns both store and cache statistics
func (cs *CachedStore) Stats() Stats {
	stats := cs.store.Stats()
	cacheStats := cs.cache.Stats()

	// Add cache stats to store stats
	stats.CacheHits = cacheStats.Hits
	stats.CacheMisses = cacheStats.Misses
	stats.CacheHitRate = cacheStats.HitRate
	stats.CacheSize = cacheStats.Size
	stats.CacheEvictions = cacheStats.Evicts

	return stats
}

// Close closes the underlying store
func (cs *CachedStore) Close() error {
	// Clear cache
	cs.cache.Clear()

	// Close underlying store
	return cs.store.Close()
}

// ClearCache removes all entries from the cache
func (cs *CachedStore) ClearCache() {
	cs.cache.Clear()
}

// GetCacheStats returns cache statistics
func (cs *CachedStore) GetCacheStats() CacheStats {
	return cs.cache.Stats()
}

// Unwrap returns the underlying store (useful for testing)
func (cs *CachedStore) Unwrap() Store {
	return cs.store
}
