package storage

import (
	"context"
	"encoding/binary"
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

// CAS Tests

func TestDurableStore_GetWithVersion(t *testing.T) {
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

	t.Run("NonExistentKey", func(t *testing.T) {
		_, version, err := store.GetWithVersion(ctx, "nonexistent")
		if err != ErrKeyNotFound {
			t.Errorf("Expected ErrKeyNotFound, got %v", err)
		}
		if version != 0 {
			t.Errorf("Expected version 0 for non-existent key, got %d", version)
		}
	})

	t.Run("ExistingKey", func(t *testing.T) {
		// Put a key
		if err := store.Put(ctx, "mykey", []byte("value1")); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		// Get with version
		value, version, err := store.GetWithVersion(ctx, "mykey")
		if err != nil {
			t.Fatalf("GetWithVersion failed: %v", err)
		}
		if string(value) != "value1" {
			t.Errorf("Expected 'value1', got '%s'", string(value))
		}
		if version != 1 {
			t.Errorf("Expected version 1, got %d", version)
		}
	})

	t.Run("VersionIncrementsOnUpdate", func(t *testing.T) {
		// Update the key
		if err := store.Put(ctx, "mykey", []byte("value2")); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		// Get with version
		value, version, err := store.GetWithVersion(ctx, "mykey")
		if err != nil {
			t.Fatalf("GetWithVersion failed: %v", err)
		}
		if string(value) != "value2" {
			t.Errorf("Expected 'value2', got '%s'", string(value))
		}
		if version != 2 {
			t.Errorf("Expected version 2, got %d", version)
		}
	})
}

func TestDurableStore_CompareAndSwap_Success(t *testing.T) {
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

	// Create initial value
	if err := store.Put(ctx, "counter", []byte("0")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Get current version
	_, version, err := store.GetWithVersion(ctx, "counter")
	if err != nil {
		t.Fatalf("GetWithVersion failed: %v", err)
	}
	if version != 1 {
		t.Fatalf("Expected version 1, got %d", version)
	}

	// Perform CAS with correct version
	newVersion, success, err := store.CompareAndSwap(ctx, "counter", 1, []byte("1"))
	if err != nil {
		t.Fatalf("CompareAndSwap failed: %v", err)
	}
	if !success {
		t.Fatal("CAS should have succeeded")
	}
	if newVersion != 2 {
		t.Errorf("Expected new version 2, got %d", newVersion)
	}

	// Verify value updated
	value, err := store.Get(ctx, "counter")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(value) != "1" {
		t.Errorf("Expected '1', got '%s'", string(value))
	}
}

func TestDurableStore_CompareAndSwap_VersionMismatch(t *testing.T) {
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

	// Create initial value
	if err := store.Put(ctx, "counter", []byte("0")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Update to version 2
	if err := store.Put(ctx, "counter", []byte("1")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Attempt CAS with stale version 1 (current is 2)
	currentVersion, success, err := store.CompareAndSwap(ctx, "counter", 1, []byte("2"))
	if err != nil {
		t.Fatalf("CompareAndSwap failed: %v", err)
	}
	if success {
		t.Fatal("CAS should have failed due to version mismatch")
	}
	if currentVersion != 2 {
		t.Errorf("Expected current version 2, got %d", currentVersion)
	}

	// Verify value unchanged
	value, err := store.Get(ctx, "counter")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(value) != "1" {
		t.Errorf("Expected '1' (unchanged), got '%s'", string(value))
	}
}

func TestDurableStore_CompareAndSwap_KeyNotFound(t *testing.T) {
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

	// Attempt CAS on non-existent key
	version, success, err := store.CompareAndSwap(ctx, "nonexistent", 1, []byte("value"))
	if err != nil {
		t.Fatalf("CompareAndSwap should not error for non-existent key, got: %v", err)
	}
	if success {
		t.Fatal("CAS should fail for non-existent key")
	}
	if version != 0 {
		t.Errorf("Expected version 0 for non-existent key, got %d", version)
	}
}

func TestDurableStore_SetIfNotExists(t *testing.T) {
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

	t.Run("CreateNewKey", func(t *testing.T) {
		version, created, err := store.SetIfNotExists(ctx, "newkey", []byte("newvalue"))
		if err != nil {
			t.Fatalf("SetIfNotExists failed: %v", err)
		}
		if !created {
			t.Fatal("Expected key to be created")
		}
		if version != 1 {
			t.Errorf("Expected version 1, got %d", version)
		}

		// Verify value
		value, err := store.Get(ctx, "newkey")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if string(value) != "newvalue" {
			t.Errorf("Expected 'newvalue', got '%s'", string(value))
		}
	})

	t.Run("ExistingKey", func(t *testing.T) {
		// Try to create the same key again
		version, created, err := store.SetIfNotExists(ctx, "newkey", []byte("anothervalue"))
		if err != nil {
			t.Fatalf("SetIfNotExists failed: %v", err)
		}
		if created {
			t.Fatal("Expected key creation to fail (already exists)")
		}
		if version != 1 {
			t.Errorf("Expected existing version 1, got %d", version)
		}

		// Verify value unchanged
		value, err := store.Get(ctx, "newkey")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if string(value) != "newvalue" {
			t.Errorf("Expected 'newvalue' (unchanged), got '%s'", string(value))
		}
	})
}

func TestDurableStore_CAS_WALRecovery(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Phase 1: Create store, perform CAS operations
	func() {
		store, err := NewDurableStore(DurableStoreConfig{
			DataDir:       dir,
			SyncOnWrite:   true,
			SnapshotEvery: 0, // Manual snapshots
		})
		if err != nil {
			t.Fatalf("Failed to create store: %v", err)
		}

		// Create initial key
		if err := store.Put(ctx, "counter", []byte("0")); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		// Perform successful CAS
		_, _, err = store.CompareAndSwap(ctx, "counter", 1, []byte("1"))
		if err != nil {
			t.Fatalf("CAS 1 failed: %v", err)
		}

		_, _, err = store.CompareAndSwap(ctx, "counter", 2, []byte("2"))
		if err != nil {
			t.Fatalf("CAS 2 failed: %v", err)
		}

		// Perform failed CAS (wrong version)
		_, _, err = store.CompareAndSwap(ctx, "counter", 1, []byte("wrong"))
		if err != nil {
			t.Fatalf("CAS wrong failed: %v", err)
		}

		// One more successful CAS
		_, _, err = store.CompareAndSwap(ctx, "counter", 3, []byte("3"))
		if err != nil {
			t.Fatalf("CAS 3 failed: %v", err)
		}

		t.Logf("Before close - WAL index: %d", store.walIndex)
		store.Close()
	}()

	// Phase 2: Recover from WAL
	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:       dir,
		SyncOnWrite:   true,
		SnapshotEvery: 0,
	})
	if err != nil {
		t.Fatalf("Failed to recover store: %v", err)
	}
	defer store.Close()

	// Verify final value
	value, version, err := store.GetWithVersion(ctx, "counter")
	if err != nil {
		t.Fatalf("GetWithVersion failed: %v", err)
	}
	if string(value) != "3" {
		t.Errorf("Expected '3' after recovery, got '%s'", string(value))
	}
	if version != 4 {
		t.Errorf("Expected version 4 after recovery, got %d", version)
	}

	t.Log("WAL recovery with CAS operations successful")
}

func TestDurableStore_CAS_SnapshotRecovery(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Phase 1: Create store, perform CAS, snapshot, more CAS
	func() {
		store, err := NewDurableStore(DurableStoreConfig{
			DataDir:       dir,
			SyncOnWrite:   true,
			SnapshotEvery: 0,
		})
		if err != nil {
			t.Fatalf("Failed to create store: %v", err)
		}

		// Create keys with CAS
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("key-%d", i)
			store.Put(ctx, key, []byte("0"))
			store.CompareAndSwap(ctx, key, 1, []byte("1"))
		}

		// Take snapshot
		_, err = store.Snapshot(ctx)
		if err != nil {
			t.Fatalf("Failed to create snapshot: %v", err)
		}

		// More CAS operations after snapshot
		for i := 0; i < 5; i++ {
			key := fmt.Sprintf("key-%d", i)
			store.CompareAndSwap(ctx, key, 2, []byte("2"))
		}

		store.Close()
	}()

	// Phase 2: Recover from snapshot + WAL
	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:       dir,
		SyncOnWrite:   true,
		SnapshotEvery: 0,
	})
	if err != nil {
		t.Fatalf("Failed to recover: %v", err)
	}
	defer store.Close()

	// Verify keys 0-4 have version 3 (Put + CAS + CAS after snapshot)
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key-%d", i)
		value, version, err := store.GetWithVersion(ctx, key)
		if err != nil {
			t.Errorf("Key %s not found: %v", key, err)
			continue
		}
		if string(value) != "2" {
			t.Errorf("Key %s: expected '2', got '%s'", key, string(value))
		}
		if version != 3 {
			t.Errorf("Key %s: expected version 3, got %d", key, version)
		}
	}

	// Verify keys 5-9 have version 2 (Put + CAS before snapshot)
	for i := 5; i < 10; i++ {
		key := fmt.Sprintf("key-%d", i)
		value, version, err := store.GetWithVersion(ctx, key)
		if err != nil {
			t.Errorf("Key %s not found: %v", key, err)
			continue
		}
		if string(value) != "1" {
			t.Errorf("Key %s: expected '1', got '%s'", key, string(value))
		}
		if version != 2 {
			t.Errorf("Key %s: expected version 2, got %d", key, version)
		}
	}

	t.Log("Snapshot recovery with CAS operations successful")
}

