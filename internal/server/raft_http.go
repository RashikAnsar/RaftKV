package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/RashikAnsar/raftkv/internal/consensus"
	"github.com/RashikAnsar/raftkv/internal/observability"
)

// RaftHTTPServer is an HTTP server that integrates with Raft consensus
type RaftHTTPServer struct {
	raft    *consensus.RaftNode
	router  *mux.Router
	server  *http.Server
	logger  *observability.Logger
	metrics *observability.Metrics
}

// RaftHTTPServerConfig contains configuration for the Raft HTTP server
type RaftHTTPServerConfig struct {
	Addr            string
	RaftNode        *consensus.RaftNode
	Logger          *observability.Logger
	Metrics         *observability.Metrics
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	MaxRequestSize  int64
	EnableRateLimit bool
	RateLimit       int
}

// NewRaftHTTPServer creates a new Raft-aware HTTP server
func NewRaftHTTPServer(config RaftHTTPServerConfig) *RaftHTTPServer {
	// Defaults
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 10 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 10 * time.Second
	}
	if config.MaxRequestSize == 0 {
		config.MaxRequestSize = 1024 * 1024 // 1MB default
	}

	srv := &RaftHTTPServer{
		raft:    config.RaftNode,
		logger:  config.Logger,
		metrics: config.Metrics,
	}

	// Create router
	router := mux.NewRouter()

	// Apply middleware
	router.Use(RecoveryMiddleware(config.Logger))
	router.Use(LoggingMiddleware(config.Logger))
	router.Use(MetricsMiddleware(config.Metrics))
	router.Use(CORSMiddleware())

	if config.EnableRateLimit {
		router.Use(RateLimitMiddleware(config.RateLimit))
	}

	// Register routes
	srv.registerRoutes(router, config.MaxRequestSize)

	srv.router = router
	srv.server = &http.Server{
		Addr:         config.Addr,
		Handler:      router,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
	}

	return srv
}

// registerRoutes registers all HTTP routes
func (s *RaftHTTPServer) registerRoutes(router *mux.Router, maxRequestSize int64) {
	// Key operations
	router.HandleFunc("/keys/{key}", s.limitRequestSize(s.handleGet, maxRequestSize)).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/keys/{key}", s.limitRequestSize(s.handlePut, maxRequestSize)).Methods(http.MethodPut, http.MethodOptions)
	router.HandleFunc("/keys/{key}", s.limitRequestSize(s.handleDelete, maxRequestSize)).Methods(http.MethodDelete, http.MethodOptions)
	router.HandleFunc("/keys", s.limitRequestSize(s.handleList, maxRequestSize)).Methods(http.MethodGet, http.MethodOptions)

	// Raft cluster management
	router.HandleFunc("/cluster/join", s.limitRequestSize(s.handleJoin, maxRequestSize)).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/cluster/remove", s.limitRequestSize(s.handleRemove, maxRequestSize)).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/cluster/nodes", s.handleNodes).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/cluster/leader", s.handleLeader).Methods(http.MethodGet, http.MethodOptions)

	// Admin operations
	router.HandleFunc("/admin/snapshot", s.limitRequestSize(s.handleSnapshot, maxRequestSize)).Methods(http.MethodPost, http.MethodOptions)

	// Health and metrics
	router.HandleFunc("/health", s.handleHealth).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/ready", s.handleReady).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/stats", s.handleStats).Methods(http.MethodGet, http.MethodOptions)
	router.Handle("/metrics", promhttp.Handler()).Methods(http.MethodGet, http.MethodOptions)

	// Root
	router.HandleFunc("/", s.handleRoot).Methods(http.MethodGet, http.MethodOptions)
}

// limitRequestSize middleware limits request body size
func (s *RaftHTTPServer) limitRequestSize(handler http.HandlerFunc, maxSize int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxSize)
		handler(w, r)
	}
}

// handleGet retrieves a value by key (reads from local store)
func (s *RaftHTTPServer) handleGet(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	if key == "" {
		s.respondError(w, http.StatusBadRequest, "key is required")
		return
	}

	// Read from local store (may be stale on followers)
	value, err := s.raft.Get(r.Context(), key)
	if err != nil {
		s.logger.Error("Failed to get key",
			zap.String("key", key),
			zap.Error(err),
		)
		s.respondError(w, http.StatusNotFound, "key not found")
		return
	}

	// Return raw bytes
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Raft-State", s.raft.GetState())
	w.Header().Set("X-Raft-Leader", s.raft.GetLeader())
	w.WriteHeader(http.StatusOK)
	w.Write(value)
}

