package sharding

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const (
	GB = 1024 * 1024 * 1024 // 1 GB in bytes
)

// setupIntegrationTest creates a fully wired setup for integration testing
func setupIntegrationTest(t *testing.T, nodeID string) (*Manager, *MetaFSM, *Router, *MockRaft) {
	logger := zaptest.NewLogger(t)

	// Create components
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)
	mockRaft := NewMockRaft()

	// Wire up MockRaft to actually apply commands to FSM
	mockRaft.applyFunc = func(cmd []byte, timeout time.Duration) raft.ApplyFuture {
		// Apply the command to FSM
		log := &raft.Log{
			Index: uint64(len(mockRaft.appliedLogs) + 1),
			Data:  cmd,
		}
		result := fsm.Apply(log)

		mockRaft.appliedLogs = append(mockRaft.appliedLogs, cmd)

		return &MockApplyFuture{
			err:      nil,
			response: result,
		}
	}

	// Register router as FSM listener
	fsm.RegisterListener(router)

	// Create manager
	config := DefaultManagerConfig(nodeID)
	manager := NewManager(config, mockRaft, fsm, router, logger)

	return manager, fsm, router, mockRaft
}

// TestIntegration_FullShardingWorkflow tests the complete sharding workflow
func TestIntegration_FullShardingWorkflow(t *testing.T) {
	// Setup
	manager, fsm, router, _ := setupIntegrationTest(t, "node1")

	// Start manager
	err := manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// 1. Register nodes
	t.Log("Registering nodes...")
	err = manager.RegisterNode("node1", "localhost:8001", 10*GB)
	require.NoError(t, err)
	err = manager.RegisterNode("node2", "localhost:8002", 10*GB)
	require.NoError(t, err)
	err = manager.RegisterNode("node3", "localhost:8003", 10*GB)
	require.NoError(t, err)

	// Wait for nodes to propagate to router
	time.Sleep(100 * time.Millisecond)

	// Verify nodes are registered
	nodes := router.GetAllNodes()
	assert.Len(t, nodes, 3)

	// 2. Create shards
	t.Log("Creating shards...")
	err = manager.CreateShard(0, []string{"node1", "node2", "node3"})
	require.NoError(t, err)
	err = manager.CreateShard(1, []string{"node1", "node2", "node3"})
	require.NoError(t, err)
	err = manager.CreateShard(2, []string{"node1", "node2", "node3"})
	require.NoError(t, err)

	// Wait for shards to be added to router
	time.Sleep(100 * time.Millisecond)

	// Verify shards are created
	shards := fsm.shardMap.GetActiveShards()
	assert.Len(t, shards, 3)

	// 3. Route keys to shards
	t.Log("Testing key routing...")
	routeInfo, err := router.RouteKey("user:alice")
	require.NoError(t, err)
	assert.Contains(t, []int{0, 1, 2}, routeInfo.ShardID)
	assert.Len(t, routeInfo.Replicas, 3)
	assert.False(t, routeInfo.IsMigrating)

	// Route multiple keys and verify distribution
	keyToShard := make(map[string]int)
	shardCounts := make(map[int]int)

	for i := 0; i < 300; i++ {
		key := fmt.Sprintf("user:%d", i)
		routeInfo, err := router.RouteKey(key)
		require.NoError(t, err)

		keyToShard[key] = routeInfo.ShardID
		shardCounts[routeInfo.ShardID]++
	}

	// Each shard should get ~100 keys (300 / 3 shards)
	t.Logf("Key distribution across shards: %v", shardCounts)
	for shardID, count := range shardCounts {
		assert.Greater(t, count, 50, "Shard %d should have reasonable number of keys", shardID)
		assert.Less(t, count, 150, "Shard %d should not have too many keys", shardID)
	}

	// 4. Test shard removal
	t.Log("Testing shard removal...")
	err = manager.RemoveShard(2)
	require.NoError(t, err)

	// Verify shard was removed
	time.Sleep(100 * time.Millisecond) // Allow time for listener notification
	shards = fsm.shardMap.GetActiveShards()
	assert.Len(t, shards, 2)

	// Router should still work with remaining shards
	routeInfo, err = router.RouteKey("user:bob")
	require.NoError(t, err)
	assert.Contains(t, []int{0, 1}, routeInfo.ShardID) // Should only route to shard 0 or 1 now
}

