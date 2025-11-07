package storage

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashicorp/raft"
)

// RaftLogStore implements raft.LogStore interface using append-only files
// Each log entry is stored with its index, allowing fast lookups
type RaftLogStore struct {
	mu sync.RWMutex

	dir  string
	file *os.File // Current log file

	// In-memory index: log index -> file offset
	index map[uint64]int64

	firstIdx uint64 // First log index (0 if empty)
	lastIdx  uint64 // Last log index (0 if empty)

	currentOffset int64 // Current write offset in file
}

// RaftLogEntry is the on-disk format for Raft log entries
type RaftLogEntry struct {
	Index      uint64
	Term       uint64
	Type       raft.LogType
	Data       []byte
	Extensions []byte
	AppendedAt int64 // Unix nanoseconds
}

const (
	raftLogFileName = "raft-log.dat"
	entryHeaderSize = 8 + 8 + 1 + 4 + 4 + 8 // index + term + type + data_len + ext_len + timestamp
)

// NewRaftLogStore creates a new Raft log store
func NewRaftLogStore(dir string) (*RaftLogStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	logPath := filepath.Join(dir, raftLogFileName)

	// Open or create log file
	file, err := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	store := &RaftLogStore{
		dir:   dir,
		file:  file,
		index: make(map[uint64]int64),
	}

	// Recover index from existing file
	if err := store.recover(); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to recover log store: %w", err)
	}

	return store, nil
}

// FirstIndex returns the first index written. 0 for no entries.
func (s *RaftLogStore) FirstIndex() (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.firstIdx, nil
}

// LastIndex returns the last index written. 0 for no entries.
func (s *RaftLogStore) LastIndex() (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastIdx, nil
}

// GetLog retrieves a log entry at a given index
func (s *RaftLogStore) GetLog(index uint64, log *raft.Log) error {
	s.mu.RLock()
	offset, exists := s.index[index]
	s.mu.RUnlock()

	if !exists {
		return raft.ErrLogNotFound
	}

	// Read entry from file at offset
	entry, err := s.readEntryAt(offset)
	if err != nil {
		return fmt.Errorf("failed to read entry at offset %d: %w", offset, err)
	}

	// Populate raft.Log
	log.Index = entry.Index
	log.Term = entry.Term
	log.Type = entry.Type
	log.Data = entry.Data
	log.Extensions = entry.Extensions
	log.AppendedAt = time.Unix(0, entry.AppendedAt)

	return nil
}

// StoreLog stores a single log entry
func (s *RaftLogStore) StoreLog(log *raft.Log) error {
	return s.StoreLogs([]*raft.Log{log})
}

// StoreLogs stores multiple log entries (batch operation for efficiency)
func (s *RaftLogStore) StoreLogs(logs []*raft.Log) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, log := range logs {
		// Convert to our format
		entry := &RaftLogEntry{
			Index:      log.Index,
			Term:       log.Term,
			Type:       log.Type,
			Data:       log.Data,
			Extensions: log.Extensions,
			AppendedAt: log.AppendedAt.UnixNano(),
		}

		// Serialize entry
		data, err := s.serializeEntry(entry)
		if err != nil {
			return fmt.Errorf("failed to serialize entry %d: %w", log.Index, err)
		}

		// Write to file and get offset
		offset := s.currentOffset
		n, err := s.file.Write(data)
		if err != nil {
			return fmt.Errorf("failed to write entry %d: %w", log.Index, err)
		}

		// Sync to disk (Raft needs durability)
		if err := s.file.Sync(); err != nil {
			return fmt.Errorf("failed to sync entry %d: %w", log.Index, err)
		}

		// Update in-memory index
		s.index[log.Index] = offset
		s.currentOffset += int64(n)

		// Update first/last indices
		if s.firstIdx == 0 || log.Index < s.firstIdx {
			s.firstIdx = log.Index
		}
		if log.Index > s.lastIdx {
			s.lastIdx = log.Index
		}
	}

	return nil
}

// DeleteRange deletes log entries in the range [min, max] inclusive
func (s *RaftLogStore) DeleteRange(min, max uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove from in-memory index
	for idx := min; idx <= max; idx++ {
		delete(s.index, idx)
	}

	// Update first index
	if min <= s.firstIdx && max >= s.firstIdx {
		// Find new first index
		s.firstIdx = max + 1
		if s.firstIdx > s.lastIdx {
			s.firstIdx = 0
			s.lastIdx = 0
		}
	}

	// Note: We don't actually delete from the file (append-only)
	// The deleted entries are just removed from the index
	// Compaction would be handled by log rotation/cleanup (TODO: future enhancement)

	return nil
}

// serializeEntry converts RaftLogEntry to bytes
func (s *RaftLogStore) serializeEntry(entry *RaftLogEntry) ([]byte, error) {
	dataLen := len(entry.Data)
	extLen := len(entry.Extensions)

	totalSize := entryHeaderSize + dataLen + extLen
	buf := make([]byte, totalSize)

	offset := 0

	// Write header
	binary.BigEndian.PutUint64(buf[offset:], entry.Index)
	offset += 8
	binary.BigEndian.PutUint64(buf[offset:], entry.Term)
	offset += 8
	buf[offset] = byte(entry.Type)
	offset += 1
	binary.BigEndian.PutUint32(buf[offset:], uint32(dataLen))
	offset += 4
	binary.BigEndian.PutUint32(buf[offset:], uint32(extLen))
	offset += 4
	binary.BigEndian.PutUint64(buf[offset:], uint64(entry.AppendedAt))
	offset += 8

	// Write data
	copy(buf[offset:], entry.Data)
	offset += dataLen

	// Write extensions
	if extLen > 0 {
		copy(buf[offset:], entry.Extensions)
	}

	return buf, nil
}

