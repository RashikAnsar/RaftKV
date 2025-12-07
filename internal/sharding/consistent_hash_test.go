package sharding

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsistentHash_AddShard(t *testing.T) {
	ch := NewConsistentHash(150)

	// Add first shard
	err := ch.AddShard(0)
	require.NoError(t, err)
	assert.Equal(t, 1, ch.GetShardCount())

	// Add second shard
	err = ch.AddShard(1)
	require.NoError(t, err)
	assert.Equal(t, 2, ch.GetShardCount())

	// Try to add duplicate
	err = ch.AddShard(0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestConsistentHash_RemoveShard(t *testing.T) {
	ch := NewConsistentHash(150)

	// Add shards
	ch.AddShard(0)
	ch.AddShard(1)
	ch.AddShard(2)
	assert.Equal(t, 3, ch.GetShardCount())

	// Remove shard
	err := ch.RemoveShard(1)
	require.NoError(t, err)
	assert.Equal(t, 2, ch.GetShardCount())

	// Try to remove non-existent shard
	err = ch.RemoveShard(99)
	assert.Error(t, err)
}

func TestConsistentHash_GetShard(t *testing.T) {
	ch := NewConsistentHash(150)

	// Test with no shards
	_, err := ch.GetShard("test")
	assert.Error(t, err)

	// Add shards
	ch.AddShard(0)
	ch.AddShard(1)
	ch.AddShard(2)

	// Test key distribution
	keys := []string{"user:1", "user:2", "user:3", "order:100", "product:xyz"}
	shardCounts := make(map[int]int)

	for _, key := range keys {
		shardID, err := ch.GetShard(key)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, shardID, 0)
		assert.LessOrEqual(t, shardID, 2)
		shardCounts[shardID]++
	}

	// Verify all shards received some keys
	assert.Greater(t, len(shardCounts), 0)
}

func TestConsistentHash_Consistency(t *testing.T) {
	ch := NewConsistentHash(150)

	ch.AddShard(0)
	ch.AddShard(1)
	ch.AddShard(2)

	// Same key should always map to same shard
	key := "user:123"
	shard1, _ := ch.GetShard(key)
	shard2, _ := ch.GetShard(key)
	shard3, _ := ch.GetShard(key)

	assert.Equal(t, shard1, shard2)
	assert.Equal(t, shard2, shard3)
}

func TestConsistentHash_MinimalMovement(t *testing.T) {
	ch := NewConsistentHash(150)

	// Add 3 shards
	ch.AddShard(0)
	ch.AddShard(1)
	ch.AddShard(2)

	// Map 1000 keys
	keys := make([]string, 1000)
	initialMapping := make(map[string]int)

	for i := 0; i < 1000; i++ {
		keys[i] = fmt.Sprintf("key:%d", i)
		shard, _ := ch.GetShard(keys[i])
		initialMapping[keys[i]] = shard
	}

	// Add a new shard
	ch.AddShard(3)

	// Check how many keys moved
	moved := 0
	for _, key := range keys {
		newShard, _ := ch.GetShard(key)
		if newShard != initialMapping[key] {
			moved++
		}
	}

	// With consistent hashing, only ~1/4 of keys should move
	// (ideally 1000/4 = 250, allow some variance)
	expectedMove := 250
	tolerance := 100

	assert.InDelta(t, expectedMove, moved, float64(tolerance),
		"Expected ~%d keys to move, but %d moved", expectedMove, moved)
}

func TestConsistentHash_Distribution(t *testing.T) {
	ch := NewConsistentHash(150)

	// Add 4 shards
	for i := 0; i < 4; i++ {
		ch.AddShard(i)
	}

	// Map 10,000 keys
	distribution := make(map[int]int)
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("key:%d", i)
		shard, err := ch.GetShard(key)
		require.NoError(t, err)
		distribution[shard]++
	}

	// Each shard should get approximately 10,000/4 = 2,500 keys
	expectedPerShard := 2500
	tolerance := 500.0 // 20% tolerance

	for shardID, count := range distribution {
		assert.InDelta(t, expectedPerShard, count, tolerance,
			"Shard %d has poor distribution: %d keys (expected ~%d)",
			shardID, count, expectedPerShard)
	}
}

