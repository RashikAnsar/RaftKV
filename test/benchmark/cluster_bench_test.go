package benchmark

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/RashikAnsar/raftkv/internal/consensus"
	"github.com/RashikAnsar/raftkv/internal/storage"
)

// BenchmarkCluster represents a benchmark cluster of Raft nodes
type BenchmarkCluster struct {
	nodes  []*consensus.RaftNode
	stores []*storage.DurableStore
	dirs   []string
	logger *zap.Logger
}

// getFreePort gets an available port
func getFreePort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := listener.Addr().String()
	listener.Close()
	return addr, nil
}

// NewBenchmarkCluster creates a new benchmark cluster
func NewBenchmarkCluster(b *testing.B, nodeCount int) *BenchmarkCluster {
	b.Helper()

	logger := zap.NewNop() // Use no-op logger for benchmarks

	cluster := &BenchmarkCluster{
		nodes:  make([]*consensus.RaftNode, nodeCount),
		stores: make([]*storage.DurableStore, nodeCount),
		dirs:   make([]string, nodeCount),
		logger: logger,
	}

	// Create nodes
	for i := 0; i < nodeCount; i++ {
		tmpDir, err := os.MkdirTemp("", fmt.Sprintf("bench-cluster-node%d-*", i))
		if err != nil {
			b.Fatalf("Failed to create temp dir: %v", err)
		}
		cluster.dirs[i] = tmpDir

		store, err := storage.NewDurableStore(storage.DurableStoreConfig{
			DataDir:       filepath.Join(tmpDir, "store"),
			SyncOnWrite:   false,
			SnapshotEvery: 10000,
		})
		if err != nil {
			b.Fatalf("Failed to create store: %v", err)
		}
		cluster.stores[i] = store

		raftAddr, err := getFreePort()
		if err != nil {
			b.Fatalf("Failed to get free port: %v", err)
		}

		node, err := consensus.NewRaftNode(consensus.RaftConfig{
			NodeID:    fmt.Sprintf("node%d", i),
			RaftAddr:  raftAddr,
			RaftDir:   filepath.Join(tmpDir, "raft"),
			Bootstrap: i == 0,
			Store:     store,
			Logger:    logger,
		})
		if err != nil {
			b.Fatalf("Failed to create node: %v", err)
		}
		cluster.nodes[i] = node
	}

	return cluster
}

// WaitForLeader waits for a leader to be elected
func (bc *BenchmarkCluster) WaitForLeader(timeout time.Duration) *consensus.RaftNode {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, node := range bc.nodes {
			if node.IsLeader() {
				return node
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

// JoinNodes joins all non-bootstrap nodes to the cluster
func (bc *BenchmarkCluster) JoinNodes() error {
	// Wait for bootstrap node to become leader
	leader := bc.WaitForLeader(10 * time.Second)
	if leader == nil {
		return fmt.Errorf("bootstrap node failed to become leader")
	}

	// Join remaining nodes
	for i := 1; i < len(bc.nodes); i++ {
		node := bc.nodes[i]
		nodeID := fmt.Sprintf("node%d", i)

		if err := leader.Join(nodeID, node.RaftAddr); err != nil {
			return fmt.Errorf("failed to join node %d: %w", i, err)
		}

		// Wait a bit for the node to join
		time.Sleep(200 * time.Millisecond)
	}

	return nil
}

// Close shuts down the cluster
func (bc *BenchmarkCluster) Close() {
	for _, node := range bc.nodes {
		if node != nil {
			node.Shutdown()
		}
	}
	for _, store := range bc.stores {
		if store != nil {
			store.Close()
		}
	}
	for _, dir := range bc.dirs {
		os.RemoveAll(dir)
	}
}

// BenchmarkCluster_ThreeNodeWrites benchmarks write performance on a 3-node cluster
func BenchmarkCluster_ThreeNodeWrites(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping slow cluster benchmark in short mode")
	}

	cluster := NewBenchmarkCluster(b, 3)
	defer cluster.Close()

	// Join all nodes to the cluster
	if err := cluster.JoinNodes(); err != nil {
		b.Fatalf("Failed to join nodes: %v", err)
	}

	// Wait for leader election
	leader := cluster.WaitForLeader(10 * time.Second)
	if leader == nil {
		b.Fatal("No leader elected")
	}

	// Wait a bit more for cluster to stabilize
	time.Sleep(500 * time.Millisecond)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("key-%d", i),
			Value: []byte(fmt.Sprintf("value-%d", i)),
		}
		err := leader.Apply(cmd, 5*time.Second)
		if err != nil {
			b.Errorf("Apply failed: %v", err)
		}
	}

	b.StopTimer()
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
}

