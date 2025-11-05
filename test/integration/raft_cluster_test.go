package integration_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/RashikAnsar/raftkv/internal/consensus"
	"github.com/RashikAnsar/raftkv/internal/storage"
)

// Helper function to get a free port
func getFreePort(t *testing.T) string {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()
	return addr
}

// Helper function to create DurableStore for tests
func createTestStore(t *testing.T, dir string) *storage.DurableStore {
	store, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       filepath.Join(dir, "store"),
		SyncOnWrite:   false,
		SnapshotEvery: 1000,
	})
	if err != nil {
		t.Fatalf("Failed to create DurableStore: %v", err)
	}
	return store
}

// TestCluster represents a test cluster of Raft nodes
type TestCluster struct {
	nodes   []*consensus.RaftNode
	stores  []*storage.DurableStore
	dirs    []string
	logger  *zap.Logger
	t       *testing.T
}

// NewTestCluster creates a new test cluster with the specified number of nodes
func NewTestCluster(t *testing.T, nodeCount int) *TestCluster {
	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	cluster := &TestCluster{
		nodes:  make([]*consensus.RaftNode, nodeCount),
		stores: make([]*storage.DurableStore, nodeCount),
		dirs:   make([]string, nodeCount),
		logger: logger,
		t:      t,
	}

	// Create nodes
	for i := 0; i < nodeCount; i++ {
		tmpDir, err := os.MkdirTemp("", fmt.Sprintf("raft-cluster-node%d-*", i))
		if err != nil {
			t.Fatalf("Failed to create temp dir for node %d: %v", i, err)
		}
		cluster.dirs[i] = tmpDir

		store := createTestStore(t, tmpDir)
		cluster.stores[i] = store

		raftAddr := getFreePort(t)

		node, err := consensus.NewRaftNode(consensus.RaftConfig{
			NodeID:    fmt.Sprintf("node%d", i),
			RaftAddr:  raftAddr,
			RaftDir:   filepath.Join(tmpDir, "raft"),
			Bootstrap: i == 0, // Only bootstrap the first node
			Store:     store,
			Logger:    logger,
		})
		if err != nil {
			t.Fatalf("Failed to create node %d: %v", i, err)
		}
		cluster.nodes[i] = node
	}

	return cluster
}

