package sharding

import (
	"sync"
	"time"
)

// ShardState represents the state of a shard
type ShardState int

const (
	ShardStateActive ShardState = iota
	ShardStateMigrating
	ShardStateSplitting
	ShardStateInactive
)

func (s ShardState) String() string {
	switch s {
	case ShardStateActive:
		return "active"
	case ShardStateMigrating:
		return "migrating"
	case ShardStateSplitting:
		return "splitting"
	case ShardStateInactive:
		return "inactive"
	default:
		return "unknown"
	}
}

// MigrationState represents the state of a shard migration
type MigrationState int

const (
	MigrationStatePreparing MigrationState = iota
	MigrationStateCopying
	MigrationStateSyncing
	MigrationStateDualWrite
	MigrationStateCutover
	MigrationStateCleanup
	MigrationStateComplete
	MigrationStateFailed
	MigrationStateRolledBack
)

func (m MigrationState) String() string {
	switch m {
	case MigrationStatePreparing:
		return "preparing"
	case MigrationStateCopying:
		return "copying"
	case MigrationStateSyncing:
		return "syncing"
	case MigrationStateDualWrite:
		return "dual_write"
	case MigrationStateCutover:
		return "cutover"
	case MigrationStateCleanup:
		return "cleanup"
	case MigrationStateComplete:
		return "complete"
	case MigrationStateFailed:
		return "failed"
	case MigrationStateRolledBack:
		return "rolled_back"
	default:
		return "unknown"
	}
}

// NodeState represents the state of a node
type NodeState int

const (
	NodeStateActive NodeState = iota
	NodeStateDraining
	NodeStateDown
	NodeStateRemoved
)

func (n NodeState) String() string {
	switch n {
	case NodeStateActive:
		return "active"
	case NodeStateDraining:
		return "draining"
	case NodeStateDown:
		return "down"
	case NodeStateRemoved:
		return "removed"
	default:
		return "unknown"
	}
}

