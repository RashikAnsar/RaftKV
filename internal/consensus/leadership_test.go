package consensus

import (
	"testing"
	"time"

	"github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// Mock Raft for testing
type mockRaft struct {
	state          raft.RaftState
	leaderAddr     raft.ServerAddress
	leaderID       raft.ServerID
	stats          map[string]string
	transferCalled bool
	transferError  error
	configuration  raft.Configuration
}

func (m *mockRaft) State() raft.RaftState {
	return m.state
}

func (m *mockRaft) LeaderWithID() (raft.ServerAddress, raft.ServerID) {
	return m.leaderAddr, m.leaderID
}

func (m *mockRaft) Stats() map[string]string {
	if m.stats == nil {
		return map[string]string{
			"term":          "5",
			"commit_index":  "100",
			"applied_index": "100",
			"last_log_index": "100",
			"last_log_term":  "5",
			"last_contact":   "never",
		}
	}
	return m.stats
}

func (m *mockRaft) LeadershipTransfer() raft.Future {
	m.transferCalled = true
	return &mockFuture{err: m.transferError}
}

func (m *mockRaft) GetConfiguration() raft.ConfigurationFuture {
	return &mockConfigurationFuture{
		config: m.configuration,
	}
}

type mockFuture struct {
	err error
}

func (f *mockFuture) Error() error {
	return f.err
}

func (f *mockFuture) Response() interface{} {
	return nil
}

func (f *mockFuture) Index() uint64 {
	return 0
}

type mockConfigurationFuture struct {
	config raft.Configuration
	err    error
}

func (f *mockConfigurationFuture) Error() error {
	return f.err
}

func (f *mockConfigurationFuture) Index() uint64 {
	return 0
}

func (f *mockConfigurationFuture) Configuration() raft.Configuration {
	return f.config
}

func TestLeadershipManager_GetLeadershipInfo(t *testing.T) {
	logger := zap.NewNop()

	t.Run("LeaderState", func(t *testing.T) {
		mockR := &mockRaft{
			state:      raft.Leader,
			leaderAddr: "localhost:7000",
			leaderID:   "node1",
		}

		lm := NewLeadershipManager(mockR, "node1", logger)

		info, err := lm.GetLeadershipInfo()
		assert.NoError(t, err)
		assert.NotNil(t, info)
		assert.Equal(t, "node1", info.NodeID)
		assert.True(t, info.IsLeader)
		assert.Equal(t, "leader", info.State)
		assert.Equal(t, "node1", info.LeaderID)
		assert.Equal(t, "localhost:7000", info.LeaderAddress)
		assert.Equal(t, uint64(5), info.Term)
		assert.Equal(t, uint64(100), info.CommitIndex)
		assert.Equal(t, uint64(100), info.AppliedIndex)
	})

	t.Run("FollowerState", func(t *testing.T) {
		mockR := &mockRaft{
			state:      raft.Follower,
			leaderAddr: "localhost:7000",
			leaderID:   "node1",
			stats: map[string]string{
				"term":           "5",
				"commit_index":   "95",
				"applied_index":  "95",
				"last_log_index": "95",
				"last_log_term":  "5",
				"last_contact":   "50ms",
			},
		}

		lm := NewLeadershipManager(mockR, "node2", logger)

		info, err := lm.GetLeadershipInfo()
		assert.NoError(t, err)
		assert.NotNil(t, info)
		assert.Equal(t, "node2", info.NodeID)
		assert.False(t, info.IsLeader)
		assert.Equal(t, "follower", info.State)
		assert.Equal(t, "node1", info.LeaderID)
		assert.Equal(t, uint64(5), info.Term)
		assert.True(t, info.LastContactAge > 0)
	})

	t.Run("CandidateState", func(t *testing.T) {
		mockR := &mockRaft{
			state:      raft.Candidate,
			leaderAddr: "",
			leaderID:   "",
		}

		lm := NewLeadershipManager(mockR, "node3", logger)

		info, err := lm.GetLeadershipInfo()
		assert.NoError(t, err)
		assert.NotNil(t, info)
		assert.Equal(t, "node3", info.NodeID)
		assert.False(t, info.IsLeader)
		assert.Equal(t, "candidate", info.State)
		assert.Equal(t, "", info.LeaderID)
	})
}

func TestLeadershipManager_Stepdown(t *testing.T) {
	logger := zap.NewNop()

	t.Run("SuccessfulStepdown", func(t *testing.T) {
		mockR := &mockRaft{
			state:      raft.Leader,
			leaderAddr: "localhost:7000",
			leaderID:   "node1",
		}

		lm := NewLeadershipManager(mockR, "node1", logger)
		lm.currentLeaderID = "node1"

		err := lm.Stepdown()
		assert.NoError(t, err)
		assert.True(t, mockR.transferCalled)
		assert.Equal(t, 1, lm.leadershipChanges)
		assert.Len(t, lm.electionHistory, 1)
		assert.Equal(t, "stepdown", lm.electionHistory[0].Reason)
	})

	t.Run("NotLeader", func(t *testing.T) {
		mockR := &mockRaft{
			state:      raft.Follower,
			leaderAddr: "localhost:7001",
			leaderID:   "node2",
		}

		lm := NewLeadershipManager(mockR, "node1", logger)

		err := lm.Stepdown()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not the leader")
		assert.False(t, mockR.transferCalled)
	})

	t.Run("StepdownFails", func(t *testing.T) {
		mockR := &mockRaft{
			state:         raft.Leader,
			leaderAddr:    "localhost:7000",
			leaderID:      "node1",
			transferError: raft.ErrNotLeader,
		}

		lm := NewLeadershipManager(mockR, "node1", logger)

		err := lm.Stepdown()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to stepdown")
	})
}

func TestLeadershipManager_TransferLeadership(t *testing.T) {
	logger := zap.NewNop()

	t.Run("SuccessfulTransfer", func(t *testing.T) {
		mockR := &mockRaft{
			state:      raft.Leader,
			leaderAddr: "localhost:7000",
			leaderID:   "node1",
			configuration: raft.Configuration{
				Servers: []raft.Server{
					{ID: "node1", Address: "localhost:7000"},
					{ID: "node2", Address: "localhost:7001"},
					{ID: "node3", Address: "localhost:7002"},
				},
			},
		}

		lm := NewLeadershipManager(mockR, "node1", logger)
		lm.currentLeaderID = "node1"

		err := lm.TransferLeadership("node2")
		assert.NoError(t, err)
		assert.True(t, mockR.transferCalled)
		assert.Equal(t, 1, lm.leadershipChanges)
		assert.Len(t, lm.electionHistory, 1)
		assert.Equal(t, "transfer", lm.electionHistory[0].Reason)
		assert.Equal(t, "node2", lm.electionHistory[0].NewLeaderID)
	})

	t.Run("NotLeader", func(t *testing.T) {
		mockR := &mockRaft{
			state:      raft.Follower,
			leaderAddr: "localhost:7001",
			leaderID:   "node2",
		}

		lm := NewLeadershipManager(mockR, "node1", logger)

		err := lm.TransferLeadership("node2")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not the leader")
		assert.False(t, mockR.transferCalled)
	})

	t.Run("TargetNodeNotFound", func(t *testing.T) {
		mockR := &mockRaft{
			state:      raft.Leader,
			leaderAddr: "localhost:7000",
			leaderID:   "node1",
			configuration: raft.Configuration{
				Servers: []raft.Server{
					{ID: "node1", Address: "localhost:7000"},
					{ID: "node2", Address: "localhost:7001"},
				},
			},
		}

		lm := NewLeadershipManager(mockR, "node1", logger)

		err := lm.TransferLeadership("node999")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found in cluster")
		assert.False(t, mockR.transferCalled)
	})
}

func TestLeadershipManager_TrackLeadershipChange(t *testing.T) {
	logger := zap.NewNop()

	t.Run("DetectsLeadershipChange", func(t *testing.T) {
		mockR := &mockRaft{
			state:      raft.Follower,
			leaderAddr: "localhost:7001",
			leaderID:   "node2",
			stats: map[string]string{
				"term":           "5",
				"commit_index":   "100",
				"applied_index":  "100",
				"last_log_index": "100",
				"last_log_term":  "5",
			},
		}

		lm := NewLeadershipManager(mockR, "node1", logger)
		lm.currentLeaderID = "node1" // Simulate this was the old leader
		lm.lastKnownTerm = 4

		// Track leadership change
		lm.TrackLeadershipChange()

		assert.Equal(t, "node2", lm.currentLeaderID)
		assert.Equal(t, uint64(5), lm.lastKnownTerm)
		assert.Equal(t, 1, lm.leadershipChanges)
		assert.Len(t, lm.electionHistory, 1)
		assert.Equal(t, "node1", lm.electionHistory[0].OldLeaderID)
		assert.Equal(t, "node2", lm.electionHistory[0].NewLeaderID)
		assert.Equal(t, uint64(5), lm.electionHistory[0].Term)
		assert.Equal(t, "new_term_election", lm.electionHistory[0].Reason)
	})

	t.Run("NoChangeDetected", func(t *testing.T) {
		mockR := &mockRaft{
			state:      raft.Leader,
			leaderAddr: "localhost:7000",
			leaderID:   "node1",
		}

		lm := NewLeadershipManager(mockR, "node1", logger)
		lm.currentLeaderID = "node1"
		lm.lastKnownTerm = 5

		// Track leadership change (should detect no change)
		lm.TrackLeadershipChange()

		assert.Equal(t, 0, lm.leadershipChanges)
		assert.Len(t, lm.electionHistory, 0)
	})
}

func TestLeadershipManager_ElectionHistory(t *testing.T) {
	logger := zap.NewNop()

	t.Run("TracksMultipleElections", func(t *testing.T) {
		mockR := &mockRaft{
			state:      raft.Leader,
			leaderAddr: "localhost:7000",
			leaderID:   "node1",
		}

		lm := NewLeadershipManager(mockR, "node1", logger)

		// Add multiple events
		for i := 0; i < 5; i++ {
			event := ElectionEvent{
				Timestamp:   time.Now(),
				OldLeaderID: "node1",
				NewLeaderID: "node2",
				Term:        uint64(i + 1),
				Reason:      "election",
			}
			lm.addElectionEvent(event)
		}

		history := lm.GetElectionHistory()
		assert.Len(t, history, 5)
	})

	t.Run("LimitsHistorySize", func(t *testing.T) {
		mockR := &mockRaft{
			state:      raft.Leader,
			leaderAddr: "localhost:7000",
			leaderID:   "node1",
		}

		lm := NewLeadershipManager(mockR, "node1", logger)
		lm.maxHistorySize = 10

		// Add more events than the max size
		for i := 0; i < 20; i++ {
			event := ElectionEvent{
				Timestamp:   time.Now(),
				OldLeaderID: "node1",
				NewLeaderID: "node2",
				Term:        uint64(i + 1),
				Reason:      "election",
			}
			lm.addElectionEvent(event)
		}

		assert.Len(t, lm.electionHistory, 10)
		// Should keep the most recent 10
		assert.Equal(t, uint64(11), lm.electionHistory[0].Term)
		assert.Equal(t, uint64(20), lm.electionHistory[9].Term)
	})
}

func TestStateToString(t *testing.T) {
	tests := []struct {
		name     string
		state    raft.RaftState
		expected string
	}{
		{"Leader", raft.Leader, "leader"},
		{"Follower", raft.Follower, "follower"},
		{"Candidate", raft.Candidate, "candidate"},
		{"Shutdown", raft.Shutdown, "shutdown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stateToString(tt.state)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLeadershipManager_StartLeadershipTracking(t *testing.T) {
	logger := zap.NewNop()

	mockR := &mockRaft{
		state:      raft.Leader,
		leaderAddr: "localhost:7000",
		leaderID:   "node1",
	}

	lm := NewLeadershipManager(mockR, "node1", logger)

	// Start tracking
	stopCh := lm.StartLeadershipTracking(100 * time.Millisecond)

	// Let it run for a bit
	time.Sleep(250 * time.Millisecond)

	// Stop tracking
	close(stopCh)

	// Give it time to stop
	time.Sleep(150 * time.Millisecond)
}
