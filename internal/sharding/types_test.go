package sharding

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShardState_String(t *testing.T) {
	tests := []struct {
		state    ShardState
		expected string
	}{
		{ShardStateActive, "active"},
		{ShardStateMigrating, "migrating"},
		{ShardStateSplitting, "splitting"},
		{ShardStateInactive, "inactive"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.state.String())
	}
}

func TestMigrationState_String(t *testing.T) {
	tests := []struct {
		state    MigrationState
		expected string
	}{
		{MigrationStatePreparing, "preparing"},
		{MigrationStateCopying, "copying"},
		{MigrationStateSyncing, "syncing"},
		{MigrationStateDualWrite, "dual_write"},
		{MigrationStateCutover, "cutover"},
		{MigrationStateCleanup, "cleanup"},
		{MigrationStateComplete, "complete"},
		{MigrationStateFailed, "failed"},
		{MigrationStateRolledBack, "rolled_back"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.state.String())
	}
}

func TestNodeState_String(t *testing.T) {
	tests := []struct {
		state    NodeState
		expected string
	}{
		{NodeStateActive, "active"},
		{NodeStateDraining, "draining"},
		{NodeStateDown, "down"},
		{NodeStateRemoved, "removed"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.state.String())
	}
}

func TestShardMap_NewShardMap(t *testing.T) {
	sm := NewShardMap()

	assert.NotNil(t, sm)
	assert.Equal(t, uint64(0), sm.Version)
	assert.Empty(t, sm.Shards)
	assert.Empty(t, sm.Nodes)
	assert.Empty(t, sm.Migrations)
}