// BenchmarkCluster_FiveNodeWrites benchmarks write performance on a 5-node cluster
func BenchmarkCluster_FiveNodeWrites(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping slow cluster benchmark in short mode")
	}

	cluster := NewBenchmarkCluster(b, 5)
	defer cluster.Close()

	// Join all nodes to the cluster
	if err := cluster.JoinNodes(); err != nil {
		b.Fatalf("Failed to join nodes: %v", err)
	}

	leader := cluster.WaitForLeader(10 * time.Second)
	if leader == nil {
		b.Fatal("No leader elected")
	}

	time.Sleep(500 * time.Millisecond)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("key-%d", i),
			Value: []byte(fmt.Sprintf("value-%d", i)),
		}
		err := leader.Apply(cmd, 5*time.Second)
		if err != nil {
			b.Errorf("Apply failed: %v", err)
		}
	}

	b.StopTimer()
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
}

// BenchmarkCluster_ConcurrentWrites benchmarks concurrent writes to a cluster
func BenchmarkCluster_ConcurrentWrites(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping slow cluster benchmark in short mode")
	}

	cluster := NewBenchmarkCluster(b, 3)
	defer cluster.Close()

	// Join all nodes to the cluster
	if err := cluster.JoinNodes(); err != nil {
		b.Fatalf("Failed to join nodes: %v", err)
	}

	leader := cluster.WaitForLeader(10 * time.Second)
	if leader == nil {
		b.Fatal("No leader elected")
	}

	time.Sleep(500 * time.Millisecond)

	workers := []int{1, 5, 10}

	for _, numWorkers := range workers {
		b.Run(fmt.Sprintf("Workers-%d", numWorkers), func(b *testing.B) {
			b.ResetTimer()

			var wg sync.WaitGroup
			opsPerWorker := b.N / numWorkers

			for w := 0; w < numWorkers; w++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					for i := 0; i < opsPerWorker; i++ {
						cmd := consensus.Command{
							Op:    consensus.OpTypePut,
							Key:   fmt.Sprintf("key-w%d-%d", workerID, i),
							Value: []byte(fmt.Sprintf("value-%d", i)),
						}
						err := leader.Apply(cmd, 5*time.Second)
						if err != nil {
							b.Errorf("Apply failed: %v", err)
						}
					}
				}(w)
			}

			wg.Wait()
			b.StopTimer()
			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
		})
	}
}

// BenchmarkCluster_Reads benchmarks read performance from different nodes
func BenchmarkCluster_Reads(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping slow cluster benchmark in short mode")
	}

	cluster := NewBenchmarkCluster(b, 3)
	defer cluster.Close()

	// Join all nodes to the cluster
	if err := cluster.JoinNodes(); err != nil {
		b.Fatalf("Failed to join nodes: %v", err)
	}

	leader := cluster.WaitForLeader(10 * time.Second)
	if leader == nil {
		b.Fatal("No leader elected")
	}

	time.Sleep(500 * time.Millisecond)

	// Pre-populate data
	ctx := context.Background()
	for i := 0; i < 1000; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("key-%d", i),
			Value: []byte(fmt.Sprintf("value-%d", i)),
		}
		err := leader.Apply(cmd, 5*time.Second)
		if err != nil {
			b.Fatalf("Failed to populate data: %v", err)
		}
	}

	// Wait for replication
	time.Sleep(1 * time.Second)

	b.Run("ReadFromLeader", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i%1000)
			_, err := leader.Get(ctx, key)
			if err != nil {
				b.Errorf("Get failed: %v", err)
			}
		}
		b.StopTimer()
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
	})

	b.Run("ReadFromFollower", func(b *testing.B) {
		follower := cluster.nodes[1]
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i%1000)
			_, err := follower.Get(ctx, key)
			if err != nil {
				b.Errorf("Get failed: %v", err)
			}
		}
		b.StopTimer()
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
	})
}

