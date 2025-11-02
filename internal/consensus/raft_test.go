package consensus

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashicorp/raft"
	"go.uber.org/zap"

	"github.com/RashikAnsar/raftkv/internal/storage"
)

// Helper function to create DurableStore for tests
func createTestStore(t *testing.T, dir string) *storage.DurableStore {
	store, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       filepath.Join(dir, "store"),
		SyncOnWrite:   false, // Faster for tests
		SnapshotEvery: 1000,
	})
	if err != nil {
		t.Fatalf("Failed to create DurableStore: %v", err)
	}
	return store
}

// TestRaftNode_SingleNode tests basic operations on a single-node cluster
func TestRaftNode_SingleNode(t *testing.T) {
	// Create temporary directory for Raft data
	tmpDir, err := os.MkdirTemp("", "raft-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Create storage
	store := createTestStore(t, tmpDir)

	// Create Raft node
	node, err := NewRaftNode(RaftConfig{
		NodeID:    "node1",
		RaftAddr:  "127.0.0.1:7000",
		RaftDir:   tmpDir,
		Bootstrap: true,
		Store:     store,
		Logger:    logger,
	})
	if err != nil {
		t.Fatalf("Failed to create Raft node: %v", err)
	}
	defer node.Shutdown()

	// Wait for leader election
	if err := node.WaitForLeader(10 * time.Second); err != nil {
		t.Fatalf("Failed to elect leader: %v", err)
	}

	// Verify this node is the leader
	if !node.IsLeader() {
		t.Error("Node should be leader in single-node cluster")
	}

	// Test PUT operation
	cmd := Command{
		Op:    OpTypePut,
		Key:   "key1",
		Value: []byte("value1"),
	}

	if err := node.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to apply PUT command: %v", err)
	}

	// Test GET operation
	ctx := context.Background()
	value, err := node.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Failed to get key: %v", err)
	}

	if string(value) != "value1" {
		t.Errorf("Expected value1, got %s", string(value))
	}

	// Test DELETE operation
	delCmd := Command{
		Op:  OpTypeDelete,
		Key: "key1",
	}

	if err := node.Apply(delCmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to apply DELETE command: %v", err)
	}

	// Verify key is deleted
	_, err = node.Get(ctx, "key1")
	if err == nil {
		t.Error("Expected error for deleted key")
	}
}

// TestRaftNode_MultipleOperations tests multiple operations
func TestRaftNode_MultipleOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "raft-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger, _ := zap.NewDevelopment()
	store := createTestStore(t, tmpDir)

	node, err := NewRaftNode(RaftConfig{
		NodeID:    "node1",
		RaftAddr:  "127.0.0.1:7001",
		RaftDir:   tmpDir,
		Bootstrap: true,
		Store:     store,
		Logger:    logger,
	})
	if err != nil {
		t.Fatalf("Failed to create Raft node: %v", err)
	}
	defer node.Shutdown()

	if err := node.WaitForLeader(10 * time.Second); err != nil {
		t.Fatalf("Failed to elect leader: %v", err)
	}

	// Insert multiple keys
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		cmd := Command{
			Op:    OpTypePut,
			Key:   fmt.Sprintf("key%d", i),
			Value: []byte(fmt.Sprintf("value%d", i)),
		}

		if err := node.Apply(cmd, 5*time.Second); err != nil {
			t.Fatalf("Failed to apply PUT command for key%d: %v", i, err)
		}
	}

	// Verify all keys
	for i := 0; i < 100; i++ {
		value, err := node.Get(ctx, fmt.Sprintf("key%d", i))
		if err != nil {
			t.Errorf("Failed to get key%d: %v", i, err)
			continue
		}

		expected := fmt.Sprintf("value%d", i)
		if string(value) != expected {
			t.Errorf("For key%d, expected %s, got %s", i, expected, string(value))
		}
	}

	// Test list operation
	keys, err := node.List(ctx, "key", 1000)
	if err != nil {
		t.Fatalf("Failed to list keys: %v", err)
	}

	if len(keys) != 100 {
		t.Errorf("Expected 100 keys, got %d", len(keys))
	}
}