// handlePut stores a key-value pair (must go through Raft)
func (s *RaftHTTPServer) handlePut(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	if key == "" {
		s.respondError(w, http.StatusBadRequest, "key is required")
		return
	}

	// Check if we're the leader
	if !s.raft.IsLeader() {
		// Return redirect to leader
		leader := s.raft.GetLeader()
		if leader == "" {
			s.respondError(w, http.StatusServiceUnavailable, "no leader elected")
			return
		}

		// Return 307 redirect with leader address
		w.Header().Set("Location", fmt.Sprintf("http://%s%s", leader, r.URL.Path))
		w.Header().Set("X-Raft-Leader", leader)
		s.respondError(w, http.StatusTemporaryRedirect, fmt.Sprintf("not leader, redirect to %s", leader))
		return
	}

	// Read value from body
	value, err := io.ReadAll(r.Body)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	// Apply command through Raft
	cmd := consensus.Command{
		Op:    consensus.OpTypePut,
		Key:   key,
		Value: value,
	}

	if err := s.raft.Apply(cmd, 5*time.Second); err != nil {
		s.logger.Error("Failed to apply PUT command",
			zap.String("key", key),
			zap.Error(err),
		)
		s.respondError(w, http.StatusInternalServerError, "failed to replicate command")
		return
	}

	w.Header().Set("X-Raft-Leader", s.raft.GetLeader())
	w.WriteHeader(http.StatusCreated)
}

// handleDelete removes a key (must go through Raft)
func (s *RaftHTTPServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	if key == "" {
		s.respondError(w, http.StatusBadRequest, "key is required")
		return
	}

	// Check if we're the leader
	if !s.raft.IsLeader() {
		// Return redirect to leader
		leader := s.raft.GetLeader()
		if leader == "" {
			s.respondError(w, http.StatusServiceUnavailable, "no leader elected")
			return
		}

		w.Header().Set("Location", fmt.Sprintf("http://%s%s", leader, r.URL.Path))
		w.Header().Set("X-Raft-Leader", leader)
		s.respondError(w, http.StatusTemporaryRedirect, fmt.Sprintf("not leader, redirect to %s", leader))
		return
	}

	// Apply command through Raft
	cmd := consensus.Command{
		Op:  consensus.OpTypeDelete,
		Key: key,
	}

	if err := s.raft.Apply(cmd, 5*time.Second); err != nil {
		s.logger.Error("Failed to apply DELETE command",
			zap.String("key", key),
			zap.Error(err),
		)
		s.respondError(w, http.StatusInternalServerError, "failed to replicate command")
		return
	}

	w.Header().Set("X-Raft-Leader", s.raft.GetLeader())
	w.WriteHeader(http.StatusNoContent)
}

// handleList lists keys by prefix (reads from local store)
func (s *RaftHTTPServer) handleList(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	limit := 1000 // Default limit

	// Parse limit from query
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
	}

	keys, err := s.raft.List(r.Context(), prefix, limit)
	if err != nil {
		s.logger.Error("Failed to list keys",
			zap.String("prefix", prefix),
			zap.Error(err),
		)
		s.respondError(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"keys":   keys,
		"count":  len(keys),
		"prefix": prefix,
		"limit":  limit,
		"leader": s.raft.GetLeader(),
		"state":  s.raft.GetState(),
	})
}

// handleJoin adds a new node to the cluster
func (s *RaftHTTPServer) handleJoin(w http.ResponseWriter, r *http.Request) {
	// Only leader can add nodes
	if !s.raft.IsLeader() {
		leader := s.raft.GetLeader()
		w.Header().Set("X-Raft-Leader", leader)
		s.respondError(w, http.StatusTemporaryRedirect, fmt.Sprintf("not leader, redirect to %s", leader))
		return
	}

	// Parse request body
	var req struct {
		NodeID string `json:"node_id"`
		Addr   string `json:"addr"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.NodeID == "" || req.Addr == "" {
		s.respondError(w, http.StatusBadRequest, "node_id and addr are required")
		return
	}

	// Add voter to cluster
	if err := s.raft.AddVoter(req.NodeID, req.Addr, 10*time.Second); err != nil {
		s.logger.Error("Failed to add voter",
			zap.String("node_id", req.NodeID),
			zap.String("addr", req.Addr),
			zap.Error(err),
		)
		s.respondError(w, http.StatusInternalServerError, "failed to add node")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "node added successfully",
		"node_id": req.NodeID,
		"addr":    req.Addr,
	})
}

// handleRemove removes a node from the cluster
func (s *RaftHTTPServer) handleRemove(w http.ResponseWriter, r *http.Request) {
	// Only leader can remove nodes
	if !s.raft.IsLeader() {
		leader := s.raft.GetLeader()
		w.Header().Set("X-Raft-Leader", leader)
		s.respondError(w, http.StatusTemporaryRedirect, fmt.Sprintf("not leader, redirect to %s", leader))
		return
	}

	// Parse request body
	var req struct {
		NodeID string `json:"node_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.NodeID == "" {
		s.respondError(w, http.StatusBadRequest, "node_id is required")
		return
	}

	// Remove server from cluster
	if err := s.raft.RemoveServer(req.NodeID, 10*time.Second); err != nil {
		s.logger.Error("Failed to remove server",
			zap.String("node_id", req.NodeID),
			zap.Error(err),
		)
		s.respondError(w, http.StatusInternalServerError, "failed to remove node")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "node removed successfully",
		"node_id": req.NodeID,
	})
}