func TestShardMap_AddShard(t *testing.T) {
	sm := NewShardMap()

	shard := &ShardInfo{
		ID:            1,
		State:         ShardStateActive,
		Replicas:      []string{"node1", "node2", "node3"},
		LeaderNodeID:  "node1",
		KeyRangeStart: 0,
		KeyRangeEnd:   1000,
		DataSize:      1024,
		KeyCount:      100,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	sm.AddShard(shard)

	assert.Equal(t, uint64(1), sm.Version)
	retrieved, ok := sm.GetShard(1)
	require.True(t, ok)
	assert.Equal(t, shard.ID, retrieved.ID)
	assert.Equal(t, shard.State, retrieved.State)
}

func TestShardMap_UpdateShard(t *testing.T) {
	sm := NewShardMap()

	shard := &ShardInfo{
		ID:       1,
		State:    ShardStateActive,
		DataSize: 1024,
	}
	sm.AddShard(shard)

	initialVersion := sm.Version

	// Update shard
	sm.UpdateShard(1, func(s *ShardInfo) {
		s.DataSize = 2048
		s.State = ShardStateMigrating
	})

	assert.Greater(t, sm.Version, initialVersion)
	retrieved, _ := sm.GetShard(1)
	assert.Equal(t, int64(2048), retrieved.DataSize)
	assert.Equal(t, ShardStateMigrating, retrieved.State)
}

func TestShardMap_AddNode(t *testing.T) {
	sm := NewShardMap()

	node := &NodeInfo{
		ID:           "node1",
		Address:      "localhost:8080",
		State:        NodeStateActive,
		Capacity:     1073741824, // 1GB
		Used:         0,
		Load:         0.0,
		Shards:       []int{1, 2, 3},
		LastHeartbeat: time.Now(),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	sm.AddNode(node)

	assert.Equal(t, uint64(1), sm.Version)
	retrieved, ok := sm.GetNode("node1")
	require.True(t, ok)
	assert.Equal(t, node.ID, retrieved.ID)
	assert.Equal(t, node.Address, retrieved.Address)
}

func TestShardMap_UpdateNode(t *testing.T) {
	sm := NewShardMap()

	node := &NodeInfo{
		ID:    "node1",
		State: NodeStateActive,
		Load:  0.5,
	}
	sm.AddNode(node)

	initialVersion := sm.Version

	// Update node
	sm.UpdateNode("node1", func(n *NodeInfo) {
		n.Load = 0.8
		n.State = NodeStateDraining
	})

	assert.Greater(t, sm.Version, initialVersion)
	retrieved, _ := sm.GetNode("node1")
	assert.Equal(t, 0.8, retrieved.Load)
	assert.Equal(t, NodeStateDraining, retrieved.State)
}

func TestShardMap_AddMigration(t *testing.T) {
	sm := NewShardMap()

	migration := &MigrationInfo{
		ID:          "migration-1",
		ShardID:     1,
		SourceNodes: []string{"node1", "node2"},
		TargetNodes: []string{"node3", "node4"},
		State:       MigrationStateCopying,
		Progress:    0.5,
		KeysCopied:  5000,
		TotalKeys:   10000,
		StartTime:   time.Now(),
	}

	sm.AddMigration(migration)

	assert.Equal(t, uint64(1), sm.Version)
	retrieved, ok := sm.GetMigration("migration-1")
	require.True(t, ok)
	assert.Equal(t, migration.ID, retrieved.ID)
	assert.Equal(t, migration.ShardID, retrieved.ShardID)
}

func TestShardMap_GetMigrationForShard(t *testing.T) {
	sm := NewShardMap()

	// Add active migration for shard 1
	migration1 := &MigrationInfo{
		ID:      "migration-1",
		ShardID: 1,
		State:   MigrationStateCopying,
	}
	sm.AddMigration(migration1)

	// Add completed migration for shard 2
	migration2 := &MigrationInfo{
		ID:      "migration-2",
		ShardID: 2,
		State:   MigrationStateComplete,
	}
	sm.AddMigration(migration2)

	// Should find active migration for shard 1
	found := sm.GetMigrationForShard(1)
	require.NotNil(t, found)
	assert.Equal(t, "migration-1", found.ID)

	// Should not find migration for shard 2 (completed)
	found = sm.GetMigrationForShard(2)
	assert.Nil(t, found)

	// Should not find migration for non-existent shard
	found = sm.GetMigrationForShard(999)
	assert.Nil(t, found)
}

func TestShardMap_RemoveMigration(t *testing.T) {
	sm := NewShardMap()

	migration := &MigrationInfo{
		ID:      "migration-1",
		ShardID: 1,
		State:   MigrationStateComplete,
	}
	sm.AddMigration(migration)

	// Remove migration
	sm.RemoveMigration("migration-1")

	_, ok := sm.GetMigration("migration-1")
	assert.False(t, ok)
}

func TestShardMap_GetActiveShards(t *testing.T) {
	sm := NewShardMap()

	// Add active shards
	sm.AddShard(&ShardInfo{ID: 1, State: ShardStateActive})
	sm.AddShard(&ShardInfo{ID: 2, State: ShardStateActive})

	// Add inactive shard
	sm.AddShard(&ShardInfo{ID: 3, State: ShardStateInactive})

	// Add migrating shard
	sm.AddShard(&ShardInfo{ID: 4, State: ShardStateMigrating})

	active := sm.GetActiveShards()
	assert.Len(t, active, 2)

	ids := make(map[int]bool)
	for _, shard := range active {
		ids[shard.ID] = true
	}
	assert.True(t, ids[1])
	assert.True(t, ids[2])
	assert.False(t, ids[3])
	assert.False(t, ids[4])
}

func TestShardMap_GetActiveNodes(t *testing.T) {
	sm := NewShardMap()

	// Add active nodes
	sm.AddNode(&NodeInfo{ID: "node1", State: NodeStateActive})
	sm.AddNode(&NodeInfo{ID: "node2", State: NodeStateActive})

	// Add draining node
	sm.AddNode(&NodeInfo{ID: "node3", State: NodeStateDraining})

	// Add down node
	sm.AddNode(&NodeInfo{ID: "node4", State: NodeStateDown})

	active := sm.GetActiveNodes()
	assert.Len(t, active, 2)

	ids := make(map[string]bool)
	for _, node := range active {
		ids[node.ID] = true
	}
	assert.True(t, ids["node1"])
	assert.True(t, ids["node2"])
	assert.False(t, ids["node3"])
	assert.False(t, ids["node4"])
}

func TestShardMap_GetActiveMigrations(t *testing.T) {
	sm := NewShardMap()

	// Add active migrations
	sm.AddMigration(&MigrationInfo{ID: "m1", ShardID: 1, State: MigrationStateCopying})
	sm.AddMigration(&MigrationInfo{ID: "m2", ShardID: 2, State: MigrationStateSyncing})

	// Add completed migration
	sm.AddMigration(&MigrationInfo{ID: "m3", ShardID: 3, State: MigrationStateComplete})

	// Add failed migration
	sm.AddMigration(&MigrationInfo{ID: "m4", ShardID: 4, State: MigrationStateFailed})

	active := sm.GetActiveMigrations()
	assert.Len(t, active, 2)

	ids := make(map[string]bool)
	for _, migration := range active {
		ids[migration.ID] = true
	}
	assert.True(t, ids["m1"])
	assert.True(t, ids["m2"])
	assert.False(t, ids["m3"])
	assert.False(t, ids["m4"])
}

func TestShardMap_Clone(t *testing.T) {
	sm := NewShardMap()

	// Add some data
	sm.AddShard(&ShardInfo{
		ID:       1,
		State:    ShardStateActive,
		Replicas: []string{"node1", "node2"},
	})
	sm.AddNode(&NodeInfo{
		ID:     "node1",
		State:  NodeStateActive,
		Shards: []int{1, 2},
	})
	sm.AddMigration(&MigrationInfo{
		ID:          "m1",
		ShardID:     1,
		SourceNodes: []string{"node1"},
		TargetNodes: []string{"node2"},
		State:       MigrationStateCopying,
	})

	// Clone
	clone := sm.Clone()

	// Verify clone is equal
	assert.Equal(t, sm.Version, clone.Version)
	assert.Equal(t, len(sm.Shards), len(clone.Shards))
	assert.Equal(t, len(sm.Nodes), len(clone.Nodes))
	assert.Equal(t, len(sm.Migrations), len(clone.Migrations))

	// Verify deep copy - modifications to original don't affect clone
	sm.UpdateShard(1, func(s *ShardInfo) {
		s.State = ShardStateMigrating
	})

	clonedShard, _ := clone.GetShard(1)
	assert.Equal(t, ShardStateActive, clonedShard.State) // Should still be active

	// Verify slice deep copy
	sm.Shards[1].Replicas[0] = "modified"
	assert.Equal(t, "node1", clone.Shards[1].Replicas[0]) // Should be unchanged
}

func TestShardMap_Concurrent(t *testing.T) {
	sm := NewShardMap()

	// Add initial shard
	sm.AddShard(&ShardInfo{
		ID:    1,
		State: ShardStateActive,
	})

	// Concurrent reads and writes
	done := make(chan bool)

	// Reader goroutines
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				sm.GetShard(1)
				sm.GetActiveShards()
			}
			done <- true
		}()
	}

	// Writer goroutines
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				sm.UpdateShard(1, func(s *ShardInfo) {
					s.KeyCount++
				})
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 15; i++ {
		<-done
	}

	// Verify final state
	shard, ok := sm.GetShard(1)
	require.True(t, ok)
	assert.Equal(t, int64(500), shard.KeyCount) // 5 writers * 100 increments
}
