package sharding

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	"go.uber.org/zap"
)

// ManagerConfig contains configuration for the shard manager
type ManagerConfig struct {
	// NodeID is the ID of this node
	NodeID string

	// RebalanceInterval is how often to check for rebalancing opportunities
	RebalanceInterval time.Duration

	// MaxShardSize is the maximum data size per shard before splitting
	MaxShardSize int64

	// MinShardSize is the minimum data size per shard before merging
	MinShardSize int64

	// ReplicationFactor is the number of replicas per shard
	ReplicationFactor int
}

// DefaultManagerConfig returns a default configuration
func DefaultManagerConfig(nodeID string) *ManagerConfig {
	return &ManagerConfig{
		NodeID:            nodeID,
		RebalanceInterval: 60 * time.Second,
		MaxShardSize:      10 * 1024 * 1024 * 1024, // 10GB
		MinShardSize:      1 * 1024 * 1024 * 1024,  // 1GB
		ReplicationFactor: 3,
	}
}

// RaftNode is an interface for the subset of Raft methods we need
type RaftNode interface {
	Apply([]byte, time.Duration) raft.ApplyFuture
	State() raft.RaftState
}

// Manager manages shard lifecycle, migrations, and rebalancing
type Manager struct {
	mu     sync.RWMutex
	config *ManagerConfig
	raft   RaftNode
	fsm    *MetaFSM
	router *Router
	logger *zap.Logger

	// Background task management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics and state
	activeMigrations map[string]*MigrationInfo
}

// NewManager creates a new shard manager
func NewManager(config *ManagerConfig, raftNode RaftNode, fsm *MetaFSM, router *Router, logger *zap.Logger) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		config:           config,
		raft:             raftNode,
		fsm:              fsm,
		router:           router,
		logger:           logger,
		ctx:              ctx,
		cancel:           cancel,
		activeMigrations: make(map[string]*MigrationInfo),
	}
}

// Start starts the shard manager background tasks
func (m *Manager) Start() error {
	m.logger.Info("Starting shard manager",
		zap.String("node_id", m.config.NodeID))

	// Start rebalancing task
	m.wg.Add(1)
	go m.rebalanceLoop()

	// Register as listener for shard map updates
	m.fsm.RegisterListener(m)

	return nil
}

// Stop stops the shard manager
func (m *Manager) Stop() error {
	m.logger.Info("Stopping shard manager")

	m.cancel()
	m.wg.Wait()

	return nil
}

// OnShardMapUpdate implements UpdateListener
func (m *Manager) OnShardMapUpdate(shardMap *ShardMap) {
	// Update router with new shard map
	if err := m.router.UpdateShardMap(shardMap); err != nil {
		m.logger.Error("Failed to update router",
			zap.Error(err))
	}

	// Update active migrations tracking
	m.mu.Lock()
	m.activeMigrations = make(map[string]*MigrationInfo)
	for _, migration := range shardMap.GetActiveMigrations() {
		m.activeMigrations[migration.ID] = migration
	}
	m.mu.Unlock()
}

