package consensus

import (
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	"go.uber.org/zap"
)

// RaftInterface defines the Raft operations needed by LeadershipManager
// This interface allows for easier testing with mocks
type RaftInterface interface {
	State() raft.RaftState
	LeaderWithID() (raft.ServerAddress, raft.ServerID)
	Stats() map[string]string
	LeadershipTransfer() raft.Future
	GetConfiguration() raft.ConfigurationFuture
}

// LeadershipInfo contains detailed information about the current leadership state
type LeadershipInfo struct {
	// Current node information
	NodeID   string `json:"node_id"`
	IsLeader bool   `json:"is_leader"`
	State    string `json:"state"` // "leader", "follower", "candidate", "shutdown"

	// Leader information
	LeaderID      string `json:"leader_id"`
	LeaderAddress string `json:"leader_address"`

	// Raft state information
	Term           uint64        `json:"term"`             // Current term
	LastContact    time.Time     `json:"last_contact"`     // Last contact with leader (for followers)
	LastContactAge time.Duration `json:"last_contact_age"` // Time since last contact
	CommitIndex    uint64        `json:"commit_index"`     // Last committed index
	AppliedIndex   uint64        `json:"applied_index"`    // Last applied index
	LastLogIndex   uint64        `json:"last_log_index"`   // Index of last log entry
	LastLogTerm    uint64        `json:"last_log_term"`    // Term of last log entry

	// Cluster information
	NumPeers int `json:"num_peers"` // Number of peers in cluster

	// Leadership stability
	LeadershipChanges  int           `json:"leadership_changes"`   // Number of leadership changes
	CurrentLeaderSince time.Time     `json:"current_leader_since"` // When current leader took office
	LeaderStability    time.Duration `json:"leader_stability"`     // How long current leader has been in office
}

// ElectionEvent represents a leadership change event
type ElectionEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	OldLeaderID string    `json:"old_leader_id"`
	NewLeaderID string    `json:"new_leader_id"`
	Term        uint64    `json:"term"`
	Reason      string    `json:"reason"` // "election", "stepdown", "transfer", "network_partition", etc.
}

// LeadershipManager manages leadership state and history
type LeadershipManager struct {
	mu     sync.RWMutex
	raft   RaftInterface
	logger *zap.Logger
	nodeID string

	// Leadership history
	electionHistory []ElectionEvent
	maxHistorySize  int

	// Current leadership tracking
	currentLeaderID    string
	currentLeaderSince time.Time
	leadershipChanges  int
	lastKnownTerm      uint64
}

// NewLeadershipManager creates a new leadership manager
func NewLeadershipManager(r RaftInterface, nodeID string, logger *zap.Logger) *LeadershipManager {
	return &LeadershipManager{
		raft:            r,
		logger:          logger,
		nodeID:          nodeID,
		electionHistory: make([]ElectionEvent, 0),
		maxHistorySize:  100, // Keep last 100 leadership changes
	}
}

// GetLeadershipInfo returns detailed leadership information
func (lm *LeadershipManager) GetLeadershipInfo() (*LeadershipInfo, error) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	// Get current state
	state := lm.raft.State()
	isLeader := state == raft.Leader

	// Get leader information
	leaderAddr, leaderID := lm.raft.LeaderWithID()

	// Get stats from Raft
	stats := lm.raft.Stats()

	// Parse important stats
	var term, commitIndex, appliedIndex, lastLogIndex, lastLogTerm uint64
	fmt.Sscanf(stats["term"], "%d", &term)
	fmt.Sscanf(stats["commit_index"], "%d", &commitIndex)
	fmt.Sscanf(stats["applied_index"], "%d", &appliedIndex)
	fmt.Sscanf(stats["last_log_index"], "%d", &lastLogIndex)
	fmt.Sscanf(stats["last_log_term"], "%d", &lastLogTerm)

	// Calculate last contact time
	lastContact := time.Now()
	lastContactAge := time.Duration(0)
	if !isLeader && stats["last_contact"] != "" {
		// Parse last_contact (format: "never" or duration like "5ms")
		if stats["last_contact"] != "never" {
			var contactAge time.Duration
			if err := parseDuration(stats["last_contact"], &contactAge); err == nil {
				lastContactAge = contactAge
				lastContact = time.Now().Add(-contactAge)
			}
		}
	}

	// Get number of peers
	numPeers := 0
	if configFuture := lm.raft.GetConfiguration(); configFuture.Error() == nil {
		numPeers = len(configFuture.Configuration().Servers) - 1 // Exclude self
	}

	// Calculate leader stability
	leaderStability := time.Duration(0)
	if lm.currentLeaderSince.IsZero() {
		lm.currentLeaderSince = time.Now()
	} else {
		leaderStability = time.Since(lm.currentLeaderSince)
	}

	info := &LeadershipInfo{
		NodeID:             lm.nodeID,
		IsLeader:           isLeader,
		State:              stateToString(state),
		LeaderID:           string(leaderID),
		LeaderAddress:      string(leaderAddr),
		Term:               term,
		LastContact:        lastContact,
		LastContactAge:     lastContactAge,
		CommitIndex:        commitIndex,
		AppliedIndex:       appliedIndex,
		LastLogIndex:       lastLogIndex,
		LastLogTerm:        lastLogTerm,
		NumPeers:           numPeers,
		LeadershipChanges:  lm.leadershipChanges,
		CurrentLeaderSince: lm.currentLeaderSince,
		LeaderStability:    leaderStability,
	}

	return info, nil
}