// TestIntegration_MigrationWorkflow tests the complete migration workflow
func TestIntegration_MigrationWorkflow(t *testing.T) {
	// Setup
	manager, fsm, _, _ := setupIntegrationTest(t, "node1")
	migrator := NewMigrator(DefaultMigratorConfig(), manager, zaptest.NewLogger(t))

	err := manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Register nodes
	manager.RegisterNode("node1", "localhost:8001", 10*GB)
	manager.RegisterNode("node2", "localhost:8002", 10*GB)
	manager.RegisterNode("node3", "localhost:8003", 10*GB)
	manager.RegisterNode("node4", "localhost:8004", 10*GB)

	// Create shard
	err = manager.CreateShard(1, []string{"node1", "node2", "node3"})
	require.NoError(t, err)

	// Create mock stores
	sourceStore := NewMockShardStore()
	targetStore := NewMockShardStore()

	// Populate source with data
	t.Log("Populating source shard with data...")
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%03d", i)
		value := []byte(fmt.Sprintf("value-%d", i))
		sourceStore.Put(context.Background(), key, value)
	}

	// Start migration
	t.Log("Starting migration...")
	migrationID, err := manager.StartMigration(1, []string{"node1", "node2"}, []string{"node3", "node4"})
	require.NoError(t, err)
	assert.NotEmpty(t, migrationID)

	// Verify migration was created
	migration, ok := fsm.shardMap.GetMigration(migrationID)
	require.True(t, ok)
	assert.Equal(t, MigrationStatePreparing, migration.State)

	// Run migration worker
	t.Log("Executing data migration...")
	ctx := context.Background()
	err = migrator.StartMigration(ctx, migrationID, sourceStore, targetStore)
	require.NoError(t, err)

	// Wait for migration to complete
	time.Sleep(1 * time.Second)

	// Verify data was copied
	assert.Equal(t, 100, len(targetStore.data))
	for key, value := range sourceStore.data {
		targetValue, exists := targetStore.data[key]
		assert.True(t, exists, "Key %s should exist in target", key)
		assert.Equal(t, value, targetValue, "Value for key %s should match", key)
	}

	// Check migration status
	status, err := migrator.GetMigrationStatus(migrationID)
	require.NoError(t, err)
	assert.Equal(t, int64(100), status.KeysCopied)
	assert.Equal(t, int64(100), status.TotalKeys)
}

// TestIntegration_ConcurrentRouting tests concurrent routing operations
func TestIntegration_ConcurrentRouting(t *testing.T) {
	// Setup
	manager, _, router, _ := setupIntegrationTest(t, "node1")

	manager.Start()
	defer manager.Stop()

	// Setup cluster
	manager.RegisterNode("node1", "localhost:8001", 10*GB)
	manager.RegisterNode("node2", "localhost:8002", 10*GB)
	manager.RegisterNode("node3", "localhost:8003", 10*GB)

	for i := 0; i < 4; i++ {
		manager.CreateShard(i, []string{"node1", "node2", "node3"})
	}

	// Concurrent routing test
	t.Log("Testing concurrent routing...")
	var wg sync.WaitGroup
	errors := make(chan error, 100)
	numGoroutines := 10
	requestsPerGoroutine := 100

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for i := 0; i < requestsPerGoroutine; i++ {
				key := fmt.Sprintf("user:%d:%d", goroutineID, i)
				_, err := router.RouteKey(key)
				if err != nil {
					errors <- err
					return
				}
			}
		}(g)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Errorf("Routing error: %v", err)
		errorCount++
	}
	assert.Equal(t, 0, errorCount, "Should have no routing errors")

	// Verify router stats
	stats := router.GetRoutingStats()
	assert.Equal(t, 4, stats["shard_count"])
	assert.Greater(t, stats["shard_map_version"], uint64(0))
}

