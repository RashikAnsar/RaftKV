package storage

import (
	"context"
	"fmt"
	"testing"
)

func TestDurableStore_BasicOperations(t *testing.T) {
	dir := t.TempDir()

	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:     dir,
		SyncOnWrite: true,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
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

	t.Run("Delete", func(t *testing.T) {
		store.Put(ctx, "key2", []byte("value2"))

		err := store.Delete(ctx, "key2")
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		_, err = store.Get(ctx, "key2")
		if err != ErrKeyNotFound {
			t.Errorf("Expected ErrKeyNotFound, got %v", err)
		}
	})
}

func TestDurableStore_CrashRecovery(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Phase 1: Write data
	func() {
		store, err := NewDurableStore(DurableStoreConfig{
			DataDir:     dir,
			SyncOnWrite: true,
		})
		if err != nil {
			t.Fatalf("Failed to create store: %v", err)
		}

		// Write some data
		for i := 0; i < 100; i++ {
			key := fmt.Sprintf("key-%d", i)
			value := fmt.Sprintf("value-%d", i)

			if err := store.Put(ctx, key, []byte(value)); err != nil {
				t.Fatalf("Put failed: %v", err)
			}
		}

		// Delete some keys
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("key-%d", i)
			if err := store.Delete(ctx, key); err != nil {
				t.Fatalf("Delete failed: %v", err)
			}
		}

		store.Close()
	}()

	// Phase 2: "Crash" and recover
	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:     dir,
		SyncOnWrite: true,
	})
	if err != nil {
		t.Fatalf("Failed to recover store: %v", err)
	}
	defer store.Close()

	// Verify data was recovered
	t.Run("Verify surviving keys", func(t *testing.T) {
		for i := 10; i < 100; i++ {
			key := fmt.Sprintf("key-%d", i)
			expectedValue := fmt.Sprintf("value-%d", i)

			value, err := store.Get(ctx, key)
			if err != nil {
				t.Errorf("Key %s not found after recovery", key)
				continue
			}

			if string(value) != expectedValue {
				t.Errorf("Key %s: expected '%s', got '%s'", key, expectedValue, string(value))
			}
		}
	})

	t.Run("Verify deleted keys", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("key-%d", i)

			_, err := store.Get(ctx, key)
			if err != ErrKeyNotFound {
				t.Errorf("Key %s should be deleted, got error: %v", key, err)
			}
		}
	})

	// Verify stats
	stats := store.Stats()
	if stats.KeyCount != 90 {
		t.Errorf("Expected 90 keys after recovery, got %d", stats.KeyCount)
	}
}

func TestDurableStore_UpdateExistingKey(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Phase 1: Write initial value
	func() {
		store, err := NewDurableStore(DurableStoreConfig{
			DataDir:     dir,
			SyncOnWrite: true,
		})
		if err != nil {
			t.Fatalf("Failed to create store: %v", err)
		}

		store.Put(ctx, "key", []byte("value1"))
		store.Put(ctx, "key", []byte("value2"))
		store.Put(ctx, "key", []byte("value3"))

		store.Close()
	}()

	// Phase 2: Recover and verify
	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:     dir,
		SyncOnWrite: true,
	})
	if err != nil {
		t.Fatalf("Failed to recover store: %v", err)
	}
	defer store.Close()

	value, err := store.Get(ctx, "key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	// Should have the last value
	if string(value) != "value3" {
		t.Errorf("Expected 'value3', got '%s'", string(value))
	}

	// Should have only 1 key (overwritten, not duplicated)
	stats := store.Stats()
	if stats.KeyCount != 1 {
		t.Errorf("Expected 1 key, got %d", stats.KeyCount)
	}
}

func TestDurableStore_DeleteAndRecreate(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Phase 1: Write, delete, write again
	func() {
		store, err := NewDurableStore(DurableStoreConfig{
			DataDir:     dir,
			SyncOnWrite: true,
		})
		if err != nil {
			t.Fatalf("Failed to create store: %v", err)
		}

		store.Put(ctx, "key", []byte("value1"))
		store.Delete(ctx, "key")
		store.Put(ctx, "key", []byte("value2"))

		store.Close()
	}()

	// Phase 2: Recover and verify
	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:     dir,
		SyncOnWrite: true,
	})
	if err != nil {
		t.Fatalf("Failed to recover store: %v", err)
	}
	defer store.Close()

	value, err := store.Get(ctx, "key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if string(value) != "value2" {
		t.Errorf("Expected 'value2', got '%s'", string(value))
	}
}

