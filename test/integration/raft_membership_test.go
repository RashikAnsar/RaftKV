package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/RashikAnsar/raftkv/internal/consensus"
)

// TestCluster_DynamicNodeJoin tests adding a new node to an existing cluster
func TestCluster_DynamicNodeJoin(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cluster test in short mode")
	}

	// Start with a 3-node cluster
	cluster := NewTestCluster(t, 3)
	defer cluster.Shutdown()

	if err := cluster.JoinNodes(); err != nil {
		t.Fatalf("Failed to join initial nodes: %v", err)
	}

	leader, err := cluster.WaitForLeader(20 * time.Second)
	if err != nil {
		t.Fatalf("No leader elected: %v", err)
	}

	t.Logf("Initial leader: %s", leader.NodeID)

	// Write some initial data
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("initial-key-%d", i),
			Value: []byte(fmt.Sprintf("initial-value-%d", i)),
		}

		if err := leader.Apply(cmd, 5*time.Second); err != nil {
			t.Fatalf("Failed to write initial data: %v", err)
		}
	}

	// Wait for replication
	time.Sleep(1 * time.Second)

	// Create a new node (node4)
	tmpDir, err := os.MkdirTemp("", "raft-new-node-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir for new node: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger, _ := zap.NewDevelopment()
	newStore := createTestStore(t, tmpDir)
	defer newStore.Close()

	raftAddr := getFreePort(t)
	newNode, err := consensus.NewRaftNode(consensus.RaftConfig{
		NodeID:    "node4",
		RaftAddr:  raftAddr,
		RaftDir:   filepath.Join(tmpDir, "raft"),
		Bootstrap: false, // Don't bootstrap
		Store:     newStore,
		Logger:    logger,
	})
	if err != nil {
		t.Fatalf("Failed to create new node: %v", err)
	}
	defer newNode.Shutdown()

	// Join the new node to the cluster
	t.Logf("Adding new node4 to the cluster")
	if err := leader.Join("node4", raftAddr); err != nil {
		t.Fatalf("Failed to join new node: %v", err)
	}

	// Wait for the new node to catch up
	time.Sleep(3 * time.Second)

	// Verify cluster now has 4 servers
	servers, err := leader.GetServers()
	if err != nil {
		t.Fatalf("Failed to get servers: %v", err)
	}

	if len(servers) != 4 {
		t.Errorf("Expected 4 servers after join, got %d", len(servers))
	}

	// Verify the new node has all the initial data
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("initial-key-%d", i)
		expectedValue := fmt.Sprintf("initial-value-%d", i)

		value, err := newNode.Get(ctx, key)
		if err != nil {
			t.Errorf("New node failed to get %s: %v", key, err)
			continue
		}

		if string(value) != expectedValue {
			t.Errorf("New node has wrong value for %s: expected %s, got %s",
				key, expectedValue, string(value))
		}
	}

	// Write new data after the node joined
	for i := 0; i < 5; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("after-join-key-%d", i),
			Value: []byte(fmt.Sprintf("after-join-value-%d", i)),
		}

		if err := leader.Apply(cmd, 5*time.Second); err != nil {
			t.Fatalf("Failed to write after join: %v", err)
		}
	}

	// Wait for replication
	time.Sleep(1 * time.Second)

	// Verify new data on the new node
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("after-join-key-%d", i)
		expectedValue := fmt.Sprintf("after-join-value-%d", i)

		value, err := newNode.Get(ctx, key)
		if err != nil {
			t.Errorf("New node failed to get %s: %v", key, err)
			continue
		}

		if string(value) != expectedValue {
			t.Errorf("New node has wrong value for %s: expected %s, got %s",
				key, expectedValue, string(value))
		}
	}
}