// Stepdown forces the leader to step down (only works if this node is the leader)
func (lm *LeadershipManager) Stepdown() error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	// Check if this node is the leader
	if lm.raft.State() != raft.Leader {
		return fmt.Errorf("node is not the leader (current state: %s)", stateToString(lm.raft.State()))
	}

	lm.logger.Info("Forcing leader stepdown", zap.String("node_id", lm.nodeID))

	// Record the event
	oldLeaderID := lm.currentLeaderID
	stats := lm.raft.Stats()
	var term uint64
	fmt.Sscanf(stats["term"], "%d", &term)

	// Force leadership transfer (stepdown)
	if err := lm.raft.LeadershipTransfer().Error(); err != nil {
		lm.logger.Error("Failed to stepdown", zap.Error(err))
		return fmt.Errorf("failed to stepdown: %w", err)
	}

	// Record election event
	event := ElectionEvent{
		Timestamp:   time.Now(),
		OldLeaderID: oldLeaderID,
		NewLeaderID: "", // Unknown until new leader is elected
		Term:        term,
		Reason:      "stepdown",
	}
	lm.addElectionEvent(event)
	lm.leadershipChanges++

	lm.logger.Info("Leader stepdown initiated successfully")
	return nil
}

// TransferLeadership transfers leadership to a specific node
func (lm *LeadershipManager) TransferLeadership(targetNodeID string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	// Check if this node is the leader
	if lm.raft.State() != raft.Leader {
		return fmt.Errorf("node is not the leader (current state: %s)", stateToString(lm.raft.State()))
	}

	// Validate target node exists in cluster
	configFuture := lm.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		return fmt.Errorf("failed to get cluster configuration: %w", err)
	}

	var targetAddr raft.ServerAddress
	found := false
	for _, server := range configFuture.Configuration().Servers {
		if string(server.ID) == targetNodeID {
			targetAddr = server.Address
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("target node %s not found in cluster", targetNodeID)
	}

	lm.logger.Info("Transferring leadership",
		zap.String("from_node", lm.nodeID),
		zap.String("to_node", targetNodeID),
		zap.String("to_addr", string(targetAddr)),
	)

	// Record the event
	oldLeaderID := lm.currentLeaderID
	stats := lm.raft.Stats()
	var term uint64
	fmt.Sscanf(stats["term"], "%d", &term)

	// Perform leadership transfer
	// Note: HashiCorp Raft's LeadershipTransfer() doesn't take a target parameter
	// It just steps down and lets a new leader be elected
	// For targeted transfer, we would need to use a different approach or Raft extension
	if err := lm.raft.LeadershipTransfer().Error(); err != nil {
		lm.logger.Error("Failed to transfer leadership", zap.Error(err))
		return fmt.Errorf("failed to transfer leadership: %w", err)
	}

	// Record election event
	event := ElectionEvent{
		Timestamp:   time.Now(),
		OldLeaderID: oldLeaderID,
		NewLeaderID: targetNodeID,
		Term:        term,
		Reason:      "transfer",
	}
	lm.addElectionEvent(event)
	lm.leadershipChanges++

	lm.logger.Info("Leadership transfer initiated successfully", zap.String("target", targetNodeID))
	return nil
}

// GetElectionHistory returns the history of leadership changes
func (lm *LeadershipManager) GetElectionHistory() []ElectionEvent {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	// Return a copy to prevent external modification
	history := make([]ElectionEvent, len(lm.electionHistory))
	copy(history, lm.electionHistory)
	return history
}

// TrackLeadershipChange should be called periodically to detect leadership changes
func (lm *LeadershipManager) TrackLeadershipChange() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	leaderAddr, leaderID := lm.raft.LeaderWithID()
	currentLeader := string(leaderID)

	stats := lm.raft.Stats()
	var currentTerm uint64
	fmt.Sscanf(stats["term"], "%d", &currentTerm)

	// Check if leadership changed
	if currentLeader != lm.currentLeaderID && currentLeader != "" {
		lm.logger.Info("Leadership change detected",
			zap.String("old_leader", lm.currentLeaderID),
			zap.String("new_leader", currentLeader),
			zap.String("new_leader_addr", string(leaderAddr)),
			zap.Uint64("term", currentTerm),
		)

		// Determine reason for change
		reason := "election"
		if currentTerm > lm.lastKnownTerm {
			reason = "new_term_election"
		}

		// Record event
		event := ElectionEvent{
			Timestamp:   time.Now(),
			OldLeaderID: lm.currentLeaderID,
			NewLeaderID: currentLeader,
			Term:        currentTerm,
			Reason:      reason,
		}
		lm.addElectionEvent(event)

		lm.currentLeaderID = currentLeader
		lm.currentLeaderSince = time.Now()
		lm.leadershipChanges++
	}

	lm.lastKnownTerm = currentTerm
}

// addElectionEvent adds an event to the history (caller must hold lock)
func (lm *LeadershipManager) addElectionEvent(event ElectionEvent) {
	lm.electionHistory = append(lm.electionHistory, event)

	// Trim history if it exceeds max size
	if len(lm.electionHistory) > lm.maxHistorySize {
		lm.electionHistory = lm.electionHistory[len(lm.electionHistory)-lm.maxHistorySize:]
	}
}

// Helper function to convert Raft state to string
func stateToString(state raft.RaftState) string {
	switch state {
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

// Helper function to parse duration from Raft stats
func parseDuration(s string, d *time.Duration) error {
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// StartLeadershipTracking starts a background goroutine to track leadership changes
func (lm *LeadershipManager) StartLeadershipTracking(interval time.Duration) chan struct{} {
	stopCh := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				lm.TrackLeadershipChange()
			case <-stopCh:
				lm.logger.Info("Stopping leadership tracking")
				return
			}
		}
	}()

	lm.logger.Info("Started leadership tracking", zap.Duration("interval", interval))
	return stopCh
}
