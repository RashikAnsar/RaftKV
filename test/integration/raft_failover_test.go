package integration_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/RashikAnsar/raftkv/internal/consensus"
)

// TestCluster_LeaderElection tests that a leader is elected in a fresh cluster
func TestCluster_LeaderElection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cluster test in short mode")
	}

	cluster := NewTestCluster(t, 3)
	defer cluster.Shutdown()

	if err := cluster.JoinNodes(); err != nil {
		t.Fatalf("Failed to join nodes: %v", err)
	}

	// Wait for leader election
	leader, err := cluster.WaitForLeader(15 * time.Second)
	if err != nil {
		t.Fatalf("No leader elected: %v", err)
	}

	t.Logf("Leader elected: %s", leader.NodeID)

	// Verify only one leader
	leaderCount := 0
	for _, node := range cluster.nodes {
		if node.IsLeader() {
			leaderCount++
		}
	}

	if leaderCount != 1 {
		t.Errorf("Expected exactly 1 leader, found %d", leaderCount)
	}

	// Verify all nodes agree on the leader
	leaderAddr, leaderID := leader.GetLeader()
	t.Logf("Leader address: %s, ID: %s", leaderAddr, leaderID)

	for i, node := range cluster.nodes {
		nodeLeaderAddr, nodeLeaderID := node.GetLeader()
		if nodeLeaderAddr != leaderAddr || nodeLeaderID != leaderID {
			t.Errorf("Node %d disagrees on leader: got (%s, %s), expected (%s, %s)",
				i, nodeLeaderAddr, nodeLeaderID, leaderAddr, leaderID)
		}
	}
}

// TestCluster_LeaderFailover tests that a new leader is elected when the current leader fails
func TestCluster_LeaderFailover(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cluster test in short mode")
	}

	cluster := NewTestCluster(t, 3)
	defer cluster.Shutdown()

	if err := cluster.JoinNodes(); err != nil {
		t.Fatalf("Failed to join nodes: %v", err)
	}

	// Wait for initial leader
	leader, err := cluster.WaitForLeader(20 * time.Second)
	if err != nil {
		t.Fatalf("No initial leader elected: %v", err)
	}

	initialLeaderID := leader.NodeID
	t.Logf("Initial leader: %s", initialLeaderID)

	// Write some data to the cluster
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("pre-failover-key-%d", i),
			Value: []byte(fmt.Sprintf("pre-failover-value-%d", i)),
		}

		if err := leader.Apply(cmd, 5*time.Second); err != nil {
			t.Fatalf("Failed to apply PUT before failover: %v", err)
		}
	}

	// Wait for replication
	time.Sleep(1 * time.Second)

	// Shut down the leader
	t.Logf("Shutting down leader: %s", initialLeaderID)
	leader.Shutdown()

	// Wait for new leader election
	time.Sleep(2 * time.Second)

	// Find the new leader
	var newLeader *consensus.RaftNode
	deadline := time.Now().Add(20 * time.Second)

	for time.Now().Before(deadline) {
		for _, node := range cluster.nodes {
			if node.NodeID != initialLeaderID && node.IsLeader() {
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
		t.Fatalf("No new leader elected after original leader shutdown")
	}

	t.Logf("New leader elected: %s", newLeader.NodeID)

	// Verify the new leader is different from the old one
	if newLeader.NodeID == initialLeaderID {
		t.Error("New leader should be different from the old leader")
	}

	// Verify old data is still accessible
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("pre-failover-key-%d", i)
		value, err := newLeader.Get(ctx, key)
		if err != nil {
			t.Errorf("Failed to get pre-failover key %s from new leader: %v", key, err)
			continue
		}

		expectedValue := fmt.Sprintf("pre-failover-value-%d", i)
		if string(value) != expectedValue {
			t.Errorf("For key %s, expected %s, got %s", key, expectedValue, string(value))
		}
	}

	// Write new data to the new leader
	for i := 0; i < 10; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("post-failover-key-%d", i),
			Value: []byte(fmt.Sprintf("post-failover-value-%d", i)),
		}

		if err := newLeader.Apply(cmd, 5*time.Second); err != nil {
			t.Fatalf("Failed to apply PUT after failover: %v", err)
		}
	}

	// Wait for replication
	time.Sleep(1 * time.Second)

	// Wait for data to be replicated and available
	time.Sleep(2 * time.Second)

	// Verify new data on remaining nodes
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("post-failover-key-%d", i)
		expectedValue := []byte(fmt.Sprintf("post-failover-value-%d", i))

		for j, node := range cluster.nodes {
			if node.NodeID == initialLeaderID {
				continue // Skip the shutdown node
			}

			value, err := node.Get(ctx, key)
			if err != nil {
				t.Errorf("Node %d failed to get post-failover key %s: %v", j, key, err)
				continue
			}

			if string(value) != string(expectedValue) {
				t.Errorf("Node %d has wrong value for %s: expected %s, got %s",
					j, key, string(expectedValue), string(value))
			}
		}
	}
}

