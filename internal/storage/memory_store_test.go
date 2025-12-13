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

// CAS (Compare-And-Swap) Tests

func TestMemoryStore_GetWithVersion(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	t.Run("Get version for new key", func(t *testing.T) {
		err := store.Put(ctx, "key1", []byte("value1"))
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		value, version, err := store.GetWithVersion(ctx, "key1")
		if err != nil {
			t.Fatalf("GetWithVersion failed: %v", err)
		}

		if string(value) != "value1" {
			t.Errorf("Expected 'value1', got '%s'", string(value))
		}

		if version != 1 {
			t.Errorf("Expected version 1 for new key, got %d", version)
		}
	})

	t.Run("Version increments on update", func(t *testing.T) {
		store.Put(ctx, "key2", []byte("v1"))
		_, v1, _ := store.GetWithVersion(ctx, "key2")

		store.Put(ctx, "key2", []byte("v2"))
		_, v2, _ := store.GetWithVersion(ctx, "key2")

		store.Put(ctx, "key2", []byte("v3"))
		_, v3, _ := store.GetWithVersion(ctx, "key2")

		if v1 != 1 || v2 != 2 || v3 != 3 {
			t.Errorf("Expected versions [1,2,3], got [%d,%d,%d]", v1, v2, v3)
		}
	})

	t.Run("Get version for non-existent key", func(t *testing.T) {
		_, _, err := store.GetWithVersion(ctx, "nonexistent")
		if err != ErrKeyNotFound {
			t.Errorf("Expected ErrKeyNotFound, got %v", err)
		}
	})

	t.Run("Get version with empty key", func(t *testing.T) {
		_, _, err := store.GetWithVersion(ctx, "")
		if err != ErrInvalidKey {
			t.Errorf("Expected ErrInvalidKey, got %v", err)
		}
	})
}

func TestMemoryStore_CompareAndSwap_Success(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	// Setup: Create a key with version 1
	store.Put(ctx, "counter", []byte("0"))

	t.Run("CAS succeeds with correct version", func(t *testing.T) {
		newVersion, success, err := store.CompareAndSwap(ctx, "counter", 1, []byte("1"))
		if err != nil {
			t.Fatalf("CAS failed: %v", err)
		}

		if !success {
			t.Error("CAS should have succeeded")
		}

		if newVersion != 2 {
			t.Errorf("Expected new version 2, got %d", newVersion)
		}

		// Verify the value was updated
		value, _ := store.Get(ctx, "counter")
		if string(value) != "1" {
			t.Errorf("Expected value '1', got '%s'", string(value))
		}
	})

	t.Run("CAS succeeds multiple times", func(t *testing.T) {
		store.Put(ctx, "seq", []byte("0"))

		for i := 1; i <= 5; i++ {
			newVersion, success, err := store.CompareAndSwap(ctx, "seq", uint64(i), []byte(fmt.Sprintf("%d", i)))
			if err != nil {
				t.Fatalf("CAS iteration %d failed: %v", i, err)
			}
			if !success {
				t.Errorf("CAS iteration %d should have succeeded", i)
			}
			if newVersion != uint64(i+1) {
				t.Errorf("Expected version %d, got %d", i+1, newVersion)
			}
		}

		// Final value should be "5" with version 6
		value, version, _ := store.GetWithVersion(ctx, "seq")
		if string(value) != "5" || version != 6 {
			t.Errorf("Expected value='5' version=6, got value='%s' version=%d", string(value), version)
		}
	})
}

func TestMemoryStore_CompareAndSwap_VersionMismatch(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	// Setup: Create a key with version 1
	store.Put(ctx, "counter", []byte("10"))

	t.Run("CAS fails with wrong version", func(t *testing.T) {
		currentVersion, success, err := store.CompareAndSwap(ctx, "counter", 99, []byte("20"))
		if err != nil {
			t.Fatalf("CAS failed: %v", err)
		}

		if success {
			t.Error("CAS should have failed due to version mismatch")
		}

		if currentVersion != 1 {
			t.Errorf("Expected current version 1, got %d", currentVersion)
		}

		// Verify value was NOT updated
		value, _ := store.Get(ctx, "counter")
		if string(value) != "10" {
			t.Errorf("Value should not have changed, got '%s'", string(value))
		}
	})

	t.Run("CAS fails after concurrent update", func(t *testing.T) {
		store.Put(ctx, "race", []byte("v1"))
		_, v1, _ := store.GetWithVersion(ctx, "race")

		// Simulate concurrent update
		store.Put(ctx, "race", []byte("v2"))

		// Now CAS with stale version should fail
		_, success, err := store.CompareAndSwap(ctx, "race", v1, []byte("v3"))
		if err != nil {
			t.Fatalf("CAS failed: %v", err)
		}

		if success {
			t.Error("CAS should have failed - version was stale")
		}

		// Value should still be "v2"
		value, _ := store.Get(ctx, "race")
		if string(value) != "v2" {
			t.Errorf("Expected 'v2', got '%s'", string(value))
		}
	})
}

