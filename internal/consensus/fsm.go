package consensus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

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
	store  storage.Store
	logger *zap.Logger
}

func NewFSM(store storage.Store, logger *zap.Logger) *FSM {
	return &FSM{
		store:  store,
		logger: logger,
	}
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

	ctx := context.Background()

	switch cmd.Op {
	case OpTypePut:
		if err := f.store.Put(ctx, cmd.Key, cmd.Value); err != nil {
			f.logger.Error("Failed to apply PUT",
				zap.String("key", cmd.Key),
				zap.Error(err),
			)
			return err
		}
		f.logger.Debug("Applied PUT",
			zap.String("key", cmd.Key),
			zap.Uint64("index", log.Index),
		)
		return nil

	case OpTypeDelete:
		if err := f.store.Delete(ctx, cmd.Key); err != nil {
			f.logger.Error("Failed to apply DELETE",
				zap.String("key", cmd.Key),
				zap.Error(err),
			)
			return err
		}
		f.logger.Debug("Applied DELETE",
			zap.String("key", cmd.Key),
			zap.Uint64("index", log.Index),
		)
		return nil

	default:
		f.logger.Error("Unknown operation type", zap.String("op", cmd.Op))
		return fmt.Errorf("unknown operation: %s", cmd.Op)
	}
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

	snapshot := &fsmSnapshot{
		data:   data,
		logger: f.logger,
	}

	f.logger.Info("FSM snapshot created", zap.Int("keys", len(data)))
	return snapshot, nil
}

func (f *FSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()

	f.logger.Info("Restoring FSM from snapshot")

	var data map[string][]byte
	if err := json.NewDecoder(rc).Decode(&data); err != nil {
		return fmt.Errorf("failed to decode snapshot: %w", err)
	}

	if ms, ok := f.store.(*storage.MemoryStore); ok {
		ms.Reset()
	}

	ctx := context.Background()
	for key, value := range data {
		if err := f.store.Put(ctx, key, value); err != nil {
			return fmt.Errorf("failed to restore key %s: %w", key, err)
		}
	}

	f.logger.Info("FSM restored from snapshot", zap.Int("keys", len(data)))
	return nil
}

type fsmSnapshot struct {
	data   map[string][]byte
	logger *zap.Logger
}

func (s *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	s.logger.Info("Persisting FSM snapshot", zap.Int("keys", len(s.data)))

	if err := json.NewEncoder(sink).Encode(s.data); err != nil {
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
