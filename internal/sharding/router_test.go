package sharding

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestRouter_NewRouter(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewRouter(150, logger)

	assert.NotNil(t, router)
	assert.Equal(t, 0, router.GetShardCount())
	assert.True(t, router.IsHealthy() == false) // No shards yet
}

func TestRouter_AddShard(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewRouter(150, logger)

	shard := &ShardInfo{
		ID:            1,
		State:         ShardStateActive,
		Replicas:      []string{"node1", "node2", "node3"},
		LeaderNodeID:  "node1",
		KeyRangeStart: 0,
		KeyRangeEnd:   1000,
	}

	err := router.AddShard(shard)
	require.NoError(t, err)

	assert.Equal(t, 1, router.GetShardCount())
	assert.True(t, router.IsHealthy())

	// Verify we can get shard info
	retrieved, err := router.GetShardInfo(1)
	require.NoError(t, err)
	assert.Equal(t, shard.ID, retrieved.ID)
	assert.Equal(t, shard.Replicas, retrieved.Replicas)
}

func TestRouter_AddShardDuplicate(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewRouter(150, logger)

	shard := &ShardInfo{
		ID:       1,
		State:    ShardStateActive,
		Replicas: []string{"node1"},
	}

	err := router.AddShard(shard)
	require.NoError(t, err)

	// Try to add duplicate
	err = router.AddShard(shard)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestRouter_RemoveShard(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewRouter(150, logger)

	// Add shards
	for i := 1; i <= 3; i++ {
		shard := &ShardInfo{
			ID:       i,
			State:    ShardStateActive,
			Replicas: []string{"node1"},
		}
		router.AddShard(shard)
	}

	assert.Equal(t, 3, router.GetShardCount())

	// Remove shard
	err := router.RemoveShard(2)
	require.NoError(t, err)

	assert.Equal(t, 2, router.GetShardCount())

	// Verify shard is gone
	_, err = router.GetShardInfo(2)
	assert.Error(t, err)
}

func TestRouter_RouteKey(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewRouter(150, logger)

	// Add shards
	for i := 0; i < 3; i++ {
		shard := &ShardInfo{
			ID:           i,
			State:        ShardStateActive,
			Replicas:     []string{fmt.Sprintf("node%d", i)},
			LeaderNodeID: fmt.Sprintf("node%d", i),
		}
		router.AddShard(shard)
	}

	// Route some keys
	keys := []string{"user:1", "user:2", "user:3", "order:100"}
	shardCounts := make(map[int]int)

	for _, key := range keys {
		route, err := router.RouteKey(key)
		require.NoError(t, err)
		assert.NotNil(t, route)
		assert.GreaterOrEqual(t, route.ShardID, 0)
		assert.LessOrEqual(t, route.ShardID, 2)
		assert.NotEmpty(t, route.Replicas)
		assert.NotEmpty(t, route.LeaderNodeID)
		assert.False(t, route.IsMigrating)
		shardCounts[route.ShardID]++
	}

	// Verify keys are distributed
	assert.Greater(t, len(shardCounts), 0)
}

func TestRouter_RouteKeyWithMigration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewRouter(150, logger)

	// Add shard
	shard := &ShardInfo{
		ID:           1,
		State:        ShardStateMigrating,
		Replicas:     []string{"node1", "node2"},
		LeaderNodeID: "node1",
	}
	router.AddShard(shard)

	// Add migration
	migration := &MigrationInfo{
		ID:          "migration-1",
		ShardID:     1,
		SourceNodes: []string{"node1"},
		TargetNodes: []string{"node2"},
		State:       MigrationStateCopying,
	}
	router.shardMap.AddMigration(migration)

	// Route key
	route, err := router.RouteKey("test-key")
	require.NoError(t, err)

	assert.Equal(t, 1, route.ShardID)
	assert.True(t, route.IsMigrating)
	assert.NotNil(t, route.MigrationInfo)
	assert.Equal(t, "migration-1", route.MigrationInfo.ID)
	assert.Equal(t, MigrationStateCopying, route.MigrationInfo.State)
}