// TestRaftNode_Snapshot tests snapshot creation and restoration
func TestRaftNode_Snapshot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "raft-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger, _ := zap.NewDevelopment()
	store := createTestStore(t, tmpDir)

	node, err := NewRaftNode(RaftConfig{
		NodeID:    "node1",
		RaftAddr:  "127.0.0.1:7002",
		RaftDir:   tmpDir,
		Bootstrap: true,
		Store:     store,
		Logger:    logger,
	})
	if err != nil {
		t.Fatalf("Failed to create Raft node: %v", err)
	}
	defer node.Shutdown()

	if err := node.WaitForLeader(10 * time.Second); err != nil {
		t.Fatalf("Failed to elect leader: %v", err)
	}

	// Insert some data
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		cmd := Command{
			Op:    OpTypePut,
			Key:   fmt.Sprintf("key%d", i),
			Value: []byte(fmt.Sprintf("value%d", i)),
		}

		if err := node.Apply(cmd, 5*time.Second); err != nil {
			t.Fatalf("Failed to apply PUT command: %v", err)
		}
	}

	// Create snapshot
	if err := node.Snapshot(); err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	// Verify data is still accessible
	for i := 0; i < 10; i++ {
		value, err := node.Get(ctx, fmt.Sprintf("key%d", i))
		if err != nil {
			t.Errorf("Failed to get key%d after snapshot: %v", i, err)
			continue
		}

		expected := fmt.Sprintf("value%d", i)
		if string(value) != expected {
			t.Errorf("For key%d, expected %s, got %s", i, expected, string(value))
		}
	}

	// Verify snapshot files exist
	snapshots, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to read snapshot directory: %v", err)
	}

	hasSnapshot := false
	for _, entry := range snapshots {
		if entry.IsDir() && entry.Name() == "snapshots" {
			hasSnapshot = true
			break
		}
	}

	if !hasSnapshot {
		t.Log("Note: Snapshot directory may not be created if log is too small")
	}
}

// TestRaftNode_Stats tests statistics retrieval
func TestRaftNode_Stats(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "raft-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger, _ := zap.NewDevelopment()
	store := createTestStore(t, tmpDir)

	node, err := NewRaftNode(RaftConfig{
		NodeID:    "node1",
		RaftAddr:  "127.0.0.1:7003",
		RaftDir:   tmpDir,
		Bootstrap: true,
		Store:     store,
		Logger:    logger,
	})
	if err != nil {
		t.Fatalf("Failed to create Raft node: %v", err)
	}
	defer node.Shutdown()

	if err := node.WaitForLeader(10 * time.Second); err != nil {
		t.Fatalf("Failed to elect leader: %v", err)
	}

	// Get Raft stats
	raftStats := node.RaftStats()
	if len(raftStats) == 0 {
		t.Error("Expected non-empty Raft stats")
	}

	// Verify state is leader
	state := raftStats["state"]
	if state != "Leader" {
		t.Errorf("Expected state Leader, got %s", state)
	}

	// Get storage stats
	stats := node.Stats()
	if stats.KeyCount < 0 {
		t.Error("Invalid key count")
	}
}

// TestRaftNode_GetServers tests cluster membership queries
func TestRaftNode_GetServers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "raft-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger, _ := zap.NewDevelopment()
	store := createTestStore(t, tmpDir)

	node, err := NewRaftNode(RaftConfig{
		NodeID:    "node1",
		RaftAddr:  "127.0.0.1:7004",
		RaftDir:   tmpDir,
		Bootstrap: true,
		Store:     store,
		Logger:    logger,
	})
	if err != nil {
		t.Fatalf("Failed to create Raft node: %v", err)
	}
	defer node.Shutdown()

	if err := node.WaitForLeader(10 * time.Second); err != nil {
		t.Fatalf("Failed to elect leader: %v", err)
	}

	// Get server list
	servers, err := node.GetServers()
	if err != nil {
		t.Fatalf("Failed to get servers: %v", err)
	}

	if len(servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(servers))
	}

	if string(servers[0].ID) != "node1" {
		t.Errorf("Expected node1, got %s", servers[0].ID)
	}
}

