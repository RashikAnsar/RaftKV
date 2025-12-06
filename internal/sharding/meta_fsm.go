package sharding

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/hashicorp/raft"
	"go.uber.org/zap"
)

// CommandType represents the type of command being applied to the FSM
type CommandType string

const (
	CommandAddShard       CommandType = "add_shard"
	CommandRemoveShard    CommandType = "remove_shard"
	CommandUpdateShard    CommandType = "update_shard"
	CommandAddNode        CommandType = "add_node"
	CommandRemoveNode     CommandType = "remove_node"
	CommandUpdateNode     CommandType = "update_node"
	CommandAddMigration   CommandType = "add_migration"
	CommandUpdateMigration CommandType = "update_migration"
	CommandRemoveMigration CommandType = "remove_migration"
)

// Command represents a command to be applied to the meta FSM
type Command struct {
	Type CommandType `json:"type"`
	Data json.RawMessage `json:"data"`
}

// MetaFSM is the Finite State Machine for the meta-cluster
// It stores and manages the shard map
type MetaFSM struct {
	mu        sync.RWMutex
	shardMap  *ShardMap
	logger    *zap.Logger
	listeners []UpdateListener
}

// UpdateListener is notified when the shard map is updated
type UpdateListener interface {
	OnShardMapUpdate(shardMap *ShardMap)
}

// NewMetaFSM creates a new MetaFSM instance
func NewMetaFSM(logger *zap.Logger) *MetaFSM {
	return &MetaFSM{
		shardMap:  NewShardMap(),
		logger:    logger,
		listeners: make([]UpdateListener, 0),
	}
}

// Apply applies a Raft log entry to the FSM
func (fsm *MetaFSM) Apply(log *raft.Log) interface{} {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	var cmd Command
	if err := json.Unmarshal(log.Data, &cmd); err != nil {
		fsm.logger.Error("Failed to unmarshal command",
			zap.Error(err),
			zap.Uint64("log_index", log.Index))
		return fmt.Errorf("failed to unmarshal command: %w", err)
	}

	fsm.logger.Debug("Applying command",
		zap.String("type", string(cmd.Type)),
		zap.Uint64("log_index", log.Index))

	var err error
	switch cmd.Type {
	case CommandAddShard:
		err = fsm.applyAddShard(cmd.Data)
	case CommandRemoveShard:
		err = fsm.applyRemoveShard(cmd.Data)
	case CommandUpdateShard:
		err = fsm.applyUpdateShard(cmd.Data)
	case CommandAddNode:
		err = fsm.applyAddNode(cmd.Data)
	case CommandRemoveNode:
		err = fsm.applyRemoveNode(cmd.Data)
	case CommandUpdateNode:
		err = fsm.applyUpdateNode(cmd.Data)
	case CommandAddMigration:
		err = fsm.applyAddMigration(cmd.Data)
	case CommandUpdateMigration:
		err = fsm.applyUpdateMigration(cmd.Data)
	case CommandRemoveMigration:
		err = fsm.applyRemoveMigration(cmd.Data)
	default:
		err = fmt.Errorf("unknown command type: %s", cmd.Type)
	}

	if err != nil {
		fsm.logger.Error("Failed to apply command",
			zap.String("type", string(cmd.Type)),
			zap.Error(err))
		return err
	}

	// Notify listeners of the update
	fsm.notifyListeners()

	return nil
}

// Snapshot returns a snapshot of the FSM state
func (fsm *MetaFSM) Snapshot() (raft.FSMSnapshot, error) {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()

	// Create a deep copy of the shard map
	snapshot := fsm.shardMap.Clone()

	fsm.logger.Info("Creating FSM snapshot",
		zap.Uint64("version", snapshot.Version),
		zap.Int("num_shards", len(snapshot.Shards)))

	return &MetaFSMSnapshot{
		shardMap: snapshot,
		logger:   fsm.logger,
	}, nil
}

