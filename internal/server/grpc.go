package server

import (
	"context"
	"fmt"
	"net"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	pb "github.com/RashikAnsar/raftkv/api/proto"
	"github.com/RashikAnsar/raftkv/internal/consensus"
	"github.com/RashikAnsar/raftkv/internal/observability"
	"github.com/RashikAnsar/raftkv/internal/security"
	"github.com/RashikAnsar/raftkv/internal/storage"
	"github.com/RashikAnsar/raftkv/internal/watch"
)

// GRPCServer implements the KVStore gRPC service
type GRPCServer struct {
	pb.UnimplementedKVStoreServer
	raftNode     *consensus.RaftNode
	logger       *zap.Logger
	metrics      *observability.Metrics
	server       *grpc.Server
	addr         string
	watchManager *watch.WatchManager
}

// GRPCServerConfig holds configuration for the gRPC server
type GRPCServerConfig struct {
	Addr     string
	RaftNode *consensus.RaftNode
	Logger   *zap.Logger
	Metrics  *observability.Metrics

	// TLS configuration
	TLSConfig *security.TLSConfig // Optional TLS config (nil = insecure, non-nil = TLS)
}

// NewGRPCServer creates a new gRPC server
func NewGRPCServer(config GRPCServerConfig) *GRPCServer {
	s := &GRPCServer{
		raftNode: config.RaftNode,
		logger:   config.Logger,
		metrics:  config.Metrics,
		addr:     config.Addr,
	}

	// Prepare gRPC server options
	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(s.loggingInterceptor),
	}

	// Add TLS credentials if configured
	if config.TLSConfig != nil {
		if err := security.ValidateTLSConfig(config.TLSConfig); err != nil {
			config.Logger.Error("Invalid TLS configuration", zap.Error(err))
		} else {
			tlsConfig, err := security.LoadServerTLSConfig(config.TLSConfig)
			if err != nil {
				config.Logger.Error("Failed to load TLS configuration", zap.Error(err))
			} else {
				creds := credentials.NewTLS(tlsConfig)
				opts = append(opts, grpc.Creds(creds))
				config.Logger.Info("TLS enabled for gRPC server",
					zap.Bool("mtls", config.TLSConfig.EnableMTLS),
					zap.String("cert", config.TLSConfig.CertFile),
				)
			}
		}
	}

	// Create gRPC server with options
	s.server = grpc.NewServer(opts...)

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
		leaderAddr, _ := s.raftNode.GetLeader()
		return nil, status.Errorf(codes.FailedPrecondition,
			"consistent read requires leader (current leader: %s)", leaderAddr)
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
		leaderAddr, _ := s.raftNode.GetLeader()
		return &pb.PutResponse{
			Success: false,
			Error:   "not the leader",
			Leader:  leaderAddr,
		}, status.Errorf(codes.FailedPrecondition, "not the leader, redirect to: %s", leaderAddr)
	}

	// Create command and apply to Raft
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   req.Key,
		Value: req.Value,
	}

	// Handle TTL if provided
	if req.TtlSeconds > 0 {
		cmd.TTL = time.Duration(req.TtlSeconds) * time.Second
	}

	if err := s.raftNode.Apply(cmd, 5*time.Second); err != nil {
		s.logger.Error("Failed to apply PUT command",
			zap.String("key", req.Key),
			zap.Error(err),
		)
		leaderAddr, _ := s.raftNode.GetLeader()
		return &pb.PutResponse{
			Success: false,
			Error:   err.Error(),
			Leader:  leaderAddr,
		}, status.Error(codes.Internal, "failed to apply command")
	}

	leaderAddr, _ := s.raftNode.GetLeader()
	return &pb.PutResponse{
		Success: true,
		Leader:  leaderAddr,
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
		leaderAddr, _ := s.raftNode.GetLeader()
		return &pb.DeleteResponse{
			Success: false,
			Error:   "not the leader",
			Leader:  leaderAddr,
		}, status.Errorf(codes.FailedPrecondition, "not the leader, redirect to: %s", leaderAddr)
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
		leaderAddr, _ := s.raftNode.GetLeader()
		return &pb.DeleteResponse{
			Success: false,
			Error:   err.Error(),
			Leader:  leaderAddr,
		}, status.Error(codes.Internal, "failed to apply command")
	}

	leaderAddr, _ := s.raftNode.GetLeader()
	return &pb.DeleteResponse{
		Success: true,
		Leader:  leaderAddr,
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

	// Use the enhanced ListWithOptions API
	limit := int(req.Limit)
	if limit == 0 {
		limit = 100 // Default limit
	}

	opts := storage.ListOptions{
		Prefix:  req.Prefix,
		Start:   req.Start,
		End:     req.End,
		Limit:   limit,
		Cursor:  req.Cursor,
		Reverse: req.Reverse,
	}

	result, err := s.raftNode.ListWithOptions(ctx, opts)
	if err != nil {
		s.logger.Error("Failed to list keys",
			zap.String("prefix", req.Prefix),
			zap.Error(err),
		)
		return nil, status.Error(codes.Internal, "failed to list keys")
	}

	return &pb.ListResponse{
		Keys:       result.Keys,
		Total:      int32(len(result.Keys)), // Deprecated, use Count
		Count:      int32(len(result.Keys)),
		HasMore:    result.HasMore,
		NextCursor: result.NextCursor,
	}, nil
}

