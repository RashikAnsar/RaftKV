//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/RashikAnsar/raftkv/internal/sharding"
)

const (
	GB = 1024 * 1024 * 1024 // 1 GB in bytes
)

// MockRaft is a mock implementation of RaftNode interface for integration testing
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

func (m *MockApplyFuture) Error() error                                  { return m.err }
func (m *MockApplyFuture) Response() interface{}                         { return m.response }
func (m *MockApplyFuture) Index() uint64                                 { return 1 }
func (m *MockApplyFuture) Term() uint64                                  { return 1 }
func (m *MockApplyFuture) Resp() interface{}                             { return m.response }
func (m *MockApplyFuture) ResultCh() <-chan raft.RPCResponse { return nil }

// MockShardStore is a mock implementation of ShardStore for integration testing
type MockShardStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func NewMockShardStore() *MockShardStore {
	return &MockShardStore{
		data: make(map[string][]byte),
	}
}

func (m *MockShardStore) Scan(ctx context.Context) (sharding.Iterator, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create snapshot of keys
	keys := make([]string, 0, len(m.data))
	values := make(map[string][]byte)
	for k, v := range m.data {
		keys = append(keys, k)
		values[k] = v
	}

	return &MockIterator{
		keys:   keys,
		values: values,
		index:  -1,
	}, nil
}

func (m *MockShardStore) Get(ctx context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	value, exists := m.data[key]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	return value, nil
}

func (m *MockShardStore) Put(ctx context.Context, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data[key] = value
	return nil
}

func (m *MockShardStore) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.data, key)
	return nil
}

func (m *MockShardStore) Count(ctx context.Context) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return int64(len(m.data)), nil
}

func (m *MockShardStore) GetAllData() map[string][]byte {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]byte)
	for k, v := range m.data {
		result[k] = v
	}
	return result
}

// MockIterator implements Iterator
type MockIterator struct {
	keys   []string
	values map[string][]byte
	index  int
	err    error
}

func (m *MockIterator) Next() bool {
	m.index++
	return m.index < len(m.keys)
}

func (m *MockIterator) Key() string {
	if m.index < 0 || m.index >= len(m.keys) {
		return ""
	}
	return m.keys[m.index]
}

func (m *MockIterator) Value() []byte {
	key := m.Key()
	if key == ""  {
		return nil
	}
	return m.values[key]
}

func (m *MockIterator) Error() error {
	return m.err
}

func (m *MockIterator) Close() error {
	return nil
}

// setupShardingTest creates a fully wired setup for sharding integration testing
func setupShardingTest(t *testing.T, nodeID string) (*sharding.Manager, *sharding.MetaFSM, *sharding.Router, *MockRaft) {
	logger := zaptest.NewLogger(t)

	// Create components
	fsm := sharding.NewMetaFSM(logger)
	router := sharding.NewRouter(150, logger)
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
	config := sharding.DefaultManagerConfig(nodeID)
	manager := sharding.NewManager(config, mockRaft, fsm, router, logger)

	return manager, fsm, router, mockRaft
}

// TestIntegration_Sharding_FullWorkflow tests the complete sharding workflow
func TestIntegration_Sharding_FullWorkflow(t *testing.T) {
	// Setup
	manager, fsm, router, _ := setupShardingTest(t, "node1")

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
	shards := fsm.GetShardMap().GetActiveShards()
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
	shards = fsm.GetShardMap().GetActiveShards()
	assert.Len(t, shards, 2)

	// Router should still work with remaining shards
	routeInfo, err = router.RouteKey("user:bob")
	require.NoError(t, err)
	assert.Contains(t, []int{0, 1}, routeInfo.ShardID) // Should only route to shard 0 or 1 now
}

// TestIntegration_Sharding_Migration tests the complete migration workflow
func TestIntegration_Sharding_Migration(t *testing.T) {
	// Setup
	manager, fsm, _, _ := setupShardingTest(t, "node1")
	migrator := sharding.NewMigrator(sharding.DefaultMigratorConfig(), manager, zaptest.NewLogger(t))

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
	migration, ok := fsm.GetShardMap().GetMigration(migrationID)
	require.True(t, ok)
	assert.Equal(t, sharding.MigrationStatePreparing, migration.State)

	// Run migration worker
	t.Log("Executing data migration...")
	ctx := context.Background()
	err = migrator.StartMigration(ctx, migrationID, sourceStore, targetStore)
	require.NoError(t, err)

	// Wait for migration to complete
	time.Sleep(1 * time.Second)

	// Verify data was copied
	sourceData := sourceStore.GetAllData()
	targetData := targetStore.GetAllData()
	assert.Equal(t, 100, len(targetData))
	for key, value := range sourceData {
		targetValue, exists := targetData[key]
		assert.True(t, exists, "Key %s should exist in target", key)
		assert.Equal(t, value, targetValue, "Value for key %s should match", key)
	}

	// Check migration status
	status, err := migrator.GetMigrationStatus(migrationID)
	require.NoError(t, err)
	assert.Equal(t, int64(100), status.KeysCopied)
	assert.Equal(t, int64(100), status.TotalKeys)
}

// TestIntegration_Sharding_ConcurrentRouting tests concurrent routing operations
func TestIntegration_Sharding_ConcurrentRouting(t *testing.T) {
	// Setup
	manager, _, router, _ := setupShardingTest(t, "node1")

	manager.Start()
	defer manager.Stop()

	// Setup cluster
	manager.RegisterNode("node1", "localhost:8001", 10*GB)
	manager.RegisterNode("node2", "localhost:8002", 10*GB)
	manager.RegisterNode("node3", "localhost:8003", 10*GB)

	for i := 0; i < 4; i++ {
		manager.CreateShard(i, []string{"node1", "node2", "node3"})
	}

	time.Sleep(100 * time.Millisecond)

	// Concurrent routing test
	t.Log("Testing concurrent routing with 1,000 requests...")
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

// TestIntegration_Sharding_ConsistentHashingStability tests hash ring stability
func TestIntegration_Sharding_ConsistentHashingStability(t *testing.T) {
	// Setup
	manager, _, router, _ := setupShardingTest(t, "node1")

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

// Note: NodeFailureHandling test removed as it requires access to internal
// Command types. Node failure handling is tested at the unit test level.
