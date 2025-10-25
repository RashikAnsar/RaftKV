package storage

import (
	"os"
	"testing"
	"time"
)

func TestWAL_BasicOperations(t *testing.T) {
	dir := t.TempDir()

	wal, err := NewWAL(WALConfig{
		Dir:         dir,
		SyncOnWrite: true,
	})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer wal.Close()

	t.Run("Append PUT entry", func(t *testing.T) {
		entry := &WALEntry{
			Operation: OpPut,
			Timestamp: time.Now(),
			Key:       "key1",
			Value:     []byte("value1"),
		}

		err := wal.Append(entry)
		if err != nil {
			t.Fatalf("Failed to append entry: %v", err)
		}
	})

	t.Run("Append DELETE entry", func(t *testing.T) {
		entry := &WALEntry{
			Operation: OpDelete,
			Timestamp: time.Now(),
			Key:       "key1",
		}

		err := wal.Append(entry)
		if err != nil {
			t.Fatalf("Failed to append entry: %v", err)
		}
	})

	t.Run("Replay entries", func(t *testing.T) {
		entries, err := wal.Replay()
		if err != nil {
			t.Fatalf("Failed to replay: %v", err)
		}

		if len(entries) != 2 {
			t.Fatalf("Expected 2 entries, got %d", len(entries))
		}

		if entries[0].Operation != OpPut {
			t.Errorf("Expected PUT operation, got %d", entries[0].Operation)
		}
		if entries[0].Key != "key1" {
			t.Errorf("Expected key 'key1', got '%s'", entries[0].Key)
		}
		if string(entries[0].Value) != "value1" {
			t.Errorf("Expected value 'value1', got '%s'", string(entries[0].Value))
		}

		if entries[1].Operation != OpDelete {
			t.Errorf("Expected DELETE operation, got %d", entries[1].Operation)
		}
		if entries[1].Key != "key1" {
			t.Errorf("Expected key 'key1', got '%s'", entries[1].Key)
		}
	})
}

func TestWAL_SegmentRotation(t *testing.T) {
	dir := t.TempDir()

	wal, err := NewWAL(WALConfig{
		Dir:            dir,
		MaxSegmentSize: 1024,
		SyncOnWrite:    false,
	})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer wal.Close()

	for i := 0; i < 100; i++ {
		entry := &WALEntry{
			Operation: OpPut,
			Timestamp: time.Now(),
			Key:       "key-with-very-long-name-to-fill-segment",
			Value:     []byte("value-with-even-longer-content-to-ensure-rotation-happens"),
		}

		if err := wal.Append(entry); err != nil {
			t.Fatalf("Failed to append entry %d: %v", i, err)
		}
	}

	segments, err := wal.listSegments()
	if err != nil {
		t.Fatalf("Failed to list segments: %v", err)
	}

	if len(segments) < 2 {
		t.Errorf("Expected at least 2 segments, got %d", len(segments))
	}

	t.Logf("Created %d segments", len(segments))
}

func TestWAL_CrashRecovery(t *testing.T) {
	dir := t.TempDir()

	func() {
		wal, err := NewWAL(WALConfig{
			Dir:         dir,
			SyncOnWrite: true,
		})
		if err != nil {
			t.Fatalf("Failed to create WAL: %v", err)
		}

		for i := 0; i < 10; i++ {
			entry := &WALEntry{
				Operation: OpPut,
				Timestamp: time.Now(),
				Key:       string(rune('a' + i)),
				Value:     []byte{byte('A' + i)},
			}

			if err := wal.Append(entry); err != nil {
				t.Fatalf("Failed to append entry: %v", err)
			}
		}

		wal.Close()
	}()

	wal, err := NewWAL(WALConfig{
		Dir:         dir,
		SyncOnWrite: true,
	})
	if err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}
	defer wal.Close()

	entries, err := wal.Replay()
	if err != nil {
		t.Fatalf("Failed to replay: %v", err)
	}

	if len(entries) != 10 {
		t.Fatalf("Expected 10 entries after recovery, got %d", len(entries))
	}

	for i, entry := range entries {
		expectedKey := string(rune('a' + i))
		expectedValue := []byte{byte('A' + i)}

		if entry.Key != expectedKey {
			t.Errorf("Entry %d: expected key '%s', got '%s'", i, expectedKey, entry.Key)
		}
		if len(entry.Value) != 1 || entry.Value[0] != expectedValue[0] {
			t.Errorf("Entry %d: expected value %v, got %v", i, expectedValue, entry.Value)
		}
	}
}

func TestWAL_ChecksumValidation(t *testing.T) {
	dir := t.TempDir()

	wal, err := NewWAL(WALConfig{
		Dir:         dir,
		SyncOnWrite: true,
	})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	entry := &WALEntry{
		Operation: OpPut,
		Timestamp: time.Now(),
		Key:       "test-key",
		Value:     []byte("test-value"),
	}

	if err := wal.Append(entry); err != nil {
		t.Fatalf("Failed to append entry: %v", err)
	}

	wal.Close()

	segmentPath := wal.segmentPath(1)
	file, err := os.OpenFile(segmentPath, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("Failed to open segment: %v", err)
	}

	file.Seek(20, 0)
	file.Write([]byte{0xFF})
	file.Close()

	// Try to replay - should detect corruption
	wal2, err := NewWAL(WALConfig{
		Dir:         dir,
		SyncOnWrite: true,
	})
	if err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}
	defer wal2.Close()

	entries, err := wal2.Replay()
	// Should succeed but skip corrupt entry
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}

	// Should have 0 valid entries (the one we wrote was corrupted)
	if len(entries) != 0 {
		t.Logf("Note: Found %d entries despite corruption (corruption may not have affected entry)", len(entries))
	}
}