// readEntryAt reads a log entry from the given file offset
func (s *RaftLogStore) readEntryAt(offset int64) (*RaftLogEntry, error) {
	// Read header first
	headerBuf := make([]byte, entryHeaderSize)
	if _, err := s.file.ReadAt(headerBuf, offset); err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	// Parse header
	idx := 0
	index := binary.BigEndian.Uint64(headerBuf[idx:])
	idx += 8
	term := binary.BigEndian.Uint64(headerBuf[idx:])
	idx += 8
	logType := raft.LogType(headerBuf[idx])
	idx += 1
	dataLen := binary.BigEndian.Uint32(headerBuf[idx:])
	idx += 4
	extLen := binary.BigEndian.Uint32(headerBuf[idx:])
	idx += 4
	appendedAt := int64(binary.BigEndian.Uint64(headerBuf[idx:]))

	// Read data and extensions
	payloadSize := int(dataLen + extLen)
	payloadBuf := make([]byte, payloadSize)
	if _, err := s.file.ReadAt(payloadBuf, offset+entryHeaderSize); err != nil {
		return nil, fmt.Errorf("failed to read payload: %w", err)
	}

	entry := &RaftLogEntry{
		Index:      index,
		Term:       term,
		Type:       logType,
		Data:       payloadBuf[:dataLen],
		AppendedAt: appendedAt,
	}

	if extLen > 0 {
		entry.Extensions = payloadBuf[dataLen:]
	}

	return entry, nil
}

// recover rebuilds the in-memory index by scanning the log file
func (s *RaftLogStore) recover() error {
	info, err := s.file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	fileSize := info.Size()
	if fileSize == 0 {
		// Empty file, new log store
		s.currentOffset = 0
		return nil
	}

	// Scan file and rebuild index
	offset := int64(0)

	for offset < fileSize {
		// Read header
		headerBuf := make([]byte, entryHeaderSize)
		n, err := s.file.ReadAt(headerBuf, offset)
		if err != nil || n < entryHeaderSize {
			// Reached end or corrupted entry
			break
		}

		// Parse header to get index and payload size
		index := binary.BigEndian.Uint64(headerBuf[0:8])
		dataLen := binary.BigEndian.Uint32(headerBuf[17:21])
		extLen := binary.BigEndian.Uint32(headerBuf[21:25])

		// Calculate full entry size
		entrySize := entryHeaderSize + int64(dataLen) + int64(extLen)

		// Validate that the full entry exists in the file
		if offset+entrySize > fileSize {
			// Incomplete entry at end of file - truncate here
			fmt.Printf("Warning: Incomplete Raft log entry at offset %d, truncating\n", offset)
			break
		}

		// Store offset in index
		s.index[index] = offset

		// Update first/last
		if s.firstIdx == 0 || index < s.firstIdx {
			s.firstIdx = index
		}
		if index > s.lastIdx {
			s.lastIdx = index
		}

		// Move to next entry
		offset += entrySize
	}

	s.currentOffset = offset

	fmt.Printf("Recovered %d Raft log entries (first: %d, last: %d)\n",
		len(s.index), s.firstIdx, s.lastIdx)

	return nil
}

// Close closes the log store
func (s *RaftLogStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file != nil {
		return s.file.Close()
	}
	return nil
}

// CompactLog compacts the Raft log by rewriting the file without deleted entries
// This physically removes entries that were deleted via DeleteRange
func (s *RaftLogStore) CompactLog() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.index) == 0 {
		return nil // Nothing to compact
	}

	// Create a temporary file for the compacted log
	tempPath := filepath.Join(s.dir, raftLogFileName+".compact")
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tempFile.Close()

	// Write all indexed entries to the new file
	newIndex := make(map[uint64]int64)
	newOffset := int64(0)

	// Sort indices to write in order
	var indices []uint64
	for idx := range s.index {
		indices = append(indices, idx)
	}

	// Simple sort (bubble sort is fine for small numbers)
	for i := 0; i < len(indices); i++ {
		for j := i + 1; j < len(indices); j++ {
			if indices[i] > indices[j] {
				indices[i], indices[j] = indices[j], indices[i]
			}
		}
	}

	// Copy each entry to the new file
	for _, idx := range indices {
		oldOffset := s.index[idx]

		// Read entry from old file
		entry, err := s.readEntryAt(oldOffset)
		if err != nil {
			return fmt.Errorf("failed to read entry %d: %w", idx, err)
		}

		// Serialize and write to new file
		data, err := s.serializeEntry(entry)
		if err != nil {
			return fmt.Errorf("failed to serialize entry %d: %w", idx, err)
		}

		n, err := tempFile.Write(data)
		if err != nil {
			return fmt.Errorf("failed to write entry %d: %w", idx, err)
		}

		// Update new index
		newIndex[idx] = newOffset
		newOffset += int64(n)
	}

	// Sync new file
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	// Close old file
	if err := s.file.Close(); err != nil {
		return fmt.Errorf("failed to close old file: %w", err)
	}

	// Close temp file
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Replace old file with new file
	oldPath := filepath.Join(s.dir, raftLogFileName)
	if err := os.Rename(tempPath, oldPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Reopen the new file
	newFile, err := os.OpenFile(oldPath, os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to reopen log file: %w", err)
	}

	// Update state
	s.file = newFile
	s.index = newIndex
	s.currentOffset = newOffset

	fmt.Printf("Raft log compacted: %d entries, %d bytes\n", len(newIndex), newOffset)

	return nil
}

// GetStorageSize returns the disk usage of the Raft log in bytes
func (s *RaftLogStore) GetStorageSize() (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, err := s.file.Stat()
	if err != nil {
		return 0, err
	}

	return info.Size(), nil
}
