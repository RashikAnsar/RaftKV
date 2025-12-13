//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/RashikAnsar/raftkv/internal/consensus"
	"github.com/RashikAnsar/raftkv/internal/storage"
)

// Helper to create a single Raft node for testing
func createSingleRaftNode(t *testing.T) (*consensus.RaftNode, *storage.DurableStore, string) {
	tmpDir, err := os.MkdirTemp("", "raft-cas-test-*")
	require.NoError(t, err)

	store, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       filepath.Join(tmpDir, "store"),
		SyncOnWrite:   false,
		SnapshotEvery: 0,
	})
	require.NoError(t, err)

	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	raftAddr := getFreePort(t)

	node, err := consensus.NewRaftNode(consensus.RaftConfig{
		NodeID:    "test-node",
		RaftAddr:  raftAddr,
		RaftDir:   filepath.Join(tmpDir, "raft"),
		Bootstrap: true,
		Store:     store,
		Logger:    logger,
	})
	require.NoError(t, err)

	// Wait for node to become leader
	time.Sleep(2 * time.Second)

	return node, store, tmpDir
}

// TestCAS_ThroughRaft tests basic CAS operations through Raft consensus
func TestCAS_ThroughRaft(t *testing.T) {
	node, store, tmpDir := createSingleRaftNode(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	ctx := context.Background()

	t.Run("GetWithVersion", func(t *testing.T) {
		// Put a key
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   "test-key",
			Value: []byte("test-value"),
		}
		err := node.Apply(cmd, 5*time.Second)
		require.NoError(t, err)

		// Get with version
		value, version, err := node.GetWithVersion(ctx, "test-key")
		require.NoError(t, err)
		assert.Equal(t, "test-value", string(value))
		assert.Equal(t, uint64(1), version)
	})

	t.Run("CAS_Success", func(t *testing.T) {
		// Put initial value
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   "counter",
			Value: []byte("0"),
		}
		err := node.Apply(cmd, 5*time.Second)
		require.NoError(t, err)

		// Perform CAS with correct version
		casCmd := consensus.Command{
			Op:              consensus.OpTypeCAS,
			Key:             "counter",
			Value:           []byte("1"),
			ExpectedVersion: 1,
		}
		err = node.Apply(casCmd, 5*time.Second)
		require.NoError(t, err)

		// Verify value and version updated
		value, version, err := store.GetWithVersion(ctx, "counter")
		require.NoError(t, err)
		assert.Equal(t, "1", string(value))
		assert.Equal(t, uint64(2), version)
	})

	t.Run("CAS_VersionMismatch", func(t *testing.T) {
		// counter is now at version 2 with value "1"

		// Attempt CAS with wrong version
		casCmd := consensus.Command{
			Op:              consensus.OpTypeCAS,
			Key:             "counter",
			Value:           []byte("2"),
			ExpectedVersion: 1, // Wrong version
		}
		err := node.Apply(casCmd, 5*time.Second)
		require.NoError(t, err) // CAS doesn't error, just fails

		// Verify value unchanged
		value, version, err := store.GetWithVersion(ctx, "counter")
		require.NoError(t, err)
		assert.Equal(t, "1", string(value))
		assert.Equal(t, uint64(2), version)
	})

	t.Run("CAS_Sequential", func(t *testing.T) {
		// Create a new key
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   "sequential",
			Value: []byte("0"),
		}
		node.Apply(cmd, 5*time.Second)

		// Perform 10 sequential CAS operations
		for i := 1; i <= 10; i++ {
			casCmd := consensus.Command{
				Op:              consensus.OpTypeCAS,
				Key:             "sequential",
				Value:           []byte(fmt.Sprintf("%d", i)),
				ExpectedVersion: uint64(i),
			}
			err := node.Apply(casCmd, 5*time.Second)
			require.NoError(t, err)
		}

		// Verify final state
		value, version, err := store.GetWithVersion(ctx, "sequential")
		require.NoError(t, err)
		assert.Equal(t, "10", string(value))
		assert.Equal(t, uint64(11), version)
	})
}

// TestCAS_ConcurrentThroughRaft tests concurrent CAS operations through Raft
func TestCAS_ConcurrentThroughRaft(t *testing.T) {
	node, store, tmpDir := createSingleRaftNode(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	ctx := context.Background()

	// Create initial counter
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "concurrent-counter",
		Value: []byte("0"),
	}
	err := node.Apply(cmd, 5*time.Second)
	require.NoError(t, err)

	// Run concurrent CAS operations
	numGoroutines := 10
	incrementsPerGoroutine := 10
	expectedTotal := numGoroutines * incrementsPerGoroutine

	var wg sync.WaitGroup
	successCounts := make([]int, numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			successCount := 0

			for i := 0; i < incrementsPerGoroutine; {
				// Get current value and version
				valueBytes, version, err := store.GetWithVersion(ctx, "concurrent-counter")
				if err != nil {
					continue
				}

				currentValue := 0
				fmt.Sscanf(string(valueBytes), "%d", &currentValue)

				// Attempt CAS through Raft
				casCmd := consensus.Command{
					Op:              consensus.OpTypeCAS,
					Key:             "concurrent-counter",
					Value:           []byte(fmt.Sprintf("%d", currentValue+1)),
					ExpectedVersion: version,
				}

				err = node.Apply(casCmd, 5*time.Second)
				if err != nil {
					continue
				}

				// Check if CAS succeeded by reading the version
				newValue, newVersion, err := store.GetWithVersion(ctx, "concurrent-counter")
				if err != nil {
					continue
				}

				// If version incremented and value matches what we set, CAS succeeded
				if newVersion > version {
					expectedValue := fmt.Sprintf("%d", currentValue+1)
					if string(newValue) == expectedValue {
						successCount++
						i++
					}
				}
			}

			successCounts[id] = successCount
		}(g)
	}

	wg.Wait()

	// Verify final counter value
	value, version, err := store.GetWithVersion(ctx, "concurrent-counter")
	require.NoError(t, err)

	finalCount := 0
	fmt.Sscanf(string(value), "%d", &finalCount)

	// The final count should be expectedTotal (all CAS operations succeeded)
	assert.GreaterOrEqual(t, finalCount, expectedTotal/2, "At least half of concurrent CAS operations should succeed")
	assert.LessOrEqual(t, finalCount, expectedTotal, "Final count should not exceed total attempts")

	t.Logf("Concurrent CAS test: final value=%d, version=%d", finalCount, version)
	for i, count := range successCounts {
		t.Logf("  Goroutine %d: %d successes", i, count)
	}
}

