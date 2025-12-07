package sharding

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// MockShardStore is a mock implementation of ShardStore for testing
type MockShardStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func NewMockShardStore() *MockShardStore {
	return &MockShardStore{
		data: make(map[string][]byte),
	}
}

func (m *MockShardStore) Scan(ctx context.Context) (Iterator, error) {
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

// GetAllData returns a copy of all data (thread-safe)
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
	if key == "" {
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

// FailingMockShardStore simulates failures
type FailingMockShardStore struct {
	*MockShardStore
	mu        sync.RWMutex
	failPut   bool
	failCount int
}

func NewFailingMockShardStore() *FailingMockShardStore {
	return &FailingMockShardStore{
		MockShardStore: NewMockShardStore(),
	}
}

func (f *FailingMockShardStore) Put(ctx context.Context, key string, value []byte) error {
	f.mu.Lock()
	shouldFail := f.failPut
	if shouldFail {
		f.failCount++
	}
	f.mu.Unlock()

	if shouldFail {
		return errors.New("simulated put failure")
	}
	return f.MockShardStore.Put(ctx, key, value)
}

func (f *FailingMockShardStore) SetFailPut(fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failPut = fail
}

func (f *FailingMockShardStore) GetFailCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.failCount
}

func TestMigrator_NewMigrator(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultMigratorConfig()
	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)
	manager := NewManager(DefaultManagerConfig("node1"), mockRaft, fsm, router, logger)

	migrator := NewMigrator(config, manager, logger)

	assert.NotNil(t, migrator)
	assert.Equal(t, config.BatchSize, migrator.config.BatchSize)
}

func TestMigrator_StartMigration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultMigratorConfig()
	config.ProgressUpdateInterval = 100 * time.Millisecond

	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)
	manager := NewManager(DefaultManagerConfig("node1"), mockRaft, fsm, router, logger)

	// Add migration to FSM
	migration := &MigrationInfo{
		ID:      "migration-1",
		ShardID: 1,
		State:   MigrationStatePreparing,
	}
	fsm.shardMap.AddMigration(migration)

	migrator := NewMigrator(config, manager, logger)

	// Create source and target stores
	sourceStore := NewMockShardStore()
	targetStore := NewMockShardStore()

	// Add some data to source
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key-%d", i)
		value := []byte(fmt.Sprintf("value-%d", i))
		sourceStore.Put(context.Background(), key, value)
	}

	ctx := context.Background()
	err := migrator.StartMigration(ctx, "migration-1", sourceStore, targetStore)
	require.NoError(t, err)

	// Wait for migration to complete
	time.Sleep(500 * time.Millisecond)

	// Verify data was copied (using thread-safe methods)
	sourceData := sourceStore.GetAllData()
	targetData := targetStore.GetAllData()
	assert.Equal(t, len(sourceData), len(targetData))

	for key, value := range sourceData {
		targetValue, exists := targetData[key]
		assert.True(t, exists, "Key %s should exist in target", key)
		assert.Equal(t, value, targetValue)
	}
}

func TestMigrator_StartMigrationAlreadyRunning(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultMigratorConfig()

	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)
	manager := NewManager(DefaultManagerConfig("node1"), mockRaft, fsm, router, logger)

	migrator := NewMigrator(config, manager, logger)

	sourceStore := NewMockShardStore()
	targetStore := NewMockShardStore()

	ctx := context.Background()
	err := migrator.StartMigration(ctx, "migration-1", sourceStore, targetStore)
	require.NoError(t, err)

	// Try to start again
	err = migrator.StartMigration(ctx, "migration-1", sourceStore, targetStore)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestMigrator_StopMigration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultMigratorConfig()

	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)
	manager := NewManager(DefaultManagerConfig("node1"), mockRaft, fsm, router, logger)

	migrator := NewMigrator(config, manager, logger)

	sourceStore := NewMockShardStore()
	targetStore := NewMockShardStore()

	ctx := context.Background()
	err := migrator.StartMigration(ctx, "migration-1", sourceStore, targetStore)
	require.NoError(t, err)

	// Stop migration
	err = migrator.StopMigration("migration-1")
	require.NoError(t, err)

	// Verify migration is removed
	_, err = migrator.GetMigrationStatus("migration-1")
	assert.Error(t, err)
}

func TestMigrator_GetMigrationStatus(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultMigratorConfig()

	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)
	manager := NewManager(DefaultManagerConfig("node1"), mockRaft, fsm, router, logger)

	// Add migration
	migration := &MigrationInfo{
		ID:      "migration-1",
		ShardID: 1,
		State:   MigrationStatePreparing,
	}
	fsm.shardMap.AddMigration(migration)

	migrator := NewMigrator(config, manager, logger)

	sourceStore := NewMockShardStore()
	targetStore := NewMockShardStore()

	// Add data
	for i := 0; i < 100; i++ {
		sourceStore.Put(context.Background(), fmt.Sprintf("key-%d", i), []byte("value"))
	}

	ctx := context.Background()
	err := migrator.StartMigration(ctx, "migration-1", sourceStore, targetStore)
	require.NoError(t, err)

	// Get status
	time.Sleep(100 * time.Millisecond)
	status, err := migrator.GetMigrationStatus("migration-1")
	require.NoError(t, err)

	assert.Equal(t, "migration-1", status.MigrationID)
	assert.GreaterOrEqual(t, status.KeysCopied, int64(0))
	assert.Equal(t, int64(100), status.TotalKeys)
}