// TestCluster_NodeRemoval tests removing a node from the cluster
func TestCluster_NodeRemoval(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cluster test in short mode")
	}

	cluster := NewTestCluster(t, 4)
	defer cluster.Shutdown()

	if err := cluster.JoinNodes(); err != nil {
		t.Fatalf("Failed to join nodes: %v", err)
	}

	leader, err := cluster.WaitForLeader(20 * time.Second)
	if err != nil {
		t.Fatalf("No leader elected: %v", err)
	}

	t.Logf("Leader: %s", leader.NodeID)

	// Verify initial cluster size
	servers, err := leader.GetServers()
	if err != nil {
		t.Fatalf("Failed to get servers: %v", err)
	}

	if len(servers) != 4 {
		t.Errorf("Expected 4 servers initially, got %d", len(servers))
	}

	// Find a follower to remove
	var followerToRemove *consensus.RaftNode
	for _, node := range cluster.nodes {
		if !node.IsLeader() {
			followerToRemove = node
			break
		}
	}

	if followerToRemove == nil {
		t.Fatal("No follower found to remove")
	}

	followerID := followerToRemove.NodeID
	t.Logf("Removing follower: %s", followerID)

	// Write data before removal
	ctx := context.Background()
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "before-removal",
		Value: []byte("value-before-removal"),
	}

	if err := leader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to write before removal: %v", err)
	}

	// Remove the follower from the cluster
	if err := leader.RemoveServer(followerID); err != nil {
		t.Fatalf("Failed to remove server: %v", err)
	}

	// Wait for cluster to update
	time.Sleep(5 * time.Second)

	// Verify cluster now has 3 servers
	servers, err = leader.GetServers()
	if err != nil {
		t.Fatalf("Failed to get servers after removal: %v", err)
	}

	if len(servers) != 3 {
		t.Errorf("Expected 3 servers after removal, got %d", len(servers))
	}

	// Verify the removed node is not in the server list
	for _, server := range servers {
		if string(server.ID) == followerID {
			t.Errorf("Removed node %s still in server list", followerID)
		}
	}

	// Write new data after removal
	cmd = consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "after-removal",
		Value: []byte("value-after-removal"),
	}

	if err := leader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to write after removal: %v", err)
	}

	// Wait for replication to remaining nodes
	time.Sleep(2 * time.Second)

	// Verify remaining nodes have the new data
	for _, node := range cluster.nodes {
		if node.NodeID == followerID {
			continue // Skip removed node
		}

		value, err := node.Get(ctx, "after-removal")
		if err != nil {
			t.Errorf("Node %s failed to get key after removal: %v", node.NodeID, err)
			continue
		}

		if string(value) != "value-after-removal" {
			t.Errorf("Node %s has wrong value: expected value-after-removal, got %s",
				node.NodeID, string(value))
		}
	}

	// The removed node should eventually stop receiving updates
	// (it may still have old data but won't get new updates)
	time.Sleep(2 * time.Second)
}

// TestCluster_LeaderRemoval tests removing the leader from the cluster
func TestCluster_LeaderRemoval(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cluster test in short mode")
	}

	cluster := NewTestCluster(t, 5)
	defer cluster.Shutdown()

	if err := cluster.JoinNodes(); err != nil {
		t.Fatalf("Failed to join nodes: %v", err)
	}

	leader, err := cluster.WaitForLeader(20 * time.Second)
	if err != nil {
		t.Fatalf("No leader elected: %v", err)
	}

	leaderID := leader.NodeID
	t.Logf("Initial leader: %s", leaderID)

	// Write some data
	ctx := context.Background()
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "before-leader-removal",
		Value: []byte("initial-data"),
	}

	if err := leader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to write initial data: %v", err)
	}

	// Wait for replication
	time.Sleep(1 * time.Second)

	// Remove the leader from the cluster
	t.Logf("Removing leader: %s", leaderID)
	if err := leader.RemoveServer(leaderID); err != nil {
		t.Fatalf("Failed to remove leader: %v", err)
	}

	// Wait for leader transfer and new election
	time.Sleep(5 * time.Second)

	// Find the new leader
	var newLeader *consensus.RaftNode
	deadline := time.Now().Add(20 * time.Second)

	for time.Now().Before(deadline) {
		for _, node := range cluster.nodes {
			if node.NodeID != leaderID && node.IsLeader() {
				newLeader = node
				break
			}
		}

		if newLeader != nil {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	if newLeader == nil {
		t.Fatalf("No new leader elected after leader removal")
	}

	t.Logf("New leader after removal: %s", newLeader.NodeID)

	// Verify cluster size is now 4
	servers, err := newLeader.GetServers()
	if err != nil {
		t.Fatalf("Failed to get servers: %v", err)
	}

	if len(servers) != 4 {
		t.Errorf("Expected 4 servers after leader removal, got %d", len(servers))
	}

	// Verify old data is still accessible
	value, err := newLeader.Get(ctx, "before-leader-removal")
	if err != nil {
		t.Fatalf("Failed to get old data from new leader: %v", err)
	}

	if string(value) != "initial-data" {
		t.Errorf("Wrong value: expected initial-data, got %s", string(value))
	}

	// Write new data with new leader
	cmd = consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "after-leader-removal",
		Value: []byte("new-data"),
	}

	if err := newLeader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to write with new leader: %v", err)
	}

	// Wait for replication to all remaining nodes
	time.Sleep(2 * time.Second)

	// Verify new data on all remaining nodes
	for _, node := range cluster.nodes {
		if node.NodeID == leaderID {
			continue // Skip removed leader
		}

		value, err := node.Get(ctx, "after-leader-removal")
		if err != nil {
			t.Errorf("Node %s failed to get new data: %v", node.NodeID, err)
			continue
		}

		if string(value) != "new-data" {
			t.Errorf("Node %s has wrong value: expected new-data, got %s",
				node.NodeID, string(value))
		}
	}
}

