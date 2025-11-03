package storage

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// RaftStableStore implements raft.StableStore interface
// Stores Raft metadata (current term, last vote, etc.) in a JSON file
type RaftStableStore struct {
	mu       sync.RWMutex
	filePath string
	data     map[string][]byte
}

// NewRaftStableStore creates a new stable store for Raft metadata
func NewRaftStableStore(dir string) (*RaftStableStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	store := &RaftStableStore{
		filePath: filepath.Join(dir, "stable.json"),
		data:     make(map[string][]byte),
	}

	// Load existing data if file exists
	if err := store.load(); err != nil {
		return nil, fmt.Errorf("failed to load stable store: %w", err)
	}

	return store, nil
}

// Set stores a key/value pair durably
func (s *RaftStableStore) Set(key []byte, val []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Store the value
	s.data[string(key)] = val

	// Persist to disk atomically
	return s.persist()
}

// Get returns the value for key, or empty byte slice if not found
func (s *RaftStableStore) Get(key []byte) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	val, exists := s.data[string(key)]
	if !exists {
		return nil, nil // Raft expects nil for missing keys, not an error
	}

	// Return a copy to prevent external modification
	result := make([]byte, len(val))
	copy(result, val)

	return result, nil
}

// SetUint64 stores a uint64 value
func (s *RaftStableStore) SetUint64(key []byte, val uint64) error {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, val)
	return s.Set(key, buf)
}

// GetUint64 returns the uint64 value for key, or 0 if not found
func (s *RaftStableStore) GetUint64(key []byte) (uint64, error) {
	val, err := s.Get(key)
	if err != nil {
		return 0, err
	}
	if val == nil || len(val) == 0 {
		return 0, nil
	}
	if len(val) != 8 {
		return 0, fmt.Errorf("invalid uint64 value: expected 8 bytes, got %d", len(val))
	}
	return binary.LittleEndian.Uint64(val), nil
}

// persist writes data to disk atomically using temp file + rename
func (s *RaftStableStore) persist() error {
	// Marshal to JSON with indentation for human readability
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	// Write to temporary file first
	tmpPath := s.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename (ensures durability)
	if err := os.Rename(tmpPath, s.filePath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// load reads data from disk
func (s *RaftStableStore) load() error {
	data, err := os.ReadFile(s.filePath)
	if os.IsNotExist(err) {
		// New store, no data yet - this is normal
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Empty file is valid (new store)
	if len(data) == 0 {
		return nil
	}

	// Unmarshal JSON
	if err := json.Unmarshal(data, &s.data); err != nil {
		return fmt.Errorf("failed to unmarshal data: %w", err)
	}

	return nil
}

// Close is a no-op for file-based store (everything is already persisted)
func (s *RaftStableStore) Close() error {
	return nil
}