// TestCluster_MultipleFailovers tests multiple consecutive leader failovers
func TestCluster_MultipleFailovers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cluster test in short mode")
	}

	cluster := NewTestCluster(t, 5)
	defer cluster.Shutdown()

	if err := cluster.JoinNodes(); err != nil {
		t.Fatalf("Failed to join nodes: %v", err)
	}

	ctx := context.Background()
	shutdownNodeIDs := make(map[string]bool)

	// Perform 2 consecutive failovers
	for failoverRound := 0; failoverRound < 2; failoverRound++ {
		// Wait for leader
		leader, err := cluster.WaitForLeader(20 * time.Second)
		if err != nil {
			t.Fatalf("Round %d: No leader elected: %v", failoverRound, err)
		}

		leaderID := leader.NodeID
		t.Logf("Round %d: Current leader: %s", failoverRound, leaderID)

		// Write data
		key := fmt.Sprintf("failover-round-%d-key", failoverRound)
		value := []byte(fmt.Sprintf("failover-round-%d-value", failoverRound))

		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   key,
			Value: value,
		}

		if err := leader.Apply(cmd, 5*time.Second); err != nil {
			t.Fatalf("Round %d: Failed to apply PUT: %v", failoverRound, err)
		}

		// Wait for replication
		time.Sleep(2 * time.Second)

		// Verify data on all active nodes before failover
		for _, node := range cluster.nodes {
			// Skip nodes that have been shut down
			if shutdownNodeIDs[node.NodeID] {
				continue
			}

			val, err := node.Get(ctx, key)
			if err != nil {
				t.Errorf("Round %d: Node %s failed to get key before failover: %v",
					failoverRound, node.NodeID, err)
				continue
			}

			if string(val) != string(value) {
				t.Errorf("Round %d: Node %s has wrong value before failover: expected %s, got %s",
					failoverRound, node.NodeID, string(value), string(val))
			}
		}

		// Shut down the leader
		t.Logf("Round %d: Shutting down leader: %s", failoverRound, leaderID)
		leader.Shutdown()
		shutdownNodeIDs[leaderID] = true

		// Wait for new leader election
		time.Sleep(5 * time.Second)
	}

	// After 2 failovers, we should still have 3 nodes (5 - 2 = 3)
	// Find the current leader
	newLeader, err := cluster.WaitForLeader(20 * time.Second)
	if err != nil {
		t.Fatalf("No leader after multiple failovers: %v", err)
	}

	t.Logf("Final leader after multiple failovers: %s", newLeader.NodeID)

	// Wait for data replication
	time.Sleep(2 * time.Second)

	// Verify all historical data is still accessible
	for failoverRound := 0; failoverRound < 2; failoverRound++ {
		key := fmt.Sprintf("failover-round-%d-key", failoverRound)
		expectedValue := fmt.Sprintf("failover-round-%d-value", failoverRound)

		value, err := newLeader.Get(ctx, key)
		if err != nil {
			t.Errorf("Failed to get key from round %d: %v", failoverRound, err)
			continue
		}

		if string(value) != expectedValue {
			t.Errorf("Wrong value from round %d: expected %s, got %s",
				failoverRound, expectedValue, string(value))
		}
	}

	// Write new data to verify cluster is still functional
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "post-multiple-failovers-key",
		Value: []byte("post-multiple-failovers-value"),
	}

	if err := newLeader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to write after multiple failovers: %v", err)
	}

	// Wait for replication
	time.Sleep(2 * time.Second)

	// Verify the new write
	value, err := newLeader.Get(ctx, "post-multiple-failovers-key")
	if err != nil {
		t.Fatalf("Failed to read after multiple failovers: %v", err)
	}

	if string(value) != "post-multiple-failovers-value" {
		t.Errorf("Wrong value after multiple failovers: expected post-multiple-failovers-value, got %s",
			string(value))
	}
}

