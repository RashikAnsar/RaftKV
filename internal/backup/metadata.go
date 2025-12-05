package backup

import (
	"context"
	"encoding/json"
	"io"
	"time"
)

// BackupMetadata contains information about a backup
type BackupMetadata struct {
	ID              string    `json:"id"`               // Unique backup ID
	Timestamp       time.Time `json:"timestamp"`        // When the backup was created
	RaftIndex       uint64    `json:"raft_index"`       // Raft log index at backup time
	RaftTerm        uint64    `json:"raft_term"`        // Raft term at backup time
	Size            int64     `json:"size"`             // Backup size in bytes
	CompressedSize  int64     `json:"compressed_size"`  // Size after compression
	Compressed      bool      `json:"compressed"`       // Whether backup is compressed
	Encrypted       bool      `json:"encrypted"`        // Whether backup is encrypted
	CompressionType string    `json:"compression_type"` // gzip, zstd, etc.
	EncryptionType  string    `json:"encryption_type"`  // aes-256-gcm, etc.
	NodeID          string    `json:"node_id"`          // Node that created the backup
	ClusterID       string    `json:"cluster_id"`       // Cluster identifier
	Version         string    `json:"version"`          // RaftKV version
	Checksum        string    `json:"checksum"`         // SHA256 checksum
	KeyCount        int64     `json:"key_count"`        // Number of keys in backup
	Status          string    `json:"status"`           // complete, partial, failed
	Error           string    `json:"error,omitempty"`  // Error message if failed
}

// ToJSON serializes metadata to JSON
func (m *BackupMetadata) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

// FromJSON deserializes metadata from JSON
func FromJSON(data []byte) (*BackupMetadata, error) {
	var m BackupMetadata
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ToMap converts metadata to a map for storage provider metadata
func (m *BackupMetadata) ToMap() map[string]string {
	return map[string]string{
		"id":               m.ID,
		"timestamp":        m.Timestamp.Format(time.RFC3339),
		"raft_index":       string(rune(m.RaftIndex)),
		"raft_term":        string(rune(m.RaftTerm)),
		"size":             string(rune(m.Size)),
		"compressed_size":  string(rune(m.CompressedSize)),
		"compressed":       string(rune(boolToInt(m.Compressed))),
		"encrypted":        string(rune(boolToInt(m.Encrypted))),
		"compression_type": m.CompressionType,
		"encryption_type":  m.EncryptionType,
		"node_id":          m.NodeID,
		"cluster_id":       m.ClusterID,
		"version":          m.Version,
		"checksum":         m.Checksum,
		"key_count":        string(rune(m.KeyCount)),
		"status":           m.Status,
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// BackupCatalog manages a catalog of backups
type BackupCatalog struct {
	provider StorageProvider
	prefix   string // Catalog key prefix (e.g., "backups/")
}

// NewBackupCatalog creates a new backup catalog
func NewBackupCatalog(provider StorageProvider, prefix string) *BackupCatalog {
	if prefix == "" {
		prefix = "backups/"
	}
	return &BackupCatalog{
		provider: provider,
		prefix:   prefix,
	}
}

// SaveMetadata saves backup metadata to the catalog
func (c *BackupCatalog) SaveMetadata(ctx context.Context, metadata *BackupMetadata) error {
	key := c.metadataKey(metadata.ID)

	data, err := metadata.ToJSON()
	if err != nil {
		return err
	}

	reader := &bytesReader{data: data}
	return c.provider.Upload(ctx, key, reader, metadata.ToMap())
}

// GetMetadata retrieves backup metadata from the catalog
func (c *BackupCatalog) GetMetadata(ctx context.Context, backupID string) (*BackupMetadata, error) {
	key := c.metadataKey(backupID)

	rc, err := c.provider.Download(ctx, key)
	if err != nil {
		if err == ErrObjectNotFound {
			return nil, ErrBackupNotFound
		}
		return nil, err
	}
	defer rc.Close()

	data := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
	}

	return FromJSON(data)
}

// ListBackups returns all backups in the catalog
func (c *BackupCatalog) ListBackups(ctx context.Context) ([]*BackupMetadata, error) {
	objects, err := c.provider.List(ctx, c.prefix)
	if err != nil {
		return nil, err
	}

	var backups []*BackupMetadata
	for _, obj := range objects {
		// Only list metadata files
		if !isMetadataKey(obj.Key) {
			continue
		}

		metadata, err := c.GetMetadata(ctx, extractBackupID(obj.Key))
		if err != nil {
			// Skip invalid metadata files
			continue
		}
		backups = append(backups, metadata)
	}

	return backups, nil
}

// DeleteMetadata removes backup metadata from the catalog
func (c *BackupCatalog) DeleteMetadata(ctx context.Context, backupID string) error {
	key := c.metadataKey(backupID)
	return c.provider.Delete(ctx, key)
}

// metadataKey returns the storage key for backup metadata
func (c *BackupCatalog) metadataKey(backupID string) string {
	return c.prefix + backupID + "/metadata.json"
}

// dataKey returns the storage key for backup data
func (c *BackupCatalog) DataKey(backupID string) string {
	return c.prefix + backupID + "/data.db"
}

// isMetadataKey checks if a key is a metadata file
func isMetadataKey(key string) bool {
	return len(key) > 14 && key[len(key)-14:] == "/metadata.json"
}

// extractBackupID extracts the backup ID from a metadata key
func extractBackupID(key string) string {
	// Remove prefix and /metadata.json suffix
	start := 0
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			start = i + 1
			break
		}
	}
	end := len(key) - 14 // Remove /metadata.json
	if end > start {
		return key[start:end]
	}
	return key
}

// bytesReader wraps a byte slice as an io.Reader
type bytesReader struct {
	data []byte
	pos  int
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	if r.pos >= len(r.data) {
		return n, io.EOF
	}
	return n, nil
}
