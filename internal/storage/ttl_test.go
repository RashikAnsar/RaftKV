package storage

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStore_PutWithTTL(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Test putting a key with TTL
	err := store.PutWithTTL(ctx, "test-key", []byte("test-value"), 1*time.Second)
	if err != nil {
		t.Fatalf("PutWithTTL failed: %v", err)
	}

	// Verify the key exists immediately
	value, err := store.Get(ctx, "test-key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(value) != "test-value" {
		t.Errorf("Expected value 'test-value', got '%s'", string(value))
	}

	// Wait for expiration
	time.Sleep(1200 * time.Millisecond)

	// Verify the key has expired
	_, err = store.Get(ctx, "test-key")
	if err != ErrKeyNotFound {
		t.Errorf("Expected ErrKeyNotFound after expiration, got: %v", err)
	}
}

func TestMemoryStore_GetTTL(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Test getting TTL for non-existent key
	_, err := store.GetTTL(ctx, "non-existent")
	if err != ErrKeyNotFound {
		t.Errorf("Expected ErrKeyNotFound for non-existent key, got: %v", err)
	}

	// Put a key without TTL
	err = store.Put(ctx, "no-ttl-key", []byte("value"))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Get TTL for key without TTL
	ttl, err := store.GetTTL(ctx, "no-ttl-key")
	if err != nil {
		t.Fatalf("GetTTL failed: %v", err)
	}
	if ttl != 0 {
		t.Errorf("Expected TTL 0 for key without TTL, got: %v", ttl)
	}

	// Put a key with TTL
	err = store.PutWithTTL(ctx, "ttl-key", []byte("value"), 10*time.Second)
	if err != nil {
		t.Fatalf("PutWithTTL failed: %v", err)
	}

	// Get TTL for key with TTL
	ttl, err = store.GetTTL(ctx, "ttl-key")
	if err != nil {
		t.Fatalf("GetTTL failed: %v", err)
	}
	if ttl <= 0 || ttl > 10*time.Second {
		t.Errorf("Expected TTL around 10s, got: %v", ttl)
	}
}

func TestMemoryStore_SetTTL(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Test setting TTL for non-existent key
	err := store.SetTTL(ctx, "non-existent", 1*time.Second)
	if err != ErrKeyNotFound {
		t.Errorf("Expected ErrKeyNotFound for non-existent key, got: %v", err)
	}

	// Put a key without TTL
	err = store.Put(ctx, "test-key", []byte("value"))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Set TTL for the key
	err = store.SetTTL(ctx, "test-key", 1*time.Second)
	if err != nil {
		t.Fatalf("SetTTL failed: %v", err)
	}

	// Verify TTL was set
	ttl, err := store.GetTTL(ctx, "test-key")
	if err != nil {
		t.Fatalf("GetTTL failed: %v", err)
	}
	if ttl <= 0 || ttl > 1*time.Second {
		t.Errorf("Expected TTL around 1s, got: %v", ttl)
	}

	// Wait for expiration
	time.Sleep(1200 * time.Millisecond)

	// Verify the key has expired
	_, err = store.Get(ctx, "test-key")
	if err != ErrKeyNotFound {
		t.Errorf("Expected ErrKeyNotFound after expiration, got: %v", err)
	}
}

func TestMemoryStore_UpdateTTL(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Put a key with initial TTL
	err := store.PutWithTTL(ctx, "test-key", []byte("value"), 5*time.Second)
	if err != nil {
		t.Fatalf("PutWithTTL failed: %v", err)
	}

	// Wait a bit
	time.Sleep(500 * time.Millisecond)

	// Update TTL to a shorter duration
	err = store.SetTTL(ctx, "test-key", 500*time.Millisecond)
	if err != nil {
		t.Fatalf("SetTTL failed: %v", err)
	}

	// Wait for the updated TTL to expire
	time.Sleep(700 * time.Millisecond)

	// Verify the key has expired
	_, err = store.Get(ctx, "test-key")
	if err != ErrKeyNotFound {
		t.Errorf("Expected ErrKeyNotFound after updated TTL expiration, got: %v", err)
	}
}