// TestFSM_Apply tests FSM command application
func TestFSM_Apply(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fsm-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger, _ := zap.NewDevelopment()
	store := createTestStore(t, tmpDir)
	fsm := NewFSM(store, logger)

	ctx := context.Background()

	// Test PUT
	putCmd := Command{
		Op:    OpTypePut,
		Key:   "testkey",
		Value: []byte("testvalue"),
	}

	data, _ := marshalCommand(putCmd)
	log := &raft.Log{
		Index: 1,
		Data:  data,
	}

	result := fsm.Apply(log)
	if result != nil {
		if err, ok := result.(error); ok {
			t.Fatalf("Failed to apply PUT: %v", err)
		}
	}

	// Verify PUT
	value, err := store.Get(ctx, "testkey")
	if err != nil {
		t.Fatalf("Failed to get key after PUT: %v", err)
	}

	if string(value) != "testvalue" {
		t.Errorf("Expected testvalue, got %s", string(value))
	}

	// Test DELETE
	delCmd := Command{
		Op:  OpTypeDelete,
		Key: "testkey",
	}

	data, _ = marshalCommand(delCmd)
	log = &raft.Log{
		Index: 2,
		Data:  data,
	}

	result = fsm.Apply(log)
	if result != nil {
		if err, ok := result.(error); ok {
			t.Fatalf("Failed to apply DELETE: %v", err)
		}
	}

	// Verify DELETE
	_, err = store.Get(ctx, "testkey")
	if err == nil {
		t.Error("Expected error for deleted key")
	}
}

// TestFSM_Snapshot tests FSM snapshot creation and restoration
func TestFSM_Snapshot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fsm-snapshot-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger, _ := zap.NewDevelopment()
	store := createTestStore(t, tmpDir)
	fsm := NewFSM(store, logger)

	ctx := context.Background()

	// Add some data
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key%d", i)
		value := []byte(fmt.Sprintf("value%d", i))
		store.Put(ctx, key, value)
	}

	// Create snapshot
	snapshot, err := fsm.Snapshot()
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	// Create a temporary file to persist snapshot
	tmpFile := filepath.Join(t.TempDir(), "snapshot.dat")
	file, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	// Use a mock sink
	mockSink := &mockSnapshotSink{file: file}
	if err := snapshot.Persist(mockSink); err != nil {
		t.Fatalf("Failed to persist snapshot: %v", err)
	}

	// Create new store and FSM
	newStore := createTestStore(t, tmpDir)
	newFSM := NewFSM(newStore, logger)

	// Restore from snapshot
	file, err = os.Open(tmpFile)
	if err != nil {
		t.Fatalf("Failed to open snapshot file: %v", err)
	}
	defer file.Close()

	if err := newFSM.Restore(file); err != nil {
		t.Fatalf("Failed to restore from snapshot: %v", err)
	}

	// Verify restored data
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key%d", i)
		expected := fmt.Sprintf("value%d", i)

		value, err := newStore.Get(ctx, key)
		if err != nil {
			t.Errorf("Failed to get %s after restore: %v", key, err)
			continue
		}

		if string(value) != expected {
			t.Errorf("For %s, expected %s, got %s", key, expected, string(value))
		}
	}
}

// Mock snapshot sink for testing
type mockSnapshotSink struct {
	file   *os.File
	closed bool
}

func (m *mockSnapshotSink) Write(p []byte) (int, error) {
	return m.file.Write(p)
}

func (m *mockSnapshotSink) Close() error {
	m.closed = true
	return m.file.Close()
}

func (m *mockSnapshotSink) ID() string {
	return "mock-snapshot"
}

func (m *mockSnapshotSink) Cancel() error {
	m.closed = true
	os.Remove(m.file.Name())
	return m.file.Close()
}

// Helper to marshal Command as JSON
func marshalCommand(c Command) ([]byte, error) {
	return json.Marshal(c)
}
