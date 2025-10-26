package storage

import (
	"fmt"
	"testing"
	"time"
)

func TestSnapshot_CreateAndRestore(t *testing.T) {
	dir := t.TempDir()

	sm, err := NewSnapshotManager(dir)
	if err != nil {
		t.Fatalf("Failed to create snapshot manager: %v", err)
	}

	// Create test data
	data := map[string][]byte{
		"key1": []byte("value1"),
		"key2": []byte("value2"),
		"key3": []byte("value3"),
	}

	// Create snapshot
	err = sm.Create(1000, data)
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	// Restore snapshot
	snapshot, err := sm.Restore()
	if err != nil {
		t.Fatalf("Failed to restore snapshot: %v", err)
	}

	if snapshot == nil {
		t.Fatal("Expected snapshot, got nil")
	}

	// Verify snapshot contents
	if snapshot.Index != 1000 {
		t.Errorf("Expected index 1000, got %d", snapshot.Index)
	}

	if snapshot.KeyCount != 3 {
		t.Errorf("Expected 3 keys, got %d", snapshot.KeyCount)
	}

	if len(snapshot.Data) != 3 {
		t.Errorf("Expected 3 data entries, got %d", len(snapshot.Data))
	}

	for key, expectedValue := range data {
		actualValue, exists := snapshot.Data[key]
		if !exists {
			t.Errorf("Key %s not found in snapshot", key)
			continue
		}

		if string(actualValue) != string(expectedValue) {
			t.Errorf("Key %s: expected %s, got %s", key, expectedValue, actualValue)
		}
	}
}

func TestSnapshot_NoSnapshots(t *testing.T) {
	dir := t.TempDir()

	sm, err := NewSnapshotManager(dir)
	if err != nil {
		t.Fatalf("Failed to create snapshot manager: %v", err)
	}

	// Try to restore when no snapshots exist
	snapshot, err := sm.Restore()
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	if snapshot != nil {
		t.Error("Expected nil snapshot, got non-nil")
	}
}

func TestSnapshot_MultipleSnapshots(t *testing.T) {
	dir := t.TempDir()

	sm, err := NewSnapshotManager(dir)
	if err != nil {
		t.Fatalf("Failed to create snapshot manager: %v", err)
	}

	// Create multiple snapshots
	for i := 1; i <= 5; i++ {
		data := map[string][]byte{
			fmt.Sprintf("key%d", i): []byte(fmt.Sprintf("value%d", i)),
		}

		err := sm.Create(uint64(i*1000), data)
		if err != nil {
			t.Fatalf("Failed to create snapshot %d: %v", i, err)
		}

		// Small delay to ensure different timestamps
		time.Sleep(1 * time.Millisecond)
	}

	// List snapshots
	list, err := sm.List()
	if err != nil {
		t.Fatalf("Failed to list snapshots: %v", err)
	}

	if len(list) != 5 {
		t.Fatalf("Expected 5 snapshots, got %d", len(list))
	}

	// Verify they're sorted by index (newest first)
	for i := 0; i < len(list)-1; i++ {
		if list[i].Index <= list[i+1].Index {
			t.Error("Snapshots not sorted correctly (newest first)")
		}
	}

	// Restore should get the latest
	snapshot, err := sm.Restore()
	if err != nil {
		t.Fatalf("Failed to restore: %v", err)
	}

	if snapshot.Index != 5000 {
		t.Errorf("Expected latest snapshot (index 5000), got %d", snapshot.Index)
	}
}

func TestSnapshot_RestoreFromIndex(t *testing.T) {
	dir := t.TempDir()

	sm, err := NewSnapshotManager(dir)
	if err != nil {
		t.Fatalf("Failed to create snapshot manager: %v", err)
	}

	// Create snapshots at different indices
	for i := uint64(1); i <= 3; i++ {
		data := map[string][]byte{
			fmt.Sprintf("key%d", i): []byte(fmt.Sprintf("value%d", i)),
		}
		sm.Create(i*1000, data)
	}

	// Restore specific snapshot
	snapshot, err := sm.RestoreFromIndex(2000)
	if err != nil {
		t.Fatalf("Failed to restore from index: %v", err)
	}

	if snapshot.Index != 2000 {
		t.Errorf("Expected index 2000, got %d", snapshot.Index)
	}

	if string(snapshot.Data["key2"]) != "value2" {
		t.Errorf("Wrong data in snapshot")
	}

	// Try to restore non-existent snapshot
	_, err = sm.RestoreFromIndex(9999)
	if err == nil {
		t.Error("Expected error for non-existent snapshot")
	}
}

func TestSnapshot_DeleteOldSnapshots(t *testing.T) {
	dir := t.TempDir()

	sm, err := NewSnapshotManager(dir)
	if err != nil {
		t.Fatalf("Failed to create snapshot manager: %v", err)
	}

	// Create 10 snapshots
	for i := 1; i <= 10; i++ {
		data := map[string][]byte{
			"key": []byte("value"),
		}
		sm.Create(uint64(i), data)
	}

	// Keep only 3 most recent
	err = sm.DeleteOldSnapshots(3)
	if err != nil {
		t.Fatalf("Failed to delete old snapshots: %v", err)
	}

	// Verify only 3 remain
	list, err := sm.List()
	if err != nil {
		t.Fatalf("Failed to list snapshots: %v", err)
	}

	if len(list) != 3 {
		t.Errorf("Expected 3 snapshots, got %d", len(list))
	}

	// Verify they're the newest ones
	if list[0].Index != 10 || list[1].Index != 9 || list[2].Index != 8 {
		t.Error("Wrong snapshots kept")
	}
}

