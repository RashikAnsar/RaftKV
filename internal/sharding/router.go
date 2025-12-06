package sharding

import (
	"fmt"
	"sync"

	"go.uber.org/zap"
)

// RouteInfo contains routing information for a key
type RouteInfo struct {
	ShardID       int      // Target shard ID
	Replicas      []string // Node IDs hosting this shard
	LeaderNodeID  string   // Current leader node ID
	IsMigrating   bool     // Whether shard is being migrated
	MigrationInfo *MigrationInfo
}

// Router routes requests to appropriate shards based on consistent hashing
type Router struct {
	mu             sync.RWMutex
	consistentHash *ConsistentHash
	shardMap       *ShardMap
	logger         *zap.Logger
}

// NewRouter creates a new router instance
func NewRouter(numVirtualNodes int, logger *zap.Logger) *Router {
	return &Router{
		consistentHash: NewConsistentHash(numVirtualNodes),
		shardMap:       NewShardMap(),
		logger:         logger,
	}
}

// RouteKey determines the routing information for a given key
func (r *Router) RouteKey(key string) (*RouteInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Get shard ID from consistent hash
	shardID, err := r.consistentHash.GetShard(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get shard for key %s: %w", key, err)
	}

	// Get shard information
	shard, ok := r.shardMap.GetShard(shardID)
	if !ok {
		return nil, fmt.Errorf("shard %d not found in shard map", shardID)
	}

	// Check if shard is being migrated
	migration := r.shardMap.GetMigrationForShard(shardID)
	isMigrating := migration != nil

	routeInfo := &RouteInfo{
		ShardID:       shardID,
		Replicas:      shard.Replicas,
		LeaderNodeID:  shard.LeaderNodeID,
		IsMigrating:   isMigrating,
		MigrationInfo: migration,
	}

	return routeInfo, nil
}

// GetShardInfo returns information about a specific shard
func (r *Router) GetShardInfo(shardID int) (*ShardInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	shard, ok := r.shardMap.GetShard(shardID)
	if !ok {
		return nil, fmt.Errorf("shard %d not found", shardID)
	}

	return shard, nil
}

// GetNodeInfo returns information about a specific node
func (r *Router) GetNodeInfo(nodeID string) (*NodeInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	node, ok := r.shardMap.GetNode(nodeID)
	if !ok {
		return nil, fmt.Errorf("node %s not found", nodeID)
	}

	return node, nil
}

// UpdateShardMap updates the router's shard map with new metadata
// This is typically called when the meta-cluster broadcasts updates
func (r *Router) UpdateShardMap(newShardMap *ShardMap) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Validate shard map version (should be monotonically increasing)
	if newShardMap.Version <= r.shardMap.Version {
		r.logger.Warn("Ignoring stale shard map update",
			zap.Uint64("current_version", r.shardMap.Version),
			zap.Uint64("new_version", newShardMap.Version))
		return nil
	}

	// Update consistent hash with new shards
	// First, find shards that were removed
	currentShards := r.consistentHash.GetShards()
	currentShardMap := make(map[int]bool)
	for _, shardID := range currentShards {
		currentShardMap[shardID] = true
	}

	newShardMap.mu.RLock()
	newShardIDs := make(map[int]bool)
	for shardID := range newShardMap.Shards {
		newShardIDs[shardID] = true
	}
	newShardMap.mu.RUnlock()

	// Remove shards that no longer exist
	for shardID := range currentShardMap {
		if !newShardIDs[shardID] {
			if err := r.consistentHash.RemoveShard(shardID); err != nil {
				r.logger.Error("Failed to remove shard from consistent hash",
					zap.Int("shard_id", shardID),
					zap.Error(err))
			} else {
				r.logger.Info("Removed shard from routing",
					zap.Int("shard_id", shardID))
			}
		}
	}

	// Add new shards
	for shardID := range newShardIDs {
		if !currentShardMap[shardID] {
			if err := r.consistentHash.AddShard(shardID); err != nil {
				r.logger.Error("Failed to add shard to consistent hash",
					zap.Int("shard_id", shardID),
					zap.Error(err))
			} else {
				r.logger.Info("Added shard to routing",
					zap.Int("shard_id", shardID))
			}
		}
	}

	// Update shard map
	r.shardMap = newShardMap.Clone()

	r.logger.Info("Updated shard map",
		zap.Uint64("version", r.shardMap.Version),
		zap.Int("num_shards", len(r.shardMap.Shards)),
		zap.Int("num_nodes", len(r.shardMap.Nodes)),
		zap.Int("num_migrations", len(r.shardMap.Migrations)))

	return nil
}

// AddShard adds a new shard to the routing table
func (r *Router) AddShard(shard *ShardInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Add to consistent hash
	if err := r.consistentHash.AddShard(shard.ID); err != nil {
		return fmt.Errorf("failed to add shard to consistent hash: %w", err)
	}

	// Add to shard map
	r.shardMap.AddShard(shard)

	r.logger.Info("Added shard to router",
		zap.Int("shard_id", shard.ID),
		zap.Strings("replicas", shard.Replicas))

	return nil
}

// RemoveShard removes a shard from the routing table
func (r *Router) RemoveShard(shardID int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Remove from consistent hash
	if err := r.consistentHash.RemoveShard(shardID); err != nil {
		return fmt.Errorf("failed to remove shard from consistent hash: %w", err)
	}

	// Remove from shard map (we'll add this method)
	r.shardMap.mu.Lock()
	delete(r.shardMap.Shards, shardID)
	r.shardMap.Version++
	r.shardMap.mu.Unlock()

	r.logger.Info("Removed shard from router",
		zap.Int("shard_id", shardID))

	return nil
}

// GetAllShards returns all active shards
func (r *Router) GetAllShards() []*ShardInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.shardMap.GetActiveShards()
}

// GetAllNodes returns all active nodes
func (r *Router) GetAllNodes() []*NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.shardMap.GetActiveNodes()
}

// GetActiveMigrations returns all active migrations
func (r *Router) GetActiveMigrations() []*MigrationInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.shardMap.GetActiveMigrations()
}

// GetShardCount returns the number of shards in the routing table
func (r *Router) GetShardCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.consistentHash.GetShardCount()
}

// GetShardMapVersion returns the current version of the shard map
func (r *Router) GetShardMapVersion() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.shardMap.Version
}

// IsHealthy checks if the router is in a healthy state
// Returns false if there are no shards or if critical migrations are failing
func (r *Router) IsHealthy() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Must have at least one shard
	if r.consistentHash.GetShardCount() == 0 {
		return false
	}

	// Check for failed migrations
	for _, migration := range r.shardMap.Migrations {
		if migration.State == MigrationStateFailed {
			r.logger.Warn("Router unhealthy: migration failed",
				zap.String("migration_id", migration.ID),
				zap.Int("shard_id", migration.ShardID))
			return false
		}
	}

	return true
}

// GetRoutingStats returns statistics about routing distribution
func (r *Router) GetRoutingStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["shard_count"] = r.consistentHash.GetShardCount()
	stats["node_count"] = len(r.shardMap.Nodes)
	stats["active_migrations"] = len(r.shardMap.GetActiveMigrations())
	stats["shard_map_version"] = r.shardMap.Version
	stats["virtual_node_distribution"] = r.consistentHash.GetDistribution()

	return stats
}
