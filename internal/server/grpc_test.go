package server

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/RashikAnsar/raftkv/api/proto"
	"github.com/RashikAnsar/raftkv/internal/consensus"
	"github.com/RashikAnsar/raftkv/internal/storage"
)

const bufSize = 1024 * 1024

// setupTestGRPCServer creates a test gRPC server with in-memory backend
func setupTestGRPCServer(t *testing.T) (*GRPCServer, pb.KVStoreClient, func()) {
	// Create a new bufconn listener for each test
	lis := bufconn.Listen(bufSize)

	bufDialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}
	// Create in-memory store
	store := storage.NewMemoryStore()

	// Create logger
	logger := zap.NewNop()

	// Create temporary Raft node (single node cluster for testing)
	raftConfig := consensus.RaftConfig{
		NodeID:    "test-node-1",
		RaftAddr:  "localhost:7000",
		RaftDir:   t.TempDir(),
		Bootstrap: true,
		Store:     store,
		Logger:    logger,
	}

	raftNode, err := consensus.NewRaftNode(raftConfig)
	if err != nil {
		t.Fatalf("Failed to create Raft node: %v", err)
	}

	// Wait for leader election
	if err := raftNode.WaitForLeader(5 * time.Second); err != nil {
		t.Fatalf("Failed to wait for leader: %v", err)
	}

	// Create gRPC server
	serverConfig := GRPCServerConfig{
		Addr:     "bufnet",
		RaftNode: raftNode,
		Logger:   logger,
		Metrics:  nil, // Skip metrics in tests to avoid registry conflicts
	}

	server := NewGRPCServer(serverConfig)

	// Start server on bufconn (service already registered in NewGRPCServer)
	go func() {
		if err := server.server.Serve(lis); err != nil {
			t.Logf("Server exited with error: %v", err)
		}
	}()

	// Create client connection
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}

	client := pb.NewKVStoreClient(conn)

	cleanup := func() {
		conn.Close()
		server.Stop()
		raftNode.Shutdown()
		lis.Close()
	}

	return server, client, cleanup
}

func TestGRPCServer_PutAndGet(t *testing.T) {
	_, client, cleanup := setupTestGRPCServer(t)
	defer cleanup()

	ctx := context.Background()

	// Test Put
	putResp, err := client.Put(ctx, &pb.PutRequest{
		Key:   "test-key",
		Value: []byte("test-value"),
	})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if !putResp.Success {
		t.Fatalf("Put returned success=false: %s", putResp.Error)
	}

	// Test Get
	getResp, err := client.Get(ctx, &pb.GetRequest{
		Key: "test-key",
	})
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !getResp.Found {
		t.Fatal("Get returned found=false")
	}
	if string(getResp.Value) != "test-value" {
		t.Fatalf("Expected 'test-value', got '%s'", string(getResp.Value))
	}
}

func TestGRPCServer_GetNonExistent(t *testing.T) {
	_, client, cleanup := setupTestGRPCServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get non-existent key
	getResp, err := client.Get(ctx, &pb.GetRequest{
		Key: "non-existent-key",
	})
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if getResp.Found {
		t.Fatal("Expected found=false for non-existent key")
	}
}

func TestGRPCServer_Delete(t *testing.T) {
	_, client, cleanup := setupTestGRPCServer(t)
	defer cleanup()

	ctx := context.Background()

	// Put a key
	_, err := client.Put(ctx, &pb.PutRequest{
		Key:   "delete-key",
		Value: []byte("delete-value"),
	})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify it exists
	getResp, err := client.Get(ctx, &pb.GetRequest{
		Key: "delete-key",
	})
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !getResp.Found {
		t.Fatal("Key should exist before delete")
	}

	// Delete the key
	delResp, err := client.Delete(ctx, &pb.DeleteRequest{
		Key: "delete-key",
	})
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if !delResp.Success {
		t.Fatalf("Delete returned success=false: %s", delResp.Error)
	}

	// Verify it's gone
	getResp, err = client.Get(ctx, &pb.GetRequest{
		Key: "delete-key",
	})
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if getResp.Found {
		t.Fatal("Key should not exist after delete")
	}
}