// ShardInfo contains information about a shard
type ShardInfo struct {
	ID            int        `json:"id"`
	State         ShardState `json:"state"`
	Replicas      []string   `json:"replicas"`       // Node IDs hosting this shard
	LeaderNodeID  string     `json:"leader_node_id"` // Current leader
	KeyRangeStart uint32     `json:"key_range_start"`
	KeyRangeEnd   uint32     `json:"key_range_end"`
	DataSize      int64      `json:"data_size"`  // Bytes
	KeyCount      int64      `json:"key_count"`  // Number of keys
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// NodeInfo contains information about a node
type NodeInfo struct {
	ID           string    `json:"id"`
	Address      string    `json:"address"`       // Host:Port
	State        NodeState `json:"state"`
	Capacity     int64     `json:"capacity"`      // Max data size in bytes
	Used         int64     `json:"used"`          // Current usage in bytes
	Load         float64   `json:"load"`          // Current load (0.0-1.0)
	Shards       []int     `json:"shards"`        // Shard IDs hosted on this node
	LastHeartbeat time.Time `json:"last_heartbeat"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// MigrationInfo contains information about a shard migration
type MigrationInfo struct {
	ID            string         `json:"id"`
	ShardID       int            `json:"shard_id"`
	SourceNodes   []string       `json:"source_nodes"`
	TargetNodes   []string       `json:"target_nodes"`
	State         MigrationState `json:"state"`
	Progress      float64        `json:"progress"`       // 0.0-1.0
	KeysCopied    int64          `json:"keys_copied"`
	TotalKeys     int64          `json:"total_keys"`
	BytesCopied   int64          `json:"bytes_copied"`
	TotalBytes    int64          `json:"total_bytes"`
	StartTime     time.Time      `json:"start_time"`
	EndTime       *time.Time     `json:"end_time,omitempty"`
	LastCheckpoint *Checkpoint   `json:"last_checkpoint,omitempty"`
	Error         string         `json:"error,omitempty"`
}

// Checkpoint represents a point-in-time snapshot for migration resumption
type Checkpoint struct {
	RaftIndex uint64    `json:"raft_index"`
	Timestamp time.Time `json:"timestamp"`
	KeysCopied int64     `json:"keys_copied"`
	BytesCopied int64    `json:"bytes_copied"`
}

// ShardMap contains the complete shard topology
type ShardMap struct {
	mu         sync.RWMutex
	Version    uint64                   `json:"version"`    // Monotonically increasing
	Shards     map[int]*ShardInfo       `json:"shards"`     // ShardID → Info
	Nodes      map[string]*NodeInfo     `json:"nodes"`      // NodeID → Info
	Migrations map[string]*MigrationInfo `json:"migrations"` // MigrationID → Info
	UpdatedAt  time.Time                `json:"updated_at"`
}

// NewShardMap creates a new empty shard map
func NewShardMap() *ShardMap {
	return &ShardMap{
		Version:    0,
		Shards:     make(map[int]*ShardInfo),
		Nodes:      make(map[string]*NodeInfo),
		Migrations: make(map[string]*MigrationInfo),
		UpdatedAt:  time.Now(),
	}
}

// GetShard returns shard information
func (sm *ShardMap) GetShard(shardID int) (*ShardInfo, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	shard, ok := sm.Shards[shardID]
	return shard, ok
}

// GetNode returns node information
func (sm *ShardMap) GetNode(nodeID string) (*NodeInfo, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	node, ok := sm.Nodes[nodeID]
	return node, ok
}

// GetMigration returns migration information
func (sm *ShardMap) GetMigration(migrationID string) (*MigrationInfo, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	migration, ok := sm.Migrations[migrationID]
	return migration, ok
}

// GetMigrationForShard returns active migration for a shard if any
func (sm *ShardMap) GetMigrationForShard(shardID int) *MigrationInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for _, migration := range sm.Migrations {
		if migration.ShardID == shardID &&
		   migration.State != MigrationStateComplete &&
		   migration.State != MigrationStateFailed &&
		   migration.State != MigrationStateRolledBack {
			return migration
		}
	}
	return nil
}

// AddShard adds a new shard to the map
func (sm *ShardMap) AddShard(shard *ShardInfo) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.Shards[shard.ID] = shard
	sm.Version++
	sm.UpdatedAt = time.Now()
}

// UpdateShard updates shard information
func (sm *ShardMap) UpdateShard(shardID int, updateFn func(*ShardInfo)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if shard, ok := sm.Shards[shardID]; ok {
		updateFn(shard)
		shard.UpdatedAt = time.Now()
		sm.Version++
		sm.UpdatedAt = time.Now()
	}
}

// AddNode adds a new node to the map
func (sm *ShardMap) AddNode(node *NodeInfo) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.Nodes[node.ID] = node
	sm.Version++
	sm.UpdatedAt = time.Now()
}

// UpdateNode updates node information
func (sm *ShardMap) UpdateNode(nodeID string, updateFn func(*NodeInfo)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if node, ok := sm.Nodes[nodeID]; ok {
		updateFn(node)
		node.UpdatedAt = time.Now()
		sm.Version++
		sm.UpdatedAt = time.Now()
	}
}

// AddMigration adds a new migration to the map
func (sm *ShardMap) AddMigration(migration *MigrationInfo) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.Migrations[migration.ID] = migration
	sm.Version++
	sm.UpdatedAt = time.Now()
}

// UpdateMigration updates migration information
func (sm *ShardMap) UpdateMigration(migrationID string, updateFn func(*MigrationInfo)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if migration, ok := sm.Migrations[migrationID]; ok {
		updateFn(migration)
		sm.Version++
		sm.UpdatedAt = time.Now()
	}
}

// RemoveMigration removes a completed migration from the map
func (sm *ShardMap) RemoveMigration(migrationID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.Migrations, migrationID)
	sm.Version++
	sm.UpdatedAt = time.Now()
}

// GetActiveShards returns all active shards
func (sm *ShardMap) GetActiveShards() []*ShardInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	shards := make([]*ShardInfo, 0)
	for _, shard := range sm.Shards {
		if shard.State == ShardStateActive {
			shards = append(shards, shard)
		}
	}
	return shards
}

// GetActiveNodes returns all active nodes
func (sm *ShardMap) GetActiveNodes() []*NodeInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	nodes := make([]*NodeInfo, 0)
	for _, node := range sm.Nodes {
		if node.State == NodeStateActive {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

// GetActiveMigrations returns all active migrations
func (sm *ShardMap) GetActiveMigrations() []*MigrationInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	migrations := make([]*MigrationInfo, 0)
	for _, migration := range sm.Migrations {
		if migration.State != MigrationStateComplete &&
		   migration.State != MigrationStateFailed &&
		   migration.State != MigrationStateRolledBack {
			migrations = append(migrations, migration)
		}
	}
	return migrations
}

// Clone creates a deep copy of the shard map
func (sm *ShardMap) Clone() *ShardMap {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	clone := &ShardMap{
		Version:    sm.Version,
		Shards:     make(map[int]*ShardInfo, len(sm.Shards)),
		Nodes:      make(map[string]*NodeInfo, len(sm.Nodes)),
		Migrations: make(map[string]*MigrationInfo, len(sm.Migrations)),
		UpdatedAt:  sm.UpdatedAt,
	}

	// Deep copy shards
	for id, shard := range sm.Shards {
		shardCopy := *shard
		shardCopy.Replicas = make([]string, len(shard.Replicas))
		copy(shardCopy.Replicas, shard.Replicas)
		clone.Shards[id] = &shardCopy
	}

	// Deep copy nodes
	for id, node := range sm.Nodes {
		nodeCopy := *node
		nodeCopy.Shards = make([]int, len(node.Shards))
		copy(nodeCopy.Shards, node.Shards)
		clone.Nodes[id] = &nodeCopy
	}

	// Deep copy migrations
	for id, migration := range sm.Migrations {
		migrationCopy := *migration
		migrationCopy.SourceNodes = make([]string, len(migration.SourceNodes))
		copy(migrationCopy.SourceNodes, migration.SourceNodes)
		migrationCopy.TargetNodes = make([]string, len(migration.TargetNodes))
		copy(migrationCopy.TargetNodes, migration.TargetNodes)
		clone.Migrations[id] = &migrationCopy
	}

	return clone
}