// WaitForLeader waits for a leader to be elected in the cluster
func (tc *TestCluster) WaitForLeader(timeout time.Duration) (*consensus.RaftNode, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		for _, node := range tc.nodes {
			if node.IsLeader() {
				return node, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	return nil, fmt.Errorf("no leader elected within timeout")
}

// GetLeader returns the current leader node
func (tc *TestCluster) GetLeader() *consensus.RaftNode {
	for _, node := range tc.nodes {
		if node.IsLeader() {
			return node
		}
	}
	return nil
}

// GetFollowers returns all follower nodes
func (tc *TestCluster) GetFollowers() []*consensus.RaftNode {
	followers := make([]*consensus.RaftNode, 0)
	for _, node := range tc.nodes {
		if !node.IsLeader() {
			followers = append(followers, node)
		}
	}
	return followers
}

// JoinNodes joins nodes to the cluster (must be called after bootstrap node is ready)
func (tc *TestCluster) JoinNodes() error {
	// Wait for bootstrap node to become leader
	leader, err := tc.WaitForLeader(10 * time.Second)
	if err != nil {
		return fmt.Errorf("bootstrap node failed to become leader: %w", err)
	}

	// Join remaining nodes
	for i := 1; i < len(tc.nodes); i++ {
		node := tc.nodes[i]
		nodeID := fmt.Sprintf("node%d", i)

		if err := leader.Join(nodeID, node.RaftAddr); err != nil {
			return fmt.Errorf("failed to join node %d: %w", i, err)
		}

		// Wait a bit for the node to join
		time.Sleep(500 * time.Millisecond)
	}

	return nil
}

// Shutdown shuts down all nodes in the cluster
func (tc *TestCluster) Shutdown() {
	for _, node := range tc.nodes {
		if node != nil {
			node.Shutdown()
		}
	}

	for _, dir := range tc.dirs {
		os.RemoveAll(dir)
	}
}

// VerifyDataConsistency verifies that all nodes have the same data
func (tc *TestCluster) VerifyDataConsistency(ctx context.Context, key string, expectedValue []byte) error {
	// Wait a bit for replication
	time.Sleep(500 * time.Millisecond)

	for i, node := range tc.nodes {
		value, err := node.Get(ctx, key)
		if err != nil {
			return fmt.Errorf("node %d failed to get key %s: %w", i, key, err)
		}

		if string(value) != string(expectedValue) {
			return fmt.Errorf("node %d has inconsistent data: expected %s, got %s",
				i, string(expectedValue), string(value))
		}
	}

	return nil
}

// TestThreeNodeCluster_BasicOperations tests basic operations on a 3-node cluster
func TestThreeNodeCluster_BasicOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cluster test in short mode")
	}

	cluster := NewTestCluster(t, 3)
	defer cluster.Shutdown()

	// Join nodes to cluster
	if err := cluster.JoinNodes(); err != nil {
		t.Fatalf("Failed to join nodes: %v", err)
	}

	// Wait for stable leader
	leader, err := cluster.WaitForLeader(15 * time.Second)
	if err != nil {
		t.Fatalf("No leader elected: %v", err)
	}

	t.Logf("Leader elected: %s", leader.NodeID)

	// Verify we have 3 servers in the cluster
	servers, err := leader.GetServers()
	if err != nil {
		t.Fatalf("Failed to get servers: %v", err)
	}

	if len(servers) != 3 {
		t.Errorf("Expected 3 servers, got %d", len(servers))
	}

	ctx := context.Background()

	// Test PUT operation on leader
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "test-key",
		Value: []byte("test-value"),
	}

	if err := leader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to apply PUT command: %v", err)
	}

	// Verify data consistency across all nodes
	if err := cluster.VerifyDataConsistency(ctx, "test-key", []byte("test-value")); err != nil {
		t.Errorf("Data consistency check failed: %v", err)
	}

	// Test multiple operations
	for i := 0; i < 10; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("key-%d", i),
			Value: []byte(fmt.Sprintf("value-%d", i)),
		}

		if err := leader.Apply(cmd, 5*time.Second); err != nil {
			t.Fatalf("Failed to apply PUT command %d: %v", i, err)
		}
	}

	// Wait for replication
	time.Sleep(1 * time.Second)

	// Verify all keys on all nodes
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key-%d", i)
		expectedValue := []byte(fmt.Sprintf("value-%d", i))

		if err := cluster.VerifyDataConsistency(ctx, key, expectedValue); err != nil {
			t.Errorf("Consistency check failed for %s: %v", key, err)
		}
	}

	// Test read from follower
	followers := cluster.GetFollowers()
	if len(followers) > 0 {
		follower := followers[0]
		value, err := follower.Get(ctx, "test-key")
		if err != nil {
			t.Errorf("Failed to read from follower: %v", err)
		} else if string(value) != "test-value" {
			t.Errorf("Follower has wrong value: expected test-value, got %s", string(value))
		}
	}

	// Test DELETE operation
	delCmd := consensus.Command{
		Op:  consensus.OpTypeDelete,
		Key: "test-key",
	}

	if err := leader.Apply(delCmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to apply DELETE command: %v", err)
	}

	// Wait for replication
	time.Sleep(500 * time.Millisecond)

	// Verify key is deleted on all nodes
	for i, node := range cluster.nodes {
		_, err := node.Get(ctx, "test-key")
		if err == nil {
			t.Errorf("Node %d still has deleted key", i)
		}
	}
}

// TestFiveNodeCluster_Operations tests operations on a 5-node cluster for stronger consensus
func TestFiveNodeCluster_Operations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cluster test in short mode")
	}

	cluster := NewTestCluster(t, 5)
	defer cluster.Shutdown()

	// Join nodes to cluster
	if err := cluster.JoinNodes(); err != nil {
		t.Fatalf("Failed to join nodes: %v", err)
	}

	// Wait for stable leader
	leader, err := cluster.WaitForLeader(15 * time.Second)
	if err != nil {
		t.Fatalf("No leader elected: %v", err)
	}

	t.Logf("Leader elected in 5-node cluster: %s", leader.NodeID)

	// Verify we have 5 servers
	servers, err := leader.GetServers()
	if err != nil {
		t.Fatalf("Failed to get servers: %v", err)
	}

	if len(servers) != 5 {
		t.Errorf("Expected 5 servers, got %d", len(servers))
	}

	ctx := context.Background()

	// Test batch operations
	batchSize := 50
	for i := 0; i < batchSize; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("batch-key-%d", i),
			Value: []byte(fmt.Sprintf("batch-value-%d", i)),
		}

		if err := leader.Apply(cmd, 5*time.Second); err != nil {
			t.Fatalf("Failed to apply batch PUT %d: %v", i, err)
		}
	}

	// Wait for replication
	time.Sleep(2 * time.Second)

	// Verify random keys across all nodes
	testKeys := []int{0, 10, 25, 40, 49}
	for _, i := range testKeys {
		key := fmt.Sprintf("batch-key-%d", i)
		expectedValue := []byte(fmt.Sprintf("batch-value-%d", i))

		if err := cluster.VerifyDataConsistency(ctx, key, expectedValue); err != nil {
			t.Errorf("Consistency check failed for %s: %v", key, err)
		}
	}

	// Verify list operation returns correct count
	keys, err := leader.List(ctx, "batch-key-", 100)
	if err != nil {
		t.Fatalf("Failed to list keys: %v", err)
	}

	if len(keys) != batchSize {
		t.Errorf("Expected %d keys, got %d", batchSize, len(keys))
	}
}