// TestCluster_FollowerFailure tests that the cluster continues operating when a follower fails
func TestCluster_FollowerFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cluster test in short mode")
	}

	cluster := NewTestCluster(t, 3)
	defer cluster.Shutdown()

	if err := cluster.JoinNodes(); err != nil {
		t.Fatalf("Failed to join nodes: %v", err)
	}

	// Wait for leader
	leader, err := cluster.WaitForLeader(20 * time.Second)
	if err != nil {
		t.Fatalf("No leader elected: %v", err)
	}

	t.Logf("Leader: %s", leader.NodeID)

	// Find a follower to shut down
	var followerToShutdown *consensus.RaftNode
	for _, node := range cluster.nodes {
		if !node.IsLeader() {
			followerToShutdown = node
			break
		}
	}

	if followerToShutdown == nil {
		t.Fatal("No follower found to shut down")
	}

	followerID := followerToShutdown.NodeID
	t.Logf("Shutting down follower: %s", followerID)

	// Write some data before follower shutdown
	ctx := context.Background()
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "before-follower-shutdown",
		Value: []byte("value-before-shutdown"),
	}

	if err := leader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to write before follower shutdown: %v", err)
	}

	// Shut down the follower
	followerToShutdown.Shutdown()

	// Wait for cluster to stabilize
	time.Sleep(2 * time.Second)

	// Verify leader is still the same
	if !leader.IsLeader() {
		t.Error("Leader changed after follower shutdown (it shouldn't)")
	}

	// Write data after follower shutdown
	cmd = consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "after-follower-shutdown",
		Value: []byte("value-after-shutdown"),
	}

	if err := leader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to write after follower shutdown: %v", err)
	}

	// Wait for replication to remaining nodes
	time.Sleep(2 * time.Second)

	// Verify data on remaining nodes
	for _, node := range cluster.nodes {
		if node.NodeID == followerID {
			continue // Skip the shutdown node
		}

		value, err := node.Get(ctx, "after-follower-shutdown")
		if err != nil {
			t.Errorf("Node %s failed to get key after follower shutdown: %v", node.NodeID, err)
			continue
		}

		if string(value) != "value-after-shutdown" {
			t.Errorf("Node %s has wrong value: expected value-after-shutdown, got %s",
				node.NodeID, string(value))
		}
	}
}