// CompareAndSwap performs an atomic compare-and-swap operation
func (s *GRPCServer) CompareAndSwap(ctx context.Context, req *pb.CompareAndSwapRequest) (*pb.CompareAndSwapResponse, error) {
	// Check if we're the leader
	if !s.raftNode.IsLeader() {
		leaderAddr, _ := s.raftNode.GetLeader()
		return &pb.CompareAndSwapResponse{
			Success: false,
			Error:   fmt.Sprintf("not leader, redirect to %s", leaderAddr),
			Leader:  leaderAddr,
		}, status.Error(codes.FailedPrecondition, "not leader")
	}

	// Create CAS command
	cmd := consensus.Command{
		Op:              consensus.OpTypeCAS,
		Key:             req.Key,
		Value:           req.NewValue,
		ExpectedVersion: req.ExpectedVersion,
	}

	// Apply through Raft
	if err := s.raftNode.Apply(cmd, 5*time.Second); err != nil {
		s.logger.Error("Failed to apply CAS command",
			zap.String("key", req.Key),
			zap.Uint64("expected_version", req.ExpectedVersion),
			zap.Error(err),
		)
		return &pb.CompareAndSwapResponse{
			Success: false,
			Error:   "failed to replicate command",
		}, status.Error(codes.Internal, "failed to replicate command")
	}

	// Read back the current version to confirm
	_, currentVersion, err := s.raftNode.GetWithVersion(ctx, req.Key)
	if err != nil {
		// CAS was applied but we can't read it back - still return success
		// since the operation was committed to Raft
		return &pb.CompareAndSwapResponse{
			Success:        true,
			CurrentVersion: req.ExpectedVersion + 1, // Assume version was incremented
		}, nil
	}

	leaderAddr, _ := s.raftNode.GetLeader()
	return &pb.CompareAndSwapResponse{
		Success:        true,
		CurrentVersion: currentVersion,
		Leader:         leaderAddr,
	}, nil
}

