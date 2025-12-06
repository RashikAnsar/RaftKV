package sharding

import (
	"bytes"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// MockUpdateListener for testing
type MockUpdateListener struct {
	mu      sync.Mutex
	updates []*ShardMap
}

func (m *MockUpdateListener) OnShardMapUpdate(shardMap *ShardMap) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updates = append(m.updates, shardMap)
}

func (m *MockUpdateListener) GetUpdateCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.updates)
}

func (m *MockUpdateListener) GetLastUpdate() *ShardMap {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.updates) == 0 {
		return nil
	}
	return m.updates[len(m.updates)-1]
}

func TestMetaFSM_NewMetaFSM(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fsm := NewMetaFSM(logger)

	assert.NotNil(t, fsm)
	shardMap := fsm.GetShardMap()
	assert.NotNil(t, shardMap)
	assert.Equal(t, uint64(0), shardMap.Version)
}

func TestMetaFSM_ApplyAddShard(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fsm := NewMetaFSM(logger)

	shard := &ShardInfo{
		ID:           1,
		State:        ShardStateActive,
		Replicas:     []string{"node1", "node2"},
		LeaderNodeID: "node1",
	}

	// Create command
	cmd := Command{
		Type: CommandAddShard,
	}
	var err error
	cmd.Data, err = json.Marshal(shard)
	require.NoError(t, err)

	cmdBytes, err := json.Marshal(cmd)
	require.NoError(t, err)

	// Apply command
	log := &raft.Log{
		Index: 1,
		Data:  cmdBytes,
	}

	result := fsm.Apply(log)
	assert.Nil(t, result)

	// Verify shard was added
	shardMap := fsm.GetShardMap()
	retrievedShard, ok := shardMap.GetShard(1)
	require.True(t, ok)
	assert.Equal(t, shard.ID, retrievedShard.ID)
	assert.Equal(t, shard.Replicas, retrievedShard.Replicas)
}

func TestMetaFSM_ApplyRemoveShard(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fsm := NewMetaFSM(logger)

	// First add a shard
	shard := &ShardInfo{ID: 1, State: ShardStateActive}
	fsm.shardMap.AddShard(shard)

	// Create remove command
	cmd := Command{
		Type: CommandRemoveShard,
	}
	var err error
	cmd.Data, err = json.Marshal(1)
	require.NoError(t, err)

	cmdBytes, err := json.Marshal(cmd)
	require.NoError(t, err)

	// Apply command
	log := &raft.Log{
		Index: 2,
		Data:  cmdBytes,
	}

	result := fsm.Apply(log)
	assert.Nil(t, result)

	// Verify shard was removed
	shardMap := fsm.GetShardMap()
	_, ok := shardMap.GetShard(1)
	assert.False(t, ok)
}

func TestMetaFSM_ApplyUpdateShard(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fsm := NewMetaFSM(logger)

	// Add initial shard
	shard := &ShardInfo{
		ID:       1,
		State:    ShardStateActive,
		DataSize: 1024,
	}
	fsm.shardMap.AddShard(shard)

	// Create update command
	updatedShard := &ShardInfo{
		ID:       1,
		State:    ShardStateMigrating,
		DataSize: 2048,
	}

	cmd := Command{
		Type: CommandUpdateShard,
	}
	var err error
	cmd.Data, err = json.Marshal(UpdateShardCmd{
		ShardID: 1,
		Shard:   updatedShard,
	})
	require.NoError(t, err)

	cmdBytes, err := json.Marshal(cmd)
	require.NoError(t, err)

	// Apply command
	log := &raft.Log{
		Index: 3,
		Data:  cmdBytes,
	}

	result := fsm.Apply(log)
	assert.Nil(t, result)

	// Verify shard was updated
	shardMap := fsm.GetShardMap()
	retrieved, ok := shardMap.GetShard(1)
	require.True(t, ok)
	assert.Equal(t, ShardStateMigrating, retrieved.State)
	assert.Equal(t, int64(2048), retrieved.DataSize)
}