// Restore restores the FSM state from a snapshot
func (fsm *MetaFSM) Restore(snapshot io.ReadCloser) error {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	defer snapshot.Close()

	var shardMap ShardMap
	if err := json.NewDecoder(snapshot).Decode(&shardMap); err != nil {
		fsm.logger.Error("Failed to decode snapshot", zap.Error(err))
		return fmt.Errorf("failed to decode snapshot: %w", err)
	}

	fsm.shardMap = &shardMap

	fsm.logger.Info("Restored FSM from snapshot",
		zap.Uint64("version", shardMap.Version),
		zap.Int("num_shards", len(shardMap.Shards)))

	// Notify listeners of the restored state
	fsm.notifyListeners()

	return nil
}

// GetShardMap returns a copy of the current shard map
func (fsm *MetaFSM) GetShardMap() *ShardMap {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()

	return fsm.shardMap.Clone()
}

// RegisterListener registers a listener to be notified of shard map updates
func (fsm *MetaFSM) RegisterListener(listener UpdateListener) {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	fsm.listeners = append(fsm.listeners, listener)
}

// notifyListeners notifies all registered listeners of a shard map update
func (fsm *MetaFSM) notifyListeners() {
	// Make a copy for listeners
	snapshot := fsm.shardMap.Clone()

	// Notify in background to avoid blocking Apply
	go func() {
		for _, listener := range fsm.listeners {
			listener.OnShardMapUpdate(snapshot)
		}
	}()
}

// Command application methods

func (fsm *MetaFSM) applyAddShard(data json.RawMessage) error {
	var shard ShardInfo
	if err := json.Unmarshal(data, &shard); err != nil {
		return fmt.Errorf("failed to unmarshal shard: %w", err)
	}

	fsm.shardMap.AddShard(&shard)

	fsm.logger.Info("Added shard",
		zap.Int("shard_id", shard.ID),
		zap.Strings("replicas", shard.Replicas))

	return nil
}

func (fsm *MetaFSM) applyRemoveShard(data json.RawMessage) error {
	var shardID int
	if err := json.Unmarshal(data, &shardID); err != nil {
		return fmt.Errorf("failed to unmarshal shard ID: %w", err)
	}

	fsm.shardMap.mu.Lock()
	delete(fsm.shardMap.Shards, shardID)
	fsm.shardMap.Version++
	fsm.shardMap.mu.Unlock()

	fsm.logger.Info("Removed shard", zap.Int("shard_id", shardID))

	return nil
}

type UpdateShardCmd struct {
	ShardID int           `json:"shard_id"`
	Shard   *ShardInfo    `json:"shard"`
}

func (fsm *MetaFSM) applyUpdateShard(data json.RawMessage) error {
	var cmd UpdateShardCmd
	if err := json.Unmarshal(data, &cmd); err != nil {
		return fmt.Errorf("failed to unmarshal update shard command: %w", err)
	}

	fsm.shardMap.mu.Lock()
	if existing, ok := fsm.shardMap.Shards[cmd.ShardID]; ok {
		// Update fields
		if cmd.Shard != nil {
			*existing = *cmd.Shard
		}
		fsm.shardMap.Version++
	}
	fsm.shardMap.mu.Unlock()

	fsm.logger.Debug("Updated shard", zap.Int("shard_id", cmd.ShardID))

	return nil
}

func (fsm *MetaFSM) applyAddNode(data json.RawMessage) error {
	var node NodeInfo
	if err := json.Unmarshal(data, &node); err != nil {
		return fmt.Errorf("failed to unmarshal node: %w", err)
	}

	fsm.shardMap.AddNode(&node)

	fsm.logger.Info("Added node",
		zap.String("node_id", node.ID),
		zap.String("address", node.Address))

	return nil
}