func TestDurableStore_EmptyRecovery(t *testing.T) {
	dir := t.TempDir()

	// Create store with no data
	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:     dir,
		SyncOnWrite: true,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	stats := store.Stats()
	if stats.KeyCount != 0 {
		t.Errorf("Expected 0 keys in new store, got %d", stats.KeyCount)
	}

	store.Close()

	// Reopen - should work fine
	store2, err := NewDurableStore(DurableStoreConfig{
		DataDir:     dir,
		SyncOnWrite: true,
	})
	if err != nil {
		t.Fatalf("Failed to reopen store: %v", err)
	}
	defer store2.Close()

	stats2 := store2.Stats()
	if stats2.KeyCount != 0 {
		t.Errorf("Expected 0 keys after reopen, got %d", stats2.KeyCount)
	}
}

func TestDurableStore_LargeDataset(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large dataset test in short mode")
	}

	dir := t.TempDir()
	ctx := context.Background()

	const numKeys = 10000

	// Phase 1: Write large dataset
	func() {
		store, err := NewDurableStore(DurableStoreConfig{
			DataDir:     dir,
			SyncOnWrite: false, // Faster for bulk writes
		})
		if err != nil {
			t.Fatalf("Failed to create store: %v", err)
		}

		for i := 0; i < numKeys; i++ {
			key := fmt.Sprintf("key-%06d", i)
			value := []byte(fmt.Sprintf("value-%06d", i))

			if err := store.Put(ctx, key, value); err != nil {
				t.Fatalf("Put failed at %d: %v", i, err)
			}
		}

		// Force sync before close
		store.Sync()
		store.Close()
	}()

	// Phase 2: Recover and verify
	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:     dir,
		SyncOnWrite: true,
	})
	if err != nil {
		t.Fatalf("Failed to recover store: %v", err)
	}
	defer store.Close()

	stats := store.Stats()
	if stats.KeyCount != numKeys {
		t.Errorf("Expected %d keys after recovery, got %d", numKeys, stats.KeyCount)
	}

	// Spot check some keys
	for i := 0; i < 100; i++ {
		idx := i * (numKeys / 100) // Sample across the dataset
		if idx >= numKeys {
			break
		}

		key := fmt.Sprintf("key-%06d", idx)
		expectedValue := fmt.Sprintf("value-%06d", idx)

		value, err := store.Get(ctx, key)
		if err != nil {
			t.Errorf("Key %s not found", key)
			continue
		}

		if string(value) != expectedValue {
			t.Errorf("Key %s: expected '%s', got '%s'", key, expectedValue, string(value))
		}
	}
}

// Benchmarks

func BenchmarkDurableStore_Put(b *testing.B) {
	dir := b.TempDir()

	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:     dir,
		SyncOnWrite: false,
	})
	if err != nil {
		b.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		store.Put(ctx, key, []byte("value"))
	}
}

func BenchmarkDurableStore_PutSync(b *testing.B) {
	dir := b.TempDir()

	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:     dir,
		SyncOnWrite: true,
	})
	if err != nil {
		b.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		store.Put(ctx, key, []byte("value"))
	}
}

func BenchmarkDurableStore_Get(b *testing.B) {
	dir := b.TempDir()

	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:     dir,
		SyncOnWrite: false,
	})
	if err != nil {
		b.Fatalf("Failed to create store: %v", err)
	}
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

func BenchmarkDurableStore_Recovery(b *testing.B) {
	dir := b.TempDir()

	// Prepare a store with data
	func() {
		store, _ := NewDurableStore(DurableStoreConfig{
			DataDir:     dir,
			SyncOnWrite: false,
		})

		ctx := context.Background()
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("key-%d", i)
			store.Put(ctx, key, []byte("value"))
		}

		store.Sync()
		store.Close()
	}()

	// Benchmark recovery
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store, err := NewDurableStore(DurableStoreConfig{
			DataDir:     dir,
			SyncOnWrite: false,
		})
		if err != nil {
			b.Fatalf("Recovery failed: %v", err)
		}
		store.Close()
	}
}
