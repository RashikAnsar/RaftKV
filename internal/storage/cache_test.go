package storage

import (
	"fmt"
	"testing"
	"time"
)

func TestLRUCache_BasicOperations(t *testing.T) {
	cache := NewLRUCache(CacheConfig{MaxSize: 3, TTL: 0})

	// Test Put and Get
	cache.Put("key1", []byte("value1"))
	cache.Put("key2", []byte("value2"))
	cache.Put("key3", []byte("value3"))

	// Verify all entries exist
	if val, ok := cache.Get("key1"); !ok || string(val) != "value1" {
		t.Errorf("Expected key1=value1, got %s", val)
	}
	if val, ok := cache.Get("key2"); !ok || string(val) != "value2" {
		t.Errorf("Expected key2=value2, got %s", val)
	}
	if val, ok := cache.Get("key3"); !ok || string(val) != "value3" {
		t.Errorf("Expected key3=value3, got %s", val)
	}

	// Test cache size
	if cache.Size() != 3 {
		t.Errorf("Expected cache size 3, got %d", cache.Size())
	}
}

func TestLRUCache_Eviction(t *testing.T) {
	cache := NewLRUCache(CacheConfig{MaxSize: 3, TTL: 0})

	// Fill cache to capacity
	cache.Put("key1", []byte("value1"))
	cache.Put("key2", []byte("value2"))
	cache.Put("key3", []byte("value3"))

	// Add one more - should evict key1 (least recently used)
	cache.Put("key4", []byte("value4"))

	// key1 should be evicted
	if _, ok := cache.Get("key1"); ok {
		t.Error("Expected key1 to be evicted")
	}

	// Others should still exist
	if _, ok := cache.Get("key2"); !ok {
		t.Error("Expected key2 to exist")
	}
	if _, ok := cache.Get("key3"); !ok {
		t.Error("Expected key3 to exist")
	}
	if _, ok := cache.Get("key4"); !ok {
		t.Error("Expected key4 to exist")
	}

	// Check size
	if cache.Size() != 3 {
		t.Errorf("Expected cache size 3, got %d", cache.Size())
	}
}

func TestLRUCache_LRUOrder(t *testing.T) {
	cache := NewLRUCache(CacheConfig{MaxSize: 3, TTL: 0})

	// Add entries
	cache.Put("key1", []byte("value1"))
	cache.Put("key2", []byte("value2"))
	cache.Put("key3", []byte("value3"))

	// Access key1 - makes it most recently used
	cache.Get("key1")

	// Add key4 - should evict key2 (now least recently used)
	cache.Put("key4", []byte("value4"))

	// key2 should be evicted
	if _, ok := cache.Get("key2"); ok {
		t.Error("Expected key2 to be evicted")
	}

	// key1 should still exist (was accessed recently)
	if _, ok := cache.Get("key1"); !ok {
		t.Error("Expected key1 to exist")
	}
}

func TestLRUCache_Update(t *testing.T) {
	cache := NewLRUCache(CacheConfig{MaxSize: 3, TTL: 0})

	// Add entry
	cache.Put("key1", []byte("value1"))

	// Update entry
	cache.Put("key1", []byte("updated"))

	// Should get updated value
	if val, ok := cache.Get("key1"); !ok || string(val) != "updated" {
		t.Errorf("Expected updated value, got %s", val)
	}

	// Size should still be 1
	if cache.Size() != 1 {
		t.Errorf("Expected cache size 1, got %d", cache.Size())
	}
}

func TestLRUCache_Delete(t *testing.T) {
	cache := NewLRUCache(CacheConfig{MaxSize: 3, TTL: 0})

	cache.Put("key1", []byte("value1"))
	cache.Put("key2", []byte("value2"))

	// Delete key1
	cache.Delete("key1")

	// key1 should not exist
	if _, ok := cache.Get("key1"); ok {
		t.Error("Expected key1 to be deleted")
	}

	// key2 should still exist
	if _, ok := cache.Get("key2"); !ok {
		t.Error("Expected key2 to exist")
	}

	// Size should be 1
	if cache.Size() != 1 {
		t.Errorf("Expected cache size 1, got %d", cache.Size())
	}
}