func (fsm *MetaFSM) applyRemoveNode(data json.RawMessage) error {
	var nodeID string
	if err := json.Unmarshal(data, &nodeID); err != nil {
		return fmt.Errorf("failed to unmarshal node ID: %w", err)
	}

	fsm.shardMap.mu.Lock()
	delete(fsm.shardMap.Nodes, nodeID)
	fsm.shardMap.Version++
	fsm.shardMap.mu.Unlock()

	fsm.logger.Info("Removed node", zap.String("node_id", nodeID))

	return nil
}

type UpdateNodeCmd struct {
	NodeID string    `json:"node_id"`
	Node   *NodeInfo `json:"node"`
}

func (fsm *MetaFSM) applyUpdateNode(data json.RawMessage) error {
	var cmd UpdateNodeCmd
	if err := json.Unmarshal(data, &cmd); err != nil {
		return fmt.Errorf("failed to unmarshal update node command: %w", err)
	}

	fsm.shardMap.mu.Lock()
	if existing, ok := fsm.shardMap.Nodes[cmd.NodeID]; ok {
		if cmd.Node != nil {
			*existing = *cmd.Node
		}
		fsm.shardMap.Version++
	}
	fsm.shardMap.mu.Unlock()

	fsm.logger.Debug("Updated node", zap.String("node_id", cmd.NodeID))

	return nil
}

func (fsm *MetaFSM) applyAddMigration(data json.RawMessage) error {
	var migration MigrationInfo
	if err := json.Unmarshal(data, &migration); err != nil {
		return fmt.Errorf("failed to unmarshal migration: %w", err)
	}

	fsm.shardMap.AddMigration(&migration)

	fsm.logger.Info("Added migration",
		zap.String("migration_id", migration.ID),
		zap.Int("shard_id", migration.ShardID))

	return nil
}

type UpdateMigrationCmd struct {
	MigrationID string         `json:"migration_id"`
	Migration   *MigrationInfo `json:"migration"`
}

func (fsm *MetaFSM) applyUpdateMigration(data json.RawMessage) error {
	var cmd UpdateMigrationCmd
	if err := json.Unmarshal(data, &cmd); err != nil {
		return fmt.Errorf("failed to unmarshal update migration command: %w", err)
	}

	fsm.shardMap.mu.Lock()
	if existing, ok := fsm.shardMap.Migrations[cmd.MigrationID]; ok {
		if cmd.Migration != nil {
			*existing = *cmd.Migration
		}
		fsm.shardMap.Version++
	}
	fsm.shardMap.mu.Unlock()

	fsm.logger.Debug("Updated migration",
		zap.String("migration_id", cmd.MigrationID))

	return nil
}

func (fsm *MetaFSM) applyRemoveMigration(data json.RawMessage) error {
	var migrationID string
	if err := json.Unmarshal(data, &migrationID); err != nil {
		return fmt.Errorf("failed to unmarshal migration ID: %w", err)
	}

	fsm.shardMap.RemoveMigration(migrationID)

	fsm.logger.Info("Removed migration", zap.String("migration_id", migrationID))

	return nil
}

// MetaFSMSnapshot represents a point-in-time snapshot of the MetaFSM
type MetaFSMSnapshot struct {
	shardMap *ShardMap
	logger   *zap.Logger
}

// Persist writes the snapshot to the given sink
func (s *MetaFSMSnapshot) Persist(sink raft.SnapshotSink) error {
	err := func() error {
		// Encode the shard map as JSON
		if err := json.NewEncoder(sink).Encode(s.shardMap); err != nil {
			return fmt.Errorf("failed to encode shard map: %w", err)
		}

		return nil
	}()

	if err != nil {
		sink.Cancel()
		s.logger.Error("Failed to persist snapshot", zap.Error(err))
		return err
	}

	s.logger.Info("Persisted snapshot",
		zap.Uint64("version", s.shardMap.Version))

	return sink.Close()
}

// Release releases any resources held by the snapshot
func (s *MetaFSMSnapshot) Release() {
	// Nothing to release
}
