package storage

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// WAL format constants
	walMagic1  = 0xAB
	walMagic2  = 0xCD
	walVersion = 0x01

	// Operation types
	OpPut    = 0x01
	OpDelete = 0x02

	// Size constants
	walHeaderSize   = 16 // magic(2) + version(1) + op(1) + timestamp(8) + length(4)
	walChecksumSize = 4

	// Segment constants
	maxSegmentSize = 64 * 1024 * 1024 // 64MB per segment
	walFilePattern = "%09d.wal"       // 000000001.wal
)

type WALEntry struct {
	Operation byte
	Timestamp time.Time
	Key       string
	Value     []byte
}

type WAL struct {
	mu sync.Mutex

	dir           string   // Directory containing WAL files
	currentFile   *os.File // Current active segment
	currentWriter *bufio.Writer
	currentIndex  uint64 // Current segment index (e.g., 1, 2, 3...)
	currentSize   int64  // Current segment size in bytes

	maxSegmentSize int64 // Max size before rotation
	syncOnWrite    bool  // Call fsync after each write
}

type WALConfig struct {
	Dir            string
	MaxSegmentSize int64
	SyncOnWrite    bool // true = durable, false = faster
}

// NewWAL creates a new WAL instance
func NewWAL(config WALConfig) (*WAL, error) {
	if config.MaxSegmentSize == 0 {
		config.MaxSegmentSize = maxSegmentSize
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(config.Dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	w := &WAL{
		dir:            config.Dir,
		maxSegmentSize: config.MaxSegmentSize,
		syncOnWrite:    config.SyncOnWrite,
	}

	// Find the latest segment or create a new one
	if err := w.openLatestSegment(); err != nil {
		return nil, err
	}

	return w, nil
}

func (w *WAL) openLatestSegment() error {
	segments, err := w.listSegments()
	if err != nil {
		return err
	}

	if len(segments) == 0 {
		return w.createNewSegment(1)
	}

	latestIndex := segments[len(segments)-1]
	path := w.segmentPath(latestIndex)

	file, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open segment %d: %w", latestIndex, err)
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("failed to stat segment: %w", err)
	}

	w.currentFile = file
	w.currentWriter = bufio.NewWriter(file)
	w.currentIndex = latestIndex
	w.currentSize = info.Size()

	return nil
}

func (w *WAL) createNewSegment(index uint64) error {
	path := w.segmentPath(index)

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create segment %d: %w", index, err)
	}

	w.currentFile = file
	w.currentWriter = bufio.NewWriter(file)
	w.currentIndex = index
	w.currentSize = 0

	return nil
}

func (w *WAL) Append(entry *WALEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := w.serializeEntry(entry)
	if err != nil {
		return fmt.Errorf("failed to serialize entry: %w", err)
	}

	if w.currentSize+int64(len(data)) > w.maxSegmentSize {
		if err := w.rotateSegment(); err != nil {
			return fmt.Errorf("failed to rotate segment: %w", err)
		}
	}

	n, err := w.currentWriter.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write entry: %w", err)
	}

	w.currentSize += int64(n)

	if err := w.currentWriter.Flush(); err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}

	if w.syncOnWrite {
		if err := w.currentFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync: %w", err)
		}
	}

	return nil
}

func (w *WAL) serializeEntry(entry *WALEntry) ([]byte, error) {
	keyLen := len(entry.Key)
	valueLen := len(entry.Value)

	dataSize := 4 + keyLen // key_len + key
	if entry.Operation == OpPut {
		dataSize += 4 + valueLen // value_len + value
	}

	totalSize := walHeaderSize + dataSize + walChecksumSize
	buf := make([]byte, totalSize)

	// Write header
	buf[0] = walMagic1
	buf[1] = walMagic2
	buf[2] = walVersion
	buf[3] = entry.Operation
	binary.BigEndian.PutUint64(buf[4:12], uint64(entry.Timestamp.UnixNano()))
	binary.BigEndian.PutUint32(buf[12:16], uint32(dataSize))

	// Write data
	offset := walHeaderSize
	binary.BigEndian.PutUint32(buf[offset:offset+4], uint32(keyLen))
	offset += 4
	copy(buf[offset:offset+keyLen], entry.Key)
	offset += keyLen

	if entry.Operation == OpPut {
		binary.BigEndian.PutUint32(buf[offset:offset+4], uint32(valueLen))
		offset += 4
		copy(buf[offset:offset+valueLen], entry.Value)
		offset += valueLen
	}

	// Calculate and write checksum (excludes the checksum field itself)
	checksumData := buf[:totalSize-walChecksumSize]
	checksum := crc32.ChecksumIEEE(checksumData)
	binary.BigEndian.PutUint32(buf[offset:offset+4], checksum)

	return buf, nil
}

