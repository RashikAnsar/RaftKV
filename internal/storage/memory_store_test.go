package storage

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMemoryStore_BasicOperations(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx := context.Background()

	t.Run("Put and Get", func(t *testing.T) {
		err := store.Put(ctx, "key1", []byte("value1"))
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		value, err := store.Get(ctx, "key1")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		if string(value) != "value1" {
			t.Errorf("Expected 'value1', got '%s'", string(value))
		}
	})

	t.Run("Get non-existent key", func(t *testing.T) {
		_, err := store.Get(ctx, "nonexistent")
		if err != ErrKeyNotFound {
			t.Errorf("Expected ErrKeyNotFound, got %v", err)
		}
	})

	t.Run("Update existing key", func(t *testing.T) {
		store.Put(ctx, "key1", []byte("value1"))
		store.Put(ctx, "key1", []byte("value2"))

		value, _ := store.Get(ctx, "key1")
		if string(value) != "value2" {
			t.Errorf("Expected 'value2', got '%s'", string(value))
		}
	})

	t.Run("Delete", func(t *testing.T) {
		store.Put(ctx, "key2", []byte("value2"))

		err := store.Delete(ctx, "key2")
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		_, err = store.Get(ctx, "key2")
		if err != ErrKeyNotFound {
			t.Errorf("Expected ErrKeyNotFound after delete, got %v", err)
		}
	})

	t.Run("Delete idempotent", func(t *testing.T) {
		err := store.Delete(ctx, "nonexistent")
		if err != nil {
			t.Errorf("Delete should be idempotent, got error: %v", err)
		}
	})
}

func TestMemoryStore_List(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx := context.Background()

	store.Put(ctx, "user:1", []byte("alice"))
	store.Put(ctx, "user:2", []byte("bob"))
	store.Put(ctx, "user:3", []byte("charlie"))
	store.Put(ctx, "post:1", []byte("hello"))
	store.Put(ctx, "post:2", []byte("world"))

	t.Run("List with prefix", func(t *testing.T) {
		keys, err := store.List(ctx, "user:", 0)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(keys) != 3 {
			t.Errorf("Expected 3 keys, got %d", len(keys))
		}
	})

	t.Run("List all keys", func(t *testing.T) {
		keys, err := store.List(ctx, "", 0)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(keys) != 5 {
			t.Errorf("Expected 5 keys, got %d", len(keys))
		}
	})

	t.Run("List with limit", func(t *testing.T) {
		keys, err := store.List(ctx, "user:", 2)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(keys) != 2 {
			t.Errorf("Expected 2 keys (limit), got %d", len(keys))
		}
	})
}

func TestMemoryStore_Concurrency(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx := context.Background()

	const (
		numGoroutines = 100
		numOperations = 100
	)

	t.Run("Concurrent writes", func(t *testing.T) {
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				defer wg.Done()
				for j := 0; j < numOperations; j++ {
					key := fmt.Sprintf("key-%d-%d", id, j)
					value := fmt.Sprintf("value-%d-%d", id, j)

					if err := store.Put(ctx, key, []byte(value)); err != nil {
						t.Errorf("Put failed: %v", err)
						return
					}
				}
			}(i)
		}

		wg.Wait()

		stats := store.Stats()
		expectedOps := int64(numGoroutines * numOperations)
		if stats.Puts != expectedOps {
			t.Errorf("Expected %d puts, got %d", expectedOps, stats.Puts)
		}
		if stats.KeyCount != expectedOps {
			t.Errorf("Expected %d keys, got %d", expectedOps, stats.KeyCount)
		}
	})

	t.Run("Concurrent reads and writes", func(t *testing.T) {
		store.Reset()

		for i := 0; i < 100; i++ {
			store.Put(ctx, fmt.Sprintf("key-%d", i), []byte("value"))
		}

		var wg sync.WaitGroup
		wg.Add(200) // 100 readers + 100 writers

		// Writers
		for i := 0; i < 100; i++ {
			go func(id int) {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					key := fmt.Sprintf("key-%d", id)
					store.Put(ctx, key, []byte(fmt.Sprintf("value-%d", j)))
				}
			}(i)
		}

		// Readers
		for i := 0; i < 100; i++ {
			go func(id int) {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					key := fmt.Sprintf("key-%d", id)
					store.Get(ctx, key)
				}
			}(i)
		}

		wg.Wait()

		// No assertions needed - we're testing for race conditions
	})
}

func TestMemoryStore_ContextCancellation(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	t.Run("Get respects context", func(t *testing.T) {
		_, err := store.Get(ctx, "key")
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled, got %v", err)
		}
	})

	t.Run("Put respects context", func(t *testing.T) {
		err := store.Put(ctx, "key", []byte("value"))
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled, got %v", err)
		}
	})

	t.Run("Delete respects context", func(t *testing.T) {
		err := store.Delete(ctx, "key")
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled, got %v", err)
		}
	})

	t.Run("List respects context", func(t *testing.T) {
		_, err := store.List(ctx, "", 0)
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled, got %v", err)
		}
	})
}

