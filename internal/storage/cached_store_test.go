package storage

import (
	"context"
	"testing"
)

func TestCachedStore_GetCacheHit(t *testing.T) {
	ctx := context.Background()
	baseStore := NewMemoryStore()
	cachedStore := NewCachedStore(baseStore, CacheConfig{MaxSize: 100, TTL: 0})

	// Put a value
	if err := cachedStore.Put(ctx, "key1", []byte("value1")); err != nil {
		t.Fatalf("Failed to put: %v", err)
	}

	// First get - should be cache hit (write-through)
	val, err := cachedStore.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Failed to get: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("Expected value1, got %s", val)
	}

	// Second get - should also be cache hit
	val2, err := cachedStore.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Failed to get: %v", err)
	}
	if string(val2) != "value1" {
		t.Errorf("Expected value1, got %s", val2)
	}

	// Check cache stats
	stats := cachedStore.Stats()
	if stats.CacheHits != 2 {
		t.Errorf("Expected 2 cache hits, got %d", stats.CacheHits)
	}
	if stats.CacheMisses != 0 {
		t.Errorf("Expected 0 cache misses, got %d", stats.CacheMisses)
	}
}

func TestCachedStore_GetCacheMiss(t *testing.T) {
	ctx := context.Background()
	baseStore := NewMemoryStore()
	cachedStore := NewCachedStore(baseStore, CacheConfig{MaxSize: 100, TTL: 0})

	// Put directly in base store (bypass cache)
	if err := baseStore.Put(ctx, "key1", []byte("value1")); err != nil {
		t.Fatalf("Failed to put: %v", err)
	}

	// Get - should be cache miss
	val, err := cachedStore.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Failed to get: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("Expected value1, got %s", val)
	}

	// Check cache stats
	stats := cachedStore.Stats()
	if stats.CacheMisses != 1 {
		t.Errorf("Expected 1 cache miss, got %d", stats.CacheMisses)
	}

	// Second get - should be cache hit (value was cached after first miss)
	val2, err := cachedStore.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Failed to get: %v", err)
	}
	if string(val2) != "value1" {
		t.Errorf("Expected value1, got %s", val2)
	}

	stats = cachedStore.Stats()
	if stats.CacheHits != 1 {
		t.Errorf("Expected 1 cache hit, got %d", stats.CacheHits)
	}
}

func TestCachedStore_DeleteInvalidatesCache(t *testing.T) {
	ctx := context.Background()
	baseStore := NewMemoryStore()
	cachedStore := NewCachedStore(baseStore, CacheConfig{MaxSize: 100, TTL: 0})

	// Put a value
	if err := cachedStore.Put(ctx, "key1", []byte("value1")); err != nil {
		t.Fatalf("Failed to put: %v", err)
	}

	// Get to confirm it's in cache
	cachedStore.Get(ctx, "key1")

	// Delete
	if err := cachedStore.Delete(ctx, "key1"); err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

	// Try to get - should not exist
	_, err := cachedStore.Get(ctx, "key1")
	if err != ErrKeyNotFound {
		t.Errorf("Expected ErrKeyNotFound, got %v", err)
	}

	// Cache should have been invalidated
	stats := cachedStore.GetCacheStats()
	if stats.Size != 0 {
		t.Errorf("Expected cache size 0, got %d", stats.Size)
	}
}

func TestCachedStore_PutUpdatesCache(t *testing.T) {
	ctx := context.Background()
	baseStore := NewMemoryStore()
	cachedStore := NewCachedStore(baseStore, CacheConfig{MaxSize: 100, TTL: 0})

	// Put initial value
	if err := cachedStore.Put(ctx, "key1", []byte("value1")); err != nil {
		t.Fatalf("Failed to put: %v", err)
	}

	// Update value
	if err := cachedStore.Put(ctx, "key1", []byte("updated")); err != nil {
		t.Fatalf("Failed to put: %v", err)
	}

	// Get should return updated value from cache
	val, err := cachedStore.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Failed to get: %v", err)
	}
	if string(val) != "updated" {
		t.Errorf("Expected updated, got %s", val)
	}

	// Should be cache hit
	stats := cachedStore.Stats()
	if stats.CacheHits != 1 {
		t.Errorf("Expected 1 cache hit, got %d", stats.CacheHits)
	}
}

