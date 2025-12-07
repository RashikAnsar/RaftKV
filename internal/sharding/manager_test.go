package sharding

import (
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// MockRaft is a mock implementation of RaftNode interface for testing
type MockRaft struct {
	state       raft.RaftState
	applyFunc   func([]byte, time.Duration) raft.ApplyFuture
	appliedLogs [][]byte
}

func NewMockRaft() *MockRaft {
	return &MockRaft{
		state:       raft.Leader,
		appliedLogs: make([][]byte, 0),
	}
}

func (m *MockRaft) State() raft.RaftState {
	return m.state
}

func (m *MockRaft) Apply(cmd []byte, timeout time.Duration) raft.ApplyFuture {
	m.appliedLogs = append(m.appliedLogs, cmd)
	if m.applyFunc != nil {
		return m.applyFunc(cmd, timeout)
	}
	return &MockApplyFuture{err: nil}
}

// MockApplyFuture implements raft.ApplyFuture
type MockApplyFuture struct {
	err      error
	response interface{}
}

func (m *MockApplyFuture) Error() error                { return m.err }
func (m *MockApplyFuture) Response() interface{}       { return m.response }
func (m *MockApplyFuture) Index() uint64               { return 1 }
func (m *MockApplyFuture) Term() uint64                { return 1 }
func (m *MockApplyFuture) Resp() interface{}           { return m.response }
func (m *MockApplyFuture) ResultCh() <-chan raft.RPCResponse {
	return nil
}

func TestManager_NewManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultManagerConfig("node1")
	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)

	manager := NewManager(config, mockRaft, fsm, router, logger)

	assert.NotNil(t, manager)
	assert.Equal(t, config.NodeID, manager.config.NodeID)
}

func TestManager_CreateShard(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultManagerConfig("node1")
	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)

	manager := NewManager(config, mockRaft, fsm, router, logger)

	replicas := []string{"node1", "node2", "node3"}
	err := manager.CreateShard(1, replicas)
	require.NoError(t, err)

	// Verify command was applied to Raft
	assert.Equal(t, 1, len(mockRaft.appliedLogs))
}

func TestManager_CreateShardNotLeader(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultManagerConfig("node1")
	mockRaft := NewMockRaft()
	mockRaft.state = raft.Follower // Not leader
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)

	manager := NewManager(config, mockRaft, fsm, router, logger)

	replicas := []string{"node1", "node2", "node3"}
	err := manager.CreateShard(1, replicas)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not leader")
}

func TestManager_RemoveShard(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultManagerConfig("node1")
	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)

	// Add a shard to FSM first
	shard := &ShardInfo{
		ID:    1,
		State: ShardStateActive,
	}
	fsm.shardMap.AddShard(shard)

	manager := NewManager(config, mockRaft, fsm, router, logger)

	err := manager.RemoveShard(1)
	require.NoError(t, err)

	// Verify command was applied
	assert.Equal(t, 1, len(mockRaft.appliedLogs))
}

func TestManager_RemoveShardWithActiveMigration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultManagerConfig("node1")
	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)

	// Add shard and active migration
	shard := &ShardInfo{
		ID:    1,
		State: ShardStateMigrating,
	}
	fsm.shardMap.AddShard(shard)

	migration := &MigrationInfo{
		ID:      "migration-1",
		ShardID: 1,
		State:   MigrationStateCopying,
	}
	fsm.shardMap.AddMigration(migration)

	manager := NewManager(config, mockRaft, fsm, router, logger)

	err := manager.RemoveShard(1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "active migration")
}

func TestManager_StartMigration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultManagerConfig("node1")
	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)

	// Add shard
	shard := &ShardInfo{
		ID:       1,
		State:    ShardStateActive,
		KeyCount: 1000,
	}
	fsm.shardMap.AddShard(shard)

	manager := NewManager(config, mockRaft, fsm, router, logger)

	sourceNodes := []string{"node1", "node2"}
	targetNodes := []string{"node3", "node4"}
	migrationID, err := manager.StartMigration(1, sourceNodes, targetNodes)
	require.NoError(t, err)

	assert.NotEmpty(t, migrationID)
	assert.Contains(t, migrationID, "migration-1-")

	// Verify command was applied
	assert.Equal(t, 1, len(mockRaft.appliedLogs))
}

func TestManager_StartMigrationShardNotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultManagerConfig("node1")
	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)

	manager := NewManager(config, mockRaft, fsm, router, logger)

	sourceNodes := []string{"node1"}
	targetNodes := []string{"node2"}
	_, err := manager.StartMigration(999, sourceNodes, targetNodes)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_StartMigrationAlreadyMigrating(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultManagerConfig("node1")
	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)

	// Add shard with active migration
	shard := &ShardInfo{
		ID:    1,
		State: ShardStateMigrating,
	}
	fsm.shardMap.AddShard(shard)

	migration := &MigrationInfo{
		ID:      "existing-migration",
		ShardID: 1,
		State:   MigrationStateCopying,
	}
	fsm.shardMap.AddMigration(migration)

	manager := NewManager(config, mockRaft, fsm, router, logger)

	_, err := manager.StartMigration(1, []string{"node1"}, []string{"node2"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already has active migration")
}

func TestManager_UpdateMigrationState(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultManagerConfig("node1")
	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)

	// Add migration
	migration := &MigrationInfo{
		ID:         "migration-1",
		ShardID:    1,
		State:      MigrationStateCopying,
		Progress:   0.5,
		TotalKeys:  1000,
		KeysCopied: 500,
	}
	fsm.shardMap.AddMigration(migration)

	manager := NewManager(config, mockRaft, fsm, router, logger)

	err := manager.UpdateMigrationState("migration-1", MigrationStateSyncing, 0.75, 750)
	require.NoError(t, err)

	// Verify command was applied
	assert.Equal(t, 1, len(mockRaft.appliedLogs))
}

