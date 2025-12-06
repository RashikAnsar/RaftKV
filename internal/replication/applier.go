package replication

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/RashikAnsar/raftkv/internal/cdc"
	"github.com/RashikAnsar/raftkv/internal/storage"
	"go.uber.org/zap"
)

// ChangeApplier applies remote changes to the local store
// Handles idempotency, conflict detection, and ordering
type ChangeApplier struct {
	store            *storage.DurableStore
	logger           *zap.Logger
	datacenterID     string
	conflictResolver ConflictResolver

	// Track applied changes for idempotency
	mu              sync.RWMutex
	appliedIndices  map[string]uint64 // Map of source DC -> last applied Raft index
	metricsApplied  atomic.Uint64
	metricsSkipped  atomic.Uint64
	metricsConflict atomic.Uint64
}

// ConflictResolver defines the interface for resolving conflicts
type ConflictResolver interface {
	Resolve(local, remote *cdc.ChangeEvent) (*cdc.ChangeEvent, error)
}

// NewChangeApplier creates a new change applier
func NewChangeApplier(store *storage.DurableStore, datacenterID string, resolver ConflictResolver, logger *zap.Logger) *ChangeApplier {
	return &ChangeApplier{
		store:            store,
		logger:           logger,
		datacenterID:     datacenterID,
		conflictResolver: resolver,
		appliedIndices:   make(map[string]uint64),
	}
}

// Apply applies a change event to the local store
// Returns an error if the change cannot be applied
func (ca *ChangeApplier) Apply(ctx context.Context, event *cdc.ChangeEvent) error {
	// Skip events from our own datacenter (avoid loops)
	if event.DatacenterID == ca.datacenterID {
		ca.metricsSkipped.Add(1)
		ca.logger.Debug("Skipping event from own datacenter",
			zap.Uint64("index", event.RaftIndex),
		)
		return nil
	}

	// Check if we've already applied this event (idempotency)
	if ca.isAlreadyApplied(event) {
		ca.metricsSkipped.Add(1)
		ca.logger.Debug("Skipping already applied event",
			zap.String("source_dc", event.DatacenterID),
			zap.Uint64("index", event.RaftIndex),
		)
		return nil
	}

	// Apply based on operation type
	var err error
	switch event.Operation {
	case "put":
		err = ca.applyPut(ctx, event)
	case "delete":
		err = ca.applyDelete(ctx, event)
	default:
		return fmt.Errorf("unknown operation: %s", event.Operation)
	}

	if err != nil {
		ca.logger.Error("Failed to apply change",
			zap.String("operation", event.Operation),
			zap.String("key", event.Key),
			zap.Uint64("index", event.RaftIndex),
			zap.Error(err),
		)
		return err
	}

	// Mark as applied
	ca.markApplied(event)
	ca.metricsApplied.Add(1)

	ca.logger.Debug("Applied remote change",
		zap.String("operation", event.Operation),
		zap.String("key", event.Key),
		zap.String("source_dc", event.DatacenterID),
		zap.Uint64("index", event.RaftIndex),
	)

	return nil
}

// applyPut applies a PUT operation
func (ca *ChangeApplier) applyPut(ctx context.Context, event *cdc.ChangeEvent) error {
	// Check for conflicts (local modification)
	existingValue, err := ca.store.Get(ctx, event.Key)
	if err == nil && existingValue != nil {
		// Key exists locally, check for conflicts
		// TODO: In Active-Active mode, use vector clocks to detect conflicts
		// For now (Active-Passive), last write wins
		ca.logger.Debug("Overwriting existing value",
			zap.String("key", event.Key),
		)
	}

	// Apply the PUT
	return ca.store.Put(ctx, event.Key, event.Value)
}

// applyDelete applies a DELETE operation
func (ca *ChangeApplier) applyDelete(ctx context.Context, event *cdc.ChangeEvent) error {
	return ca.store.Delete(ctx, event.Key)
}

// isAlreadyApplied checks if an event has already been applied
func (ca *ChangeApplier) isAlreadyApplied(event *cdc.ChangeEvent) bool {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	lastApplied, exists := ca.appliedIndices[event.DatacenterID]
	if !exists {
		return false
	}

	// Event is already applied if its index <= last applied index
	return event.RaftIndex <= lastApplied
}

// markApplied marks an event as applied
func (ca *ChangeApplier) markApplied(event *cdc.ChangeEvent) {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	current := ca.appliedIndices[event.DatacenterID]
	if event.RaftIndex > current {
		ca.appliedIndices[event.DatacenterID] = event.RaftIndex
	}
}

// GetLastAppliedIndex returns the last applied index for a datacenter
func (ca *ChangeApplier) GetLastAppliedIndex(datacenterID string) uint64 {
	ca.mu.RLock()
	defer ca.mu.RUnlock()
	return ca.appliedIndices[datacenterID]
}

// ApplyBatch applies a batch of change events
// More efficient than applying one at a time
func (ca *ChangeApplier) ApplyBatch(ctx context.Context, events []*cdc.ChangeEvent) error {
	for _, event := range events {
		if err := ca.Apply(ctx, event); err != nil {
			// Log error but continue with remaining events
			ca.logger.Warn("Failed to apply event in batch",
				zap.Error(err),
				zap.Uint64("index", event.RaftIndex),
			)
		}
	}
	return nil
}

// Stats returns applier statistics
func (ca *ChangeApplier) Stats() ApplierStats {
	ca.mu.RLock()
	lastApplied := make(map[string]uint64, len(ca.appliedIndices))
	for k, v := range ca.appliedIndices {
		lastApplied[k] = v
	}
	ca.mu.RUnlock()

	return ApplierStats{
		Applied:     ca.metricsApplied.Load(),
		Skipped:     ca.metricsSkipped.Load(),
		Conflicts:   ca.metricsConflict.Load(),
		LastApplied: lastApplied,
	}
}

// ApplierStats contains change applier statistics
type ApplierStats struct {
	Applied     uint64
	Skipped     uint64
	Conflicts   uint64
	LastApplied map[string]uint64 // Per-DC last applied index
}

// LWWConflictResolver implements Last-Write-Wins conflict resolution
type LWWConflictResolver struct {
	datacenterID string
}

// NewLWWConflictResolver creates a new LWW conflict resolver
func NewLWWConflictResolver(datacenterID string) *LWWConflictResolver {
	return &LWWConflictResolver{
		datacenterID: datacenterID,
	}
}

// Resolve resolves a conflict using Last-Write-Wins strategy
// Compares timestamps, uses datacenter ID as tiebreaker
func (r *LWWConflictResolver) Resolve(local, remote *cdc.ChangeEvent) (*cdc.ChangeEvent, error) {
	// Compare timestamps
	if remote.Timestamp.After(local.Timestamp) {
		return remote, nil
	} else if remote.Timestamp.Before(local.Timestamp) {
		return local, nil
	}

	// Timestamps are equal, use datacenter ID as tiebreaker
	// Use lexicographic comparison for deterministic results
	if remote.DatacenterID > local.DatacenterID {
		return remote, nil
	}
	return local, nil
}
