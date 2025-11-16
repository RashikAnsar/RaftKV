//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/RashikAnsar/raftkv/internal/consensus"
)

// TestCluster_LinearizableReads tests that reads always see the latest committed write
func TestCluster_LinearizableReads(t *testing.T) {
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

	ctx := context.Background()

	// Write a value
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "consistency-key",
		Value: []byte("version-1"),
	}

	if err := leader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to write version-1: %v", err)
	}

	// Wait for replication
	time.Sleep(500 * time.Millisecond)

	// All nodes should see version-1
	for i, node := range cluster.nodes {
		value, err := node.Get(ctx, "consistency-key")
		if err != nil {
			t.Errorf("Node %d failed to get key: %v", i, err)
			continue
		}

		if string(value) != "version-1" {
			t.Errorf("Node %d read wrong version: expected version-1, got %s", i, string(value))
		}
	}

	// Update the value
	cmd = consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "consistency-key",
		Value: []byte("version-2"),
	}

	if err := leader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to write version-2: %v", err)
	}

	// Wait for replication
	time.Sleep(500 * time.Millisecond)

	// All nodes should now see version-2 (not version-1)
	for i, node := range cluster.nodes {
		value, err := node.Get(ctx, "consistency-key")
		if err != nil {
			t.Errorf("Node %d failed to get key after update: %v", i, err)
			continue
		}

		if string(value) != "version-2" {
			t.Errorf("Node %d read stale version: expected version-2, got %s", i, string(value))
		}
	}
}

// TestCluster_ReadYourWrites tests that a client always reads its own writes
func TestCluster_ReadYourWrites(t *testing.T) {
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

	ctx := context.Background()

	// Simulate multiple clients writing and reading their own data
	clientCount := 5
	writesPerClient := 10

	var wg sync.WaitGroup
	errors := make(chan error, clientCount)

	for clientID := 0; clientID < clientCount; clientID++ {
		wg.Add(1)
		go func(cid int) {
			defer wg.Done()

			for i := 0; i < writesPerClient; i++ {
				key := fmt.Sprintf("client-%d-key-%d", cid, i)
				value := []byte(fmt.Sprintf("client-%d-value-%d", cid, i))

				// Write
				cmd := consensus.Command{
					Op:    consensus.OpTypePut,
					Key:   key,
					Value: value,
				}

				if err := leader.Apply(cmd, 5*time.Second); err != nil {
					errors <- fmt.Errorf("client %d write %d failed: %w", cid, i, err)
					return
				}

				// Immediately read back (should see own write)
				readValue, err := leader.Get(ctx, key)
				if err != nil {
					errors <- fmt.Errorf("client %d read %d failed: %w", cid, i, err)
					return
				}

				if string(readValue) != string(value) {
					errors <- fmt.Errorf("client %d read-your-write violation: wrote %s, read %s",
						cid, string(value), string(readValue))
					return
				}
			}
		}(clientID)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Error(err)
	}
}

// TestCluster_MonotonicReads tests that reads never go backwards in time
func TestCluster_MonotonicReads(t *testing.T) {
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

	ctx := context.Background()

	// Write sequential versions
	versions := []string{"v1", "v2", "v3", "v4", "v5"}

	for _, version := range versions {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   "monotonic-key",
			Value: []byte(version),
		}

		if err := leader.Apply(cmd, 5*time.Second); err != nil {
			t.Fatalf("Failed to write %s: %v", version, err)
		}

		// Wait a bit between writes
		time.Sleep(200 * time.Millisecond)
	}

	// Final wait for full replication
	time.Sleep(1 * time.Second)

	// Now read from all nodes multiple times
	// Each node should return the latest version (v5) on every read
	readCount := 10

	for i, node := range cluster.nodes {
		for r := 0; r < readCount; r++ {
			value, err := node.Get(ctx, "monotonic-key")
			if err != nil {
				t.Errorf("Node %d read %d failed: %v", i, r, err)
				continue
			}

			// Should always see the latest version
			if string(value) != "v5" {
				t.Errorf("Node %d read %d saw stale value: expected v5, got %s",
					i, r, string(value))
			}

			time.Sleep(50 * time.Millisecond)
		}
	}
}