func TestMigrator_BatchCopy(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultMigratorConfig()
	config.BatchSize = 10 // Small batch size for testing

	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)
	manager := NewManager(DefaultManagerConfig("node1"), mockRaft, fsm, router, logger)

	// Add migration
	migration := &MigrationInfo{
		ID:      "migration-1",
		ShardID: 1,
		State:   MigrationStatePreparing,
	}
	fsm.shardMap.AddMigration(migration)

	migrator := NewMigrator(config, manager, logger)

	sourceStore := NewMockShardStore()
	targetStore := NewMockShardStore()

	// Add 50 keys (should require 5 batches)
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("key-%03d", i)
		value := []byte(fmt.Sprintf("value-%d", i))
		sourceStore.Put(context.Background(), key, value)
	}

	ctx := context.Background()
	err := migrator.StartMigration(ctx, "migration-1", sourceStore, targetStore)
	require.NoError(t, err)

	// Wait for migration
	time.Sleep(500 * time.Millisecond)

	// Verify all data copied (using thread-safe method)
	targetData := targetStore.GetAllData()
	assert.Equal(t, 50, len(targetData))
}

func TestMigrator_EmptySource(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultMigratorConfig()

	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)
	manager := NewManager(DefaultManagerConfig("node1"), mockRaft, fsm, router, logger)

	// Add migration
	migration := &MigrationInfo{
		ID:      "migration-1",
		ShardID: 1,
		State:   MigrationStatePreparing,
	}
	fsm.shardMap.AddMigration(migration)

	migrator := NewMigrator(config, manager, logger)

	sourceStore := NewMockShardStore()
	targetStore := NewMockShardStore()

	ctx := context.Background()
	err := migrator.StartMigration(ctx, "migration-1", sourceStore, targetStore)
	require.NoError(t, err)

	// Wait for migration
	time.Sleep(200 * time.Millisecond)

	// Verify target is also empty (using thread-safe method)
	targetData := targetStore.GetAllData()
	assert.Equal(t, 0, len(targetData))
}

func TestMigrator_CopyWithRetry(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultMigratorConfig()
	config.MaxRetries = 3
	config.RetryDelay = 10 * time.Millisecond

	mockRaft := NewMockRaft()
	fsm := NewMetaFSM(logger)
	router := NewRouter(150, logger)
	manager := NewManager(DefaultManagerConfig("node1"), mockRaft, fsm, router, logger)

	// Add migration
	migration := &MigrationInfo{
		ID:      "migration-1",
		ShardID: 1,
		State:   MigrationStatePreparing,
	}
	fsm.shardMap.AddMigration(migration)

	migrator := NewMigrator(config, manager, logger)

	sourceStore := NewMockShardStore()
	targetStore := NewFailingMockShardStore()

	// Add some data
	for i := 0; i < 5; i++ {
		sourceStore.Put(context.Background(), fmt.Sprintf("key-%d", i), []byte("value"))
	}

	// Enable failures initially
	targetStore.SetFailPut(true)

	ctx := context.Background()
	err := migrator.StartMigration(ctx, "migration-1", sourceStore, targetStore)
	require.NoError(t, err)

	// Wait a bit, then disable failures
	time.Sleep(100 * time.Millisecond)
	targetStore.SetFailPut(false)

	// Migration should eventually fail due to retries being exhausted
	time.Sleep(500 * time.Millisecond)

	// Verify some failures occurred (using thread-safe method)
	failCount := targetStore.GetFailCount()
	assert.Greater(t, failCount, 0)
}

func TestMockIterator_Empty(t *testing.T) {
	iter := &MockIterator{
		keys:   []string{},
		values: make(map[string][]byte),
		index:  -1,
	}

	assert.False(t, iter.Next())
	assert.Equal(t, "", iter.Key())
	assert.Nil(t, iter.Value())
	assert.NoError(t, iter.Error())
	assert.NoError(t, iter.Close())
}

func TestMockIterator_MultipleItems(t *testing.T) {
	iter := &MockIterator{
		keys: []string{"key1", "key2", "key3"},
		values: map[string][]byte{
			"key1": []byte("value1"),
			"key2": []byte("value2"),
			"key3": []byte("value3"),
		},
		index: -1,
	}

	count := 0
	for iter.Next() {
		assert.NotEmpty(t, iter.Key())
		assert.NotNil(t, iter.Value())
		count++
	}

	assert.Equal(t, 3, count)
}