func TestManager_CompleteMigration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultManagerConfig("node1")
	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)

	// Add shard and migration
	shard := &ShardInfo{
		ID:           1,
		State:        ShardStateMigrating,
		Replicas:     []string{"node1", "node2"},
		LeaderNodeID: "node1",
	}
	fsm.shardMap.AddShard(shard)

	migration := &MigrationInfo{
		ID:          "migration-1",
		ShardID:     1,
		SourceNodes: []string{"node1", "node2"},
		TargetNodes: []string{"node3", "node4"},
		State:       MigrationStateCutover,
		TotalKeys:   1000,
	}
	fsm.shardMap.AddMigration(migration)

	manager := NewManager(config, mockRaft, fsm, router, logger)

	err := manager.CompleteMigration("migration-1")
	require.NoError(t, err)

	// Should have applied 2 commands: update migration + update shard
	assert.Equal(t, 2, len(mockRaft.appliedLogs))
}

func TestManager_RegisterNode(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultManagerConfig("node1")
	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)

	manager := NewManager(config, mockRaft, fsm, router, logger)

	err := manager.RegisterNode("node2", "localhost:8080", 1073741824) // 1GB
	require.NoError(t, err)

	// Verify command was applied
	assert.Equal(t, 1, len(mockRaft.appliedLogs))
}

func TestManager_DeregisterNode(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultManagerConfig("node1")
	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)

	// Add node
	node := &NodeInfo{
		ID:    "node2",
		State: NodeStateActive,
	}
	fsm.shardMap.AddNode(node)

	manager := NewManager(config, mockRaft, fsm, router, logger)

	err := manager.DeregisterNode("node2")
	require.NoError(t, err)

	// Verify command was applied
	assert.Equal(t, 1, len(mockRaft.appliedLogs))
}

func TestManager_DeregisterNodeWithShards(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultManagerConfig("node1")
	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)

	// Add node and shard
	node := &NodeInfo{
		ID:    "node2",
		State: NodeStateActive,
	}
	fsm.shardMap.AddNode(node)

	shard := &ShardInfo{
		ID:       1,
		State:    ShardStateActive,
		Replicas: []string{"node2", "node3"},
	}
	fsm.shardMap.AddShard(shard)

	manager := NewManager(config, mockRaft, fsm, router, logger)

	err := manager.DeregisterNode("node2")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "still has shard")
}

func TestManager_OnShardMapUpdate(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultManagerConfig("node1")
	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)

	manager := NewManager(config, mockRaft, fsm, router, logger)

	// Create updated shard map
	newShardMap := NewShardMap()
	shard := &ShardInfo{
		ID:    1,
		State: ShardStateActive,
	}
	newShardMap.AddShard(shard)

	migration := &MigrationInfo{
		ID:      "migration-1",
		ShardID: 1,
		State:   MigrationStateCopying,
	}
	newShardMap.AddMigration(migration)

	// Trigger update
	manager.OnShardMapUpdate(newShardMap)

	// Verify router was updated
	assert.Equal(t, newShardMap.Version, manager.router.GetShardMapVersion())

	// Verify active migrations were updated
	activeMigrations := manager.GetActiveMigrations()
	assert.Len(t, activeMigrations, 1)
	assert.Equal(t, "migration-1", activeMigrations[0].ID)
}

func TestManager_GetActiveMigrations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultManagerConfig("node1")
	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)

	manager := NewManager(config, mockRaft, fsm, router, logger)

	// Initially empty
	migrations := manager.GetActiveMigrations()
	assert.Empty(t, migrations)

	// Add migrations
	shardMap := NewShardMap()
	for i := 1; i <= 3; i++ {
		migration := &MigrationInfo{
			ID:      fmt.Sprintf("migration-%d", i),
			ShardID: i,
			State:   MigrationStateCopying,
		}
		shardMap.AddMigration(migration)
	}

	manager.OnShardMapUpdate(shardMap)

	// Verify we get all active migrations
	migrations = manager.GetActiveMigrations()
	assert.Len(t, migrations, 3)
}

func TestManager_StartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultManagerConfig("node1")
	config.RebalanceInterval = 100 * time.Millisecond
	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)

	manager := NewManager(config, mockRaft, fsm, router, logger)

	// Start manager
	err := manager.Start()
	require.NoError(t, err)

	// Let it run for a bit
	time.Sleep(250 * time.Millisecond)

	// Stop manager
	err = manager.Stop()
	require.NoError(t, err)
}
