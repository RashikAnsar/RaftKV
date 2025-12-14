//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/RashikAnsar/raftkv/api/proto"
	"github.com/RashikAnsar/raftkv/internal/consensus"
	"github.com/RashikAnsar/raftkv/internal/observability"
	"github.com/RashikAnsar/raftkv/internal/server"
	"github.com/RashikAnsar/raftkv/internal/storage"
)

// TestLeadership_GetLeaderInfo tests getting current leader information
func TestLeadership_GetLeaderInfo(t *testing.T) {
	ctx := context.Background()

	// Create a test cluster
	node, store, tmpDir := createTestRaftNodeForLeadership(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	// Wait for leadership
	require.NoError(t, node.WaitForLeader(5*time.Second))

	// Get leadership info
	info, err := node.GetLeadershipInfo()
	require.NoError(t, err)
	assert.NotNil(t, info)

	// Verify leadership info
	assert.True(t, info.IsLeader)
	assert.Equal(t, "leader", info.State)
	assert.NotEmpty(t, info.NodeID)
	assert.NotEmpty(t, info.LeaderID)
	assert.NotEmpty(t, info.LeaderAddress)
	assert.Greater(t, info.Term, uint64(0))
	assert.GreaterOrEqual(t, info.CommitIndex, uint64(0))
	assert.GreaterOrEqual(t, info.AppliedIndex, uint64(0))

	// Test store operations to ensure node is functional
	require.NoError(t, store.Put(ctx, "test-key", []byte("test-value")))
	value, err := store.Get(ctx, "test-key")
	require.NoError(t, err)
	assert.Equal(t, "test-value", string(value))
}

// TestLeadership_GetElectionHistory tests election history tracking
func TestLeadership_GetElectionHistory(t *testing.T) {
	// Create a test cluster
	node, store, tmpDir := createTestRaftNodeForLeadership(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	// Wait for leadership
	require.NoError(t, node.WaitForLeader(5*time.Second))

	// Get election history
	history := node.GetElectionHistory()
	assert.NotNil(t, history)

	// If there are events, verify structure
	if len(history) > 0 {
		event := history[0]
		assert.NotZero(t, event.Timestamp)
		assert.NotEmpty(t, event.Reason)
		assert.NotZero(t, event.Term)
	}
}

// TestLeadership_Stepdown tests leader stepdown functionality
func TestLeadership_Stepdown(t *testing.T) {
	// Create a test cluster with single node
	node, store, tmpDir := createTestRaftNodeForLeadership(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	// Wait for leadership
	require.NoError(t, node.WaitForLeader(5*time.Second))
	assert.True(t, node.IsLeader())

	// Perform stepdown
	err := node.Stepdown()
	// In a single-node cluster, stepdown fails because there's no peer to transfer to
	// This is expected behavior
	if err != nil {
		assert.Contains(t, err.Error(), "cannot find peer")
	} else {
		// If stepdown succeeds, verify it's recorded
		time.Sleep(1 * time.Second)
		history := node.GetElectionHistory()
		found := false
		for _, event := range history {
			if event.Reason == "stepdown" {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected to find stepdown event in history")
	}
}

// TestLeadership_HTTP_GetLeader tests the HTTP GET /cluster/leader endpoint
func TestLeadership_HTTP_GetLeader(t *testing.T) {
	// Create test cluster
	node, store, tmpDir := createTestRaftNodeForLeadership(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	// Wait for leadership
	require.NoError(t, node.WaitForLeader(5*time.Second))

	// Create HTTP server
	logger, _ := observability.NewLogger("error")
	httpServer := server.NewHTTPServer(server.HTTPServerConfig{
		Addr:     "localhost:0",
		Store:    store,
		Logger:   logger,
		RaftNode: node,
	})

	// Get actual address
	testServer := &http.Server{
		Addr:    "localhost:18080",
		Handler: httpServer.Handler(),
	}

	go testServer.ListenAndServe()
	defer testServer.Close()

	time.Sleep(200 * time.Millisecond) // Let server start

	// Test GET /cluster/leader
	resp, err := http.Get("http://localhost:18080/cluster/leader")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.True(t, result["is_leader"].(bool))
	assert.NotEmpty(t, result["node_id"])
	assert.NotEmpty(t, result["leader_id"])
}

// TestLeadership_HTTP_GetLeadershipInfo tests the HTTP GET /cluster/leadership endpoint
func TestLeadership_HTTP_GetLeadershipInfo(t *testing.T) {
	// Create test cluster
	node, store, tmpDir := createTestRaftNodeForLeadership(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	// Wait for leadership
	require.NoError(t, node.WaitForLeader(5*time.Second))

	// Create HTTP server
	logger, _ := observability.NewLogger("error")
	httpServer := server.NewHTTPServer(server.HTTPServerConfig{
		Addr:     "localhost:0",
		Store:    store,
		Logger:   logger,
		RaftNode: node,
	})

	testServer := &http.Server{
		Addr:    "localhost:18081",
		Handler: httpServer.Handler(),
	}

	go testServer.ListenAndServe()
	defer testServer.Close()

	time.Sleep(200 * time.Millisecond) // Let server start

	// Test GET /cluster/leadership
	resp, err := http.Get("http://localhost:18081/cluster/leadership")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var info consensus.LeadershipInfo
	err = json.NewDecoder(resp.Body).Decode(&info)
	require.NoError(t, err)

	assert.True(t, info.IsLeader)
	assert.Equal(t, "leader", info.State)
	assert.NotEmpty(t, info.NodeID)
	assert.Greater(t, info.Term, uint64(0))
}

// TestLeadership_HTTP_Stepdown tests the HTTP POST /cluster/leadership/stepdown endpoint
func TestLeadership_HTTP_Stepdown(t *testing.T) {
	// Create test cluster
	node, store, tmpDir := createTestRaftNodeForLeadership(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	// Wait for leadership
	require.NoError(t, node.WaitForLeader(5*time.Second))

	// Create HTTP server
	logger, _ := observability.NewLogger("error")
	httpServer := server.NewHTTPServer(server.HTTPServerConfig{
		Addr:     "localhost:0",
		Store:    store,
		Logger:   logger,
		RaftNode: node,
	})

	testServer := &http.Server{
		Addr:    "localhost:18082",
		Handler: httpServer.Handler(),
	}

	go testServer.ListenAndServe()
	defer testServer.Close()

	time.Sleep(200 * time.Millisecond) // Let server start

	// Test POST /cluster/leadership/stepdown
	resp, err := http.Post("http://localhost:18082/cluster/leadership/stepdown", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	// In a single-node cluster, stepdown may fail because there's no peer to transfer to
	// This is expected behavior
	if resp.StatusCode == http.StatusOK {
		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		assert.Contains(t, result["message"], "stepdown")
	} else {
		// Expect internal server error due to "cannot find peer"
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		bodyBytes, _ := io.ReadAll(resp.Body)
		assert.Contains(t, string(bodyBytes), "cannot find peer")
	}
}

// TestLeadership_HTTP_ElectionHistory tests the HTTP GET /cluster/elections/history endpoint
func TestLeadership_HTTP_ElectionHistory(t *testing.T) {
	// Create test cluster
	node, store, tmpDir := createTestRaftNodeForLeadership(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	// Wait for leadership
	require.NoError(t, node.WaitForLeader(5*time.Second))

	// Create HTTP server
	logger, _ := observability.NewLogger("error")
	httpServer := server.NewHTTPServer(server.HTTPServerConfig{
		Addr:     "localhost:0",
		Store:    store,
		Logger:   logger,
		RaftNode: node,
	})

	testServer := &http.Server{
		Addr:    "localhost:18083",
		Handler: httpServer.Handler(),
	}

	go testServer.ListenAndServe()
	defer testServer.Close()

	time.Sleep(200 * time.Millisecond) // Let server start

	// Test GET /cluster/elections/history
	resp, err := http.Get("http://localhost:18083/cluster/elections/history")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Contains(t, result, "elections")
	assert.Contains(t, result, "count")
}

// TestLeadership_HTTP_TransferLeadership tests the HTTP POST /cluster/leadership/transfer endpoint
func TestLeadership_HTTP_TransferLeadership(t *testing.T) {
	// Create test cluster
	node, store, tmpDir := createTestRaftNodeForLeadership(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	// Wait for leadership
	require.NoError(t, node.WaitForLeader(5*time.Second))

	// Create HTTP server
	logger, _ := observability.NewLogger("error")
	httpServer := server.NewHTTPServer(server.HTTPServerConfig{
		Addr:     "localhost:0",
		Store:    store,
		Logger:   logger,
		RaftNode: node,
	})

	testServer := &http.Server{
		Addr:    "localhost:18084",
		Handler: httpServer.Handler(),
	}

	go testServer.ListenAndServe()
	defer testServer.Close()

	time.Sleep(200 * time.Millisecond) // Let server start

	// Test POST /cluster/leadership/transfer with invalid target (single node cluster)
	body := strings.NewReader(`{"target_node_id": "nonexistent"}`)
	resp, err := http.Post("http://localhost:18084/cluster/leadership/transfer", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should fail because target node doesn't exist
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	bodyBytes, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(bodyBytes), "not found")
}

// TestLeadership_GRPC_GetLeadershipInfo tests the gRPC GetLeadershipInfo method
func TestLeadership_GRPC_GetLeadershipInfo(t *testing.T) {
	// Create test cluster
	node, store, tmpDir := createTestRaftNodeForLeadership(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	// Wait for leadership
	require.NoError(t, node.WaitForLeader(5*time.Second))

	// Create gRPC server
	logger, _ := zap.NewDevelopment()
	grpcServer := server.NewGRPCServer(server.GRPCServerConfig{
		Addr:     "localhost:19090",
		RaftNode: node,
		Logger:   logger,
	})

	require.NoError(t, grpcServer.Start())
	defer grpcServer.Stop()

	time.Sleep(300 * time.Millisecond) // Let server start

	// Create gRPC client
	conn, err := grpc.Dial("localhost:19090", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewKVStoreClient(conn)

	// Test GetLeadershipInfo
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.GetLeadershipInfo(ctx, &pb.LeadershipInfoRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)

	assert.True(t, resp.IsLeader)
	assert.Equal(t, "leader", resp.State)
	assert.NotEmpty(t, resp.NodeId)
	assert.Greater(t, resp.Term, uint64(0))
	assert.GreaterOrEqual(t, resp.CommitIndex, uint64(0))
}

// TestLeadership_GRPC_Stepdown tests the gRPC Stepdown method
func TestLeadership_GRPC_Stepdown(t *testing.T) {
	// Create test cluster
	node, store, tmpDir := createTestRaftNodeForLeadership(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	// Wait for leadership
	require.NoError(t, node.WaitForLeader(5*time.Second))

	// Create gRPC server
	logger, _ := zap.NewDevelopment()
	grpcServer := server.NewGRPCServer(server.GRPCServerConfig{
		Addr:     "localhost:19091",
		RaftNode: node,
		Logger:   logger,
	})

	require.NoError(t, grpcServer.Start())
	defer grpcServer.Stop()

	time.Sleep(300 * time.Millisecond) // Let server start

	// Create gRPC client
	conn, err := grpc.Dial("localhost:19091", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewKVStoreClient(conn)

	// Test Stepdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.Stepdown(ctx, &pb.StepdownRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	// In a single-node cluster, stepdown fails because there's no peer to transfer to
	// This is expected behavior
	if resp.Success {
		assert.Contains(t, resp.Message, "stepdown")
	} else {
		// Expect "cannot find peer" error in single-node cluster
		assert.Contains(t, resp.Error, "cannot find peer")
	}
}

// TestLeadership_GRPC_GetElectionHistory tests the gRPC GetElectionHistory method
func TestLeadership_GRPC_GetElectionHistory(t *testing.T) {
	// Create test cluster
	node, store, tmpDir := createTestRaftNodeForLeadership(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	defer node.Shutdown()

	// Wait for leadership
	require.NoError(t, node.WaitForLeader(5*time.Second))

	// Create gRPC server
	logger, _ := zap.NewDevelopment()
	grpcServer := server.NewGRPCServer(server.GRPCServerConfig{
		Addr:     "localhost:19092",
		RaftNode: node,
		Logger:   logger,
	})

	require.NoError(t, grpcServer.Start())
	defer grpcServer.Stop()

	time.Sleep(300 * time.Millisecond) // Let server start

	// Create gRPC client
	conn, err := grpc.Dial("localhost:19092", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewKVStoreClient(conn)

	// Test GetElectionHistory
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.GetElectionHistory(ctx, &pb.ElectionHistoryRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Elections)
	assert.GreaterOrEqual(t, resp.Count, int32(0))
}

// Helper function to create a test Raft node
func createTestRaftNodeForLeadership(t *testing.T) (*consensus.RaftNode, *storage.DurableStore, string) {
	tmpDir, err := os.MkdirTemp("", "raft-leadership-test-*")
	require.NoError(t, err)

	store, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       filepath.Join(tmpDir, "store"),
		SyncOnWrite:   false,
		SnapshotEvery: 0,
	})
	require.NoError(t, err)

	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	raftAddr := fmt.Sprintf("localhost:%d", 17000+(time.Now().UnixNano()%1000))

	node, err := consensus.NewRaftNode(consensus.RaftConfig{
		NodeID:    "test-node",
		RaftAddr:  raftAddr,
		RaftDir:   filepath.Join(tmpDir, "raft"),
		Bootstrap: true,
		Store:     store,
		Logger:    logger,
	})
	require.NoError(t, err)

	return node, store, tmpDir
}