// CreateShard creates a new shard
func (m *Manager) CreateShard(shardID int, replicas []string) error {
	if !m.isLeader() {
		return fmt.Errorf("not leader, cannot create shard")
	}

	shard := &ShardInfo{
		ID:            shardID,
		State:         ShardStateActive,
		Replicas:      replicas,
		LeaderNodeID:  replicas[0], // First replica is leader
		KeyRangeStart: 0,
		KeyRangeEnd:   ^uint32(0), // Max uint32
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// Propose command to Raft
	cmd := Command{
		Type: CommandAddShard,
	}
	var err error
	cmd.Data, err = json.Marshal(shard)
	if err != nil {
		return fmt.Errorf("failed to marshal shard: %w", err)
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	// Apply to Raft
	future := m.raft.Apply(cmdBytes, 10*time.Second)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to apply command: %w", err)
	}

	m.logger.Info("Created shard",
		zap.Int("shard_id", shardID),
		zap.Strings("replicas", replicas))

	return nil
}

// RemoveShard removes a shard
func (m *Manager) RemoveShard(shardID int) error {
	if !m.isLeader() {
		return fmt.Errorf("not leader, cannot remove shard")
	}

	// Check if shard has active migration
	shardMap := m.fsm.GetShardMap()
	if migration := shardMap.GetMigrationForShard(shardID); migration != nil {
		return fmt.Errorf("shard %d has active migration %s", shardID, migration.ID)
	}

	// Propose remove command
	cmd := Command{
		Type: CommandRemoveShard,
	}
	var err error
	cmd.Data, err = json.Marshal(shardID)
	if err != nil {
		return fmt.Errorf("failed to marshal shard ID: %w", err)
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	future := m.raft.Apply(cmdBytes, 10*time.Second)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to apply command: %w", err)
	}

	m.logger.Info("Removed shard", zap.Int("shard_id", shardID))

	return nil
}

// StartMigration initiates a shard migration
func (m *Manager) StartMigration(shardID int, sourceNodes, targetNodes []string) (string, error) {
	if !m.isLeader() {
		return "", fmt.Errorf("not leader, cannot start migration")
	}

	// Validate shard exists
	shardMap := m.fsm.GetShardMap()
	shard, ok := shardMap.GetShard(shardID)
	if !ok {
		return "", fmt.Errorf("shard %d not found", shardID)
	}

	// Check if already migrating
	if migration := shardMap.GetMigrationForShard(shardID); migration != nil {
		return "", fmt.Errorf("shard %d already has active migration %s", shardID, migration.ID)
	}

	// Create migration
	migrationID := fmt.Sprintf("migration-%d-%d", shardID, time.Now().Unix())
	migration := &MigrationInfo{
		ID:          migrationID,
		ShardID:     shardID,
		SourceNodes: sourceNodes,
		TargetNodes: targetNodes,
		State:       MigrationStatePreparing,
		Progress:    0.0,
		TotalKeys:   shard.KeyCount,
		StartTime:   time.Now(),
	}

	// Propose add migration command
	cmd := Command{
		Type: CommandAddMigration,
	}
	var err error
	cmd.Data, err = json.Marshal(migration)
	if err != nil {
		return "", fmt.Errorf("failed to marshal migration: %w", err)
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to marshal command: %w", err)
	}

	future := m.raft.Apply(cmdBytes, 10*time.Second)
	if err := future.Error(); err != nil {
		return "", fmt.Errorf("failed to apply command: %w", err)
	}

	m.logger.Info("Started migration",
		zap.String("migration_id", migrationID),
		zap.Int("shard_id", shardID),
		zap.Strings("source_nodes", sourceNodes),
		zap.Strings("target_nodes", targetNodes))

	return migrationID, nil
}

// UpdateMigrationState updates the state of a migration
func (m *Manager) UpdateMigrationState(migrationID string, state MigrationState, progress float64, keysCopied int64) error {
	if !m.isLeader() {
		return fmt.Errorf("not leader, cannot update migration")
	}

	shardMap := m.fsm.GetShardMap()
	migration, ok := shardMap.GetMigration(migrationID)
	if !ok {
		return fmt.Errorf("migration %s not found", migrationID)
	}

	// Update migration fields
	updatedMigration := *migration
	updatedMigration.State = state
	updatedMigration.Progress = progress
	updatedMigration.KeysCopied = keysCopied

	if state == MigrationStateComplete || state == MigrationStateFailed {
		now := time.Now()
		updatedMigration.EndTime = &now
	}

	// Propose update command
	cmd := Command{
		Type: CommandUpdateMigration,
	}
	var err error
	cmd.Data, err = json.Marshal(UpdateMigrationCmd{
		MigrationID: migrationID,
		Migration:   &updatedMigration,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal update command: %w", err)
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	future := m.raft.Apply(cmdBytes, 10*time.Second)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to apply command: %w", err)
	}

	m.logger.Info("Updated migration state",
		zap.String("migration_id", migrationID),
		zap.String("state", state.String()),
		zap.Float64("progress", progress))

	return nil
}

// CompleteMigration marks a migration as complete and updates shard replicas
func (m *Manager) CompleteMigration(migrationID string) error {
	if !m.isLeader() {
		return fmt.Errorf("not leader, cannot complete migration")
	}

	shardMap := m.fsm.GetShardMap()
	migration, ok := shardMap.GetMigration(migrationID)
	if !ok {
		return fmt.Errorf("migration %s not found", migrationID)
	}

	// Update migration state to complete
	if err := m.UpdateMigrationState(migrationID, MigrationStateComplete, 1.0, migration.TotalKeys); err != nil {
		return fmt.Errorf("failed to update migration state: %w", err)
	}

	// Update shard replicas to target nodes
	shard, ok := shardMap.GetShard(migration.ShardID)
	if !ok {
		return fmt.Errorf("shard %d not found", migration.ShardID)
	}

	updatedShard := *shard
	updatedShard.Replicas = migration.TargetNodes
	updatedShard.LeaderNodeID = migration.TargetNodes[0]
	updatedShard.State = ShardStateActive
	updatedShard.UpdatedAt = time.Now()

	cmd := Command{
		Type: CommandUpdateShard,
	}
	var err error
	cmd.Data, err = json.Marshal(UpdateShardCmd{
		ShardID: migration.ShardID,
		Shard:   &updatedShard,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal shard update: %w", err)
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	future := m.raft.Apply(cmdBytes, 10*time.Second)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to apply shard update: %w", err)
	}

	m.logger.Info("Completed migration",
		zap.String("migration_id", migrationID),
		zap.Int("shard_id", migration.ShardID))

	return nil
}

// RegisterNode registers a new node in the cluster
func (m *Manager) RegisterNode(nodeID, address string, capacity int64) error {
	if !m.isLeader() {
		return fmt.Errorf("not leader, cannot register node")
	}

	node := &NodeInfo{
		ID:            nodeID,
		Address:       address,
		State:         NodeStateActive,
		Capacity:      capacity,
		Used:          0,
		Load:          0.0,
		Shards:        []int{},
		LastHeartbeat: time.Now(),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	cmd := Command{
		Type: CommandAddNode,
	}
	var err error
	cmd.Data, err = json.Marshal(node)
	if err != nil {
		return fmt.Errorf("failed to marshal node: %w", err)
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	future := m.raft.Apply(cmdBytes, 10*time.Second)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to apply command: %w", err)
	}

	m.logger.Info("Registered node",
		zap.String("node_id", nodeID),
		zap.String("address", address))

	return nil
}

// DeregisterNode removes a node from the cluster
func (m *Manager) DeregisterNode(nodeID string) error {
	if !m.isLeader() {
		return fmt.Errorf("not leader, cannot deregister node")
	}

	// Check if node has any shards
	shardMap := m.fsm.GetShardMap()
	for _, shard := range shardMap.Shards {
		for _, replica := range shard.Replicas {
			if replica == nodeID {
				return fmt.Errorf("node %s still has shard %d, migrate first", nodeID, shard.ID)
			}
		}
	}

	cmd := Command{
		Type: CommandRemoveNode,
	}
	var err error
	cmd.Data, err = json.Marshal(nodeID)
	if err != nil {
		return fmt.Errorf("failed to marshal node ID: %w", err)
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	future := m.raft.Apply(cmdBytes, 10*time.Second)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to apply command: %w", err)
	}

	m.logger.Info("Deregistered node", zap.String("node_id", nodeID))

	return nil
}

// GetActiveMigrations returns all active migrations
func (m *Manager) GetActiveMigrations() []*MigrationInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	migrations := make([]*MigrationInfo, 0, len(m.activeMigrations))
	for _, migration := range m.activeMigrations {
		migrations = append(migrations, migration)
	}

	return migrations
}

// rebalanceLoop periodically checks for rebalancing opportunities
func (m *Manager) rebalanceLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.RebalanceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			if m.isLeader() {
				m.checkRebalancing()
			}
		}
	}
}