func TestLRUCache_Clear(t *testing.T) {
	cache := NewLRUCache(CacheConfig{MaxSize: 3, TTL: 0})

	cache.Put("key1", []byte("value1"))
	cache.Put("key2", []byte("value2"))
	cache.Put("key3", []byte("value3"))

	// Clear cache
	cache.Clear()

	// All keys should be gone
	if _, ok := cache.Get("key1"); ok {
		t.Error("Expected key1 to be cleared")
	}
	if _, ok := cache.Get("key2"); ok {
		t.Error("Expected key2 to be cleared")
	}
	if _, ok := cache.Get("key3"); ok {
		t.Error("Expected key3 to be cleared")
	}

	// Size should be 0
	if cache.Size() != 0 {
		t.Errorf("Expected cache size 0, got %d", cache.Size())
	}
}

func TestLRUCache_TTL(t *testing.T) {
	cache := NewLRUCache(CacheConfig{MaxSize: 3, TTL: 50 * time.Millisecond})

	cache.Put("key1", []byte("value1"))

	// Should exist immediately
	if _, ok := cache.Get("key1"); !ok {
		t.Error("Expected key1 to exist")
	}

	// Wait for TTL to expire
	time.Sleep(100 * time.Millisecond)

	// Should be expired
	if _, ok := cache.Get("key1"); ok {
		t.Error("Expected key1 to be expired")
	}
}

func TestLRUCache_Stats(t *testing.T) {
	cache := NewLRUCache(CacheConfig{MaxSize: 3, TTL: 0})

	cache.Put("key1", []byte("value1"))
	cache.Put("key2", []byte("value2"))

	// 2 hits
	cache.Get("key1")
	cache.Get("key2")

	// 2 misses
	cache.Get("key3")
	cache.Get("key4")

	stats := cache.Stats()

	if stats.Hits != 2 {
		t.Errorf("Expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 2 {
		t.Errorf("Expected 2 misses, got %d", stats.Misses)
	}
	if stats.HitRate != 50.0 {
		t.Errorf("Expected 50%% hit rate, got %.2f%%", stats.HitRate)
	}
	if stats.Size != 2 {
		t.Errorf("Expected size 2, got %d", stats.Size)
	}

	// Add entries to trigger eviction
	cache.Put("key3", []byte("value3"))
	cache.Put("key4", []byte("value4"))

	stats = cache.Stats()
	if stats.Evicts != 1 {
		t.Errorf("Expected 1 eviction, got %d", stats.Evicts)
	}
}

func TestLRUCache_ValueIsolation(t *testing.T) {
	cache := NewLRUCache(CacheConfig{MaxSize: 3, TTL: 0})

	original := []byte("value1")
	cache.Put("key1", original)

	// Modify original - should not affect cached value
	original[0] = 'X'

	val, ok := cache.Get("key1")
	if !ok {
		t.Fatal("Expected key1 to exist")
	}
	if string(val) != "value1" {
		t.Errorf("Expected value1, got %s (cache was mutated)", val)
	}

	// Modify retrieved value - should not affect cache
	val[0] = 'Y'

	val2, _ := cache.Get("key1")
	if string(val2) != "value1" {
		t.Errorf("Expected value1, got %s (cache was mutated)", val2)
	}
}

func TestLRUCache_Concurrent(t *testing.T) {
	cache := NewLRUCache(CacheConfig{MaxSize: 100, TTL: 0})

	// Run concurrent operations
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				value := fmt.Sprintf("value-%d-%d", id, j)

				cache.Put(key, []byte(value))
				cache.Get(key)
				if j%10 == 0 {
					cache.Delete(key)
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not have panicked
	stats := cache.Stats()
	t.Logf("Concurrent test stats: %+v", stats)
}

// Benchmark cache operations
func BenchmarkLRUCache_Put(b *testing.B) {
	cache := NewLRUCache(CacheConfig{MaxSize: 10000, TTL: 0})
	value := []byte("benchmark-value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i%10000)
		cache.Put(key, value)
	}
}

func BenchmarkLRUCache_Get_Hit(b *testing.B) {
	cache := NewLRUCache(CacheConfig{MaxSize: 10000, TTL: 0})

	// Pre-populate cache
	for i := 0; i < 10000; i++ {
		cache.Put(fmt.Sprintf("key-%d", i), []byte("value"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i%10000)
		cache.Get(key)
	}
}

func BenchmarkLRUCache_Get_Miss(b *testing.B) {
	cache := NewLRUCache(CacheConfig{MaxSize: 10000, TTL: 0})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		cache.Get(key)
	}
}

func BenchmarkLRUCache_Mixed(b *testing.B) {
	cache := NewLRUCache(CacheConfig{MaxSize: 10000, TTL: 0})
	value := []byte("benchmark-value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i%10000)
		if i%3 == 0 {
			cache.Put(key, value)
		} else {
			cache.Get(key)
		}
	}
}