func TestDurableStore_CAS_WithBatchedWAL(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:       dir,
		SyncOnWrite:   false,
		SnapshotEvery: 0,
		BatchConfig: BatchConfig{
			MaxBatchSize: 100,
			MaxWaitTime:  10 * time.Millisecond,
			Enabled:      true,
		},
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Perform multiple CAS operations
	store.Put(ctx, "counter", []byte("0"))

	for i := 1; i <= 10; i++ {
		expectedVersion := uint64(i)
		newValue := []byte(fmt.Sprintf("%d", i))

		newVersion, success, err := store.CompareAndSwap(ctx, "counter", expectedVersion, newValue)
		if err != nil {
			t.Fatalf("CAS %d failed: %v", i, err)
		}
		if !success {
			t.Fatalf("CAS %d should have succeeded", i)
		}
		if newVersion != expectedVersion+1 {
			t.Errorf("CAS %d: expected version %d, got %d", i, expectedVersion+1, newVersion)
		}
	}

	// Wait for batch flush
	time.Sleep(50 * time.Millisecond)
	store.Close()

	// Recover and verify
	store2, err := NewDurableStore(DurableStoreConfig{
		DataDir:       dir,
		SyncOnWrite:   true,
		SnapshotEvery: 0,
	})
	if err != nil {
		t.Fatalf("Failed to recover: %v", err)
	}
	defer store2.Close()

	value, version, err := store2.GetWithVersion(ctx, "counter")
	if err != nil {
		t.Fatalf("GetWithVersion failed: %v", err)
	}
	if string(value) != "10" {
		t.Errorf("Expected '10', got '%s'", string(value))
	}
	if version != 11 {
		t.Errorf("Expected version 11, got %d", version)
	}

	t.Log("Batched WAL with CAS operations successful")
}