// checkRebalancing checks if rebalancing is needed
func (m *Manager) checkRebalancing() {
	shardMap := m.fsm.GetShardMap()

	// Check for oversized shards that need splitting
	for _, shard := range shardMap.Shards {
		if shard.DataSize > m.config.MaxShardSize {
			m.logger.Info("Shard needs splitting",
				zap.Int("shard_id", shard.ID),
				zap.Int64("size", shard.DataSize),
				zap.Int64("max_size", m.config.MaxShardSize))
			// TODO: Implement shard splitting
		}
	}

	// Check for underloaded nodes that could take more shards
	nodes := shardMap.GetActiveNodes()
	if len(nodes) == 0 {
		return
	}

	// Calculate average load
	var totalLoad float64
	for _, node := range nodes {
		totalLoad += node.Load
	}
	avgLoad := totalLoad / float64(len(nodes))

	// Find nodes significantly above/below average
	for _, node := range nodes {
		if node.Load > avgLoad*1.5 {
			m.logger.Debug("Node is overloaded",
				zap.String("node_id", node.ID),
				zap.Float64("load", node.Load),
				zap.Float64("avg_load", avgLoad))
			// TODO: Implement shard rebalancing
		}
	}
}

// isLeader returns true if this node is the Raft leader
func (m *Manager) isLeader() bool {
	return m.raft.State() == raft.Leader
}
