package consensus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/raft"
	"go.uber.org/zap"

	"github.com/RashikAnsar/raftkv/internal/security"
	"github.com/RashikAnsar/raftkv/internal/storage"
)

type RaftNode struct {
	raft      *raft.Raft
	fsm       *FSM
	transport *raft.NetworkTransport
	config    *raft.Config
	logger    *zap.Logger
	NodeID    string // Node ID (exported for testing)
	RaftAddr  string // Raft address (exported for testing)
}

type RaftConfig struct {
	// Node configuration
	NodeID   string // Unique node identifier
	RaftAddr string // Raft bind address (e.g., "localhost:7000")
	RaftDir  string // Directory for Raft data

	// Cluster configuration
	Bootstrap bool   // True if this is the first node
	JoinAddr  string // Address of existing node to join (if not bootstrap)

	// Store - changed to concrete type for unified architecture
	Store *storage.DurableStore

	// Security configuration
	TLSConfig *security.TLSConfig // Optional TLS configuration for inter-node communication

	// Logger
	Logger *zap.Logger
}

// NewRaftNode creates a new Raft node
func NewRaftNode(config RaftConfig) (*RaftNode, error) {
	// Create Raft directory
	if err := os.MkdirAll(config.RaftDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create raft directory: %w", err)
	}

	// Create FSM
	fsm := NewFSM(config.Store, config.Logger)

	// Setup Raft configuration
	raftConfig := raft.DefaultConfig()
	raftConfig.LocalID = raft.ServerID(config.NodeID)
	raftConfig.Logger = newHCLogger(config.Logger)

	// Tuning for faster elections (good for demos, adjust for production)
	raftConfig.HeartbeatTimeout = 1000 * time.Millisecond
	raftConfig.ElectionTimeout = 1000 * time.Millisecond
	raftConfig.LeaderLeaseTimeout = 500 * time.Millisecond
	raftConfig.CommitTimeout = 50 * time.Millisecond

	// Setup Raft transport (TCP or TLS)
	addr, err := net.ResolveTCPAddr("tcp", config.RaftAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve raft address: %w", err)
	}

	var transport *raft.NetworkTransport
	if config.TLSConfig != nil {
		// Create TLS transport for secure inter-node communication
		config.Logger.Info("Setting up TLS transport for Raft",
			zap.String("addr", config.RaftAddr),
			zap.String("cert", config.TLSConfig.CertFile),
		)

		// Load TLS config for Raft (handles both server and client roles)
		tlsConfig, err := security.LoadRaftTLSConfig(config.TLSConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS config for Raft: %w", err)
		}

		// Create TLS stream layer
		streamLayer, err := security.NewTLSStreamLayer(config.RaftAddr, addr, tlsConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS stream layer: %w", err)
		}

		// Create transport with TLS stream layer
		transportConfig := &raft.NetworkTransportConfig{
			Stream:          streamLayer,
			MaxPool:         3,
			Timeout:         10 * time.Second,
			Logger:          newHCLogger(config.Logger),
		}

		transport = raft.NewNetworkTransportWithConfig(transportConfig)

		config.Logger.Info("TLS transport initialized for Raft (mutual TLS enabled)")
	} else {
		// Create standard TCP transport (unencrypted)
		config.Logger.Info("Setting up TCP transport for Raft (unencrypted)", zap.String("addr", config.RaftAddr))
		transport, err = raft.NewTCPTransport(config.RaftAddr, addr, 3, 10*time.Second, os.Stderr)
		if err != nil {
			return nil, fmt.Errorf("failed to create transport: %w", err)
		}
	}

	// Create custom Raft log store (WAL-based)
	logStore, err := storage.NewRaftLogStore(filepath.Join(config.RaftDir, "log"))
	if err != nil {
		return nil, fmt.Errorf("failed to create log store: %w", err)
	}

	// Create custom Raft stable store (JSON-based)
	stableStore, err := storage.NewRaftStableStore(config.RaftDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create stable store: %w", err)
	}

	// Create snapshot store
	snapshotStore, err := raft.NewFileSnapshotStore(config.RaftDir, 2, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot store: %w", err)
	}

	// Create Raft instance with custom stores
	ra, err := raft.NewRaft(raftConfig, fsm, logStore, stableStore, snapshotStore, transport)
	if err != nil {
		return nil, fmt.Errorf("failed to create raft: %w", err)
	}

	node := &RaftNode{
		raft:      ra,
		fsm:       fsm,
		transport: transport,
		config:    raftConfig,
		logger:    config.Logger,
		NodeID:    config.NodeID,
		RaftAddr:  config.RaftAddr,
	}

	// Bootstrap cluster if this is the first node
	if config.Bootstrap {
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      raft.ServerID(config.NodeID),
					Address: transport.LocalAddr(),
				},
			},
		}

		future := ra.BootstrapCluster(configuration)
		if err := future.Error(); err != nil {
			// It's okay if already bootstrapped
			if err != raft.ErrCantBootstrap {
				return nil, fmt.Errorf("failed to bootstrap cluster: %w", err)
			}
		}

		config.Logger.Info("Cluster bootstrapped", zap.String("node_id", config.NodeID))
	} else if config.JoinAddr != "" {
		// Join existing cluster
		config.Logger.Info("Attempting to join cluster",
			zap.String("node_id", config.NodeID),
			zap.String("join_addr", config.JoinAddr),
		)

		// Send join request to existing node
		joinReq := map[string]string{
			"node_id": config.NodeID,
			"addr":    config.RaftAddr,
		}
		reqBody, err := json.Marshal(joinReq)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal join request: %w", err)
		}

		// Create HTTP client with TLS support if configured
		httpClient := &http.Client{
			Timeout: 10 * time.Second,
			// Allow automatic redirect following for leader redirects
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Allow up to 3 redirects (sufficient for leader redirects)
				if len(via) >= 3 {
					return fmt.Errorf("too many redirects")
				}
				// Copy the Content-Type header for POST redirects
				if req.Method == "POST" {
					req.Header.Set("Content-Type", "application/json")
				}
				return nil
			},
		}
		if config.TLSConfig != nil {
			tlsConfig, err := security.LoadClientTLSConfig(config.TLSConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to load TLS config for join request: %w", err)
			}
			httpClient.Transport = &http.Transport{
				TLSClientConfig: tlsConfig,
			}
		}

		req, err := http.NewRequest("POST", config.JoinAddr+"/cluster/join", bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("failed to create join request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to send join request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("join request failed with status %d: %s", resp.StatusCode, string(body))
		}

		config.Logger.Info("Successfully joined cluster",
			zap.String("node_id", config.NodeID),
			zap.String("join_addr", config.JoinAddr),
		)
	}

	return node, nil
}

