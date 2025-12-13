//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/RashikAnsar/raftkv/internal/consensus"
	"github.com/RashikAnsar/raftkv/internal/storage"
)

// TestList_BasicPaginationThroughRaft tests basic pagination through Raft
func TestList_BasicPaginationThroughRaft(t *testing.T) {
	node, store, tmpDir := createSingleRaftNode(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	ctx := context.Background()

	// Create 20 keys
	for i := 0; i < 20; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("key-%02d", i),
			Value: []byte(fmt.Sprintf("value-%02d", i)),
		}
		err := node.Apply(cmd, 5*time.Second)
		require.NoError(t, err)
	}

	// Test pagination: Get first page
	result, err := store.ListWithOptions(ctx, storage.ListOptions{
		Limit: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, 10, len(result.Keys))
	assert.True(t, result.HasMore)
	assert.NotEmpty(t, result.NextCursor)

	// Verify first page keys are sorted
	assert.Equal(t, "key-00", result.Keys[0])
	assert.Equal(t, "key-09", result.Keys[9])

	// Get second page using cursor
	result2, err := store.ListWithOptions(ctx, storage.ListOptions{
		Limit:  10,
		Cursor: result.NextCursor,
	})
	require.NoError(t, err)
	assert.Equal(t, 10, len(result2.Keys))
	assert.False(t, result2.HasMore) // No more results
	assert.Empty(t, result2.NextCursor)

	// Verify second page keys
	assert.Equal(t, "key-10", result2.Keys[0])
	assert.Equal(t, "key-19", result2.Keys[9])
}

// TestList_PrefixFiltering tests prefix filtering
func TestList_PrefixFiltering(t *testing.T) {
	node, store, tmpDir := createSingleRaftNode(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	ctx := context.Background()

	// Create keys with different prefixes
	prefixes := []string{"user:", "post:", "comment:"}
	for _, prefix := range prefixes {
		for i := 0; i < 5; i++ {
			cmd := consensus.Command{
				Op:    consensus.OpTypePut,
				Key:   fmt.Sprintf("%s%d", prefix, i),
				Value: []byte("value"),
			}
			err := node.Apply(cmd, 5*time.Second)
			require.NoError(t, err)
		}
	}

	// List keys with "user:" prefix
	result, err := store.ListWithOptions(ctx, storage.ListOptions{
		Prefix: "user:",
	})
	require.NoError(t, err)
	assert.Equal(t, 5, len(result.Keys))

	// Verify all keys have the prefix
	for _, key := range result.Keys {
		assert.Contains(t, key, "user:")
	}

	// List keys with "post:" prefix
	result2, err := store.ListWithOptions(ctx, storage.ListOptions{
		Prefix: "post:",
	})
	require.NoError(t, err)
	assert.Equal(t, 5, len(result2.Keys))

	for _, key := range result2.Keys {
		assert.Contains(t, key, "post:")
	}
}

// TestList_RangeQuery tests range queries
func TestList_RangeQuery(t *testing.T) {
	node, store, tmpDir := createSingleRaftNode(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	ctx := context.Background()

	// Create keys from a-z
	for c := 'a'; c <= 'z'; c++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   string(c),
			Value: []byte(fmt.Sprintf("value-%c", c)),
		}
		err := node.Apply(cmd, 5*time.Second)
		require.NoError(t, err)
	}

	// Query range: f to p (inclusive)
	result, err := store.ListWithOptions(ctx, storage.ListOptions{
		Start: "f",
		End:   "p",
	})
	require.NoError(t, err)

	// Should get: f, g, h, i, j, k, l, m, n, o, p (11 keys)
	assert.Equal(t, 11, len(result.Keys))
	assert.Equal(t, "f", result.Keys[0])
	assert.Equal(t, "p", result.Keys[10])

	// Verify all keys are in range
	for _, key := range result.Keys {
		assert.GreaterOrEqual(t, key, "f")
		assert.LessOrEqual(t, key, "p")
	}
}

// TestList_ReverseOrder tests reverse ordering
func TestList_ReverseOrder(t *testing.T) {
	node, store, tmpDir := createSingleRaftNode(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	ctx := context.Background()

	// Create 10 keys
	for i := 0; i < 10; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("key-%02d", i),
			Value: []byte("value"),
		}
		err := node.Apply(cmd, 5*time.Second)
		require.NoError(t, err)
	}

	// List in reverse order
	result, err := store.ListWithOptions(ctx, storage.ListOptions{
		Reverse: true,
	})
	require.NoError(t, err)

	assert.Equal(t, 10, len(result.Keys))
	// Should be in descending order
	assert.Equal(t, "key-09", result.Keys[0])
	assert.Equal(t, "key-00", result.Keys[9])

	// Verify descending order
	for i := 0; i < len(result.Keys)-1; i++ {
		assert.Greater(t, result.Keys[i], result.Keys[i+1])
	}
}

