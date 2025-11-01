package server

import (
	"context"
	"fmt"
	"net"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/RashikAnsar/raftkv/api/proto"
	"github.com/RashikAnsar/raftkv/internal/consensus"
	"github.com/RashikAnsar/raftkv/internal/observability"
)

// GRPCServer implements the KVStore gRPC service
type GRPCServer struct {
	pb.UnimplementedKVStoreServer
	raftNode *consensus.RaftNode
	logger   *zap.Logger
	metrics  *observability.Metrics
	server   *grpc.Server
	addr     string
}

// GRPCServerConfig holds configuration for the gRPC server
type GRPCServerConfig struct {
	Addr     string
	RaftNode *consensus.RaftNode
	Logger   *zap.Logger
	Metrics  *observability.Metrics
}

// NewGRPCServer creates a new gRPC server
func NewGRPCServer(config GRPCServerConfig) *GRPCServer {
	s := &GRPCServer{
		raftNode: config.RaftNode,
		logger:   config.Logger,
		metrics:  config.Metrics,
		addr:     config.Addr,
	}

	// Create gRPC server with interceptors
	s.server = grpc.NewServer(
		grpc.UnaryInterceptor(s.loggingInterceptor),
	)

	// Register service
	pb.RegisterKVStoreServer(s.server, s)

	return s
}

// Start starts the gRPC server
func (s *GRPCServer) Start() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.logger.Info("Starting gRPC server", zap.String("addr", s.addr))

	go func() {
		if err := s.server.Serve(listener); err != nil {
			s.logger.Error("gRPC server error", zap.Error(err))
		}
	}()

	return nil
}

// Stop stops the gRPC server gracefully
func (s *GRPCServer) Stop() {
	s.logger.Info("Stopping gRPC server")
	s.server.GracefulStop()
}

// Get retrieves a value for a given key
func (s *GRPCServer) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			duration := time.Since(start).Seconds()
			s.metrics.RecordStorageOperation("get", "success", duration)
		}
	}()

	// Validate request
	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key cannot be empty")
	}

	// If consistent read is required, check if we're the leader
	if req.Consistent && !s.raftNode.IsLeader() {
		leader := s.raftNode.GetLeader()
		return nil, status.Errorf(codes.FailedPrecondition,
			"consistent read requires leader (current leader: %s)", leader)
	}

	// Perform read
	value, err := s.raftNode.Get(ctx, req.Key)
	if err != nil {
		if err == context.Canceled {
			return nil, status.Error(codes.Canceled, "request canceled")
		}
		if err == context.DeadlineExceeded {
			return nil, status.Error(codes.DeadlineExceeded, "request timeout")
		}

		// Key not found is not an error in our storage layer, check if value is nil
		if value == nil {
			return &pb.GetResponse{
				Value:      nil,
				Found:      false,
				FromLeader: s.raftNode.IsLeader(),
			}, nil
		}

		s.logger.Error("Failed to get key", zap.String("key", req.Key), zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to get key")
	}

	return &pb.GetResponse{
		Value:      value,
		Found:      value != nil,
		FromLeader: s.raftNode.IsLeader(),
	}, nil
}

// Put stores a key-value pair
func (s *GRPCServer) Put(ctx context.Context, req *pb.PutRequest) (*pb.PutResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			duration := time.Since(start).Seconds()
			s.metrics.RecordStorageOperation("put", "success", duration)
		}
	}()

	// Validate request
	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key cannot be empty")
	}

	// Check if we're the leader
	if !s.raftNode.IsLeader() {
		leader := s.raftNode.GetLeader()
		return &pb.PutResponse{
			Success: false,
			Error:   "not the leader",
			Leader:  leader,
		}, status.Errorf(codes.FailedPrecondition, "not the leader, redirect to: %s", leader)
	}

	// Create command and apply to Raft
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   req.Key,
		Value: req.Value,
	}

	if err := s.raftNode.Apply(cmd, 5*time.Second); err != nil {
		s.logger.Error("Failed to apply PUT command",
			zap.String("key", req.Key),
			zap.Error(err),
		)
		return &pb.PutResponse{
			Success: false,
			Error:   err.Error(),
			Leader:  s.raftNode.GetLeader(),
		}, status.Error(codes.Internal, "failed to apply command")
	}

	return &pb.PutResponse{
		Success: true,
		Leader:  s.raftNode.GetLeader(),
	}, nil
}