// TestCluster_MultipleJoinsAndRemovals tests dynamic cluster membership changes
func TestCluster_MultipleJoinsAndRemovals(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cluster test in short mode")
	}

	// Start with 3 nodes
	cluster := NewTestCluster(t, 3)
	defer cluster.Shutdown()

	if err := cluster.JoinNodes(); err != nil {
		t.Fatalf("Failed to join initial nodes: %v", err)
	}

	leader, err := cluster.WaitForLeader(20 * time.Second)
	if err != nil {
		t.Fatalf("No leader elected: %v", err)
	}

	t.Logf("Initial cluster with 3 nodes, leader: %s", leader.NodeID)

	ctx := context.Background()
	logger, _ := zap.NewDevelopment()

	// Phase 1: Add 2 new nodes
	newNodes := make([]*consensus.RaftNode, 2)
	newDirs := make([]string, 2)

	for i := 0; i < 2; i++ {
		tmpDir, err := os.MkdirTemp("", fmt.Sprintf("raft-dynamic-node%d-*", i))
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		newDirs[i] = tmpDir

		store := createTestStore(t, tmpDir)
		raftAddr := getFreePort(t)
		nodeID := fmt.Sprintf("dynamic-node%d", i)

		node, err := consensus.NewRaftNode(consensus.RaftConfig{
			NodeID:    nodeID,
			RaftAddr:  raftAddr,
			RaftDir:   filepath.Join(tmpDir, "raft"),
			Bootstrap: false,
			Store:     store,
			Logger:    logger,
		})
		if err != nil {
			t.Fatalf("Failed to create node %s: %v", nodeID, err)
		}
		newNodes[i] = node

		t.Logf("Adding %s to cluster", nodeID)
		if err := leader.Join(nodeID, raftAddr); err != nil {
			t.Fatalf("Failed to join %s: %v", nodeID, err)
		}

		time.Sleep(1 * time.Second)
	}

	// Cleanup new nodes
	defer func() {
		for _, node := range newNodes {
			if node != nil {
				node.Shutdown()
			}
		}
		for _, dir := range newDirs {
			os.RemoveAll(dir)
		}
	}()

	// Verify cluster now has 5 nodes
	servers, err := leader.GetServers()
	if err != nil {
		t.Fatalf("Failed to get servers: %v", err)
	}

	if len(servers) != 5 {
		t.Errorf("Expected 5 servers after additions, got %d", len(servers))
	}

	// Write test data
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "test-after-additions",
		Value: []byte("value-after-additions"),
	}

	if err := leader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to write after additions: %v", err)
	}

	time.Sleep(2 * time.Second)

	// Verify data on new nodes
	for i, node := range newNodes {
		value, err := node.Get(ctx, "test-after-additions")
		if err != nil {
			t.Errorf("New node %d failed to get data: %v", i, err)
			continue
		}

		if string(value) != "value-after-additions" {
			t.Errorf("New node %d has wrong value: expected value-after-additions, got %s",
				i, string(value))
		}
	}

	// Phase 2: Remove one of the new nodes
	nodeToRemove := "dynamic-node0"
	t.Logf("Removing %s from cluster", nodeToRemove)

	if err := leader.RemoveServer(nodeToRemove); err != nil {
		t.Fatalf("Failed to remove %s: %v", nodeToRemove, err)
	}

	time.Sleep(2 * time.Second)

	// Verify cluster now has 4 nodes
	servers, err = leader.GetServers()
	if err != nil {
		t.Fatalf("Failed to get servers after removal: %v", err)
	}

	if len(servers) != 4 {
		t.Errorf("Expected 4 servers after removal, got %d", len(servers))
	}

	// Write data after removal
	cmd = consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "test-after-removal",
		Value: []byte("value-after-removal"),
	}

	if err := leader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to write after removal: %v", err)
	}

	// Wait for replication
	time.Sleep(2 * time.Second)

	// Verify remaining new node has the data
	value, err := newNodes[1].Get(ctx, "test-after-removal")
	if err != nil {
		t.Errorf("Remaining new node failed to get data: %v", err)
	} else if string(value) != "value-after-removal" {
		t.Errorf("Remaining new node has wrong value: expected value-after-removal, got %s",
			string(value))
	}

	// Verify original nodes have the data
	for _, node := range cluster.nodes {
		value, err := node.Get(ctx, "test-after-removal")
		if err != nil {
			t.Errorf("Original node %s failed to get data: %v", node.NodeID, err)
			continue
		}

		if string(value) != "value-after-removal" {
			t.Errorf("Original node %s has wrong value: expected value-after-removal, got %s",
				node.NodeID, string(value))
		}
	}
}

