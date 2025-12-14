package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/RashikAnsar/raftkv/internal/consensus"
)

// ClusterHandlers handles cluster and leadership operations
type ClusterHandlers struct {
	raftNode *consensus.RaftNode
	logger   *zap.Logger
}

// NewClusterHandlers creates a new cluster handlers instance
func NewClusterHandlers(raftNode *consensus.RaftNode, logger *zap.Logger) *ClusterHandlers {
	return &ClusterHandlers{
		raftNode: raftNode,
		logger:   logger,
	}
}

// RegisterRoutes registers cluster-related routes
func (h *ClusterHandlers) RegisterRoutes(router *mux.Router, maxRequestSize int64) {
	// Leadership endpoints
	router.HandleFunc("/cluster/leader", h.handleGetLeader).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/cluster/leadership", h.handleGetLeadership).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/cluster/leadership/stepdown", limitRequestSize(h.handleStepdown, maxRequestSize)).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/cluster/leadership/transfer", limitRequestSize(h.handleTransferLeadership, maxRequestSize)).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/cluster/elections/history", h.handleElectionHistory).Methods(http.MethodGet, http.MethodOptions)

	// Cluster membership endpoints
	router.HandleFunc("/cluster/servers", h.handleGetServers).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/cluster/join", limitRequestSize(h.handleJoin, maxRequestSize)).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/cluster/remove", limitRequestSize(h.handleRemove, maxRequestSize)).Methods(http.MethodPost, http.MethodOptions)
}

// handleGetLeader returns the current leader information
func (h *ClusterHandlers) handleGetLeader(w http.ResponseWriter, r *http.Request) {
	leaderAddr, leaderID := h.raftNode.GetLeader()

	response := map[string]interface{}{
		"leader_id":      leaderID,
		"leader_address": leaderAddr,
		"is_leader":      h.raftNode.IsLeader(),
		"node_id":        h.raftNode.NodeID,
	}

	respondJSON(w, http.StatusOK, response)
}

// handleGetLeadership returns detailed leadership information
func (h *ClusterHandlers) handleGetLeadership(w http.ResponseWriter, r *http.Request) {
	info, err := h.raftNode.GetLeadershipInfo()
	if err != nil {
		h.logger.Error("Failed to get leadership info", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to get leadership information")
		return
	}

	respondJSON(w, http.StatusOK, info)
}

// handleStepdown forces the leader to step down
func (h *ClusterHandlers) handleStepdown(w http.ResponseWriter, r *http.Request) {
	// Check if this node is the leader
	if !h.raftNode.IsLeader() {
		respondError(w, http.StatusBadRequest, "node is not the leader")
		return
	}

	if err := h.raftNode.Stepdown(); err != nil {
		h.logger.Error("Failed to stepdown", zap.Error(err))
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "leadership stepdown initiated",
		"node_id": h.raftNode.NodeID,
	})
}

// handleTransferLeadership transfers leadership to a specific node
func (h *ClusterHandlers) handleTransferLeadership(w http.ResponseWriter, r *http.Request) {
	// Check if this node is the leader
	if !h.raftNode.IsLeader() {
		respondError(w, http.StatusBadRequest, "node is not the leader")
		return
	}

	// Parse request body
	var req struct {
		TargetNodeID string `json:"target_node_id"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	if err := json.Unmarshal(body, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.TargetNodeID == "" {
		respondError(w, http.StatusBadRequest, "target_node_id is required")
		return
	}

	if err := h.raftNode.TransferLeadership(req.TargetNodeID); err != nil {
		h.logger.Error("Failed to transfer leadership",
			zap.String("target", req.TargetNodeID),
			zap.Error(err),
		)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message":        "leadership transfer initiated",
		"target_node_id": req.TargetNodeID,
	})
}

// handleElectionHistory returns the history of leadership changes
func (h *ClusterHandlers) handleElectionHistory(w http.ResponseWriter, r *http.Request) {
	history := h.raftNode.GetElectionHistory()

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"elections": history,
		"count":     len(history),
	})
}

// handleGetServers returns the list of servers in the cluster
func (h *ClusterHandlers) handleGetServers(w http.ResponseWriter, r *http.Request) {
	servers, err := h.raftNode.GetServers()
	if err != nil {
		h.logger.Error("Failed to get servers", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to get cluster servers")
		return
	}

	// Convert to a more readable format
	serverList := make([]map[string]interface{}, 0, len(servers))
	for _, server := range servers {
		serverList = append(serverList, map[string]interface{}{
			"id":       string(server.ID),
			"address":  string(server.Address),
			"suffrage": server.Suffrage.String(),
		})
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"servers": serverList,
		"count":   len(serverList),
	})
}

// handleJoin adds a new node to the cluster
func (h *ClusterHandlers) handleJoin(w http.ResponseWriter, r *http.Request) {
	// Only leader can handle join requests
	if !h.raftNode.IsLeader() {
		// Redirect to leader if known
		leaderAddr, _ := h.raftNode.GetLeader()
		if leaderAddr != "" {
			// Extract HTTP address from Raft address (assumes pattern)
			http.Redirect(w, r, "http://"+leaderAddr+"/cluster/join", http.StatusTemporaryRedirect)
			return
		}
		respondError(w, http.StatusServiceUnavailable, "no leader available")
		return
	}

	// Parse request
	var req struct {
		NodeID string `json:"node_id"`
		Addr   string `json:"addr"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	if err := json.Unmarshal(body, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.NodeID == "" || req.Addr == "" {
		respondError(w, http.StatusBadRequest, "node_id and addr are required")
		return
	}

	// Add voter to cluster
	if err := h.raftNode.Join(req.NodeID, req.Addr); err != nil {
		h.logger.Error("Failed to add node to cluster",
			zap.String("node_id", req.NodeID),
			zap.String("addr", req.Addr),
			zap.Error(err),
		)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.logger.Info("Node joined cluster",
		zap.String("node_id", req.NodeID),
		zap.String("addr", req.Addr),
	)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "node added to cluster",
		"node_id": req.NodeID,
		"addr":    req.Addr,
	})
}

// handleRemove removes a node from the cluster
func (h *ClusterHandlers) handleRemove(w http.ResponseWriter, r *http.Request) {
	// Only leader can handle remove requests
	if !h.raftNode.IsLeader() {
		respondError(w, http.StatusBadRequest, "node is not the leader")
		return
	}

	// Parse request
	var req struct {
		NodeID string `json:"node_id"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	if err := json.Unmarshal(body, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.NodeID == "" {
		respondError(w, http.StatusBadRequest, "node_id is required")
		return
	}

	// Remove server from cluster
	if err := h.raftNode.RemoveServer(req.NodeID); err != nil {
		h.logger.Error("Failed to remove node from cluster",
			zap.String("node_id", req.NodeID),
			zap.Error(err),
		)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.logger.Info("Node removed from cluster", zap.String("node_id", req.NodeID))

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "node removed from cluster",
		"node_id": req.NodeID,
	})
}

// Helper functions

func limitRequestSize(handler http.HandlerFunc, maxSize int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxSize)
		handler(w, r)
	}
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]interface{}{
		"error": message,
	})
}