func TestMemoryStore_CompareAndSwap_KeyNotFound(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	t.Run("CAS fails for non-existent key", func(t *testing.T) {
		version, success, err := store.CompareAndSwap(ctx, "nonexistent", 1, []byte("value"))
		if err != nil {
			t.Fatalf("CAS failed: %v", err)
		}

		if success {
			t.Error("CAS should fail for non-existent key")
		}

		if version != 0 {
			t.Errorf("Expected version 0 for non-existent key, got %d", version)
		}
	})
}

func TestMemoryStore_CompareAndSwap_InvalidKey(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	t.Run("CAS with empty key", func(t *testing.T) {
		_, _, err := store.CompareAndSwap(ctx, "", 1, []byte("value"))
		if err != ErrInvalidKey {
			t.Errorf("Expected ErrInvalidKey, got %v", err)
		}
	})
}

func TestMemoryStore_SetIfNotExists_NewKey(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	t.Run("SetIfNotExists succeeds for new key", func(t *testing.T) {
		version, created, err := store.SetIfNotExists(ctx, "newkey", []byte("newvalue"))
		if err != nil {
			t.Fatalf("SetIfNotExists failed: %v", err)
		}

		if !created {
			t.Error("Key should have been created")
		}

		if version != 1 {
			t.Errorf("Expected version 1, got %d", version)
		}

		// Verify value was created
		value, _ := store.Get(ctx, "newkey")
		if string(value) != "newvalue" {
			t.Errorf("Expected 'newvalue', got '%s'", string(value))
		}
	})
}

func TestMemoryStore_SetIfNotExists_ExistingKey(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	// Setup: Create an existing key
	store.Put(ctx, "existing", []byte("original"))

	t.Run("SetIfNotExists fails for existing key", func(t *testing.T) {
		version, created, err := store.SetIfNotExists(ctx, "existing", []byte("newvalue"))
		if err != nil {
			t.Fatalf("SetIfNotExists failed: %v", err)
		}

		if created {
			t.Error("Key should not have been created - already exists")
		}

		if version != 1 {
			t.Errorf("Expected existing version 1 on failure, got %d", version)
		}

		// Verify original value unchanged
		value, _ := store.Get(ctx, "existing")
		if string(value) != "original" {
			t.Errorf("Value should not have changed, got '%s'", string(value))
		}
	})
}

func TestMemoryStore_SetIfNotExists_InvalidKey(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	t.Run("SetIfNotExists with empty key", func(t *testing.T) {
		_, _, err := store.SetIfNotExists(ctx, "", []byte("value"))
		if err != ErrInvalidKey {
			t.Errorf("Expected ErrInvalidKey, got %v", err)
		}
	})
}

func TestMemoryStore_VersionIncrement(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	t.Run("Version increments correctly", func(t *testing.T) {
		// Create key
		store.Put(ctx, "test", []byte("v1"))
		_, v1, _ := store.GetWithVersion(ctx, "test")

		// Update via Put
		store.Put(ctx, "test", []byte("v2"))
		_, v2, _ := store.GetWithVersion(ctx, "test")

		// Update via CAS
		store.CompareAndSwap(ctx, "test", v2, []byte("v3"))
		_, v3, _ := store.GetWithVersion(ctx, "test")

		// Another Put
		store.Put(ctx, "test", []byte("v4"))
		_, v4, _ := store.GetWithVersion(ctx, "test")

		expected := []uint64{1, 2, 3, 4}
		actual := []uint64{v1, v2, v3, v4}

		for i := range expected {
			if expected[i] != actual[i] {
				t.Errorf("Version mismatch at step %d: expected %d, got %d", i, expected[i], actual[i])
			}
		}
	})

	t.Run("Delete doesn't affect version counter", func(t *testing.T) {
		store.Put(ctx, "del", []byte("v1"))
		_, v1, _ := store.GetWithVersion(ctx, "del")

		store.Delete(ctx, "del")

		// Recreate - should start at version 1 again
		store.Put(ctx, "del", []byte("v2"))
		_, v2, _ := store.GetWithVersion(ctx, "del")

		if v1 != 1 || v2 != 1 {
			t.Errorf("Expected both versions to be 1, got %d and %d", v1, v2)
		}
	})
}

