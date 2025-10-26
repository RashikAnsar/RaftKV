package storage

import (
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	snapshotFilePattern = "snapshot-%09d.gob" // snapshot-000000001.gob
	snapshotTempSuffix  = ".tmp"
)

type Snapshot struct {
	Index     uint64            // WAL index at snapshot time
	Timestamp time.Time         // When snapshot was taken
	KeyCount  int64             // Number of keys
	Data      map[string][]byte // All key-value pairs
}

type SnapshotMetadata struct {
	Index     uint64
	Timestamp time.Time
	KeyCount  int64
	Path      string
	Size      int64 // File size in bytes
}

type SnapshotManager struct {
	mu  sync.Mutex
	dir string // Directory for snapshot files
}

type SnapshotStats struct {
	Count          int   // Number of snapshots
	TotalSize      int64 // Total size of all snapshots
	LatestIndex    uint64
	LatestKeyCount int64
	OldestIndex    uint64
}

func NewSnapshotManager(dir string) (*SnapshotManager, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	return &SnapshotManager{
		dir: dir,
	}, nil
}

func (sm *SnapshotManager) Create(index uint64, data map[string][]byte) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	snapshot := &Snapshot{
		Index:     index,
		Timestamp: time.Now(),
		KeyCount:  int64(len(data)),
		Data:      data,
	}

	tempPath := sm.tempPath(index)
	finalPath := sm.snapshotPath(index)

	if err := sm.writeSnapshot(tempPath, snapshot); err != nil {
		return fmt.Errorf("failed to write snapshot: %w", err)
	}

	if err := os.Rename(tempPath, finalPath); err != nil {
		os.Remove(tempPath) // Clean up temp file
		return fmt.Errorf("failed to rename snapshot: %w", err)
	}

	return nil
}

func (sm *SnapshotManager) writeSnapshot(path string, snapshot *Snapshot) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := gob.NewEncoder(file)
	if err := encoder.Encode(snapshot); err != nil {
		return fmt.Errorf("failed to encode snapshot: %w", err)
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync snapshot: %w", err)
	}

	return nil
}

func (sm *SnapshotManager) Restore() (*Snapshot, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	metadata, err := sm.getLatestMetadata()
	if err != nil {
		return nil, err
	}

	if metadata == nil {
		return nil, nil
	}

	return sm.loadSnapshot(metadata.Path)
}

func (sm *SnapshotManager) RestoreFromIndex(index uint64) (*Snapshot, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	path := sm.snapshotPath(index)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("snapshot %d not found", index)
	}

	return sm.loadSnapshot(path)
}

func (sm *SnapshotManager) loadSnapshot(path string) (*Snapshot, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open snapshot: %w", err)
	}
	defer file.Close()

	var snapshot Snapshot
	decoder := gob.NewDecoder(file)
	if err := decoder.Decode(&snapshot); err != nil {
		return nil, fmt.Errorf("failed to decode snapshot: %w", err)
	}

	return &snapshot, nil
}

// List returns metadata for all snapshots, sorted by index (newest first)
func (sm *SnapshotManager) List() ([]*SnapshotMetadata, error) {
	files, err := os.ReadDir(sm.dir)
	if err != nil {
		return nil, err
	}

	var metadataList []*SnapshotMetadata

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		name := file.Name()
		if !strings.HasPrefix(name, "snapshot-") || !strings.HasSuffix(name, ".gob") {
			continue
		}

		// Skip temp files
		if strings.HasSuffix(name, snapshotTempSuffix) {
			continue
		}

		indexStr := strings.TrimPrefix(name, "snapshot-")
		indexStr = strings.TrimSuffix(indexStr, ".gob")
		index, err := strconv.ParseUint(indexStr, 10, 64)
		if err != nil {
			continue
		}

		path := filepath.Join(sm.dir, name)
		info, err := file.Info()
		if err != nil {
			continue
		}

		metadataList = append(metadataList, &SnapshotMetadata{
			Index:     index,
			Timestamp: time.Time{}, // Will be populated when snapshot is loaded
			KeyCount:  0,           // Will be populated when snapshot is loaded
			Path:      path,
			Size:      info.Size(),
		})
	}

	sort.Slice(metadataList, func(i, j int) bool {
		return metadataList[i].Index > metadataList[j].Index
	})

	return metadataList, nil
}

func (sm *SnapshotManager) getLatestMetadata() (*SnapshotMetadata, error) {
	list, err := sm.List()
	if err != nil {
		return nil, err
	}

	if len(list) == 0 {
		return nil, nil
	}

	return list[0], nil
}

// DeleteOldSnapshots deletes all snapshots except the N most recent
func (sm *SnapshotManager) DeleteOldSnapshots(keepCount int) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	list, err := sm.List()
	if err != nil {
		return err
	}

	for i, metadata := range list {
		if i < keepCount {
			continue
		}

		if err := os.Remove(metadata.Path); err != nil {
			return fmt.Errorf("failed to delete snapshot %d: %w", metadata.Index, err)
		}
	}

	return nil
}

// DeleteSnapshotsBefore deletes all snapshots before (not including) the given index
func (sm *SnapshotManager) DeleteSnapshotsBefore(beforeIndex uint64) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	list, err := sm.List()
	if err != nil {
		return err
	}

	for _, metadata := range list {
		if metadata.Index >= beforeIndex {
			continue
		}

		if err := os.Remove(metadata.Path); err != nil {
			return fmt.Errorf("failed to delete snapshot %d: %w", metadata.Index, err)
		}
	}

	return nil
}

func (sm *SnapshotManager) snapshotPath(index uint64) string {
	filename := fmt.Sprintf(snapshotFilePattern, index)
	return filepath.Join(sm.dir, filename)
}

func (sm *SnapshotManager) tempPath(index uint64) string {
	return sm.snapshotPath(index) + snapshotTempSuffix
}

func (sm *SnapshotManager) GetLatestIndex() (uint64, error) {
	metadata, err := sm.getLatestMetadata()
	if err != nil {
		return 0, err
	}

	if metadata == nil {
		return 0, nil
	}

	return metadata.Index, nil
}

func (sm *SnapshotManager) GetStats() (*SnapshotStats, error) {
	list, err := sm.List()
	if err != nil {
		return nil, err
	}

	if len(list) == 0 {
		return &SnapshotStats{}, nil
	}

	var totalSize int64
	for _, meta := range list {
		totalSize += meta.Size
	}

	return &SnapshotStats{
		Count:          len(list),
		TotalSize:      totalSize,
		LatestIndex:    list[0].Index,
		LatestKeyCount: list[0].KeyCount,
		OldestIndex:    list[len(list)-1].Index,
	}, nil
}
