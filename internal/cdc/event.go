package cdc

import (
	"time"
)

// ChangeEvent represents a change captured from the Raft FSM
// This is the core data structure for Change Data Capture
type ChangeEvent struct {
	// Raft metadata
	RaftIndex uint64 `json:"raft_index"` // Raft log index
	RaftTerm  uint64 `json:"raft_term"`  // Raft term

	// Operation details
	Operation string    `json:"operation"` // "put" or "delete"
	Key       string    `json:"key"`
	Value     []byte    `json:"value"`
	Timestamp time.Time `json:"timestamp"`

	// Replication metadata
	DatacenterID string `json:"datacenter_id"` // Source datacenter ID
	SequenceNum  uint64 `json:"sequence_num"`  // CDC sequence number (for ordering)

	// Vector clock for conflict resolution (will be implemented in Phase 3B)
	VectorClock map[string]uint64 `json:"vector_clock,omitempty"`
}

// EventType returns a human-readable event type
func (e *ChangeEvent) EventType() string {
	return e.Operation
}

// IsDelete returns true if this is a delete operation
func (e *ChangeEvent) IsDelete() bool {
	return e.Operation == "delete"
}

// IsPut returns true if this is a put operation
func (e *ChangeEvent) IsPut() bool {
	return e.Operation == "put"
}

// Clone creates a deep copy of the ChangeEvent
func (e *ChangeEvent) Clone() *ChangeEvent {
	clone := &ChangeEvent{
		RaftIndex:    e.RaftIndex,
		RaftTerm:     e.RaftTerm,
		Operation:    e.Operation,
		Key:          e.Key,
		Value:        make([]byte, len(e.Value)),
		Timestamp:    e.Timestamp,
		DatacenterID: e.DatacenterID,
		SequenceNum:  e.SequenceNum,
	}

	copy(clone.Value, e.Value)

	if e.VectorClock != nil {
		clone.VectorClock = make(map[string]uint64, len(e.VectorClock))
		for k, v := range e.VectorClock {
			clone.VectorClock[k] = v
		}
	}

	return clone
}

// EventFilter defines a predicate for filtering change events
type EventFilter func(*ChangeEvent) bool

// KeyPrefixFilter returns a filter that matches keys with the given prefix
func KeyPrefixFilter(prefix string) EventFilter {
	return func(e *ChangeEvent) bool {
		return len(e.Key) >= len(prefix) && e.Key[:len(prefix)] == prefix
	}
}

// OperationFilter returns a filter that matches a specific operation type
func OperationFilter(op string) EventFilter {
	return func(e *ChangeEvent) bool {
		return e.Operation == op
	}
}

// CompositeFilter combines multiple filters with AND logic
func CompositeFilter(filters ...EventFilter) EventFilter {
	return func(e *ChangeEvent) bool {
		for _, filter := range filters {
			if !filter(e) {
				return false
			}
		}
		return true
	}
}
