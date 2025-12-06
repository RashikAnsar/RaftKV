package replication

import (
	"fmt"
	"sync"
)

// VectorClock tracks causality across distributed datacenters
// Used for detecting concurrent updates and resolving conflicts
type VectorClock struct {
	mu     sync.RWMutex
	clocks map[string]uint64 // Map of datacenter ID -> logical clock value
}

// NewVectorClock creates a new vector clock
func NewVectorClock() *VectorClock {
	return &VectorClock{
		clocks: make(map[string]uint64),
	}
}

// NewVectorClockFrom creates a vector clock from an existing map
func NewVectorClockFrom(clocks map[string]uint64) *VectorClock {
	vc := &VectorClock{
		clocks: make(map[string]uint64, len(clocks)),
	}
	for k, v := range clocks {
		vc.clocks[k] = v
	}
	return vc
}

// Increment increments the clock for a specific datacenter
func (vc *VectorClock) Increment(datacenterID string) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.clocks[datacenterID]++
}

// Update updates the clock for a specific datacenter to a specific value
// Only updates if the new value is greater than the current value
func (vc *VectorClock) Update(datacenterID string, value uint64) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	if value > vc.clocks[datacenterID] {
		vc.clocks[datacenterID] = value
	}
}

// Merge merges another vector clock into this one
// Takes the maximum value for each datacenter
func (vc *VectorClock) Merge(other *VectorClock) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	for dc, value := range other.clocks {
		if value > vc.clocks[dc] {
			vc.clocks[dc] = value
		}
	}
}

// Get returns the clock value for a datacenter
func (vc *VectorClock) Get(datacenterID string) uint64 {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	return vc.clocks[datacenterID]
}

// Clone creates a deep copy of the vector clock
func (vc *VectorClock) Clone() *VectorClock {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	clone := &VectorClock{
		clocks: make(map[string]uint64, len(vc.clocks)),
	}
	for k, v := range vc.clocks {
		clone.clocks[k] = v
	}
	return clone
}

// ToMap returns a copy of the internal map
func (vc *VectorClock) ToMap() map[string]uint64 {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	m := make(map[string]uint64, len(vc.clocks))
	for k, v := range vc.clocks {
		m[k] = v
	}
	return m
}

// Compare compares this vector clock with another
// Returns the relationship between the two clocks
func (vc *VectorClock) Compare(other *VectorClock) ClockRelation {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	// Get all datacenter IDs from both clocks
	allDCs := make(map[string]bool)
	for dc := range vc.clocks {
		allDCs[dc] = true
	}
	for dc := range other.clocks {
		allDCs[dc] = true
	}

	lessCount := 0
	greaterCount := 0

	for dc := range allDCs {
		vcValue := vc.clocks[dc]
		otherValue := other.clocks[dc]

		if vcValue < otherValue {
			lessCount++
		} else if vcValue > otherValue {
			greaterCount++
		}
	}

	if lessCount > 0 && greaterCount > 0 {
		// Some clocks are greater, some are less = concurrent
		return ClockConcurrent
	} else if lessCount > 0 {
		// All vc clocks <= other clocks = vc happened before other
		return ClockBefore
	} else if greaterCount > 0 {
		// All vc clocks >= other clocks = vc happened after other
		return ClockAfter
	} else {
		// All clocks are equal
		return ClockEqual
	}
}

// HappenedBefore returns true if this vector clock happened before the other
func (vc *VectorClock) HappenedBefore(other *VectorClock) bool {
	return vc.Compare(other) == ClockBefore
}

// HappenedAfter returns true if this vector clock happened after the other
func (vc *VectorClock) HappenedAfter(other *VectorClock) bool {
	return vc.Compare(other) == ClockAfter
}

// IsConcurrent returns true if this vector clock is concurrent with the other
// (neither happened before the other)
func (vc *VectorClock) IsConcurrent(other *VectorClock) bool {
	return vc.Compare(other) == ClockConcurrent
}

// IsEqual returns true if the vector clocks are equal
func (vc *VectorClock) IsEqual(other *VectorClock) bool {
	return vc.Compare(other) == ClockEqual
}

// String returns a string representation of the vector clock
func (vc *VectorClock) String() string {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	return fmt.Sprintf("%v", vc.clocks)
}

// ClockRelation describes the relationship between two vector clocks
type ClockRelation int

const (
	// ClockBefore means the first clock happened before the second
	ClockBefore ClockRelation = iota

	// ClockAfter means the first clock happened after the second
	ClockAfter

	// ClockConcurrent means the clocks are concurrent (neither happened before the other)
	ClockConcurrent

	// ClockEqual means the clocks are exactly equal
	ClockEqual
)

// String returns a string representation of the clock relation
func (cr ClockRelation) String() string {
	switch cr {
	case ClockBefore:
		return "before"
	case ClockAfter:
		return "after"
	case ClockConcurrent:
		return "concurrent"
	case ClockEqual:
		return "equal"
	default:
		return "unknown"
	}
}