// TestCluster_ConcurrentWrites tests concurrent writes to the leader
func TestCluster_ConcurrentWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cluster test in short mode")
	}

	cluster := NewTestCluster(t, 3)
	defer cluster.Shutdown()

	if err := cluster.JoinNodes(); err != nil {
		t.Fatalf("Failed to join nodes: %v", err)
	}

	leader, err := cluster.WaitForLeader(15 * time.Second)
	if err != nil {
		t.Fatalf("No leader elected: %v", err)
	}

	// Test concurrent writes
	concurrency := 10
	writesPerGoroutine := 5

	done := make(chan error, concurrency)

	for g := 0; g < concurrency; g++ {
		go func(goroutineID int) {
			for i := 0; i < writesPerGoroutine; i++ {
				cmd := consensus.Command{
					Op:    consensus.OpTypePut,
					Key:   fmt.Sprintf("concurrent-g%d-k%d", goroutineID, i),
					Value: []byte(fmt.Sprintf("value-g%d-k%d", goroutineID, i)),
				}

				if err := leader.Apply(cmd, 10*time.Second); err != nil {
					done <- fmt.Errorf("goroutine %d, write %d failed: %w", goroutineID, i, err)
					return
				}
			}
			done <- nil
		}(g)
	}

	// Wait for all goroutines
	for i := 0; i < concurrency; i++ {
		if err := <-done; err != nil {
			t.Errorf("Concurrent write failed: %v", err)
		}
	}

	// Wait for replication
	time.Sleep(2 * time.Second)

	// Verify some random keys
	ctx := context.Background()
	testCases := []struct {
		goroutineID int
		keyID       int
	}{
		{0, 0},
		{5, 2},
		{9, 4},
	}

	for _, tc := range testCases {
		key := fmt.Sprintf("concurrent-g%d-k%d", tc.goroutineID, tc.keyID)
		expectedValue := []byte(fmt.Sprintf("value-g%d-k%d", tc.goroutineID, tc.keyID))

		if err := cluster.VerifyDataConsistency(ctx, key, expectedValue); err != nil {
			t.Errorf("Consistency check failed for %s: %v", key, err)
		}
	}

	// Verify total count
	totalKeys := concurrency * writesPerGoroutine
	keys, err := leader.List(ctx, "concurrent-", totalKeys*2)
	if err != nil {
		t.Fatalf("Failed to list keys: %v", err)
	}

	if len(keys) != totalKeys {
		t.Errorf("Expected %d keys, got %d", totalKeys, len(keys))
	}
}

// TestCluster_Stats tests that statistics are consistent across the cluster
func TestCluster_Stats(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cluster test in short mode")
	}

	cluster := NewTestCluster(t, 3)
	defer cluster.Shutdown()

	if err := cluster.JoinNodes(); err != nil {
		t.Fatalf("Failed to join nodes: %v", err)
	}

	leader, err := cluster.WaitForLeader(15 * time.Second)
	if err != nil {
		t.Fatalf("No leader elected: %v", err)
	}

	// Write some data
	for i := 0; i < 20; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("stats-key-%d", i),
			Value: []byte(fmt.Sprintf("stats-value-%d", i)),
		}

		if err := leader.Apply(cmd, 5*time.Second); err != nil {
			t.Fatalf("Failed to apply PUT: %v", err)
		}
	}

	// Wait for replication
	time.Sleep(1 * time.Second)

	// Check stats on all nodes
	for i, node := range cluster.nodes {
		stats := node.Stats()

		if stats.KeyCount != 20 {
			t.Errorf("Node %d has wrong key count: expected 20, got %d", i, stats.KeyCount)
		}

		if stats.Puts != 20 {
			t.Errorf("Node %d has wrong put count: expected 20, got %d", i, stats.Puts)
		}
	}

	// Test delete stats
	for i := 0; i < 5; i++ {
		cmd := consensus.Command{
			Op:  consensus.OpTypeDelete,
			Key: fmt.Sprintf("stats-key-%d", i),
		}

		if err := leader.Apply(cmd, 5*time.Second); err != nil {
			t.Fatalf("Failed to apply DELETE: %v", err)
		}
	}

	// Wait for replication
	time.Sleep(1 * time.Second)

	// Verify updated stats
	for i, node := range cluster.nodes {
		stats := node.Stats()

		if stats.KeyCount != 15 {
			t.Errorf("Node %d has wrong key count after deletes: expected 15, got %d", i, stats.KeyCount)
		}

		if stats.Deletes != 5 {
			t.Errorf("Node %d has wrong delete count: expected 5, got %d", i, stats.Deletes)
		}
	}
}