func TestMetaFSM_ApplyAddNode(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fsm := NewMetaFSM(logger)

	node := &NodeInfo{
		ID:      "node1",
		Address: "localhost:8080",
		State:   NodeStateActive,
	}

	// Create command
	cmd := Command{
		Type: CommandAddNode,
	}
	var err error
	cmd.Data, err = json.Marshal(node)
	require.NoError(t, err)

	cmdBytes, err := json.Marshal(cmd)
	require.NoError(t, err)

	// Apply command
	log := &raft.Log{
		Index: 4,
		Data:  cmdBytes,
	}

	result := fsm.Apply(log)
	assert.Nil(t, result)

	// Verify node was added
	shardMap := fsm.GetShardMap()
	retrievedNode, ok := shardMap.GetNode("node1")
	require.True(t, ok)
	assert.Equal(t, node.Address, retrievedNode.Address)
}

func TestMetaFSM_ApplyAddMigration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fsm := NewMetaFSM(logger)

	migration := &MigrationInfo{
		ID:          "migration-1",
		ShardID:     1,
		SourceNodes: []string{"node1"},
		TargetNodes: []string{"node2"},
		State:       MigrationStateCopying,
	}

	// Create command
	cmd := Command{
		Type: CommandAddMigration,
	}
	var err error
	cmd.Data, err = json.Marshal(migration)
	require.NoError(t, err)

	cmdBytes, err := json.Marshal(cmd)
	require.NoError(t, err)

	// Apply command
	log := &raft.Log{
		Index: 5,
		Data:  cmdBytes,
	}

	result := fsm.Apply(log)
	assert.Nil(t, result)

	// Verify migration was added
	shardMap := fsm.GetShardMap()
	retrieved, ok := shardMap.GetMigration("migration-1")
	require.True(t, ok)
	assert.Equal(t, migration.ShardID, retrieved.ShardID)
	assert.Equal(t, migration.State, retrieved.State)
}

func TestMetaFSM_Snapshot(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fsm := NewMetaFSM(logger)

	// Add some data
	shard := &ShardInfo{
		ID:       1,
		State:    ShardStateActive,
		Replicas: []string{"node1"},
	}
	fsm.shardMap.AddShard(shard)

	node := &NodeInfo{
		ID:    "node1",
		State: NodeStateActive,
	}
	fsm.shardMap.AddNode(node)

	// Create snapshot
	snapshot, err := fsm.Snapshot()
	require.NoError(t, err)
	assert.NotNil(t, snapshot)

	// Verify snapshot has the data
	metaSnapshot := snapshot.(*MetaFSMSnapshot)
	assert.Equal(t, 1, len(metaSnapshot.shardMap.Shards))
	assert.Equal(t, 1, len(metaSnapshot.shardMap.Nodes))
}

func TestMetaFSM_SnapshotPersistRestore(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fsm1 := NewMetaFSM(logger)

	// Add data to first FSM
	shard := &ShardInfo{
		ID:       1,
		State:    ShardStateActive,
		Replicas: []string{"node1", "node2"},
	}
	fsm1.shardMap.AddShard(shard)

	// Create and persist snapshot
	snapshot, err := fsm1.Snapshot()
	require.NoError(t, err)

	var buf bytes.Buffer
	sink := &mockSnapshotSink{buf: &buf}
	err = snapshot.Persist(sink)
	require.NoError(t, err)

	// Create new FSM and restore from snapshot
	fsm2 := NewMetaFSM(logger)
	reader := io.NopCloser(&buf)
	err = fsm2.Restore(reader)
	require.NoError(t, err)

	// Verify restored data
	shardMap := fsm2.GetShardMap()
	retrievedShard, ok := shardMap.GetShard(1)
	require.True(t, ok)
	assert.Equal(t, shard.ID, retrievedShard.ID)
	assert.Equal(t, shard.Replicas, retrievedShard.Replicas)
}