// TestList_ReverseWithPagination tests reverse order with pagination
func TestList_ReverseWithPagination(t *testing.T) {
	node, store, tmpDir := createSingleRaftNode(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	ctx := context.Background()

	// Create 15 keys
	for i := 0; i < 15; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("item-%02d", i),
			Value: []byte("value"),
		}
		err := node.Apply(cmd, 5*time.Second)
		require.NoError(t, err)
	}

	// Get first page in reverse
	result, err := store.ListWithOptions(ctx, storage.ListOptions{
		Reverse: true,
		Limit:   5,
	})
	require.NoError(t, err)

	assert.Equal(t, 5, len(result.Keys))
	assert.True(t, result.HasMore)
	assert.Equal(t, "item-14", result.Keys[0])
	assert.Equal(t, "item-10", result.Keys[4])

	// Get second page
	result2, err := store.ListWithOptions(ctx, storage.ListOptions{
		Reverse: true,
		Limit:   5,
		Cursor:  result.NextCursor,
	})
	require.NoError(t, err)

	assert.Equal(t, 5, len(result2.Keys))
	assert.True(t, result2.HasMore)
	assert.Equal(t, "item-09", result2.Keys[0])
	assert.Equal(t, "item-05", result2.Keys[4])

	// Get third page
	result3, err := store.ListWithOptions(ctx, storage.ListOptions{
		Reverse: true,
		Limit:   5,
		Cursor:  result2.NextCursor,
	})
	require.NoError(t, err)

	assert.Equal(t, 5, len(result3.Keys))
	assert.False(t, result3.HasMore)
	assert.Equal(t, "item-04", result3.Keys[0])
	assert.Equal(t, "item-00", result3.Keys[4])
}

// TestList_CombinedFilters tests combining multiple filters
func TestList_CombinedFilters(t *testing.T) {
	node, store, tmpDir := createSingleRaftNode(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	ctx := context.Background()

	// Create keys with different prefixes and numbers
	for i := 0; i < 30; i++ {
		prefix := "user:"
		if i%2 == 0 {
			prefix = "post:"
		}
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("%s%02d", prefix, i),
			Value: []byte("value"),
		}
		err := node.Apply(cmd, 5*time.Second)
		require.NoError(t, err)
	}

	// Combine: prefix="user:", range=user:10 to user:20, limit=3
	result, err := store.ListWithOptions(ctx, storage.ListOptions{
		Prefix: "user:",
		Start:  "user:10",
		End:    "user:20",
		Limit:  3,
	})
	require.NoError(t, err)

	// Should get user:11, user:13, user:15 (odd numbers from 10-20)
	assert.LessOrEqual(t, len(result.Keys), 3)

	// Verify all keys match filters
	for _, key := range result.Keys {
		assert.Contains(t, key, "user:")
		assert.GreaterOrEqual(t, key, "user:10")
		assert.LessOrEqual(t, key, "user:20")
	}
}

// TestList_EmptyStore tests listing from an empty store
func TestList_EmptyStore(t *testing.T) {
	node, store, tmpDir := createSingleRaftNode(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	ctx := context.Background()

	result, err := store.ListWithOptions(ctx, storage.ListOptions{
		Limit: 10,
	})
	require.NoError(t, err)

	assert.Equal(t, 0, len(result.Keys))
	assert.False(t, result.HasMore)
	assert.Empty(t, result.NextCursor)
}

// TestList_AfterDeleteOperations tests listing after delete operations
func TestList_AfterDeleteOperations(t *testing.T) {
	node, store, tmpDir := createSingleRaftNode(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	ctx := context.Background()

	// Create 10 keys
	for i := 0; i < 10; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("key-%02d", i),
			Value: []byte("value"),
		}
		err := node.Apply(cmd, 5*time.Second)
		require.NoError(t, err)
	}

	// Delete every other key
	for i := 0; i < 10; i += 2 {
		cmd := consensus.Command{
			Op:  consensus.OpTypeDelete,
			Key: fmt.Sprintf("key-%02d", i),
		}
		err := node.Apply(cmd, 5*time.Second)
		require.NoError(t, err)
	}

	// List remaining keys
	result, err := store.ListWithOptions(ctx, storage.ListOptions{})
	require.NoError(t, err)

	// Should have 5 keys remaining (odd numbered)
	assert.Equal(t, 5, len(result.Keys))

	// Verify only odd-numbered keys remain
	for _, key := range result.Keys {
		assert.Contains(t, []string{"key-01", "key-03", "key-05", "key-07", "key-09"}, key)
	}
}

// TestList_RaftRecovery tests that list operations work after Raft recovery
func TestList_RaftRecovery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "raft-list-recovery-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	// Phase 1: Create node, add keys, shutdown
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

		// Create 15 keys
		for i := 0; i < 15; i++ {
			cmd := consensus.Command{
				Op:    consensus.OpTypePut,
				Key:   fmt.Sprintf("data-%02d", i),
				Value: []byte(fmt.Sprintf("value-%02d", i)),
			}
			node.Apply(cmd, 5*time.Second)
		}

		node.Shutdown()
		store.Close()
	}()

	// Phase 2: Recover and verify list works
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
		Bootstrap: false,
		Store:     store,
		Logger:    logger,
	})
	require.NoError(t, err)
	defer node.Shutdown()

	time.Sleep(2 * time.Second)

	// Test pagination after recovery
	result, err := store.ListWithOptions(ctx, storage.ListOptions{
		Limit: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, 10, len(result.Keys))
	assert.True(t, result.HasMore)

	// Get second page
	result2, err := store.ListWithOptions(ctx, storage.ListOptions{
		Limit:  10,
		Cursor: result.NextCursor,
	})
	require.NoError(t, err)
	assert.Equal(t, 5, len(result2.Keys))
	assert.False(t, result2.HasMore)

	// Verify total count
	resultAll, err := store.ListWithOptions(ctx, storage.ListOptions{})
	require.NoError(t, err)
	assert.Equal(t, 15, len(resultAll.Keys))

	t.Log("List operations work correctly after Raft recovery")
}