func TestMemoryStore_CAS_ConcurrentUpdates(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	t.Run("Concurrent CAS operations", func(t *testing.T) {
		// Create a counter
		store.Put(ctx, "counter", []byte("0"))

		const numGoroutines = 10
		const incrementsPerGoroutine = 100

		var wg sync.WaitGroup
		successCount := make([]int, numGoroutines)

		// Spawn multiple goroutines trying to increment the counter
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < incrementsPerGoroutine; j++ {
					for {
						// Read current value and version
						value, version, err := store.GetWithVersion(ctx, "counter")
						if err != nil {
							continue
						}

						// Parse current value
						var current int
						fmt.Sscanf(string(value), "%d", &current)

						// Try to CAS with incremented value
						newValue := []byte(fmt.Sprintf("%d", current+1))
						_, success, err := store.CompareAndSwap(ctx, "counter", version, newValue)
						if err != nil {
							continue
						}

						if success {
							successCount[id]++
							break // Success, move to next increment
						}
						// CAS failed, retry
					}
				}
			}(i)
		}

		wg.Wait()

		// Verify final counter value
		finalValue, finalVersion, _ := store.GetWithVersion(ctx, "counter")
		var finalCount int
		fmt.Sscanf(string(finalValue), "%d", &finalCount)

		expectedCount := numGoroutines * incrementsPerGoroutine
		if finalCount != expectedCount {
			t.Errorf("Expected final count %d, got %d", expectedCount, finalCount)
		}

		// Version should be count + 1 (initial Put)
		if finalVersion != uint64(expectedCount+1) {
			t.Errorf("Expected final version %d, got %d", expectedCount+1, finalVersion)
		}

		// All goroutines should have succeeded exactly incrementsPerGoroutine times
		for i, count := range successCount {
			if count != incrementsPerGoroutine {
				t.Errorf("Goroutine %d succeeded %d times, expected %d", i, count, incrementsPerGoroutine)
			}
		}
	})
}

func TestMemoryStore_CAS_ClosedStore(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	store.Put(ctx, "key", []byte("value"))
	store.Close()

	t.Run("CAS operations fail on closed store", func(t *testing.T) {
		_, _, err := store.GetWithVersion(ctx, "key")
		if err != ErrStoreClosed {
			t.Errorf("Expected ErrStoreClosed, got %v", err)
		}

		_, _, err = store.CompareAndSwap(ctx, "key", 1, []byte("new"))
		if err != ErrStoreClosed {
			t.Errorf("Expected ErrStoreClosed, got %v", err)
		}

		_, _, err = store.SetIfNotExists(ctx, "newkey", []byte("value"))
		if err != ErrStoreClosed {
			t.Errorf("Expected ErrStoreClosed, got %v", err)
		}
	})
}

func TestMemoryStore_CAS_ContextCancellation(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("CAS operations respect context", func(t *testing.T) {
		_, _, err := store.GetWithVersion(ctx, "key")
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled, got %v", err)
		}

		_, _, err = store.CompareAndSwap(ctx, "key", 1, []byte("value"))
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled, got %v", err)
		}

		_, _, err = store.SetIfNotExists(ctx, "key", []byte("value"))
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled, got %v", err)
		}
	})
}

func TestMemoryStore_CAS_Stats(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	// Perform CAS operations
	store.Put(ctx, "key1", []byte("v1"))
	store.CompareAndSwap(ctx, "key1", 1, []byte("v2"))     // Success
	store.CompareAndSwap(ctx, "key1", 1, []byte("v3"))     // Fail - wrong version
	store.SetIfNotExists(ctx, "key2", []byte("v1"))        // Success
	store.SetIfNotExists(ctx, "key2", []byte("v2"))        // Fail - exists
	store.CompareAndSwap(ctx, "nonexistent", 1, []byte("x")) // Fail - not found

	stats := store.Stats()

	// 1 successful CAS + 2 failed CAS + 1 successful SetIfNotExists + 1 failed SetIfNotExists + 1 failed CAS = 6 total
	// Note: SetIfNotExists also increments statsPuts on success
	if stats.Puts != 2 { // Initial Put + SetIfNotExists success
		t.Errorf("Expected 2 puts, got %d", stats.Puts)
	}
}

// Benchmarks for CAS operations

func BenchmarkMemoryStore_GetWithVersion(b *testing.B) {
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
		store.GetWithVersion(ctx, key)
	}
}

func BenchmarkMemoryStore_CompareAndSwap_Success(b *testing.B) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	store.Put(ctx, "counter", []byte("0"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, version, _ := store.GetWithVersion(ctx, "counter")
		store.CompareAndSwap(ctx, "counter", version, []byte(fmt.Sprintf("%d", i)))
	}
}

func BenchmarkMemoryStore_CompareAndSwap_Contention(b *testing.B) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	store.Put(ctx, "counter", []byte("0"))

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for {
				_, version, err := store.GetWithVersion(ctx, "counter")
				if err != nil {
					continue
				}
				_, success, _ := store.CompareAndSwap(ctx, "counter", version, []byte("x"))
				if success {
					break
				}
				// Retry on failure
			}
		}
	})
}

func BenchmarkMemoryStore_SetIfNotExists(b *testing.B) {
	store := NewMemoryStore()
	defer store.Close()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		store.SetIfNotExists(ctx, key, []byte("value"))
	}
}