// Delete removes a key-value pair
func (s *GRPCServer) Delete(ctx context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			duration := time.Since(start).Seconds()
			s.metrics.RecordStorageOperation("delete", "success", duration)
		}
	}()

	// Validate request
	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key cannot be empty")
	}

	// Check if we're the leader
	if !s.raftNode.IsLeader() {
		leader := s.raftNode.GetLeader()
		return &pb.DeleteResponse{
			Success: false,
			Error:   "not the leader",
			Leader:  leader,
		}, status.Errorf(codes.FailedPrecondition, "not the leader, redirect to: %s", leader)
	}

	// Create command and apply to Raft
	cmd := consensus.Command{
		Op:  consensus.OpTypeDelete,
		Key: req.Key,
	}

	if err := s.raftNode.Apply(cmd, 5*time.Second); err != nil {
		s.logger.Error("Failed to apply DELETE command",
			zap.String("key", req.Key),
			zap.Error(err),
		)
		return &pb.DeleteResponse{
			Success: false,
			Error:   err.Error(),
			Leader:  s.raftNode.GetLeader(),
		}, status.Error(codes.Internal, "failed to apply command")
	}

	return &pb.DeleteResponse{
		Success: true,
		Leader:  s.raftNode.GetLeader(),
	}, nil
}

// List returns keys matching a prefix
func (s *GRPCServer) List(ctx context.Context, req *pb.ListRequest) (*pb.ListResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			duration := time.Since(start).Seconds()
			s.metrics.RecordStorageOperation("list", "success", duration)
		}
	}()

	// Perform list operation
	limit := int(req.Limit)
	keys, err := s.raftNode.List(ctx, req.Prefix, limit)
	if err != nil {
		s.logger.Error("Failed to list keys",
			zap.String("prefix", req.Prefix),
			zap.Error(err),
		)
		return nil, status.Error(codes.Internal, "failed to list keys")
	}

	return &pb.ListResponse{
		Keys:  keys,
		Total: int32(len(keys)),
	}, nil
}

// GetStats returns store statistics
func (s *GRPCServer) GetStats(ctx context.Context, req *pb.StatsRequest) (*pb.StatsResponse, error) {
	stats := s.raftNode.Stats()
	raftStats := s.raftNode.RaftStats()

	// Parse Raft-specific stats
	term := uint64(0)
	lastIndex := uint64(0)
	if val, ok := raftStats["term"]; ok {
		fmt.Sscanf(val, "%d", &term)
	}
	if val, ok := raftStats["last_log_index"]; ok {
		fmt.Sscanf(val, "%d", &lastIndex)
	}

	return &pb.StatsResponse{
		KeyCount:      uint64(stats.KeyCount),
		GetCount:      uint64(stats.Gets),
		PutCount:      uint64(stats.Puts),
		DeleteCount:   uint64(stats.Deletes),
		RaftState:     s.raftNode.GetState(),
		RaftLeader:    s.raftNode.GetLeader(),
		RaftTerm:      term,
		RaftLastIndex: lastIndex,
	}, nil
}

// GetLeader returns the current Raft leader
func (s *GRPCServer) GetLeader(ctx context.Context, req *pb.LeaderRequest) (*pb.LeaderResponse, error) {
	isLeader := s.raftNode.IsLeader()

	// Get node ID from Raft stats
	raftStats := s.raftNode.RaftStats()
	nodeID := raftStats["node_id"]

	// Return gRPC address instead of Raft address
	// If current instance is the leader, return's own gRPC address
	// If current instance is the follower, then instance don't know the leader's gRPC address,
	// so return empty string (client will try all known servers)
	var grpcLeaderAddr string
	if isLeader {
		grpcLeaderAddr = s.addr
	} else {
		grpcLeaderAddr = ""
	}

	return &pb.LeaderResponse{
		LeaderId:      nodeID,
		LeaderAddress: grpcLeaderAddr, // Return gRPC address, not Raft address
		IsLeader:      isLeader,
	}, nil
}

// loggingInterceptor logs all gRPC requests
func (s *GRPCServer) loggingInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	start := time.Now()

	// Call handler
	resp, err := handler(ctx, req)

	// Log request completion
	duration := time.Since(start)
	statusCode := codes.OK
	if err != nil {
		statusCode = status.Code(err)
	}

	// Use Debug level for successful requests (less verbose than HTTP)
	// Use Info/Error for failures to match HTTP behavior
	if statusCode == codes.OK {
		s.logger.Debug("gRPC request",
			zap.String("method", info.FullMethod),
			zap.Duration("duration", duration),
			zap.String("status", statusCode.String()),
		)
	} else {
		s.logger.Info("gRPC request failed",
			zap.String("method", info.FullMethod),
			zap.Duration("duration", duration),
			zap.String("status", statusCode.String()),
			zap.Error(err),
		)
	}

	return resp, err
}