func TestMetaFSM_Listeners(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fsm := NewMetaFSM(logger)

	// Register listener
	listener := &MockUpdateListener{}
	fsm.RegisterListener(listener)

	// Add a shard (should trigger listener)
	shard := &ShardInfo{
		ID:    1,
		State: ShardStateActive,
	}

	cmd := Command{
		Type: CommandAddShard,
	}
	var err error
	cmd.Data, err = json.Marshal(shard)
	require.NoError(t, err)

	cmdBytes, err := json.Marshal(cmd)
	require.NoError(t, err)

	log := &raft.Log{
		Index: 1,
		Data:  cmdBytes,
	}

	fsm.Apply(log)

	// Wait for async notification
	time.Sleep(50 * time.Millisecond)

	// Verify listener was notified
	assert.Equal(t, 1, listener.GetUpdateCount())
	lastUpdate := listener.GetLastUpdate()
	assert.NotNil(t, lastUpdate)
	_, ok := lastUpdate.GetShard(1)
	assert.True(t, ok)
}

func TestMetaFSM_MultipleCommands(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fsm := NewMetaFSM(logger)

	// Add shard
	shard := &ShardInfo{ID: 1, State: ShardStateActive}
	cmd1 := Command{Type: CommandAddShard}
	cmd1.Data, _ = json.Marshal(shard)
	cmdBytes1, _ := json.Marshal(cmd1)
	fsm.Apply(&raft.Log{Index: 1, Data: cmdBytes1})

	// Add node
	node := &NodeInfo{ID: "node1", State: NodeStateActive}
	cmd2 := Command{Type: CommandAddNode}
	cmd2.Data, _ = json.Marshal(node)
	cmdBytes2, _ := json.Marshal(cmd2)
	fsm.Apply(&raft.Log{Index: 2, Data: cmdBytes2})

	// Add migration
	migration := &MigrationInfo{
		ID:      "migration-1",
		ShardID: 1,
		State:   MigrationStateCopying,
	}
	cmd3 := Command{Type: CommandAddMigration}
	cmd3.Data, _ = json.Marshal(migration)
	cmdBytes3, _ := json.Marshal(cmd3)
	fsm.Apply(&raft.Log{Index: 3, Data: cmdBytes3})

	// Verify all were applied
	shardMap := fsm.GetShardMap()
	_, ok := shardMap.GetShard(1)
	assert.True(t, ok)

	_, ok = shardMap.GetNode("node1")
	assert.True(t, ok)

	_, ok = shardMap.GetMigration("migration-1")
	assert.True(t, ok)
}

func TestMetaFSM_InvalidCommand(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fsm := NewMetaFSM(logger)

	// Invalid JSON
	log := &raft.Log{
		Index: 1,
		Data:  []byte("invalid json"),
	}

	result := fsm.Apply(log)
	assert.Error(t, result.(error))
}

func TestMetaFSM_UnknownCommandType(t *testing.T) {
	logger := zaptest.NewLogger(t)
	fsm := NewMetaFSM(logger)

	cmd := Command{
		Type: CommandType("unknown"),
		Data: json.RawMessage("{}"),
	}

	cmdBytes, _ := json.Marshal(cmd)
	log := &raft.Log{
		Index: 1,
		Data:  cmdBytes,
	}

	result := fsm.Apply(log)
	assert.Error(t, result.(error))
	assert.Contains(t, result.(error).Error(), "unknown command type")
}

// Mock snapshot sink for testing
type mockSnapshotSink struct {
	buf      *bytes.Buffer
	canceled bool
}

func (m *mockSnapshotSink) Write(p []byte) (n int, err error) {
	return m.buf.Write(p)
}

func (m *mockSnapshotSink) Close() error {
	return nil
}

func (m *mockSnapshotSink) ID() string {
	return "mock-snapshot"
}

func (m *mockSnapshotSink) Cancel() error {
	m.canceled = true
	return nil
}