func TestDurableStore_ApplyRaftEntry_CAS(t *testing.T) {
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

	// Create initial value
	err = store.ApplyRaftEntry(ctx, 1, 1, "put", "mykey", []byte("value1"))
	if err != nil {
		t.Fatalf("ApplyRaftEntry put failed: %v", err)
	}

	// Verify initial version
	_, version, _ := store.GetWithVersion(ctx, "mykey")
	if version != 1 {
		t.Fatalf("Expected version 1, got %d", version)
	}

	// Encode CAS value: expectedVersion (8 bytes) + newValue
	casValue := make([]byte, 8)
	binary.BigEndian.PutUint64(casValue[0:8], 1) // Expected version 1
	casValue = append(casValue, []byte("value2")...)

	// Apply CAS through Raft
	err = store.ApplyRaftEntry(ctx, 2, 1, "cas", "mykey", casValue)
	if err != nil {
		t.Fatalf("ApplyRaftEntry cas failed: %v", err)
	}

	// Verify value and version updated
	value, version, err := store.GetWithVersion(ctx, "mykey")
	if err != nil {
		t.Fatalf("GetWithVersion failed: %v", err)
	}
	if string(value) != "value2" {
		t.Errorf("Expected 'value2', got '%s'", string(value))
	}
	if version != 2 {
		t.Errorf("Expected version 2, got %d", version)
	}

	// Apply CAS with wrong version (should fail silently)
	casValue2 := make([]byte, 8)
	binary.BigEndian.PutUint64(casValue2[0:8], 1) // Wrong version (expecting 2)
	casValue2 = append(casValue2, []byte("value3")...)

	err = store.ApplyRaftEntry(ctx, 3, 1, "cas", "mykey", casValue2)
	if err != nil {
		t.Fatalf("ApplyRaftEntry cas should not error on version mismatch: %v", err)
	}

	// Verify value unchanged
	value, version, err = store.GetWithVersion(ctx, "mykey")
	if err != nil {
		t.Fatalf("GetWithVersion failed: %v", err)
	}
	if string(value) != "value2" {
		t.Errorf("Expected 'value2' (unchanged), got '%s'", string(value))
	}
	if version != 2 {
		t.Errorf("Expected version 2 (unchanged), got %d", version)
	}

	// Verify Raft metadata tracked correctly
	lastIndex, lastTerm := store.LastAppliedIndex()
	if lastIndex != 3 {
		t.Errorf("Expected last Raft index 3, got %d", lastIndex)
	}
	if lastTerm != 1 {
		t.Errorf("Expected last Raft term 1, got %d", lastTerm)
	}
}