func TestGRPCServer_List(t *testing.T) {
	_, client, cleanup := setupTestGRPCServer(t)
	defer cleanup()

	ctx := context.Background()

	// Put multiple keys with different prefixes
	keys := []string{
		"user:1:name",
		"user:1:email",
		"user:2:name",
		"product:1:name",
		"product:2:price",
	}

	for _, key := range keys {
		_, err := client.Put(ctx, &pb.PutRequest{
			Key:   key,
			Value: []byte("value"),
		})
		if err != nil {
			t.Fatalf("Put failed for key %s: %v", key, err)
		}
	}

	// List all keys
	listResp, err := client.List(ctx, &pb.ListRequest{
		Prefix: "",
		Limit:  0,
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(listResp.Keys) != len(keys) {
		t.Fatalf("Expected %d keys, got %d", len(keys), len(listResp.Keys))
	}

	// List with prefix "user:"
	listResp, err = client.List(ctx, &pb.ListRequest{
		Prefix: "user:",
		Limit:  0,
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(listResp.Keys) != 3 {
		t.Fatalf("Expected 3 keys with prefix 'user:', got %d", len(listResp.Keys))
	}

	// List with prefix "product:" and limit
	listResp, err = client.List(ctx, &pb.ListRequest{
		Prefix: "product:",
		Limit:  1,
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(listResp.Keys) != 1 {
		t.Fatalf("Expected 1 key with limit=1, got %d", len(listResp.Keys))
	}
}

func TestGRPCServer_GetStats(t *testing.T) {
	_, client, cleanup := setupTestGRPCServer(t)
	defer cleanup()

	ctx := context.Background()

	// Perform some operations
	for i := 0; i < 5; i++ {
		_, err := client.Put(ctx, &pb.PutRequest{
			Key:   fmt.Sprintf("key-%d", i),
			Value: []byte(fmt.Sprintf("value-%d", i)),
		})
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Get stats
	statsResp, err := client.GetStats(ctx, &pb.StatsRequest{})
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	// Verify stats
	if statsResp.KeyCount != 5 {
		t.Fatalf("Expected 5 keys, got %d", statsResp.KeyCount)
	}
	if statsResp.PutCount != 5 {
		t.Fatalf("Expected 5 puts, got %d", statsResp.PutCount)
	}
	if statsResp.RaftState != "leader" {
		t.Fatalf("Expected raft state 'leader', got '%s'", statsResp.RaftState)
	}
}

func TestGRPCServer_GetLeader(t *testing.T) {
	_, client, cleanup := setupTestGRPCServer(t)
	defer cleanup()

	ctx := context.Background()

	// Get leader info
	leaderResp, err := client.GetLeader(ctx, &pb.LeaderRequest{})
	if err != nil {
		t.Fatalf("GetLeader failed: %v", err)
	}

	if !leaderResp.IsLeader {
		t.Fatal("Expected this node to be the leader")
	}
	if leaderResp.LeaderAddress == "" {
		t.Fatal("Expected non-empty leader address")
	}
}

func TestGRPCServer_ConcurrentOperations(t *testing.T) {
	_, client, cleanup := setupTestGRPCServer(t)
	defer cleanup()

	ctx := context.Background()
	concurrency := 10
	operationsPerGoroutine := 100

	errCh := make(chan error, concurrency)

	// Spawn concurrent writers
	for i := 0; i < concurrency; i++ {
		go func(id int) {
			for j := 0; j < operationsPerGoroutine; j++ {
				key := fmt.Sprintf("concurrent-key-%d-%d", id, j)
				value := fmt.Sprintf("value-%d-%d", id, j)

				_, err := client.Put(ctx, &pb.PutRequest{
					Key:   key,
					Value: []byte(value),
				})
				if err != nil {
					errCh <- fmt.Errorf("put failed: %w", err)
					return
				}

				// Verify immediately
				getResp, err := client.Get(ctx, &pb.GetRequest{
					Key: key,
				})
				if err != nil {
					errCh <- fmt.Errorf("get failed: %w", err)
					return
				}
				if !getResp.Found || string(getResp.Value) != value {
					errCh <- fmt.Errorf("value mismatch for key %s", key)
					return
				}
			}
			errCh <- nil
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < concurrency; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("Concurrent operation failed: %v", err)
		}
	}

	// Verify final count
	statsResp, err := client.GetStats(ctx, &pb.StatsRequest{})
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	expectedKeys := uint64(concurrency * operationsPerGoroutine)
	if statsResp.KeyCount != expectedKeys {
		t.Fatalf("Expected %d keys, got %d", expectedKeys, statsResp.KeyCount)
	}
}

func BenchmarkGRPCServer_Put(b *testing.B) {
	_, client, cleanup := setupTestGRPCServer(&testing.T{})
	defer cleanup()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%d", i)
		_, err := client.Put(ctx, &pb.PutRequest{
			Key:   key,
			Value: []byte("benchmark-value"),
		})
		if err != nil {
			b.Fatalf("Put failed: %v", err)
		}
	}
}

func BenchmarkGRPCServer_Get(b *testing.B) {
	_, client, cleanup := setupTestGRPCServer(&testing.T{})
	defer cleanup()

	ctx := context.Background()

	// Pre-populate data
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("bench-key-%d", i)
		_, err := client.Put(ctx, &pb.PutRequest{
			Key:   key,
			Value: []byte("benchmark-value"),
		})
		if err != nil {
			b.Fatalf("Put failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%d", i%1000)
		_, err := client.Get(ctx, &pb.GetRequest{
			Key: key,
		})
		if err != nil {
			b.Fatalf("Get failed: %v", err)
		}
	}
}