func TestMemoryStore_ContextTimeout(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(1 * time.Millisecond) // Ensure timeout

	_, err := store.Get(ctx, "key")
	if err != context.DeadlineExceeded {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}
}

func TestMemoryStore_ClosedStore(t *testing.T) {
	store := NewMemoryStore()
	store.Close()

	ctx := context.Background()

	t.Run("Operations on closed store fail", func(t *testing.T) {
		err := store.Put(ctx, "key", []byte("value"))
		if err != ErrStoreClosed {
			t.Errorf("Expected ErrStoreClosed, got %v", err)
		}

		_, err = store.Get(ctx, "key")
		if err != ErrStoreClosed {
			t.Errorf("Expected ErrStoreClosed, got %v", err)
		}

		err = store.Delete(ctx, "key")
		if err != ErrStoreClosed {
			t.Errorf("Expected ErrStoreClosed, got %v", err)
		}

		_, err = store.List(ctx, "", 0)
		if err != ErrStoreClosed {
			t.Errorf("Expected ErrStoreClosed, got %v", err)
		}
	})

	t.Run("Double close returns error", func(t *testing.T) {
		err := store.Close()
		if err != ErrStoreClosed {
			t.Errorf("Expected ErrStoreClosed on double close, got %v", err)
		}
	})
}

func TestMemoryStore_ValueIsolation(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx := context.Background()

	t.Run("External mutation doesn't affect stored value", func(t *testing.T) {
		original := []byte("original")
		store.Put(ctx, "key", original)

		// Modify the original slice
		original[0] = 'X'

		// Retrieved value should be unchanged
		retrieved, _ := store.Get(ctx, "key")
		if string(retrieved) != "original" {
			t.Errorf("Value was mutated externally: got '%s'", string(retrieved))
		}
	})

	t.Run("Mutation of retrieved value doesn't affect store", func(t *testing.T) {
		store.Put(ctx, "key2", []byte("original"))

		retrieved, _ := store.Get(ctx, "key2")
		retrieved[0] = 'Y'

		// Store's value should be unchanged
		retrieved2, _ := store.Get(ctx, "key2")
		if string(retrieved2) != "original" {
			t.Errorf("Stored value was mutated: got '%s'", string(retrieved2))
		}
	})
}

func TestMemoryStore_InvalidKey(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx := context.Background()

	t.Run("Empty key", func(t *testing.T) {
		err := store.Put(ctx, "", []byte("value"))
		if err != ErrInvalidKey {
			t.Errorf("Expected ErrInvalidKey, got %v", err)
		}

		_, err = store.Get(ctx, "")
		if err != ErrInvalidKey {
			t.Errorf("Expected ErrInvalidKey, got %v", err)
		}

		err = store.Delete(ctx, "")
		if err != ErrInvalidKey {
			t.Errorf("Expected ErrInvalidKey, got %v", err)
		}
	})
}

func TestMemoryStore_Stats(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx := context.Background()

	// Perform operations
	store.Put(ctx, "key1", []byte("value1"))
	store.Put(ctx, "key2", []byte("value2"))
	store.Get(ctx, "key1")
	store.Get(ctx, "nonexistent") // Should still count
	store.Delete(ctx, "key1")

	stats := store.Stats()

	if stats.Puts != 2 {
		t.Errorf("Expected 2 puts, got %d", stats.Puts)
	}
	if stats.Gets != 2 {
		t.Errorf("Expected 2 gets, got %d", stats.Gets)
	}
	if stats.Deletes != 1 {
		t.Errorf("Expected 1 delete, got %d", stats.Deletes)
	}
	if stats.KeyCount != 1 {
		t.Errorf("Expected 1 key remaining, got %d", stats.KeyCount)
	}
}

// Benchmarks

func BenchmarkMemoryStore_Put(b *testing.B) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		store.Put(ctx, key, []byte("value"))
	}
}

func BenchmarkMemoryStore_Get(b *testing.B) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	// Prepare data
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("key-%d", i)
		store.Put(ctx, key, []byte("value"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i%10000)
		store.Get(ctx, key)
	}
}

func BenchmarkMemoryStore_Delete(b *testing.B) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	// Prepare data
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		store.Put(ctx, key, []byte("value"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		store.Delete(ctx, key)
	}
}

func BenchmarkMemoryStore_ConcurrentPut(b *testing.B) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i)
			store.Put(ctx, key, []byte("value"))
			i++
		}
	})
}

func BenchmarkMemoryStore_ConcurrentGet(b *testing.B) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	// Prepare data
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("key-%d", i)
		store.Put(ctx, key, []byte("value"))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i%10000)
			store.Get(ctx, key)
			i++
		}
	})
}

func BenchmarkMemoryStore_MixedWorkload(b *testing.B) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	// Prepare data
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key-%d", i)
		store.Put(ctx, key, []byte("value"))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i%1000)
			op := i % 10

			if op < 7 { // 70% reads
				store.Get(ctx, key)
			} else if op < 9 { // 20% writes
				store.Put(ctx, key, []byte("newvalue"))
			} else { // 10% deletes
				store.Delete(ctx, key)
			}
			i++
		}
	})
}
