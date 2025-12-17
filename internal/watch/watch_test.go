package watch

import (
	"context"
	"testing"
	"time"

	"github.com/RashikAnsar/raftkv/internal/cdc"
	"github.com/RashikAnsar/raftkv/internal/storage"
	"go.uber.org/zap/zaptest"
)

func TestWatchManager_CreateWatch(t *testing.T) {
	logger := zaptest.NewLogger(t)
	publisher := cdc.NewPublisher("test-dc", logger)
	defer publisher.Close()

	store := storage.NewMemoryStore()
	config := DefaultWatchConfig()
	manager := NewWatchManager(publisher, store, logger, config)
	defer manager.Close()

	ctx := context.Background()
	req := &WatchRequest{
		KeyPrefix:        "test:",
		Operations:       []string{"put", "delete"},
		SendInitialValue: false,
	}

	watcher, err := manager.CreateWatch(ctx, req)
	if err != nil {
		t.Fatalf("Failed to create watch: %v", err)
	}

	if watcher == nil {
		t.Fatal("Expected non-nil watcher")
	}

	if watcher.GetID() == "" {
		t.Error("Expected non-empty watcher ID")
	}

	// Verify watcher is registered
	stats := manager.GetStats()
	if stats.ActiveWatchers != 1 {
		t.Errorf("Expected 1 active watcher, got %d", stats.ActiveWatchers)
	}
}

func TestWatchManager_CancelWatch(t *testing.T) {
	logger := zaptest.NewLogger(t)
	publisher := cdc.NewPublisher("test-dc", logger)
	defer publisher.Close()

	store := storage.NewMemoryStore()
	config := DefaultWatchConfig()
	manager := NewWatchManager(publisher, store, logger, config)
	defer manager.Close()

	ctx := context.Background()
	req := &WatchRequest{
		KeyPrefix: "test:",
	}

	watcher, err := manager.CreateWatch(ctx, req)
	if err != nil {
		t.Fatalf("Failed to create watch: %v", err)
	}

	watchID := watcher.GetID()

	// Cancel the watch
	err = manager.CancelWatch(watchID)
	if err != nil {
		t.Errorf("Failed to cancel watch: %v", err)
	}

	// Verify watcher is removed
	stats := manager.GetStats()
	if stats.ActiveWatchers != 0 {
		t.Errorf("Expected 0 active watchers, got %d", stats.ActiveWatchers)
	}

	// Canceling again should return error
	err = manager.CancelWatch(watchID)
	if err != ErrWatcherNotFound {
		t.Errorf("Expected ErrWatcherNotFound, got %v", err)
	}
}

func TestWatchManager_MaxWatchers(t *testing.T) {
	logger := zaptest.NewLogger(t)
	publisher := cdc.NewPublisher("test-dc", logger)
	defer publisher.Close()

	store := storage.NewMemoryStore()
	config := WatchConfig{
		MaxWatchers:         2,
		BufferSize:          10,
		InitialValueTimeout: 5 * time.Second,
	}
	manager := NewWatchManager(publisher, store, logger, config)
	defer manager.Close()

	ctx := context.Background()
	req := &WatchRequest{
		KeyPrefix: "test:",
	}

	// Create first watch
	_, err := manager.CreateWatch(ctx, req)
	if err != nil {
		t.Fatalf("Failed to create first watch: %v", err)
	}

	// Create second watch
	_, err = manager.CreateWatch(ctx, req)
	if err != nil {
		t.Fatalf("Failed to create second watch: %v", err)
	}

	// Third watch should fail
	_, err = manager.CreateWatch(ctx, req)
	if err != ErrTooManyWatchers {
		t.Errorf("Expected ErrTooManyWatchers, got %v", err)
	}
}

func TestWatcher_EventTransformation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	publisher := cdc.NewPublisher("test-dc", logger)
	defer publisher.Close()

	store := storage.NewMemoryStore()
	config := DefaultWatchConfig()
	manager := NewWatchManager(publisher, store, logger, config)
	defer manager.Close()

	ctx := context.Background()
	req := &WatchRequest{
		KeyPrefix:  "",
		Operations: []string{"put"},
	}

	watcher, err := manager.CreateWatch(ctx, req)
	if err != nil {
		t.Fatalf("Failed to create watch: %v", err)
	}

	// Publish a CDC event
	cdcEvent := &cdc.ChangeEvent{
		RaftIndex: 100,
		RaftTerm:  5,
		Operation: "put",
		Key:       "test-key",
		Value:     []byte("test-value"),
		Timestamp: time.Now(),
	}

	publisher.Publish(ctx, cdcEvent)

	// Receive the event from watcher
	watchEvent, err := watcher.RecvWithTimeout(1 * time.Second)
	if err != nil {
		t.Fatalf("Failed to receive watch event: %v", err)
	}

	if watchEvent == nil {
		t.Fatal("Expected non-nil watch event")
	}

	if watchEvent.Key != "test-key" {
		t.Errorf("Expected key 'test-key', got '%s'", watchEvent.Key)
	}

	if string(watchEvent.Value) != "test-value" {
		t.Errorf("Expected value 'test-value', got '%s'", string(watchEvent.Value))
	}

	if watchEvent.RaftIndex != 100 {
		t.Errorf("Expected raft index 100, got %d", watchEvent.RaftIndex)
	}

	if watchEvent.Operation != "put" {
		t.Errorf("Expected operation 'put', got '%s'", watchEvent.Operation)
	}
}

func TestWatcher_OperationFiltering(t *testing.T) {
	logger := zaptest.NewLogger(t)
	publisher := cdc.NewPublisher("test-dc", logger)
	defer publisher.Close()

	store := storage.NewMemoryStore()
	config := DefaultWatchConfig()
	manager := NewWatchManager(publisher, store, logger, config)
	defer manager.Close()

	ctx := context.Background()
	req := &WatchRequest{
		KeyPrefix:  "",
		Operations: []string{"put"}, // Only watch put operations
	}

	watcher, err := manager.CreateWatch(ctx, req)
	if err != nil {
		t.Fatalf("Failed to create watch: %v", err)
	}

	// Publish a delete event (should be filtered out)
	deleteEvent := &cdc.ChangeEvent{
		RaftIndex: 100,
		Operation: "delete",
		Key:       "test-key",
		Timestamp: time.Now(),
	}
	publisher.Publish(ctx, deleteEvent)

	// Publish a put event (should be received)
	putEvent := &cdc.ChangeEvent{
		RaftIndex: 101,
		Operation: "put",
		Key:       "test-key",
		Value:     []byte("value"),
		Timestamp: time.Now(),
	}
	publisher.Publish(ctx, putEvent)

	// Should receive only the put event
	watchEvent, err := watcher.RecvWithTimeout(1 * time.Second)
	if err != nil {
		t.Fatalf("Failed to receive watch event: %v", err)
	}

	if watchEvent.Operation != "put" {
		t.Errorf("Expected operation 'put', got '%s'", watchEvent.Operation)
	}

	if watchEvent.RaftIndex != 101 {
		t.Errorf("Expected raft index 101, got %d", watchEvent.RaftIndex)
	}
}