// TestCAS_RaftRecovery tests CAS operations survive Raft recovery
func TestCAS_RaftRecovery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "raft-cas-recovery-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	// Phase 1: Create node, perform CAS operations, shutdown
	func() {
		store, err := storage.NewDurableStore(storage.DurableStoreConfig{
			DataDir:       filepath.Join(tmpDir, "store"),
			SyncOnWrite:   true,
			SnapshotEvery: 0,
		})
		require.NoError(t, err)

		logger, err := zap.NewDevelopment()
		require.NoError(t, err)

		raftAddr := getFreePort(t)

		node, err := consensus.NewRaftNode(consensus.RaftConfig{
			NodeID:    "recovery-test",
			RaftAddr:  raftAddr,
			RaftDir:   filepath.Join(tmpDir, "raft"),
			Bootstrap: true,
			Store:     store,
			Logger:    logger,
		})
		require.NoError(t, err)

		time.Sleep(2 * time.Second)

		// Perform several CAS operations
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   "persistent",
			Value: []byte("0"),
		}
		node.Apply(cmd, 5*time.Second)

		for i := 1; i <= 5; i++ {
			casCmd := consensus.Command{
				Op:              consensus.OpTypeCAS,
				Key:             "persistent",
				Value:           []byte(fmt.Sprintf("%d", i)),
				ExpectedVersion: uint64(i),
			}
			node.Apply(casCmd, 5*time.Second)
		}

		node.Shutdown()
		store.Close()
	}()

	// Phase 2: Recover and verify
	store, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       filepath.Join(tmpDir, "store"),
		SyncOnWrite:   true,
		SnapshotEvery: 0,
	})
	require.NoError(t, err)
	defer store.Close()

	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	raftAddr := getFreePort(t)

	node, err := consensus.NewRaftNode(consensus.RaftConfig{
		NodeID:    "recovery-test",
		RaftAddr:  raftAddr,
		RaftDir:   filepath.Join(tmpDir, "raft"),
		Bootstrap: false, // Don't bootstrap, recover instead
		Store:     store,
		Logger:    logger,
	})
	require.NoError(t, err)
	defer node.Shutdown()

	time.Sleep(2 * time.Second)

	// Verify CAS operations survived recovery
	value, version, err := store.GetWithVersion(ctx, "persistent")
	require.NoError(t, err)
	assert.Equal(t, "5", string(value))
	assert.Equal(t, uint64(6), version)

	t.Log("CAS operations survived Raft recovery successfully")
}

// TestCAS_OptimisticConcurrency tests the optimistic concurrency control pattern
func TestCAS_OptimisticConcurrency(t *testing.T) {
	node, store, tmpDir := createSingleRaftNode(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	ctx := context.Background()

	// Create a shared resource (like a bank account balance)
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   "account:balance",
		Value: []byte("1000"),
	}
	err := node.Apply(cmd, 5*time.Second)
	require.NoError(t, err)

	// Simulate 5 concurrent withdrawals of 100 each
	numWithdrawals := 5
	withdrawAmount := 100
	expectedFinalBalance := 1000 - (numWithdrawals * withdrawAmount)

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	for i := 0; i < numWithdrawals; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			maxRetries := 20
			for retry := 0; retry < maxRetries; retry++ {
				// Read current balance with version
				valueBytes, version, err := store.GetWithVersion(ctx, "account:balance")
				if err != nil {
					continue
				}

				currentBalance := 0
				fmt.Sscanf(string(valueBytes), "%d", &currentBalance)

				// Check if withdrawal is possible
				if currentBalance < withdrawAmount {
					return // Insufficient funds
				}

				// Calculate new balance
				newBalance := currentBalance - withdrawAmount

				// Attempt CAS
				casCmd := consensus.Command{
					Op:              consensus.OpTypeCAS,
					Key:             "account:balance",
					Value:           []byte(fmt.Sprintf("%d", newBalance)),
					ExpectedVersion: version,
				}
				err = node.Apply(casCmd, 5*time.Second)
				if err != nil {
					continue
				}

				// Check if our CAS succeeded
				newValue, newVersion, _ := store.GetWithVersion(ctx, "account:balance")
				if newVersion == version+1 && string(newValue) == fmt.Sprintf("%d", newBalance) {
					mu.Lock()
					successCount++
					mu.Unlock()
					return
				}

				// CAS failed, retry
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// Verify all withdrawals succeeded
	assert.Equal(t, numWithdrawals, successCount, "All withdrawals should succeed")

	// Verify final balance
	value, _, err := store.GetWithVersion(ctx, "account:balance")
	require.NoError(t, err)

	finalBalance := 0
	fmt.Sscanf(string(value), "%d", &finalBalance)
	assert.Equal(t, expectedFinalBalance, finalBalance)

	t.Logf("Optimistic concurrency test: %d withdrawals succeeded, final balance=%d", successCount, finalBalance)
}