// Apply applies a command to the Raft cluster
func (r *RaftNode) Apply(cmd Command, timeout time.Duration) error {
	// Serialize command
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	// Apply to Raft
	future := r.raft.Apply(data, timeout)
	if err := future.Error(); err != nil {
		return fmt.Errorf("raft apply failed: %w", err)
	}

	// Check for errors in the response
	if resp := future.Response(); resp != nil {
		if err, ok := resp.(error); ok {
			return err
		}
	}

	return nil
}

// Get retrieves a value (reads are local, no Raft involved)
func (r *RaftNode) Get(ctx context.Context, key string) ([]byte, error) {
	// Direct read from local store (may be stale on followers)
	return r.fsm.store.Get(ctx, key)
}

// GetWithVersion retrieves both the value and version for a key
func (r *RaftNode) GetWithVersion(ctx context.Context, key string) ([]byte, uint64, error) {
	// Direct read from local store (may be stale on followers)
	return r.fsm.store.GetWithVersion(ctx, key)
}

// List lists keys
func (r *RaftNode) List(ctx context.Context, prefix string, limit int) ([]string, error) {
	return r.fsm.store.List(ctx, prefix, limit)
}

// Stats returns store statistics
func (r *RaftNode) Stats() storage.Stats {
	return r.fsm.store.Stats()
}

// IsLeader returns true if this node is the leader
func (r *RaftNode) IsLeader() bool {
	return r.raft.State() == raft.Leader
}

// GetLeader returns the current leader address and ID
func (r *RaftNode) GetLeader() (string, string) {
	addr, id := r.raft.LeaderWithID()
	return string(addr), string(id)
}

// GetState returns the current Raft state
func (r *RaftNode) GetState() string {
	switch r.raft.State() {
	case raft.Leader:
		return "leader"
	case raft.Candidate:
		return "candidate"
	case raft.Follower:
		return "follower"
	case raft.Shutdown:
		return "shutdown"
	default:
		return "unknown"
	}
}

// Join adds a new node to the cluster (alias for AddVoter with default timeout)
func (r *RaftNode) Join(nodeID, addr string) error {
	return r.AddVoter(nodeID, addr, 10*time.Second)
}

// AddVoter adds a new voting member to the cluster
func (r *RaftNode) AddVoter(nodeID, addr string, timeout time.Duration) error {
	r.logger.Info("Adding voter to cluster",
		zap.String("node_id", nodeID),
		zap.String("addr", addr),
	)

	future := r.raft.AddVoter(raft.ServerID(nodeID), raft.ServerAddress(addr), 0, timeout)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to add voter: %w", err)
	}

	r.logger.Info("Voter added successfully", zap.String("node_id", nodeID))
	return nil
}