func (w *WAL) rotateSegment() error {
	// Flush and close current segment
	if w.currentWriter != nil {
		if err := w.currentWriter.Flush(); err != nil {
			return err
		}
	}

	if w.currentFile != nil {
		if err := w.currentFile.Sync(); err != nil {
			return err
		}
		if err := w.currentFile.Close(); err != nil {
			return err
		}
	}

	return w.createNewSegment(w.currentIndex + 1)
}

func (w *WAL) Replay() ([]*WALEntry, error) {
	segments, err := w.listSegments()
	if err != nil {
		return nil, err
	}

	var entries []*WALEntry

	for _, index := range segments {
		segmentEntries, err := w.readSegment(index)
		if err != nil {
			return nil, fmt.Errorf("failed to read segment %d: %w", index, err)
		}
		entries = append(entries, segmentEntries...)
	}

	return entries, nil
}

func (w *WAL) readSegment(index uint64) ([]*WALEntry, error) {
	path := w.segmentPath(index)

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	var entries []*WALEntry

	for {
		entry, err := w.readEntry(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Log corruption but continue
			fmt.Printf("WARNING: Corrupt entry in segment %d: %v\n", index, err)
			continue
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

func (w *WAL) readEntry(reader *bufio.Reader) (*WALEntry, error) {
	// Read header
	header := make([]byte, walHeaderSize)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}

	// Validate magic bytes
	if header[0] != walMagic1 || header[1] != walMagic2 {
		return nil, fmt.Errorf("invalid magic bytes")
	}

	// Parse header
	version := header[2]
	if version != walVersion {
		return nil, fmt.Errorf("unsupported version: %d", version)
	}

	operation := header[3]
	timestamp := int64(binary.BigEndian.Uint64(header[4:12]))
	dataSize := binary.BigEndian.Uint32(header[12:16])

	// Read data
	data := make([]byte, dataSize)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, err
	}

	// Read checksum
	checksumBytes := make([]byte, walChecksumSize)
	if _, err := io.ReadFull(reader, checksumBytes); err != nil {
		return nil, err
	}
	expectedChecksum := binary.BigEndian.Uint32(checksumBytes)

	// Verify checksum
	checksumData := make([]byte, walHeaderSize+int(dataSize))
	copy(checksumData, header)
	copy(checksumData[walHeaderSize:], data)
	actualChecksum := crc32.ChecksumIEEE(checksumData)

	if actualChecksum != expectedChecksum {
		return nil, fmt.Errorf("checksum mismatch: expected %d, got %d", expectedChecksum, actualChecksum)
	}

	// Parse data
	entry := &WALEntry{
		Operation: operation,
		Timestamp: time.Unix(0, timestamp),
	}

	offset := 0
	keyLen := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4
	entry.Key = string(data[offset : offset+int(keyLen)])
	offset += int(keyLen)

	if operation == OpPut {
		valueLen := binary.BigEndian.Uint32(data[offset : offset+4])
		offset += 4
		entry.Value = data[offset : offset+int(valueLen)]
	}

	return entry, nil
}

func (w *WAL) listSegments() ([]uint64, error) {
	files, err := os.ReadDir(w.dir)
	if err != nil {
		return nil, err
	}

	var indices []uint64
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		name := file.Name()
		if !strings.HasSuffix(name, ".wal") {
			continue
		}

		// Parse index from filename (e.g., "000000001.wal" -> 1)
		indexStr := strings.TrimSuffix(name, ".wal")
		index, err := strconv.ParseUint(indexStr, 10, 64)
		if err != nil {
			continue
		}

		indices = append(indices, index)
	}

	sort.Slice(indices, func(i, j int) bool {
		return indices[i] < indices[j]
	})

	return indices, nil
}

func (w *WAL) segmentPath(index uint64) string {
	filename := fmt.Sprintf(walFilePattern, index)
	return filepath.Join(w.dir, filename)
}

func (w *WAL) DeleteSegmentsBefore(beforeIndex uint64) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	segments, err := w.listSegments()
	if err != nil {
		return err
	}

	for _, index := range segments {
		if index >= beforeIndex {
			break
		}

		path := w.segmentPath(index)
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("failed to delete segment %d: %w", index, err)
		}
	}

	return nil
}

func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.currentWriter != nil {
		if err := w.currentWriter.Flush(); err != nil {
			return err
		}
	}

	if w.currentFile != nil {
		if err := w.currentFile.Sync(); err != nil {
			return err
		}
		if err := w.currentFile.Close(); err != nil {
			return err
		}
	}

	return nil
}

func (w *WAL) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.currentWriter != nil {
		if err := w.currentWriter.Flush(); err != nil {
			return err
		}
	}

	if w.currentFile != nil {
		return w.currentFile.Sync()
	}

	return nil
}