// TestIntegration_ShardMapVersioning tests version tracking and updates
func TestIntegration_ShardMapVersioning(t *testing.T) {
	// Setup
	manager, _, router, _ := setupIntegrationTest(t, "node1")

	manager.Start()
	defer manager.Stop()

	// Get initial version
	initialVersion := router.GetShardMapVersion()
	t.Logf("Initial version: %d", initialVersion)

	// Add shard - should increment version
	manager.RegisterNode("node1", "localhost:8001", 10*GB)
	manager.CreateShard(0, []string{"node1"})

	time.Sleep(100 * time.Millisecond) // Allow time for listener notification

	version1 := router.GetShardMapVersion()
	assert.Greater(t, version1, initialVersion, "Version should increment after shard creation")

	// Add another shard
	manager.CreateShard(1, []string{"node1"})
	time.Sleep(100 * time.Millisecond)

	version2 := router.GetShardMapVersion()
	assert.Greater(t, version2, version1, "Version should increment again")

	// Remove shard
	manager.RemoveShard(0)
	time.Sleep(100 * time.Millisecond)

	version3 := router.GetShardMapVersion()
	assert.Greater(t, version3, version2, "Version should increment on removal")
}

// TestIntegration_NodeFailureHandling tests handling of node failures
func TestIntegration_NodeFailureHandling(t *testing.T) {
	// Setup
	manager, fsm, router, _ := setupIntegrationTest(t, "node1")

	manager.Start()
	defer manager.Stop()

	// Register nodes
	manager.RegisterNode("node1", "localhost:8001", 10*GB)
	manager.RegisterNode("node2", "localhost:8002", 10*GB)
	manager.RegisterNode("node3", "localhost:8003", 10*GB)

	// Create shard
	manager.CreateShard(0, []string{"node1", "node2", "node3"})

	// Wait for shard to be added to router
	time.Sleep(100 * time.Millisecond)

	// Verify routing works
	routeInfo, err := router.RouteKey("test:key")
	require.NoError(t, err)
	assert.Len(t, routeInfo.Replicas, 3)

	// Mark node as down by proposing a command through FSM
	node1, _ := fsm.shardMap.GetNode("node1")
	node1.State = NodeStateDown

	// Update through FSM directly (in real scenario this would be through Raft)
	cmd := Command{
		Type: CommandUpdateNode,
	}
	cmd.Data, _ = json.Marshal(node1)
	cmdBytes, _ := json.Marshal(cmd)
	fsm.Apply(&raft.Log{Index: 1, Data: cmdBytes})

	// Routing should still work (returns all replicas, client decides which to use)
	routeInfo, err = router.RouteKey("test:key")
	require.NoError(t, err)
	assert.Len(t, routeInfo.Replicas, 3) // Still returns all replicas

	// Verify node state was updated
	updatedNode, _ := fsm.shardMap.GetNode("node1")
	assert.Equal(t, NodeStateDown, updatedNode.State)
}