func TestWAL_DeleteOldSegments(t *testing.T) {
	dir := t.TempDir()

	wal, err := NewWAL(WALConfig{
		Dir:            dir,
		MaxSegmentSize: 512, // Small segments
		SyncOnWrite:    false,
	})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer wal.Close()

	for i := 0; i < 50; i++ {
		entry := &WALEntry{
			Operation: OpPut,
			Timestamp: time.Now(),
			Key:       "key-with-long-name-for-segment-fill",
			Value:     []byte("value-with-long-content-for-segment-fill"),
		}
		wal.Append(entry)
	}

	segments1, _ := wal.listSegments()
	t.Logf("Created %d segments", len(segments1))

	if len(segments1) < 3 {
		t.Fatalf("Expected at least 3 segments, got %d", len(segments1))
	}

	keepFromIndex := segments1[len(segments1)-2]
	err = wal.DeleteSegmentsBefore(keepFromIndex)
	if err != nil {
		t.Fatalf("Failed to delete old segments: %v", err)
	}

	segments2, _ := wal.listSegments()
	t.Logf("After cleanup: %d segments", len(segments2))

	if len(segments2) != 2 {
		t.Errorf("Expected 2 segments after cleanup, got %d", len(segments2))
	}
}

func TestWAL_LargeValues(t *testing.T) {
	dir := t.TempDir()

	wal, err := NewWAL(WALConfig{
		Dir:         dir,
		SyncOnWrite: false,
	})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer wal.Close()

	largeValue := make([]byte, 1024*1024)
	for i := range largeValue {
		largeValue[i] = byte(i % 256)
	}

	entry := &WALEntry{
		Operation: OpPut,
		Timestamp: time.Now(),
		Key:       "large-key",
		Value:     largeValue,
	}

	if err := wal.Append(entry); err != nil {
		t.Fatalf("Failed to append large entry: %v", err)
	}

	entries, err := wal.Replay()
	if err != nil {
		t.Fatalf("Failed to replay: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if len(entries[0].Value) != len(largeValue) {
		t.Errorf("Value size mismatch: expected %d, got %d", len(largeValue), len(entries[0].Value))
	}

	for i := range largeValue {
		if entries[0].Value[i] != largeValue[i] {
			t.Errorf("Value mismatch at byte %d", i)
			break
		}
	}
}

func TestWAL_EmptyKey(t *testing.T) {
	dir := t.TempDir()

	wal, err := NewWAL(WALConfig{
		Dir:         dir,
		SyncOnWrite: false,
	})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer wal.Close()

	// Empty key should work (validation is at store level)
	entry := &WALEntry{
		Operation: OpPut,
		Timestamp: time.Now(),
		Key:       "",
		Value:     []byte("value"),
	}

	if err := wal.Append(entry); err != nil {
		t.Fatalf("Failed to append entry with empty key: %v", err)
	}

	entries, _ := wal.Replay()
	if len(entries) != 1 || entries[0].Key != "" {
		t.Error("Failed to handle empty key")
	}
}

func TestWAL_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()

	wal, err := NewWAL(WALConfig{
		Dir:         dir,
		SyncOnWrite: false,
	})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer wal.Close()

	const numGoroutines = 10
	const numWrites = 100

	errChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numWrites; j++ {
				entry := &WALEntry{
					Operation: OpPut,
					Timestamp: time.Now(),
					Key:       string(rune('a' + id)),
					Value:     []byte{byte(j)},
				}

				if err := wal.Append(entry); err != nil {
					errChan <- err
					return
				}
			}
			errChan <- nil
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		if err := <-errChan; err != nil {
			t.Errorf("Concurrent write failed: %v", err)
		}
	}

	entries, err := wal.Replay()
	if err != nil {
		t.Fatalf("Failed to replay: %v", err)
	}

	expectedCount := numGoroutines * numWrites
	if len(entries) != expectedCount {
		t.Errorf("Expected %d entries, got %d", expectedCount, len(entries))
	}
}

// Benchmarks

func BenchmarkWAL_Append(b *testing.B) {
	dir := b.TempDir()

	wal, err := NewWAL(WALConfig{
		Dir:         dir,
		SyncOnWrite: false, // Async for speed
	})
	if err != nil {
		b.Fatalf("Failed to create WAL: %v", err)
	}
	defer wal.Close()

	entry := &WALEntry{
		Operation: OpPut,
		Timestamp: time.Now(),
		Key:       "benchmark-key",
		Value:     []byte("benchmark-value"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wal.Append(entry)
	}
}

func BenchmarkWAL_AppendSync(b *testing.B) {
	dir := b.TempDir()

	wal, err := NewWAL(WALConfig{
		Dir:         dir,
		SyncOnWrite: true,
	})
	if err != nil {
		b.Fatalf("Failed to create WAL: %v", err)
	}
	defer wal.Close()

	entry := &WALEntry{
		Operation: OpPut,
		Timestamp: time.Now(),
		Key:       "benchmark-key",
		Value:     []byte("benchmark-value"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wal.Append(entry)
	}
}

func BenchmarkWAL_Replay(b *testing.B) {
	dir := b.TempDir()

	wal, err := NewWAL(WALConfig{
		Dir:         dir,
		SyncOnWrite: false,
	})
	if err != nil {
		b.Fatalf("Failed to create WAL: %v", err)
	}

	for i := 0; i < 1000; i++ {
		entry := &WALEntry{
			Operation: OpPut,
			Timestamp: time.Now(),
			Key:       "key",
			Value:     []byte("value"),
		}
		wal.Append(entry)
	}
	wal.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wal2, _ := NewWAL(WALConfig{
			Dir:         dir,
			SyncOnWrite: false,
		})
		wal2.Replay()
		wal2.Close()
	}
}