// BenchmarkCluster_ReplicationLatency measures replication latency
func BenchmarkCluster_ReplicationLatency(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping slow cluster benchmark in short mode")
	}

	cluster := NewBenchmarkCluster(b, 3)
	defer cluster.Close()

	// Join all nodes to the cluster
	if err := cluster.JoinNodes(); err != nil {
		b.Fatalf("Failed to join nodes: %v", err)
	}

	leader := cluster.WaitForLeader(10 * time.Second)
	if leader == nil {
		b.Fatal("No leader elected")
	}

	time.Sleep(500 * time.Millisecond)

	follower := cluster.nodes[1]
	ctx := context.Background()

	b.ResetTimer()

	var totalLatency time.Duration
	successCount := 0

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key-%d", i)
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   key,
			Value: []byte(fmt.Sprintf("value-%d", i)),
		}

		start := time.Now()
		err := leader.Apply(cmd, 5*time.Second)
		if err != nil {
			b.Errorf("Apply failed: %v", err)
			continue
		}

		// Wait for replication to follower
		timeout := time.After(5 * time.Second)
		replicated := false
		for !replicated {
			select {
			case <-timeout:
				b.Errorf("Replication timeout for key %s", key)
				break
			default:
				val, err := follower.Get(ctx, key)
				if err == nil && val != nil {
					latency := time.Since(start)
					totalLatency += latency
					successCount++
					replicated = true
				} else {
					time.Sleep(10 * time.Millisecond)
				}
			}
		}
	}

	b.StopTimer()

	if successCount > 0 {
		avgLatencyMs := totalLatency.Seconds() / float64(successCount) * 1000
		b.ReportMetric(avgLatencyMs, "ms/replication")
	}
}

// BenchmarkCluster_MixedWorkload benchmarks a mixed read/write workload
func BenchmarkCluster_MixedWorkload(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping slow cluster benchmark in short mode")
	}

	cluster := NewBenchmarkCluster(b, 3)
	defer cluster.Close()

	// Join all nodes to the cluster
	if err := cluster.JoinNodes(); err != nil {
		b.Fatalf("Failed to join nodes: %v", err)
	}

	leader := cluster.WaitForLeader(10 * time.Second)
	if leader == nil {
		b.Fatal("No leader elected")
	}

	time.Sleep(500 * time.Millisecond)

	// Pre-populate some data
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		cmd := consensus.Command{
			Op:    consensus.OpTypePut,
			Key:   fmt.Sprintf("key-%d", i),
			Value: []byte(fmt.Sprintf("value-%d", i)),
		}
		leader.Apply(cmd, 5*time.Second)
	}

	time.Sleep(500 * time.Millisecond)

	b.ResetTimer()

	// 70% reads, 30% writes
	reads := 0
	writes := 0

	for i := 0; i < b.N; i++ {
		if i%10 < 7 {
			// Read
			key := fmt.Sprintf("key-%d", i%100)
			_, err := leader.Get(ctx, key)
			if err != nil {
				b.Errorf("Get failed: %v", err)
			}
			reads++
		} else {
			// Write
			cmd := consensus.Command{
				Op:    consensus.OpTypePut,
				Key:   fmt.Sprintf("key-%d", i),
				Value: []byte(fmt.Sprintf("value-%d", i)),
			}
			err := leader.Apply(cmd, 5*time.Second)
			if err != nil {
				b.Errorf("Apply failed: %v", err)
			}
			writes++
		}
	}

	b.StopTimer()
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
	b.ReportMetric(float64(reads), "reads")
	b.ReportMetric(float64(writes), "writes")
}