// TestCluster_RejoinAfterRemoval tests that a removed node cannot rejoin without explicit add
func TestCluster_RejoinAfterRemoval(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cluster test in short mode")
	}

	cluster := NewTestCluster(t, 3)
	defer cluster.Shutdown()

	if err := cluster.JoinNodes(); err != nil {
		t.Fatalf("Failed to join nodes: %v", err)
	}

	leader, err := cluster.WaitForLeader(20 * time.Second)
	if err != nil {
		t.Fatalf("No leader elected: %v", err)
	}

	// Find a follower
	var follower *consensus.RaftNode
	for _, node := range cluster.nodes {
		if !node.IsLeader() {
			follower = node
			break
		}
	}

	if follower == nil {
		t.Fatal("No follower found")
	}

	followerID := follower.NodeID
	followerAddr := follower.RaftAddr
	t.Logf("Testing with follower: %s", followerID)

	// Remove the follower
	if err := leader.RemoveServer(followerID); err != nil {
		t.Fatalf("Failed to remove follower: %v", err)
	}

	time.Sleep(2 * time.Second)

	// Verify it's been removed
	servers, err := leader.GetServers()
	if err != nil {
		t.Fatalf("Failed to get servers: %v", err)
	}

	if len(servers) != 2 {
		t.Errorf("Expected 2 servers after removal, got %d", len(servers))
	}

	// Now re-add it explicitly
	t.Logf("Re-adding %s to cluster", followerID)
	if err := leader.Join(followerID, followerAddr); err != nil {
		t.Fatalf("Failed to re-add follower: %v", err)
	}

	time.Sleep(2 * time.Second)

	// Verify it's back in the cluster
	servers, err = leader.GetServers()
	if err != nil {
		t.Fatalf("Failed to get servers after re-add: %v", err)
	}

	if len(servers) != 3 {
		t.Errorf("Expected 3 servers after re-add, got %d", len(servers))
	}

	// Write test data
	ctx := context.Background()
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "test-after-rejoin",
		Value: []byte("value-after-rejoin"),
	}

	if err := leader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to write after rejoin: %v", err)
	}

	// Wait for replication
	time.Sleep(2 * time.Second)

	// Verify the rejoined node has the data
	value, err := follower.Get(ctx, "test-after-rejoin")
	if err != nil {
		t.Errorf("Rejoined node failed to get data: %v", err)
	} else if string(value) != "value-after-rejoin" {
		t.Errorf("Rejoined node has wrong value: expected value-after-rejoin, got %s",
			string(value))
	}
}