func TestDurableStore_CAS_Concurrent(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	store, err := NewDurableStore(DurableStoreConfig{
		DataDir:       dir,
		SyncOnWrite:   false, // Faster for concurrent test
		SnapshotEvery: 0,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Create initial counter
	store.Put(ctx, "counter", []byte("0"))

	// 10 goroutines, each attempting 50 increments
	numGoroutines := 10
	incrementsPerGoroutine := 50
	expectedTotal := numGoroutines * incrementsPerGoroutine

	successCounts := make([]int, numGoroutines)
	start := make(chan struct{})
	done := make(chan int)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			<-start // Wait for start signal
			successCount := 0

			for i := 0; i < incrementsPerGoroutine; {
				// Read current value and version
				valueBytes, version, err := store.GetWithVersion(ctx, "counter")
				if err != nil {
					continue
				}

				currentValue := 0
				fmt.Sscanf(string(valueBytes), "%d", &currentValue)

				// Attempt CAS
				newValue := []byte(fmt.Sprintf("%d", currentValue+1))
				_, success, err := store.CompareAndSwap(ctx, "counter", version, newValue)
				if err != nil {
					continue
				}

				if success {
					successCount++
					i++
				}
				// If CAS fails, retry (optimistic concurrency control)
			}

			done <- successCount
		}(g)
	}

	// Start all goroutines simultaneously
	close(start)

	// Collect results
	totalSuccesses := 0
	for g := 0; g < numGoroutines; g++ {
		count := <-done
		successCounts[g] = count
		totalSuccesses += count
	}

	// Verify all increments succeeded
	if totalSuccesses != expectedTotal {
		t.Errorf("Expected %d total successes, got %d", expectedTotal, totalSuccesses)
	}

	// Verify each goroutine succeeded exactly incrementsPerGoroutine times
	for g := 0; g < numGoroutines; g++ {
		if successCounts[g] != incrementsPerGoroutine {
			t.Errorf("Goroutine %d: expected %d successes, got %d",
				g, incrementsPerGoroutine, successCounts[g])
		}
	}

	// Verify final counter value
	value, version, err := store.GetWithVersion(ctx, "counter")
	if err != nil {
		t.Fatalf("GetWithVersion failed: %v", err)
	}

	finalCount := 0
	fmt.Sscanf(string(value), "%d", &finalCount)

	if finalCount != expectedTotal {
		t.Errorf("Expected final count %d, got %d", expectedTotal, finalCount)
	}

	// Version should be increments + 1 (initial Put)
	expectedVersion := uint64(expectedTotal + 1)
	if version != expectedVersion {
		t.Errorf("Expected version %d, got %d", expectedVersion, version)
	}

	t.Logf("Concurrent CAS test passed: %d total increments, final value=%d, version=%d",
		expectedTotal, finalCount, version)
}

// Benchmarks

func BenchmarkDurableStore_CAS_Success(b *testing.B) {
	dir := b.TempDir()
	ctx := context.Background()

	store, _ := NewDurableStore(DurableStoreConfig{
		DataDir:       dir,
		SyncOnWrite:   false,
		SnapshotEvery: 0,
	})
	defer store.Close()

	store.Put(ctx, "bench", []byte("0"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, currentVersion, _ := store.GetWithVersion(ctx, "bench")
		newValue := []byte(fmt.Sprintf("%d", i+1))
		store.CompareAndSwap(ctx, "bench", currentVersion, newValue)
	}
}

func BenchmarkDurableStore_CAS_WithBatching(b *testing.B) {
	dir := b.TempDir()
	ctx := context.Background()

	store, _ := NewDurableStore(DurableStoreConfig{
		DataDir:       dir,
		SyncOnWrite:   false,
		SnapshotEvery: 0,
		BatchConfig: BatchConfig{
			MaxBatchSize: 100,
			MaxWaitTime:  10 * time.Millisecond,
			Enabled:      true,
		},
	})
	defer store.Close()

	store.Put(ctx, "bench", []byte("0"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, currentVersion, _ := store.GetWithVersion(ctx, "bench")
		newValue := []byte(fmt.Sprintf("%d", i+1))
		store.CompareAndSwap(ctx, "bench", currentVersion, newValue)
	}
}
