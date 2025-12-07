package sharding

import (
	"fmt"
	"sort"
	"sync"

	"github.com/cespare/xxhash/v2"
)

// ConsistentHash implements consistent hashing with virtual nodes
// for even distribution of keys across shards
type ConsistentHash struct {
	mu           sync.RWMutex
	ring         []uint32            // Sorted hash positions on ring
	vnodeToShard map[uint32]int      // Virtual node position → Shard ID
	shards       map[int]bool        // Active shard IDs
	numVNodes    int                 // Virtual nodes per shard
	hashFunc     func([]byte) uint32 // Hash function
}

// NewConsistentHash creates a new consistent hash ring
// numVNodes: number of virtual nodes per shard (recommended: 150)
func NewConsistentHash(numVNodes int) *ConsistentHash {
	return &ConsistentHash{
		vnodeToShard: make(map[uint32]int),
		shards:       make(map[int]bool),
		numVNodes:    numVNodes,
		hashFunc: func(data []byte) uint32 {
			return uint32(xxhash.Sum64(data))
		},
	}
}

// AddShard adds a shard to the consistent hash ring
func (ch *ConsistentHash) AddShard(shardID int) error {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if ch.shards[shardID] {
		return fmt.Errorf("shard %d already exists", shardID)
	}

	// Add virtual nodes for this shard
	for i := 0; i < ch.numVNodes; i++ {
		// Create unique key for each virtual node
		vnodeKey := fmt.Sprintf("shard-%d-vnode-%d", shardID, i)
		hash := ch.hashFunc([]byte(vnodeKey))

		ch.ring = append(ch.ring, hash)
		ch.vnodeToShard[hash] = shardID
	}

	ch.shards[shardID] = true

	// Sort ring for binary search
	sort.Slice(ch.ring, func(i, j int) bool {
		return ch.ring[i] < ch.ring[j]
	})

	return nil
}

// RemoveShard removes a shard from the consistent hash ring
func (ch *ConsistentHash) RemoveShard(shardID int) error {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if !ch.shards[shardID] {
		return fmt.Errorf("shard %d does not exist", shardID)
	}

	// Remove all virtual nodes for this shard
	newRing := make([]uint32, 0, len(ch.ring))
	for _, hash := range ch.ring {
		if ch.vnodeToShard[hash] != shardID {
			newRing = append(newRing, hash)
		} else {
			delete(ch.vnodeToShard, hash)
		}
	}

	ch.ring = newRing
	delete(ch.shards, shardID)

	return nil
}

// GetShard returns the shard ID for a given key
func (ch *ConsistentHash) GetShard(key string) (int, error) {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	if len(ch.ring) == 0 {
		return 0, fmt.Errorf("no shards available")
	}

	// Hash the key
	hash := ch.hashFunc([]byte(key))

	// Binary search to find the first virtual node >= hash
	idx := sort.Search(len(ch.ring), func(i int) bool {
		return ch.ring[i] >= hash
	})

	// Wrap around if we reached the end
	if idx == len(ch.ring) {
		idx = 0
	}

	vnodeHash := ch.ring[idx]
	shardID := ch.vnodeToShard[vnodeHash]

	return shardID, nil
}

// GetShardCount returns the number of active shards
func (ch *ConsistentHash) GetShardCount() int {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	return len(ch.shards)
}

// GetShards returns a list of all active shard IDs
func (ch *ConsistentHash) GetShards() []int {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	shards := make([]int, 0, len(ch.shards))
	for shardID := range ch.shards {
		shards = append(shards, shardID)
	}

	sort.Ints(shards)
	return shards
}

// GetDistribution returns the distribution of virtual nodes per shard
// Useful for debugging and verifying even distribution
func (ch *ConsistentHash) GetDistribution() map[int]int {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	distribution := make(map[int]int)
	for _, shardID := range ch.vnodeToShard {
		distribution[shardID]++
	}

	return distribution
}

// Clear removes all shards from the ring
func (ch *ConsistentHash) Clear() {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	ch.ring = nil
	ch.vnodeToShard = make(map[uint32]int)
	ch.shards = make(map[int]bool)
}