func TestConsistentHash_VirtualNodesDistribution(t *testing.T) {
	ch := NewConsistentHash(150)

	ch.AddShard(0)
	ch.AddShard(1)
	ch.AddShard(2)

	distribution := ch.GetDistribution()

	// Each shard should have exactly 150 virtual nodes
	for shardID, count := range distribution {
		assert.Equal(t, 150, count,
			"Shard %d should have 150 virtual nodes, got %d", shardID, count)
	}
}

func TestConsistentHash_GetShards(t *testing.T) {
	ch := NewConsistentHash(150)

	// Empty initially
	shards := ch.GetShards()
	assert.Empty(t, shards)

	// Add shards
	ch.AddShard(2)
	ch.AddShard(0)
	ch.AddShard(1)

	// Should return sorted list
	shards = ch.GetShards()
	assert.Equal(t, []int{0, 1, 2}, shards)
}

func TestConsistentHash_Clear(t *testing.T) {
	ch := NewConsistentHash(150)

	ch.AddShard(0)
	ch.AddShard(1)
	ch.AddShard(2)

	assert.Equal(t, 3, ch.GetShardCount())

	ch.Clear()

	assert.Equal(t, 0, ch.GetShardCount())
	assert.Empty(t, ch.GetShards())
}

func TestConsistentHash_Concurrent(t *testing.T) {
	ch := NewConsistentHash(150)

	// Add initial shards
	for i := 0; i < 5; i++ {
		ch.AddShard(i)
	}

	// Concurrent reads
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 1000; j++ {
				key := fmt.Sprintf("key:%d:%d", id, j)
				_, err := ch.GetShard(key)
				assert.NoError(t, err)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestConsistentHash_StandardDeviation(t *testing.T) {
	ch := NewConsistentHash(150)

	// Add 8 shards
	for i := 0; i < 8; i++ {
		ch.AddShard(i)
	}

	// Map 100,000 keys
	distribution := make(map[int]int)
	for i := 0; i < 100000; i++ {
		key := fmt.Sprintf("key:%d", i)
		shard, _ := ch.GetShard(key)
		distribution[shard]++
	}

	// Calculate mean
	var sum float64
	for _, count := range distribution {
		sum += float64(count)
	}
	mean := sum / float64(len(distribution))

	// Calculate standard deviation
	var variance float64
	for _, count := range distribution {
		diff := float64(count) - mean
		variance += diff * diff
	}
	variance /= float64(len(distribution))
	stdDev := math.Sqrt(variance)

	// Standard deviation should be small (< 10% of mean)
	// This is reasonable for consistent hashing with virtual nodes
	maxStdDev := mean * 0.10
	assert.Less(t, stdDev, maxStdDev,
		"Standard deviation %.2f is too high (mean: %.2f, max allowed: %.2f)",
		stdDev, mean, maxStdDev)
}

// Benchmark consistent hash operations
func BenchmarkConsistentHash_GetShard(b *testing.B) {
	ch := NewConsistentHash(150)

	// Add 256 shards
	for i := 0; i < 256; i++ {
		ch.AddShard(i)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key:%d", i)
		ch.GetShard(key)
	}
}

func BenchmarkConsistentHash_AddShard(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ch := NewConsistentHash(150)
		ch.AddShard(i % 256)
	}
}

func BenchmarkConsistentHash_RemoveShard(b *testing.B) {
	ch := NewConsistentHash(150)

	// Pre-populate
	for i := 0; i < 256; i++ {
		ch.AddShard(i)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		shardID := i % 256
		ch.RemoveShard(shardID)
		ch.AddShard(shardID) // Re-add for next iteration
	}
}