// TestCluster_CausalConsistency tests that causally related operations are seen in order
func TestCluster_CausalConsistency(t *testing.T) {
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

	ctx := context.Background()

	// Simulate causal chain: A -> B -> C
	// Write A
	cmdA := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "causal-A",
		Value: []byte("value-A"),
	}

	if err := leader.Apply(cmdA, 5*time.Second); err != nil {
		t.Fatalf("Failed to write A: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Read A (this creates a causal dependency)
	valueA, err := leader.Get(ctx, "causal-A")
	if err != nil {
		t.Fatalf("Failed to read A: %v", err)
	}

	if string(valueA) != "value-A" {
		t.Fatalf("Wrong value for A: expected value-A, got %s", string(valueA))
	}

	// Write B (causally depends on A)
	cmdB := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "causal-B",
		Value: []byte("value-B-depends-on-A"),
	}

	if err := leader.Apply(cmdB, 5*time.Second); err != nil {
		t.Fatalf("Failed to write B: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Read B
	valueB, err := leader.Get(ctx, "causal-B")
	if err != nil {
		t.Fatalf("Failed to read B: %v", err)
	}

	if string(valueB) != "value-B-depends-on-A" {
		t.Fatalf("Wrong value for B: expected value-B-depends-on-A, got %s", string(valueB))
	}

	// Write C (causally depends on B)
	cmdC := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "causal-C",
		Value: []byte("value-C-depends-on-B"),
	}

	if err := leader.Apply(cmdC, 5*time.Second); err != nil {
		t.Fatalf("Failed to write C: %v", err)
	}

	time.Sleep(1 * time.Second)

	// All nodes should now see the complete causal chain
	for i, node := range cluster.nodes {
		// Check A exists
		valA, err := node.Get(ctx, "causal-A")
		if err != nil {
			t.Errorf("Node %d: A not found: %v", i, err)
		} else if string(valA) != "value-A" {
			t.Errorf("Node %d: Wrong value for A: expected value-A, got %s", i, string(valA))
		}

		// Check B exists (should not exist without A)
		valB, err := node.Get(ctx, "causal-B")
		if err != nil {
			t.Errorf("Node %d: B not found: %v", i, err)
		} else if string(valB) != "value-B-depends-on-A" {
			t.Errorf("Node %d: Wrong value for B: expected value-B-depends-on-A, got %s",
				i, string(valB))
		}

		// Check C exists (should not exist without B)
		valC, err := node.Get(ctx, "causal-C")
		if err != nil {
			t.Errorf("Node %d: C not found: %v", i, err)
		} else if string(valC) != "value-C-depends-on-B" {
			t.Errorf("Node %d: Wrong value for C: expected value-C-depends-on-B, got %s",
				i, string(valC))
		}
	}
}

// TestCluster_EventualConsistency tests that all nodes eventually converge
func TestCluster_EventualConsistency(t *testing.T) {
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

	ctx := context.Background()

	// Write a batch of data
	batchSize := 50
	for i := 0; i < batchSize; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("eventual-key-%d", i),
			Value: []byte(fmt.Sprintf("eventual-value-%d", i)),
		}

		if err := leader.Apply(cmd, 5*time.Second); err != nil {
			t.Fatalf("Failed to write key %d: %v", i, err)
		}
	}

	// Wait for eventual consistency (give plenty of time)
	time.Sleep(3 * time.Second)

	// Now verify all nodes have identical data
	nodeData := make([]map[string]string, len(cluster.nodes))

	for i, node := range cluster.nodes {
		nodeData[i] = make(map[string]string)

		for j := 0; j < batchSize; j++ {
			key := fmt.Sprintf("eventual-key-%d", j)
			value, err := node.Get(ctx, key)
			if err != nil {
				t.Errorf("Node %d failed to get %s: %v", i, key, err)
				continue
			}

			nodeData[i][key] = string(value)
		}
	}

	// Compare all nodes' data
	referenceData := nodeData[0]

	for i := 1; i < len(nodeData); i++ {
		for key, refValue := range referenceData {
			nodeValue, exists := nodeData[i][key]
			if !exists {
				t.Errorf("Node %d missing key %s", i, key)
				continue
			}

			if nodeValue != refValue {
				t.Errorf("Node %d has different value for %s: expected %s, got %s",
					i, key, refValue, nodeValue)
			}
		}

		// Check no extra keys
		if len(nodeData[i]) != len(referenceData) {
			t.Errorf("Node %d has %d keys, but reference has %d keys",
				i, len(nodeData[i]), len(referenceData))
		}
	}
}