// handleNodes returns the list of nodes in the cluster
func (s *RaftHTTPServer) handleNodes(w http.ResponseWriter, r *http.Request) {
	servers, err := s.raft.GetServers()
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, "failed to get servers")
		return
	}

	nodes := make([]map[string]interface{}, len(servers))
	for i, server := range servers {
		nodes[i] = map[string]interface{}{
			"id":      string(server.ID),
			"address": string(server.Address),
			"suffrage": server.Suffrage.String(),
		}
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"nodes": nodes,
		"count": len(nodes),
	})
}

// handleLeader returns the current leader
func (s *RaftHTTPServer) handleLeader(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"leader": s.raft.GetLeader(),
		"state":  s.raft.GetState(),
	})
}

// handleSnapshot triggers a manual Raft snapshot
func (s *RaftHTTPServer) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := s.raft.Snapshot(); err != nil {
		s.logger.Error("Failed to create snapshot", zap.Error(err))
		s.respondError(w, http.StatusInternalServerError, "failed to create snapshot")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "snapshot created successfully",
	})
}

// handleHealth returns server health status
func (s *RaftHTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status": "healthy",
		"state":  s.raft.GetState(),
		"leader": s.raft.GetLeader(),
	})
}

// handleReady returns readiness status
func (s *RaftHTTPServer) handleReady(w http.ResponseWriter, r *http.Request) {
	// Node is ready if there's a leader
	if s.raft.GetLeader() == "" {
		s.respondError(w, http.StatusServiceUnavailable, "no leader elected")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ready",
		"state":  s.raft.GetState(),
		"leader": s.raft.GetLeader(),
	})
}

// handleStats returns store and Raft statistics
func (s *RaftHTTPServer) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.raft.Stats()
	raftStats := s.raft.RaftStats()

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"store": map[string]interface{}{
			"gets":      stats.Gets,
			"puts":      stats.Puts,
			"deletes":   stats.Deletes,
			"key_count": stats.KeyCount,
		},
		"raft": raftStats,
	})
}

// handleRoot returns API information
func (s *RaftHTTPServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"service": "RaftKV",
		"version": "1.0.0",
		"mode":    "cluster",
		"state":   s.raft.GetState(),
		"leader":  s.raft.GetLeader(),
		"endpoints": map[string]string{
			"GET /keys/{key}":        "Get value",
			"PUT /keys/{key}":        "Put value (leader only)",
			"DELETE /keys/{key}":     "Delete key (leader only)",
			"GET /keys?prefix=":      "List keys",
			"POST /cluster/join":     "Add node to cluster",
			"POST /cluster/remove":   "Remove node from cluster",
			"GET /cluster/nodes":     "List cluster nodes",
			"GET /cluster/leader":    "Get leader info",
			"POST /admin/snapshot":   "Create Raft snapshot",
			"GET /health":            "Health check",
			"GET /ready":             "Readiness check",
			"GET /stats":             "Statistics",
			"GET /metrics":           "Prometheus metrics",
		},
	})
}

// respondJSON sends a JSON response
func (s *RaftHTTPServer) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondError sends an error response
func (s *RaftHTTPServer) respondError(w http.ResponseWriter, status int, message string) {
	s.respondJSON(w, status, map[string]interface{}{
		"error": message,
	})
}

// Start starts the HTTP server
func (s *RaftHTTPServer) Start() error {
	s.logger.Info("Starting Raft HTTP server", zap.String("addr", s.server.Addr))
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *RaftHTTPServer) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down Raft HTTP server")
	return s.server.Shutdown(ctx)
}