// GetWithVersion retrieves the value and version for a key
func (s *GRPCServer) GetWithVersion(ctx context.Context, req *pb.GetWithVersionRequest) (*pb.GetWithVersionResponse, error) {
	value, version, err := s.raftNode.GetWithVersion(ctx, req.Key)
	if err != nil {
		s.logger.Debug("Key not found in GetWithVersion",
			zap.String("key", req.Key),
		)
		return &pb.GetWithVersionResponse{
			Found: false,
		}, nil
	}

	return &pb.GetWithVersionResponse{
		Value:   value,
		Version: version,
		Found:   true,
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

	leaderAddr, _ := s.raftNode.GetLeader()
	return &pb.StatsResponse{
		KeyCount:      uint64(stats.KeyCount),
		GetCount:      uint64(stats.Gets),
		PutCount:      uint64(stats.Puts),
		DeleteCount:   uint64(stats.Deletes),
		RaftState:     s.raftNode.GetState(),
		RaftLeader:    leaderAddr,
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

// GetLeadershipInfo returns detailed leadership information
func (s *GRPCServer) GetLeadershipInfo(ctx context.Context, req *pb.LeadershipInfoRequest) (*pb.LeadershipInfoResponse, error) {
	info, err := s.raftNode.GetLeadershipInfo()
	if err != nil {
		s.logger.Error("Failed to get leadership info", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to get leadership information")
	}

	// Convert time.Duration to milliseconds
	lastContactMs := info.LastContactAge.Milliseconds()
	leaderStabilityMs := info.LeaderStability.Milliseconds()

	return &pb.LeadershipInfoResponse{
		NodeId:            info.NodeID,
		IsLeader:          info.IsLeader,
		State:             info.State,
		LeaderId:          info.LeaderID,
		LeaderAddress:     info.LeaderAddress,
		Term:              info.Term,
		LastContactMs:     lastContactMs,
		CommitIndex:       info.CommitIndex,
		AppliedIndex:      info.AppliedIndex,
		LastLogIndex:      info.LastLogIndex,
		LastLogTerm:       info.LastLogTerm,
		NumPeers:          int32(info.NumPeers),
		LeadershipChanges: int32(info.LeadershipChanges),
		LeaderStabilityMs: leaderStabilityMs,
	}, nil
}

// Stepdown forces the leader to step down
func (s *GRPCServer) Stepdown(ctx context.Context, req *pb.StepdownRequest) (*pb.StepdownResponse, error) {
	// Check if this node is the leader
	if !s.raftNode.IsLeader() {
		return &pb.StepdownResponse{
			Success: false,
			Error:   "node is not the leader",
			Message: "stepdown can only be called on the leader",
		}, nil
	}

	if err := s.raftNode.Stepdown(); err != nil {
		s.logger.Error("Failed to stepdown", zap.Error(err))
		return &pb.StepdownResponse{
			Success: false,
			Error:   err.Error(),
			Message: "stepdown failed",
		}, nil
	}

	return &pb.StepdownResponse{
		Success: true,
		Message: "leadership stepdown initiated",
	}, nil
}

// TransferLeadership transfers leadership to a specific node
func (s *GRPCServer) TransferLeadership(ctx context.Context, req *pb.TransferLeadershipRequest) (*pb.TransferLeadershipResponse, error) {
	if req.TargetNodeId == "" {
		return &pb.TransferLeadershipResponse{
			Success: false,
			Error:   "target_node_id is required",
		}, nil
	}

	// Check if this node is the leader
	if !s.raftNode.IsLeader() {
		return &pb.TransferLeadershipResponse{
			Success:      false,
			Error:        "node is not the leader",
			TargetNodeId: req.TargetNodeId,
		}, nil
	}

	if err := s.raftNode.TransferLeadership(req.TargetNodeId); err != nil {
		s.logger.Error("Failed to transfer leadership",
			zap.String("target", req.TargetNodeId),
			zap.Error(err),
		)
		return &pb.TransferLeadershipResponse{
			Success:      false,
			Error:        err.Error(),
			Message:      "leadership transfer failed",
			TargetNodeId: req.TargetNodeId,
		}, nil
	}

	return &pb.TransferLeadershipResponse{
		Success:      true,
		Message:      "leadership transfer initiated",
		TargetNodeId: req.TargetNodeId,
	}, nil
}

// GetElectionHistory returns the history of leadership changes
func (s *GRPCServer) GetElectionHistory(ctx context.Context, req *pb.ElectionHistoryRequest) (*pb.ElectionHistoryResponse, error) {
	history := s.raftNode.GetElectionHistory()

	// Convert to protobuf format
	pbEvents := make([]*pb.ElectionEvent, 0, len(history))
	for _, event := range history {
		pbEvents = append(pbEvents, &pb.ElectionEvent{
			TimestampMs:  event.Timestamp.UnixMilli(),
			OldLeaderId:  event.OldLeaderID,
			NewLeaderId:  event.NewLeaderID,
			Term:         event.Term,
			Reason:       event.Reason,
		})
	}

	return &pb.ElectionHistoryResponse{
		Elections: pbEvents,
		Count:     int32(len(pbEvents)),
	}, nil
}

// GetTTL returns the remaining TTL for a key
func (s *GRPCServer) GetTTL(ctx context.Context, req *pb.GetTTLRequest) (*pb.GetTTLResponse, error) {
	// Validate request
	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key cannot be empty")
	}

	// Get TTL from store
	ttl, err := s.raftNode.GetTTL(ctx, req.Key)
	if err != nil {
		if err.Error() == "key not found" || err.Error() == storage.ErrKeyNotFound.Error() {
			return &pb.GetTTLResponse{
				TtlSeconds: -1,
				HasTtl:     false,
				Error:      "key not found",
			}, nil
		}
		s.logger.Error("Failed to get TTL",
			zap.String("key", req.Key),
			zap.Error(err),
		)
		return &pb.GetTTLResponse{
			TtlSeconds: -1,
			HasTtl:     false,
			Error:      err.Error(),
		}, status.Error(codes.Internal, "failed to get TTL")
	}

	// If TTL is 0, it means no expiration
	if ttl == 0 {
		return &pb.GetTTLResponse{
			TtlSeconds: 0,
			HasTtl:     false,
		}, nil
	}

	return &pb.GetTTLResponse{
		TtlSeconds: int64(ttl.Seconds()),
		HasTtl:     true,
	}, nil
}

// SetTTL updates the TTL for an existing key
func (s *GRPCServer) SetTTL(ctx context.Context, req *pb.SetTTLRequest) (*pb.SetTTLResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			duration := time.Since(start).Seconds()
			s.metrics.RecordStorageOperation("set_ttl", "success", duration)
		}
	}()

	// Validate request
	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key cannot be empty")
	}

	if req.TtlSeconds <= 0 {
		return nil, status.Error(codes.InvalidArgument, "ttl_seconds must be positive")
	}

	// Check if we're the leader
	if !s.raftNode.IsLeader() {
		leaderAddr, _ := s.raftNode.GetLeader()
		return &pb.SetTTLResponse{
			Success: false,
			Error:   "not the leader",
			Leader:  leaderAddr,
		}, status.Errorf(codes.FailedPrecondition, "not the leader, redirect to: %s", leaderAddr)
	}

	// SetTTL through store (this doesn't go through Raft currently)
	// Note: For production, you might want to replicate SetTTL operations through Raft
	ttl := time.Duration(req.TtlSeconds) * time.Second
	if err := s.raftNode.SetTTL(ctx, req.Key, ttl); err != nil {
		if err.Error() == "key not found" || err.Error() == storage.ErrKeyNotFound.Error() {
			return &pb.SetTTLResponse{
				Success: false,
				Error:   "key not found",
			}, status.Error(codes.NotFound, "key not found")
		}

		s.logger.Error("Failed to set TTL",
			zap.String("key", req.Key),
			zap.Int64("ttl_seconds", req.TtlSeconds),
			zap.Error(err),
		)
		leaderAddr, _ := s.raftNode.GetLeader()
		return &pb.SetTTLResponse{
			Success: false,
			Error:   err.Error(),
			Leader:  leaderAddr,
		}, status.Error(codes.Internal, "failed to set TTL")
	}

	leaderAddr, _ := s.raftNode.GetLeader()
	return &pb.SetTTLResponse{
		Success: true,
		Leader:  leaderAddr,
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

// SetWatchManager sets the watch manager for the gRPC server
func (s *GRPCServer) SetWatchManager(wm *watch.WatchManager) {
	s.watchManager = wm
}