func TestMemoryStore_PutWithTTL_VersionIncrement(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Put a key with TTL
	err := store.PutWithTTL(ctx, "test-key", []byte("value1"), 10*time.Second)
	if err != nil {
		t.Fatalf("PutWithTTL failed: %v", err)
	}

	// Get version
	_, version1, err := store.GetWithVersion(ctx, "test-key")
	if err != nil {
		t.Fatalf("GetWithVersion failed: %v", err)
	}
	if version1 != 1 {
		t.Errorf("Expected version 1, got: %d", version1)
	}

	// Update the key with new TTL
	err = store.PutWithTTL(ctx, "test-key", []byte("value2"), 10*time.Second)
	if err != nil {
		t.Fatalf("PutWithTTL failed: %v", err)
	}

	// Get new version
	_, version2, err := store.GetWithVersion(ctx, "test-key")
	if err != nil {
		t.Fatalf("GetWithVersion failed: %v", err)
	}
	if version2 != 2 {
		t.Errorf("Expected version 2, got: %d", version2)
	}
}

func TestMemoryStore_TTL_NegativeDuration(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Test putting a key with negative TTL
	err := store.PutWithTTL(ctx, "test-key", []byte("value"), -1*time.Second)
	if err == nil {
		t.Error("Expected error for negative TTL, got nil")
	}

	// Put a key without TTL first
	err = store.Put(ctx, "test-key", []byte("value"))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Test setting negative TTL
	err = store.SetTTL(ctx, "test-key", -1*time.Second)
	if err == nil {
		t.Error("Expected error for negative TTL, got nil")
	}
}

func TestDurableStore_PutWithTTL(t *testing.T) {
	// Create temporary directory for WAL
	dir := t.TempDir()

	config := DurableStoreConfig{
		DataDir: dir,
	}
	store, err := NewDurableStore(config)
	if err != nil {
		t.Fatalf("Failed to create durable store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Test putting a key with TTL
	err = store.PutWithTTL(ctx, "test-key", []byte("test-value"), 1*time.Second)
	if err != nil {
		t.Fatalf("PutWithTTL failed: %v", err)
	}

	// Verify the key exists immediately
	value, err := store.Get(ctx, "test-key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(value) != "test-value" {
		t.Errorf("Expected value 'test-value', got '%s'", string(value))
	}

	// Wait for expiration
	time.Sleep(1200 * time.Millisecond)

	// Verify the key has expired
	_, err = store.Get(ctx, "test-key")
	if err != ErrKeyNotFound {
		t.Errorf("Expected ErrKeyNotFound after expiration, got: %v", err)
	}
}

func TestCachedStore_PutWithTTL(t *testing.T) {
	// Create temporary directory for WAL
	dir := t.TempDir()

	durableConfig := DurableStoreConfig{
		DataDir: dir,
	}
	durableStore, err := NewDurableStore(durableConfig)
	if err != nil {
		t.Fatalf("Failed to create durable store: %v", err)
	}
	defer durableStore.Close()

	cacheConfig := CacheConfig{
		MaxSize: 100,
		TTL:     0, // No cache-level TTL
	}
	cachedStore := NewCachedStore(durableStore, cacheConfig)

	ctx := context.Background()

	// Test putting a key with TTL
	err = cachedStore.PutWithTTL(ctx, "test-key", []byte("test-value"), 1*time.Second)
	if err != nil {
		t.Fatalf("PutWithTTL failed: %v", err)
	}

	// Verify the key exists immediately (should be served from cache)
	value, err := cachedStore.Get(ctx, "test-key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(value) != "test-value" {
		t.Errorf("Expected value 'test-value', got '%s'", string(value))
	}

	// Check cache stats
	stats := cachedStore.GetCacheStats()
	if stats.Hits == 0 {
		t.Error("Expected cache hit, got 0 hits")
	}

	// Wait for expiration
	time.Sleep(1200 * time.Millisecond)

	// Clear cache to ensure we read from store
	cachedStore.ClearCache()

	// Verify the key has expired
	_, err = cachedStore.Get(ctx, "test-key")
	if err != ErrKeyNotFound {
		t.Errorf("Expected ErrKeyNotFound after expiration, got: %v", err)
	}
}

func TestMemoryStore_TTL_InvalidKey(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Test with empty key
	err := store.PutWithTTL(ctx, "", []byte("value"), 1*time.Second)
	if err != ErrInvalidKey {
		t.Errorf("Expected ErrInvalidKey for empty key, got: %v", err)
	}

	_, err = store.GetTTL(ctx, "")
	if err != ErrInvalidKey {
		t.Errorf("Expected ErrInvalidKey for empty key, got: %v", err)
	}

	err = store.SetTTL(ctx, "", 1*time.Second)
	if err != ErrInvalidKey {
		t.Errorf("Expected ErrInvalidKey for empty key, got: %v", err)
	}
}