// TestCluster_MinorityFailure tests that cluster continues when minority of nodes fail
func TestCluster_MinorityFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cluster test in short mode")
	}

	cluster := NewTestCluster(t, 5)
	defer cluster.Shutdown()

	if err := cluster.JoinNodes(); err != nil {
		t.Fatalf("Failed to join nodes: %v", err)
	}

	// Wait for leader
	leader, err := cluster.WaitForLeader(20 * time.Second)
	if err != nil {
		t.Fatalf("No leader elected: %v", err)
	}

	t.Logf("Initial leader: %s", leader.NodeID)

	ctx := context.Background()

	// Write initial data
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "before-minority-failure",
		Value: []byte("initial-value"),
	}

	if err := leader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to write initial data: %v", err)
	}

	// Shut down 2 nodes (minority in a 5-node cluster)
	shutdownNodeIDs := make(map[string]bool)
	shutdownCount := 0
	for _, node := range cluster.nodes {
		if shutdownCount >= 2 {
			break
		}

		// Don't shut down the leader initially
		if !node.IsLeader() {
			t.Logf("Shutting down node: %s", node.NodeID)
			node.Shutdown()
			shutdownNodeIDs[node.NodeID] = true
			shutdownCount++
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Wait for cluster to stabilize
	time.Sleep(5 * time.Second)

	// Find current leader (may have changed)
	currentLeader, err := cluster.WaitForLeader(20 * time.Second)
	if err != nil {
		t.Fatalf("No leader after minority failure: %v", err)
	}

	t.Logf("Current leader after minority failure: %s", currentLeader.NodeID)

	// Write new data
	cmd = consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "after-minority-failure",
		Value: []byte("value-after-failure"),
	}

	if err := currentLeader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to write after minority failure: %v", err)
	}

	// Wait for replication
	time.Sleep(3 * time.Second)

	// Verify data on remaining nodes
	activeNodeCount := 0
	for _, node := range cluster.nodes {
		// Skip nodes we explicitly shut down
		if shutdownNodeIDs[node.NodeID] {
			continue
		}

		activeNodeCount++

		// Verify old key value
		testValue, err := node.Get(ctx, "before-minority-failure")
		if err != nil {
			t.Errorf("Node %s failed to get old key: %v", node.NodeID, err)
		} else if string(testValue) != "initial-value" {
			t.Errorf("Node %s has wrong old value: expected initial-value, got %s",
				node.NodeID, string(testValue))
		}

		// Verify new key value
		value, err := node.Get(ctx, "after-minority-failure")
		if err != nil {
			t.Errorf("Node %s failed to get new key: %v", node.NodeID, err)
		} else if string(value) != "value-after-failure" {
			t.Errorf("Node %s has wrong new value: expected value-after-failure, got %s",
				node.NodeID, string(value))
		}
	}

	if activeNodeCount != 3 {
		t.Errorf("Expected 3 active nodes, found %d", activeNodeCount)
	}
}

// TestCluster_QuorumLoss tests that cluster becomes unavailable when majority of nodes fail
func TestCluster_QuorumLoss(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cluster test in short mode")
	}

	cluster := NewTestCluster(t, 3)
	defer cluster.Shutdown()

	if err := cluster.JoinNodes(); err != nil {
		t.Fatalf("Failed to join nodes: %v", err)
	}

	// Wait for leader
	leader, err := cluster.WaitForLeader(20 * time.Second)
	if err != nil {
		t.Fatalf("No leader elected: %v", err)
	}

	t.Logf("Initial leader: %s", leader.NodeID)

	// Write initial data
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "before-quorum-loss",
		Value: []byte("initial-value"),
	}

	if err := leader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to write initial data: %v", err)
	}

	// Shut down 2 nodes (majority in a 3-node cluster)
	shutdownCount := 0
	var remainingNode *consensus.RaftNode

	for _, node := range cluster.nodes {
		if shutdownCount < 2 {
			t.Logf("Shutting down node: %s", node.NodeID)
			node.Shutdown()
			shutdownCount++
		} else {
			remainingNode = node
		}
	}

	if remainingNode == nil {
		t.Fatal("No remaining node found")
	}

	// Wait for cluster to realize quorum is lost
	time.Sleep(3 * time.Second)

	// Verify remaining node is not a leader (can't achieve quorum)
	if remainingNode.IsLeader() {
		t.Error("Remaining node should not be leader after quorum loss")
	}

	// Try to write - this should timeout or fail
	cmd = consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "after-quorum-loss",
		Value: []byte("should-fail"),
	}

	// Use a short timeout since we expect this to fail
	err = remainingNode.Apply(cmd, 2*time.Second)
	if err == nil {
		t.Error("Write should fail after quorum loss, but it succeeded")
	} else {
		t.Logf("Expected error after quorum loss: %v", err)
	}

	// Reads should still work (stale reads from local state)
	ctx := context.Background()
	value, err := remainingNode.Get(ctx, "before-quorum-loss")
	if err != nil {
		t.Logf("Note: Read failed (expected behavior): %v", err)
		// This is acceptable - the node may refuse reads without quorum
	} else if string(value) != "initial-value" {
		t.Errorf("Wrong value: expected initial-value, got %s", string(value))
	} else {
		t.Logf("Stale read succeeded (expected behavior): %s", string(value))
	}
}