func TestRouter_UpdateShardMap(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewRouter(150, logger)

	// Add initial shards
	for i := 0; i < 2; i++ {
		shard := &ShardInfo{
			ID:       i,
			State:    ShardStateActive,
			Replicas: []string{"node1"},
		}
		router.AddShard(shard)
	}

	initialVersion := router.GetShardMapVersion()
	assert.Equal(t, 2, router.GetShardCount())

	// Create new shard map with different shards
	newShardMap := NewShardMap()

	// Add shards 1, 2, 3 (removing 0, adding 2 and 3)
	for i := 1; i <= 3; i++ {
		shard := &ShardInfo{
			ID:       i,
			State:    ShardStateActive,
			Replicas: []string{"node1"},
		}
		newShardMap.AddShard(shard)
	}

	// Set version after adding shards
	expectedVersion := initialVersion + 1
	newShardMap.mu.Lock()
	newShardMap.Version = expectedVersion
	newShardMap.mu.Unlock()

	// Update router
	err := router.UpdateShardMap(newShardMap)
	require.NoError(t, err)

	// Verify changes
	assert.Equal(t, 3, router.GetShardCount())
	assert.Equal(t, expectedVersion, router.GetShardMapVersion())

	// Shard 0 should be gone
	_, err = router.GetShardInfo(0)
	assert.Error(t, err)

	// Shards 1, 2, 3 should exist
	for i := 1; i <= 3; i++ {
		_, err := router.GetShardInfo(i)
		assert.NoError(t, err)
	}
}

func TestRouter_UpdateShardMapStaleVersion(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewRouter(150, logger)

	// Add initial shard
	shard := &ShardInfo{
		ID:       1,
		State:    ShardStateActive,
		Replicas: []string{"node1"},
	}
	router.AddShard(shard)

	currentVersion := router.GetShardMapVersion()

	// Try to update with stale version
	staleShardMap := NewShardMap()
	staleShardMap.Version = currentVersion - 1
	staleShardMap.AddShard(&ShardInfo{ID: 2, State: ShardStateActive})

	err := router.UpdateShardMap(staleShardMap)
	require.NoError(t, err) // No error, but should be ignored

	// Version should not change
	assert.Equal(t, currentVersion, router.GetShardMapVersion())

	// Shard 2 should not be added
	_, err = router.GetShardInfo(2)
	assert.Error(t, err)
}

func TestRouter_GetNodeInfo(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewRouter(150, logger)

	// Add node to shard map
	node := &NodeInfo{
		ID:      "node1",
		Address: "localhost:8080",
		State:   NodeStateActive,
	}
	router.shardMap.AddNode(node)

	// Get node info
	retrieved, err := router.GetNodeInfo("node1")
	require.NoError(t, err)
	assert.Equal(t, node.ID, retrieved.ID)
	assert.Equal(t, node.Address, retrieved.Address)

	// Try non-existent node
	_, err = router.GetNodeInfo("node999")
	assert.Error(t, err)
}

func TestRouter_GetAllShards(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewRouter(150, logger)

	// Add active shards
	for i := 0; i < 3; i++ {
		shard := &ShardInfo{
			ID:       i,
			State:    ShardStateActive,
			Replicas: []string{"node1"},
		}
		router.AddShard(shard)
	}

	// Add inactive shard
	inactiveShard := &ShardInfo{
		ID:    99,
		State: ShardStateInactive,
	}
	router.shardMap.AddShard(inactiveShard)

	// Get all active shards
	shards := router.GetAllShards()
	assert.Len(t, shards, 3)

	ids := make(map[int]bool)
	for _, shard := range shards {
		ids[shard.ID] = true
	}

	assert.True(t, ids[0])
	assert.True(t, ids[1])
	assert.True(t, ids[2])
	assert.False(t, ids[99]) // Inactive shard should not be included
}

func TestRouter_GetAllNodes(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewRouter(150, logger)

	// Add active nodes
	for i := 1; i <= 3; i++ {
		node := &NodeInfo{
			ID:    fmt.Sprintf("node%d", i),
			State: NodeStateActive,
		}
		router.shardMap.AddNode(node)
	}

	// Add draining node
	drainingNode := &NodeInfo{
		ID:    "node99",
		State: NodeStateDraining,
	}
	router.shardMap.AddNode(drainingNode)

	// Get all active nodes
	nodes := router.GetAllNodes()
	assert.Len(t, nodes, 3)

	ids := make(map[string]bool)
	for _, node := range nodes {
		ids[node.ID] = true
	}

	assert.True(t, ids["node1"])
	assert.True(t, ids["node2"])
	assert.True(t, ids["node3"])
	assert.False(t, ids["node99"]) // Draining node should not be included
}