func TestCachedStore_RestoreClearsCache(t *testing.T) {
	ctx := context.Background()
	baseStore := NewMemoryStore()
	cachedStore := NewCachedStore(baseStore, CacheConfig{MaxSize: 100, TTL: 0})

	// Put some values
	cachedStore.Put(ctx, "key1", []byte("value1"))
	cachedStore.Put(ctx, "key2", []byte("value2"))

	// Cache should have entries
	stats := cachedStore.GetCacheStats()
	if stats.Size != 2 {
		t.Errorf("Expected cache size 2, got %d", stats.Size)
	}

	// Note: Restore will fail on MemoryStore (not implemented), but we're testing cache clearing
	// In real usage, restore would work with DurableStore
	cachedStore.Restore(ctx, "dummy-path") // Will fail, but should clear cache

	// Cache should be cleared
	stats = cachedStore.GetCacheStats()
	if stats.Size != 0 {
		t.Errorf("Expected cache size 0 after restore, got %d", stats.Size)
	}
}

func TestCachedStore_ClearCache(t *testing.T) {
	ctx := context.Background()
	baseStore := NewMemoryStore()
	cachedStore := NewCachedStore(baseStore, CacheConfig{MaxSize: 100, TTL: 0})

	// Put some values
	cachedStore.Put(ctx, "key1", []byte("value1"))
	cachedStore.Put(ctx, "key2", []byte("value2"))
	cachedStore.Put(ctx, "key3", []byte("value3"))

	// Clear cache
	cachedStore.ClearCache()

	// Cache should be empty
	stats := cachedStore.GetCacheStats()
	if stats.Size != 0 {
		t.Errorf("Expected cache size 0, got %d", stats.Size)
	}

	// Data should still be in underlying store
	val, err := cachedStore.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Failed to get after cache clear: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("Expected value1, got %s", val)
	}

	// Should have been a cache miss
	stats2 := cachedStore.Stats()
	if stats2.CacheMisses != 1 {
		t.Errorf("Expected 1 cache miss, got %d", stats2.CacheMisses)
	}
}

func TestCachedStore_Stats(t *testing.T) {
	ctx := context.Background()
	baseStore := NewMemoryStore()
	cachedStore := NewCachedStore(baseStore, CacheConfig{MaxSize: 100, TTL: 0})

	// Perform operations
	cachedStore.Put(ctx, "key1", []byte("value1"))
	cachedStore.Put(ctx, "key2", []byte("value2"))

	cachedStore.Get(ctx, "key1") // hit
	cachedStore.Get(ctx, "key2") // hit
	cachedStore.Get(ctx, "key3") // miss

	cachedStore.Delete(ctx, "key1")

	// Check stats
	stats := cachedStore.Stats()

	// Store stats
	if stats.Puts != 2 {
		t.Errorf("Expected 2 puts, got %d", stats.Puts)
	}
	// Only 1 get actually hit the underlying store (key3 was a cache miss)
	// key1 and key2 were cache hits, so they didn't increment the store's Get counter
	if stats.Gets != 1 {
		t.Errorf("Expected 1 get (cache miss), got %d", stats.Gets)
	}
	if stats.Deletes != 1 {
		t.Errorf("Expected 1 delete, got %d", stats.Deletes)
	}

	// Cache stats
	if stats.CacheHits != 2 {
		t.Errorf("Expected 2 cache hits, got %d", stats.CacheHits)
	}
	if stats.CacheMisses != 1 {
		t.Errorf("Expected 1 cache miss, got %d", stats.CacheMisses)
	}
	if stats.CacheHitRate != 66.66666666666666 {
		t.Logf("Cache hit rate: %.2f%%", stats.CacheHitRate)
	}
}

// Benchmark cached vs uncached reads
func BenchmarkCachedStore_Get_CacheHit(b *testing.B) {
	ctx := context.Background()
	baseStore := NewMemoryStore()
	cachedStore := NewCachedStore(baseStore, CacheConfig{MaxSize: 10000, TTL: 0})

	// Pre-populate
	for i := 0; i < 1000; i++ {
		key := string(rune('a' + (i % 26))) + string(rune('a' + (i/26)%26))
		cachedStore.Put(ctx, key, []byte("value"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := string(rune('a' + (i % 26))) + string(rune('a' + (i/26)%26))
		cachedStore.Get(ctx, key)
	}
}

func BenchmarkMemoryStore_Get_NoCache(b *testing.B) {
	ctx := context.Background()
	baseStore := NewMemoryStore()

	// Pre-populate
	for i := 0; i < 1000; i++ {
		key := string(rune('a' + (i % 26))) + string(rune('a' + (i/26)%26))
		baseStore.Put(ctx, key, []byte("value"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := string(rune('a' + (i % 26))) + string(rune('a' + (i/26)%26))
		baseStore.Get(ctx, key)
	}
}
