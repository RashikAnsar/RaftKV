package storage

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestDurableStore_AutomaticSnapshot(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:       dir,
		SyncOnWrite:   false,
		SnapshotEvery: 100,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	for i := 0; i < 250; i++ {
		key := fmt.Sprintf("key-%d", i)
		value := fmt.Sprintf("value-%d", i)

		if err := store.Put(ctx, key, []byte(value)); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	time.Sleep(500 * time.Millisecond)

	stats, err := store.snapshotManager.GetStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	if stats.Count < 1 {
		t.Errorf("Expected at least 1 snapshot, got %d", stats.Count)
	}

	t.Logf("Created %d snapshots for 250 operations", stats.Count)
}

func TestDurableStore_SnapshotRecovery(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Phase 1: Write data and take snapshot
	func() {
		store, err := NewDurableStore(DurableStoreConfig{
			DataDir:       dir,
			SyncOnWrite:   true,
			SnapshotEvery: 50,
		})
		if err != nil {
			t.Fatalf("Failed to create store: %v", err)
		}

		// Write 100 keys
		for i := 0; i < 100; i++ {
			key := fmt.Sprintf("key-%03d", i)
			value := fmt.Sprintf("value-%03d", i)
			store.Put(ctx, key, []byte(value))
		}

		// Force snapshot
		_, err = store.Snapshot(ctx)
		if err != nil {
			t.Fatalf("Failed to create snapshot: %v", err)
		}

		// Write more data after snapshot
		for i := 100; i < 120; i++ {
			key := fmt.Sprintf("key-%03d", i)
			value := fmt.Sprintf("value-%03d", i)
			store.Put(ctx, key, []byte(value))
		}

		store.Close()
	}()

	// Phase 2: Recover from snapshot + WAL
	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:       dir,
		SyncOnWrite:   true,
		SnapshotEvery: 50,
	})
	if err != nil {
		t.Fatalf("Failed to recover store: %v", err)
	}
	defer store.Close()

	// Verify all 120 keys exist
	stats := store.Stats()
	if stats.KeyCount != 120 {
		t.Errorf("Expected 120 keys after recovery, got %d", stats.KeyCount)
	}

	// Spot check some keys
	testKeys := []int{0, 50, 99, 100, 119}
	for _, i := range testKeys {
		key := fmt.Sprintf("key-%03d", i)
		expectedValue := fmt.Sprintf("value-%03d", i)

		value, err := store.Get(ctx, key)
		if err != nil {
			t.Errorf("Key %s not found after recovery", key)
			continue
		}

		if string(value) != expectedValue {
			t.Errorf("Key %s: expected %s, got %s", key, expectedValue, string(value))
		}
	}

	t.Log("Recovery from snapshot + WAL successful")
}

func TestDurableStore_SnapshotWithDeletes(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Phase 1: Write, delete, snapshot
	func() {
		store, err := NewDurableStore(DurableStoreConfig{
			DataDir:       dir,
			SyncOnWrite:   true,
			SnapshotEvery: 1000, // Manual control
		})
		if err != nil {
			t.Fatalf("Failed to create store: %v", err)
		}

		// Write 100 keys
		for i := 0; i < 100; i++ {
			key := fmt.Sprintf("key-%d", i)
			store.Put(ctx, key, []byte("value"))
		}

		// Delete every other key
		for i := 0; i < 100; i += 2 {
			key := fmt.Sprintf("key-%d", i)
			store.Delete(ctx, key)
		}

		// Take snapshot (should have 50 keys)
		_, err = store.Snapshot(ctx)
		if err != nil {
			t.Fatalf("Failed to create snapshot: %v", err)
		}

		store.Close()
	}()

	// Phase 2: Recover and verify
	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:       dir,
		SyncOnWrite:   true,
		SnapshotEvery: 1000,
	})
	if err != nil {
		t.Fatalf("Failed to recover: %v", err)
	}
	defer store.Close()

	stats := store.Stats()
	if stats.KeyCount != 50 {
		t.Errorf("Expected 50 keys after recovery, got %d", stats.KeyCount)
	}

	// Verify deleted keys are gone
	for i := 0; i < 100; i += 2 {
		key := fmt.Sprintf("key-%d", i)
		_, err := store.Get(ctx, key)
		if err != ErrKeyNotFound {
			t.Errorf("Key %s should be deleted", key)
		}
	}

	// Verify remaining keys exist
	for i := 1; i < 100; i += 2 {
		key := fmt.Sprintf("key-%d", i)
		_, err := store.Get(ctx, key)
		if err != nil {
			t.Errorf("Key %s should exist", key)
		}
	}
}