func TestRouter_GetActiveMigrations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewRouter(150, logger)

	// Add active migrations
	for i := 1; i <= 3; i++ {
		migration := &MigrationInfo{
			ID:      fmt.Sprintf("migration-%d", i),
			ShardID: i,
			State:   MigrationStateCopying,
		}
		router.shardMap.AddMigration(migration)
	}

	// Add completed migration
	completedMigration := &MigrationInfo{
		ID:      "migration-99",
		ShardID: 99,
		State:   MigrationStateComplete,
	}
	router.shardMap.AddMigration(completedMigration)

	// Get active migrations
	migrations := router.GetActiveMigrations()
	assert.Len(t, migrations, 3)

	ids := make(map[string]bool)
	for _, migration := range migrations {
		ids[migration.ID] = true
	}

	assert.True(t, ids["migration-1"])
	assert.True(t, ids["migration-2"])
	assert.True(t, ids["migration-3"])
	assert.False(t, ids["migration-99"]) // Completed migration should not be included
}

func TestRouter_IsHealthy(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewRouter(150, logger)

	// Unhealthy: no shards
	assert.False(t, router.IsHealthy())

	// Add shard
	shard := &ShardInfo{
		ID:       1,
		State:    ShardStateActive,
		Replicas: []string{"node1"},
	}
	router.AddShard(shard)

	// Healthy now
	assert.True(t, router.IsHealthy())

	// Add failed migration
	failedMigration := &MigrationInfo{
		ID:      "migration-failed",
		ShardID: 1,
		State:   MigrationStateFailed,
	}
	router.shardMap.AddMigration(failedMigration)

	// Unhealthy: failed migration
	assert.False(t, router.IsHealthy())
}

func TestRouter_GetRoutingStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewRouter(150, logger)

	// Add shards
	for i := 0; i < 3; i++ {
		shard := &ShardInfo{
			ID:       i,
			State:    ShardStateActive,
			Replicas: []string{"node1"},
		}
		router.AddShard(shard)
	}

	// Add nodes
	for i := 1; i <= 2; i++ {
		node := &NodeInfo{
			ID:    fmt.Sprintf("node%d", i),
			State: NodeStateActive,
		}
		router.shardMap.AddNode(node)
	}

	// Add migration
	migration := &MigrationInfo{
		ID:      "migration-1",
		ShardID: 1,
		State:   MigrationStateCopying,
	}
	router.shardMap.AddMigration(migration)

	stats := router.GetRoutingStats()

	assert.Equal(t, 3, stats["shard_count"])
	assert.Equal(t, 2, stats["node_count"])
	assert.Equal(t, 1, stats["active_migrations"])
	assert.NotNil(t, stats["virtual_node_distribution"])

	// Check virtual node distribution
	distribution := stats["virtual_node_distribution"].(map[int]int)
	for shardID, count := range distribution {
		assert.Equal(t, 150, count, "Shard %d should have 150 virtual nodes", shardID)
	}
}

func TestRouter_Concurrent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewRouter(150, logger)

	// Add initial shards
	for i := 0; i < 5; i++ {
		shard := &ShardInfo{
			ID:       i,
			State:    ShardStateActive,
			Replicas: []string{"node1"},
		}
		router.AddShard(shard)
	}

	done := make(chan bool)

	// Concurrent readers
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("key:%d:%d", id, j)
				router.RouteKey(key)
				router.GetAllShards()
				router.IsHealthy()
			}
			done <- true
		}(i)
	}

	// Concurrent shard map updates
	for i := 0; i < 5; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				newShardMap := NewShardMap()
				newShardMap.Version = uint64(time.Now().UnixNano())

				for k := 0; k < 5; k++ {
					shard := &ShardInfo{
						ID:       k,
						State:    ShardStateActive,
						Replicas: []string{"node1"},
					}
					newShardMap.AddShard(shard)
				}

				router.UpdateShardMap(newShardMap)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 15; i++ {
		<-done
	}

	// Verify router is still functional
	assert.True(t, router.IsHealthy())
	assert.Equal(t, 5, router.GetShardCount())
}

func TestRouter_KeyConsistency(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewRouter(150, logger)

	// Add shards
	for i := 0; i < 3; i++ {
		shard := &ShardInfo{
			ID:       i,
			State:    ShardStateActive,
			Replicas: []string{"node1"},
		}
		router.AddShard(shard)
	}

	// Same key should always route to same shard
	key := "user:123"
	route1, _ := router.RouteKey(key)
	route2, _ := router.RouteKey(key)
	route3, _ := router.RouteKey(key)

	assert.Equal(t, route1.ShardID, route2.ShardID)
	assert.Equal(t, route2.ShardID, route3.ShardID)
}