func TestSnapshot_DeleteSnapshotsBefore(t *testing.T) {
	dir := t.TempDir()

	sm, err := NewSnapshotManager(dir)
	if err != nil {
		t.Fatalf("Failed to create snapshot manager: %v", err)
	}

	// Create snapshots
	for i := uint64(1); i <= 5; i++ {
		data := map[string][]byte{"key": []byte("value")}
		sm.Create(i*1000, data)
	}

	// Delete all before index 3000
	err = sm.DeleteSnapshotsBefore(3000)
	if err != nil {
		t.Fatalf("Failed to delete snapshots before: %v", err)
	}

	list, err := sm.List()
	if err != nil {
		t.Fatalf("Failed to list snapshots: %v", err)
	}

	// Should have 3000, 4000, 5000
	if len(list) != 3 {
		t.Errorf("Expected 3 snapshots remaining, got %d", len(list))
	}

	for _, meta := range list {
		if meta.Index < 3000 {
			t.Errorf("Snapshot %d should have been deleted", meta.Index)
		}
	}
}

func TestSnapshot_LargeDataset(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large dataset test in short mode")
	}

	dir := t.TempDir()

	sm, err := NewSnapshotManager(dir)
	if err != nil {
		t.Fatalf("Failed to create snapshot manager: %v", err)
	}

	// Create large dataset (10,000 keys)
	data := make(map[string][]byte)
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("key-%06d", i)
		value := []byte(fmt.Sprintf("value-%06d-with-some-longer-content-to-test-size", i))
		data[key] = value
	}

	// Create snapshot
	start := time.Now()
	err = sm.Create(1, data)
	if err != nil {
		t.Fatalf("Failed to create large snapshot: %v", err)
	}
	createTime := time.Since(start)
	t.Logf("Created snapshot with 10K keys in %v", createTime)

	// Restore snapshot
	start = time.Now()
	snapshot, err := sm.Restore()
	if err != nil {
		t.Fatalf("Failed to restore large snapshot: %v", err)
	}
	restoreTime := time.Since(start)
	t.Logf("Restored snapshot with 10K keys in %v", restoreTime)

	if len(snapshot.Data) != 10000 {
		t.Errorf("Expected 10000 keys, got %d", len(snapshot.Data))
	}

	// Verify some random keys
	for i := 0; i < 100; i++ {
		idx := i * 100
		key := fmt.Sprintf("key-%06d", idx)
		expectedValue := fmt.Sprintf("value-%06d-with-some-longer-content-to-test-size", idx)

		actualValue, exists := snapshot.Data[key]
		if !exists {
			t.Errorf("Key %s not found", key)
			continue
		}

		if string(actualValue) != expectedValue {
			t.Errorf("Key %s: value mismatch", key)
		}
	}
}

func TestSnapshot_Stats(t *testing.T) {
	dir := t.TempDir()

	sm, err := NewSnapshotManager(dir)
	if err != nil {
		t.Fatalf("Failed to create snapshot manager: %v", err)
	}

	// Initially no snapshots
	stats, err := sm.GetStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	if stats.Count != 0 {
		t.Errorf("Expected 0 snapshots, got %d", stats.Count)
	}

	// Create some snapshots
	for i := 1; i <= 3; i++ {
		data := make(map[string][]byte)
		for j := 0; j < i*10; j++ {
			data[fmt.Sprintf("key%d", j)] = []byte("value")
		}
		sm.Create(uint64(i), data)
	}

	stats, err = sm.GetStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	if stats.Count != 3 {
		t.Errorf("Expected 3 snapshots, got %d", stats.Count)
	}

	if stats.LatestIndex != 3 {
		t.Errorf("Expected latest index 3, got %d", stats.LatestIndex)
	}

	if stats.OldestIndex != 1 {
		t.Errorf("Expected oldest index 1, got %d", stats.OldestIndex)
	}

	if stats.TotalSize == 0 {
		t.Error("Expected non-zero total size")
	}

	t.Logf("Stats: Count=%d, TotalSize=%d bytes, Latest=%d, Oldest=%d",
		stats.Count, stats.TotalSize, stats.LatestIndex, stats.OldestIndex)
}

func TestSnapshot_EmptyData(t *testing.T) {
	dir := t.TempDir()

	sm, err := NewSnapshotManager(dir)
	if err != nil {
		t.Fatalf("Failed to create snapshot manager: %v", err)
	}

	// Create snapshot with empty data
	emptyData := make(map[string][]byte)
	err = sm.Create(1, emptyData)
	if err != nil {
		t.Fatalf("Failed to create empty snapshot: %v", err)
	}

	// Restore and verify
	snapshot, err := sm.Restore()
	if err != nil {
		t.Fatalf("Failed to restore empty snapshot: %v", err)
	}

	if snapshot.KeyCount != 0 {
		t.Errorf("Expected 0 keys, got %d", snapshot.KeyCount)
	}

	if len(snapshot.Data) != 0 {
		t.Errorf("Expected empty data, got %d entries", len(snapshot.Data))
	}
}

// Benchmarks

func BenchmarkSnapshot_Create(b *testing.B) {
	dir := b.TempDir()
	sm, _ := NewSnapshotManager(dir)

	// Prepare data
	data := make(map[string][]byte)
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key-%d", i)
		data[key] = []byte("value")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm.Create(uint64(i), data)
	}
}

func BenchmarkSnapshot_Restore(b *testing.B) {
	dir := b.TempDir()
	sm, _ := NewSnapshotManager(dir)

	// Create a snapshot
	data := make(map[string][]byte)
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key-%d", i)
		data[key] = []byte("value")
	}
	sm.Create(1, data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm.Restore()
	}
}