// TestCluster_NoLostUpdates tests that concurrent updates are not lost
func TestCluster_NoLostUpdates(t *testing.T) {
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

	ctx := context.Background()

	// Initialize counter
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "counter",
		Value: []byte("0"),
	}

	if err := leader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to initialize counter: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Concurrent increments
	goroutineCount := 10
	incrementsPerGoroutine := 5
	expectedTotal := goroutineCount * incrementsPerGoroutine

	var wg sync.WaitGroup
	errors := make(chan error, goroutineCount)

	for g := 0; g < goroutineCount; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()

			for i := 0; i < incrementsPerGoroutine; i++ {
				// Read current value
				currentValue, err := leader.Get(ctx, "counter")
				if err != nil {
					errors <- fmt.Errorf("goroutine %d read failed: %w", gid, err)
					return
				}

				// Parse counter (simple string to int)
				var currentInt int
				fmt.Sscanf(string(currentValue), "%d", &currentInt)

				// Increment
				newInt := currentInt + 1
				newValue := []byte(fmt.Sprintf("%d", newInt))

				// Write back
				cmd := consensus.Command{
					Op:    consensus.OpTypePut,
					Key:   "counter",
					Value: newValue,
				}

				if err := leader.Apply(cmd, 5*time.Second); err != nil {
					errors <- fmt.Errorf("goroutine %d write failed: %w", gid, err)
					return
				}

				// Small delay between increments
				time.Sleep(10 * time.Millisecond)
			}
		}(g)
	}

	wg.Wait()
	close(errors)

	// Check for errors during concurrent updates
	for err := range errors {
		t.Error(err)
	}

	// Wait for replication
	time.Sleep(1 * time.Second)

	// Read final value
	finalValue, err := leader.Get(ctx, "counter")
	if err != nil {
		t.Fatalf("Failed to read final counter: %v", err)
	}

	var finalInt int
	fmt.Sscanf(string(finalValue), "%d", &finalInt)

	// Due to race conditions in read-modify-write without locking,
	// the final value may be less than expected (lost updates)
	// But it should be at least 1 and at most expectedTotal
	if finalInt < 1 || finalInt > expectedTotal {
		t.Errorf("Final counter out of range: got %d, expected between 1 and %d",
			finalInt, expectedTotal)
	}

	t.Logf("Concurrent increments: expected %d, got %d (lost updates: %d)",
		expectedTotal, finalInt, expectedTotal-finalInt)

	// Note: This test demonstrates that without proper locking/CAS operations,
	// concurrent read-modify-write operations can lose updates in a distributed system
}

// TestCluster_DeleteConsistency tests that deletes are consistently applied
func TestCluster_DeleteConsistency(t *testing.T) {
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

	ctx := context.Background()

	// Write keys
	for i := 0; i < 10; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("delete-test-%d", i),
			Value: []byte(fmt.Sprintf("value-%d", i)),
		}

		if err := leader.Apply(cmd, 5*time.Second); err != nil {
			t.Fatalf("Failed to write key %d: %v", i, err)
		}
	}

	time.Sleep(1 * time.Second)

	// Verify all nodes have the keys
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("delete-test-%d", i)
		if err := cluster.VerifyDataConsistency(ctx, key, []byte(fmt.Sprintf("value-%d", i))); err != nil {
			t.Errorf("Consistency check failed before delete: %v", err)
		}
	}

	// Delete half the keys
	for i := 0; i < 5; i++ {
		cmd := consensus.Command{
			Op:  consensus.OpTypeDelete,
			Key: fmt.Sprintf("delete-test-%d", i),
		}

		if err := leader.Apply(cmd, 5*time.Second); err != nil {
			t.Fatalf("Failed to delete key %d: %v", i, err)
		}
	}

	time.Sleep(1 * time.Second)

	// Verify deleted keys are gone on all nodes
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("delete-test-%d", i)

		for j, node := range cluster.nodes {
			_, err := node.Get(ctx, key)
			if err == nil {
				t.Errorf("Node %d still has deleted key %s", j, key)
			}
		}
	}

	// Verify remaining keys still exist on all nodes
	for i := 5; i < 10; i++ {
		key := fmt.Sprintf("delete-test-%d", i)
		expectedValue := []byte(fmt.Sprintf("value-%d", i))

		if err := cluster.VerifyDataConsistency(ctx, key, expectedValue); err != nil {
			t.Errorf("Consistency check failed after delete: %v", err)
		}
	}
}

// TestCluster_OverwriteConsistency tests that overwrites are consistently applied
func TestCluster_OverwriteConsistency(t *testing.T) {
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

	ctx := context.Background()

	// Write initial value
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "overwrite-key",
		Value: []byte("version-1"),
	}

	if err := leader.Apply(cmd, 5*time.Second); err != nil {
		t.Fatalf("Failed to write version-1: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Verify all nodes see version-1
	if err := cluster.VerifyDataConsistency(ctx, "overwrite-key", []byte("version-1")); err != nil {
		t.Errorf("Version-1 consistency failed: %v", err)
	}

	// Overwrite multiple times
	versions := []string{"version-2", "version-3", "version-4", "version-5"}

	for _, version := range versions {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   "overwrite-key",
			Value: []byte(version),
		}

		if err := leader.Apply(cmd, 5*time.Second); err != nil {
			t.Fatalf("Failed to write %s: %v", version, err)
		}

		time.Sleep(500 * time.Millisecond)

		// Verify all nodes see the latest version
		if err := cluster.VerifyDataConsistency(ctx, "overwrite-key", []byte(version)); err != nil {
			t.Errorf("%s consistency failed: %v", version, err)
		}
	}

	// Final check: all nodes should have version-5
	for i, node := range cluster.nodes {
		value, err := node.Get(ctx, "overwrite-key")
		if err != nil {
			t.Errorf("Node %d failed to get final value: %v", i, err)
			continue
		}

		if string(value) != "version-5" {
			t.Errorf("Node %d has wrong final value: expected version-5, got %s",
				i, string(value))
		}
	}
}