// TestIntegration_MultipleActiveMigrations tests multiple concurrent migrations
func TestIntegration_MultipleActiveMigrations(t *testing.T) {
	// Setup
	manager, fsm, _, _ := setupIntegrationTest(t, "node1")
	migrator := NewMigrator(DefaultMigratorConfig(), manager, zaptest.NewLogger(t))

	manager.Start()
	defer manager.Stop()

	// Register nodes
	for i := 1; i <= 6; i++ {
		manager.RegisterNode(fmt.Sprintf("node%d", i), fmt.Sprintf("localhost:800%d", i), 10*GB)
	}

	// Create shards
	manager.CreateShard(0, []string{"node1", "node2", "node3"})
	manager.CreateShard(1, []string{"node2", "node3", "node4"})
	manager.CreateShard(2, []string{"node3", "node4", "node5"})

	// Start multiple migrations
	migration1ID, err := manager.StartMigration(0, []string{"node1", "node2"}, []string{"node5", "node6"})
	require.NoError(t, err)

	migration2ID, err := manager.StartMigration(1, []string{"node2", "node3"}, []string{"node1", "node6"})
	require.NoError(t, err)

	// Verify both migrations exist
	migration1, ok := fsm.shardMap.GetMigration(migration1ID)
	require.True(t, ok)
	assert.Equal(t, 0, migration1.ShardID)

	migration2, ok := fsm.shardMap.GetMigration(migration2ID)
	require.True(t, ok)
	assert.Equal(t, 1, migration2.ShardID)

	// Create stores for both migrations
	source1 := NewMockShardStore()
	target1 := NewMockShardStore()
	source2 := NewMockShardStore()
	target2 := NewMockShardStore()

	// Add data
	for i := 0; i < 50; i++ {
		source1.Put(context.Background(), fmt.Sprintf("shard0-key-%d", i), []byte("value"))
		source2.Put(context.Background(), fmt.Sprintf("shard1-key-%d", i), []byte("value"))
	}

	// Start both migrations concurrently
	ctx := context.Background()
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		migrator.StartMigration(ctx, migration1ID, source1, target1)
	}()

	go func() {
		defer wg.Done()
		migrator.StartMigration(ctx, migration2ID, source2, target2)
	}()

	wg.Wait()
	time.Sleep(1 * time.Second)

	// Verify both migrations completed
	assert.Equal(t, 50, len(target1.data))
	assert.Equal(t, 50, len(target2.data))
}

// TestIntegration_ConsistentHashingStability tests that hash ring remains stable
func TestIntegration_ConsistentHashingStability(t *testing.T) {
	// Setup
	manager, _, router, _ := setupIntegrationTest(t, "node1")

	manager.Start()
	defer manager.Stop()

	// Create initial shards
	manager.RegisterNode("node1", "localhost:8001", 10*GB)
	manager.CreateShard(0, []string{"node1"})
	manager.CreateShard(1, []string{"node1"})
	manager.CreateShard(2, []string{"node1"})

	// Wait for shards to be added to router
	time.Sleep(100 * time.Millisecond)

	// Route keys and remember their shards
	keyMappings := make(map[string]int)
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("stable-key-%d", i)
		routeInfo, err := router.RouteKey(key)
		require.NoError(t, err)
		keyMappings[key] = routeInfo.ShardID
	}

	// Add a new shard
	manager.CreateShard(3, []string{"node1"})
	time.Sleep(100 * time.Millisecond)

	// Re-route the same keys
	movedKeys := 0
	for key, originalShard := range keyMappings {
		routeInfo, err := router.RouteKey(key)
		require.NoError(t, err)
		if routeInfo.ShardID != originalShard {
			movedKeys++
		}
	}

	// With consistent hashing, only ~25% of keys should move (100/4 = 25)
	// Allow some tolerance
	t.Logf("Keys moved: %d out of 100 (%.1f%%)", movedKeys, float64(movedKeys))
	assert.Less(t, movedKeys, 40, "Should move less than 40% of keys")
	assert.Greater(t, movedKeys, 10, "Should move at least some keys")
}

// TestIntegration_RouterHealthCheck tests router health monitoring
func TestIntegration_RouterHealthCheck(t *testing.T) {
	// Setup
	manager, _, router, _ := setupIntegrationTest(t, "node1")

	manager.Start()
	defer manager.Stop()

	// Setup cluster
	manager.RegisterNode("node1", "localhost:8001", 10*GB)
	manager.CreateShard(0, []string{"node1"})

	// Check initial health - wait for shard to be added to router
	time.Sleep(100 * time.Millisecond)
	assert.Greater(t, router.GetShardCount(), 0)

	// Route some requests
	for i := 0; i < 10; i++ {
		_, err := router.RouteKey(fmt.Sprintf("key-%d", i))
		assert.NoError(t, err)
	}

	// Verify stats
	stats := router.GetRoutingStats()
	assert.Equal(t, 1, stats["shard_count"])
	assert.Greater(t, stats["shard_map_version"], uint64(0))

	// Remove all shards
	manager.RemoveShard(0)
	time.Sleep(100 * time.Millisecond)

	// Health should still report (but with 0 shards)
	assert.Equal(t, 0, router.GetShardCount())
}