func TestDurableStore_SnapshotPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	dir := t.TempDir()
	ctx := context.Background()

	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:       dir,
		SyncOnWrite:   false,
		SnapshotEvery: 0, // Manual snapshots only
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Write 10,000 keys
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("key-%06d", i)
		value := []byte("value-with-some-content-to-make-it-realistic")
		store.Put(ctx, key, value)
	}

	// Benchmark snapshot creation
	start := time.Now()
	_, err = store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}
	snapshotTime := time.Since(start)

	t.Logf("Snapshot of 10K keys took: %v", snapshotTime)

	// Close and reopen to test recovery
	store.Close()

	start = time.Now()
	store2, err := NewDurableStore(DurableStoreConfig{
		DataDir:       dir,
		SyncOnWrite:   false,
		SnapshotEvery: 0,
	})
	if err != nil {
		t.Fatalf("Failed to recover: %v", err)
	}
	defer store2.Close()
	recoveryTime := time.Since(start)

	t.Logf("Recovery of 10K keys took: %v", recoveryTime)

	// Verify recovery worked
	stats := store2.Stats()
	if stats.KeyCount != 10000 {
		t.Errorf("Expected 10000 keys after recovery, got %d", stats.KeyCount)
	}

	// Both should be under 1 second for 10K keys
	if snapshotTime > 1*time.Second {
		t.Errorf("Snapshot too slow: %v", snapshotTime)
	}
	if recoveryTime > 1*time.Second {
		t.Errorf("Recovery too slow: %v", recoveryTime)
	}
}

func TestDurableStore_EmptySnapshot(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:       dir,
		SyncOnWrite:   true,
		SnapshotEvery: 0,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Take snapshot of empty store
	_, err = store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Failed to create empty snapshot: %v", err)
	}

	store.Close()

	// Recover from empty snapshot
	store2, err := NewDurableStore(DurableStoreConfig{
		DataDir:       dir,
		SyncOnWrite:   true,
		SnapshotEvery: 0,
	})
	if err != nil {
		t.Fatalf("Failed to recover from empty snapshot: %v", err)
	}
	defer store2.Close()

	stats := store2.Stats()
	if stats.KeyCount != 0 {
		t.Errorf("Expected 0 keys after empty snapshot recovery, got %d", stats.KeyCount)
	}
}

func TestDurableStore_ManualRestore(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:       dir,
		SyncOnWrite:   true,
		SnapshotEvery: 0,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Write some data
	store.Put(ctx, "key1", []byte("value1"))
	store.Put(ctx, "key2", []byte("value2"))

	// Take snapshot
	snapshotPath, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	// Modify data
	store.Put(ctx, "key3", []byte("value3"))
	store.Delete(ctx, "key1")

	// Verify current state
	stats := store.Stats()
	if stats.KeyCount != 2 {
		t.Errorf("Expected 2 keys before restore, got %d", stats.KeyCount)
	}

	// Restore from snapshot
	err = store.Restore(ctx, snapshotPath)
	if err != nil {
		t.Fatalf("Failed to restore: %v", err)
	}

	// Verify restored state
	stats = store.Stats()
	if stats.KeyCount != 2 {
		t.Errorf("Expected 2 keys after restore, got %d", stats.KeyCount)
	}

	// key1 and key2 should exist, key3 should not
	_, err = store.Get(ctx, "key1")
	if err != nil {
		t.Error("key1 should exist after restore")
	}

	_, err = store.Get(ctx, "key3")
	if err != ErrKeyNotFound {
		t.Error("key3 should not exist after restore")
	}
}

// Benchmark

func BenchmarkDurableStore_SnapshotCreation(b *testing.B) {
	dir := b.TempDir()
	ctx := context.Background()

	store, _ := NewDurableStore(DurableStoreConfig{
		DataDir:       dir,
		SyncOnWrite:   false,
		SnapshotEvery: 0,
	})
	defer store.Close()

	// Prepare 1000 keys
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key-%d", i)
		store.Put(ctx, key, []byte("value"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Snapshot(ctx)
	}
}
