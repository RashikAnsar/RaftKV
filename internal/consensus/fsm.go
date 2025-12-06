package consensus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/RashikAnsar/raftkv/internal/cdc"
	"github.com/RashikAnsar/raftkv/internal/storage"
	"github.com/hashicorp/raft"
	"go.uber.org/zap"
)

const (
	OpTypePut    = "put"
	OpTypeDelete = "delete"
)

type Command struct {
	Op    string `json:"op"`
	Key   string `json:"key"`
	Value []byte `json:"value"`
}

type FSM struct {
	store       *storage.DurableStore // Changed from storage.Store interface to concrete type
	logger      *zap.Logger
	cdcPublisher *cdc.Publisher // Optional CDC publisher for change data capture
}

func NewFSM(store *storage.DurableStore, logger *zap.Logger) *FSM {
	return &FSM{
		store:  store,
		logger: logger,
	}
}

// SetCDCPublisher sets the CDC publisher for the FSM
// This is optional and used for multi-DC replication
func (f *FSM) SetCDCPublisher(publisher *cdc.Publisher) {
	f.cdcPublisher = publisher
	f.logger.Info("CDC publisher attached to FSM")
}

func (f *FSM) Apply(log *raft.Log) interface{} {
	// Decode command
	var cmd Command
	if err := json.Unmarshal(log.Data, &cmd); err != nil {
		f.logger.Error("Failed to unmarshal command",
			zap.Error(err),
			zap.ByteString("data", log.Data),
		)
		return fmt.Errorf("failed to unmarshal command: %w", err)
	}

	// Use ApplyRaftEntry with context
	ctx := context.Background()
	if err := f.store.ApplyRaftEntry(ctx, log.Index, log.Term, cmd.Op, cmd.Key, cmd.Value); err != nil {
		f.logger.Error("Failed to apply Raft entry",
			zap.String("op", cmd.Op),
			zap.String("key", cmd.Key),
			zap.Uint64("index", log.Index),
			zap.Uint64("term", log.Term),
			zap.Error(err),
		)
		return err
	}

	// Publish CDC event if publisher is configured
	if f.cdcPublisher != nil {
		event := &cdc.ChangeEvent{
			RaftIndex: log.Index,
			RaftTerm:  log.Term,
			Operation: cmd.Op,
			Key:       cmd.Key,
			Value:     cmd.Value,
			Timestamp: time.Now(),
		}

		if err := f.cdcPublisher.Publish(ctx, event); err != nil {
			// Log error but don't fail the operation
			// CDC is async and shouldn't block the main path
			f.logger.Warn("Failed to publish CDC event",
				zap.Error(err),
				zap.Uint64("index", log.Index),
			)
		}
	}

	f.logger.Debug("Applied Raft entry",
		zap.String("op", cmd.Op),
		zap.String("key", cmd.Key),
		zap.Uint64("index", log.Index),
		zap.Uint64("term", log.Term),
	)
	return nil
}

func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	f.logger.Info("Creating FSM snapshot")

	ctx := context.Background()
	keys, err := f.store.List(ctx, "", 0)
	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}

	data := make(map[string][]byte)
	for _, key := range keys {
		value, err := f.store.Get(ctx, key)
		if err != nil {
			continue
		}
		data[key] = value
	}

	// Get Raft metadata from DurableStore
	index, term := f.store.LastAppliedIndex()

	snapshot := &fsmSnapshot{
		index:  index,
		term:   term,
		data:   data,
		logger: f.logger,
	}

	f.logger.Info("FSM snapshot created",
		zap.Int("keys", len(data)),
		zap.Uint64("raft_index", index),
		zap.Uint64("raft_term", term),
	)
	return snapshot, nil
}

func (f *FSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()

	f.logger.Info("Restoring FSM from snapshot")

	var snapshotData struct {
		Index uint64            `json:"index"`
		Term  uint64            `json:"term"`
		Data  map[string][]byte `json:"data"`
	}

	decoder := json.NewDecoder(rc)
	if err := decoder.Decode(&snapshotData); err != nil {
		return fmt.Errorf("failed to decode snapshot: %w", err)
	}

	// Reset DurableStore
	f.store.Reset()

	ctx := context.Background()
	for key, value := range snapshotData.Data {
		if err := f.store.Put(ctx, key, value); err != nil {
			return fmt.Errorf("failed to restore key %s: %w", key, err)
		}
	}

	// Restore Raft tracking
	f.store.SetLastAppliedIndex(snapshotData.Index, snapshotData.Term)

	f.logger.Info("FSM restored from snapshot",
		zap.Int("keys", len(snapshotData.Data)),
		zap.Uint64("raft_index", snapshotData.Index),
		zap.Uint64("raft_term", snapshotData.Term),
	)
	return nil
}

type fsmSnapshot struct {
	index  uint64 // Raft index
	term   uint64 // Raft term
	data   map[string][]byte
	logger *zap.Logger
}

func (s *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	s.logger.Info("Persisting FSM snapshot",
		zap.Int("keys", len(s.data)),
		zap.Uint64("raft_index", s.index),
		zap.Uint64("raft_term", s.term),
	)

	// Create snapshot data structure with Raft metadata
	snapshotData := struct {
		Index uint64            `json:"index"`
		Term  uint64            `json:"term"`
		Data  map[string][]byte `json:"data"`
	}{
		Index: s.index,
		Term:  s.term,
		Data:  s.data,
	}

	if err := json.NewEncoder(sink).Encode(snapshotData); err != nil {
		sink.Cancel()
		return fmt.Errorf("failed to encode snapshot: %w", err)
	}

	if err := sink.Close(); err != nil {
		return fmt.Errorf("failed to close sink: %w", err)
	}

	s.logger.Info("FSM snapshot persisted successfully")
	return nil
}

func (s *fsmSnapshot) Release() {
	s.logger.Debug("FSM snapshot released")
}