// RemoveServer removes a node from the cluster (with default timeout)
func (r *RaftNode) RemoveServer(nodeID string) error {
	return r.RemoveServerWithTimeout(nodeID, 10*time.Second)
}

// RemoveServerWithTimeout removes a node from the cluster with custom timeout
func (r *RaftNode) RemoveServerWithTimeout(nodeID string, timeout time.Duration) error {
	r.logger.Info("Removing server from cluster", zap.String("node_id", nodeID))

	future := r.raft.RemoveServer(raft.ServerID(nodeID), 0, timeout)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to remove server: %w", err)
	}

	r.logger.Info("Server removed successfully", zap.String("node_id", nodeID))
	return nil
}

// GetServers returns the list of servers in the cluster
func (r *RaftNode) GetServers() ([]raft.Server, error) {
	future := r.raft.GetConfiguration()
	if err := future.Error(); err != nil {
		return nil, err
	}

	return future.Configuration().Servers, nil
}

// WaitForLeader waits for a leader to be elected
func (r *RaftNode) WaitForLeader(timeout time.Duration) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ticker.C:
			leaderAddr, _ := r.GetLeader()
			if leaderAddr != "" {
				r.logger.Info("Leader elected", zap.String("leader", leaderAddr))
				return nil
			}
		case <-timer.C:
			return fmt.Errorf("timeout waiting for leader")
		}
	}
}

// Snapshot triggers a manual snapshot
func (r *RaftNode) Snapshot() error {
	future := r.raft.Snapshot()
	return future.Error()
}

// Shutdown gracefully shuts down the Raft node
func (r *RaftNode) Shutdown() error {
	r.logger.Info("Shutting down Raft node")

	future := r.raft.Shutdown()
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to shutdown raft: %w", err)
	}

	if err := r.transport.Close(); err != nil {
		return fmt.Errorf("failed to close transport: %w", err)
	}

	return nil
}

// RaftStats returns Raft-specific statistics
func (r *RaftNode) RaftStats() map[string]string {
	return r.raft.Stats()
}

// hcLogger adapts zap.Logger to hashicorp/go-hclog.Logger
type hcLogger struct {
	logger *zap.Logger
}

func newHCLogger(logger *zap.Logger) *hcLogger {
	return &hcLogger{logger: logger}
}

// Implement hashicorp/go-hclog.Logger interface
func (l *hcLogger) Log(level hclog.Level, msg string, args ...interface{}) {
	fields := []zap.Field{}
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			key := fmt.Sprintf("%v", args[i])
			val := args[i+1]
			fields = append(fields, zap.Any(key, val))
		}
	}

	switch level {
	case hclog.Trace, hclog.Debug:
		l.logger.Debug(msg, fields...)
	case hclog.Info:
		l.logger.Info(msg, fields...)
	case hclog.Warn:
		l.logger.Warn(msg, fields...)
	case hclog.Error:
		l.logger.Error(msg, fields...)
	default:
		l.logger.Info(msg, fields...)
	}
}

// Simplified methods to satisfy interface
func (l *hcLogger) Trace(msg string, args ...interface{}) {
	l.Log(hclog.Trace, msg, args...)
}
func (l *hcLogger) Debug(msg string, args ...interface{}) {
	l.Log(hclog.Debug, msg, args...)
}
func (l *hcLogger) Info(msg string, args ...interface{}) {
	l.Log(hclog.Info, msg, args...)
}
func (l *hcLogger) Warn(msg string, args ...interface{}) {
	l.Log(hclog.Warn, msg, args...)
}
func (l *hcLogger) Error(msg string, args ...interface{}) {
	l.Log(hclog.Error, msg, args...)
}

// Additional methods to satisfy interface (stubs)
func (l *hcLogger) IsTrace() bool                                                { return false }
func (l *hcLogger) IsDebug() bool                                                { return false }
func (l *hcLogger) IsInfo() bool                                                 { return true }
func (l *hcLogger) IsWarn() bool                                                 { return true }
func (l *hcLogger) IsError() bool                                                { return true }
func (l *hcLogger) Name() string                                                 { return "raft" }
func (l *hcLogger) GetLevel() hclog.Level                                        { return hclog.Info }
func (l *hcLogger) With(args ...interface{}) hclog.Logger                        { return l }
func (l *hcLogger) Named(name string) hclog.Logger                               { return l }
func (l *hcLogger) ResetNamed(name string) hclog.Logger                          { return l }
func (l *hcLogger) SetLevel(level hclog.Level)                                   {}
func (l *hcLogger) StandardLogger(opts *hclog.StandardLoggerOptions) *log.Logger { return nil }
func (l *hcLogger) StandardWriter(opts *hclog.StandardLoggerOptions) io.Writer   { return os.Stderr }
func (l *hcLogger) ImpliedArgs() []interface{}                                   { return nil }
