package backup

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
)

// BackupManager handles backup operations
type BackupManager struct {
	provider   StorageProvider
	catalog    *BackupCatalog
	compressor Compressor
	encryptor  Encryptor
	config     BackupConfig
}

// BackupConfig contains backup configuration
type BackupConfig struct {
	CompressionType     CompressionType
	CompressionLevel    int
	EncryptionType      EncryptionType
	EncryptionKey       []byte
	NodeID              string
	ClusterID           string
	Version             string
	RetentionDays       int
	MaxBackups          int
	EnableIncremental   bool
	BackupPrefix        string
}

// NewBackupManager creates a new backup manager
func NewBackupManager(provider StorageProvider, config BackupConfig) (*BackupManager, error) {
	// Create compressor
	compressor, err := NewCompressor(config.CompressionType, config.CompressionLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to create compressor: %w", err)
	}

	// Create encryptor
	encryptor, err := NewEncryptor(config.EncryptionType, config.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create encryptor: %w", err)
	}

	// Create catalog
	prefix := config.BackupPrefix
	if prefix == "" {
		prefix = "backups/"
	}
	catalog := NewBackupCatalog(provider, prefix)

	return &BackupManager{
		provider:   provider,
		catalog:    catalog,
		compressor: compressor,
		encryptor:  encryptor,
		config:     config,
	}, nil
}

// CreateBackup creates a new backup from a data reader
func (m *BackupManager) CreateBackup(ctx context.Context, data io.Reader, raftIndex, raftTerm uint64) (*BackupMetadata, error) {
	// Generate backup ID
	backupID := uuid.New().String()

	// Create metadata
	metadata := &BackupMetadata{
		ID:              backupID,
		Timestamp:       time.Now(),
		RaftIndex:       raftIndex,
		RaftTerm:        raftTerm,
		Compressed:      m.config.CompressionType != CompressionTypeNone,
		Encrypted:       m.config.EncryptionType != EncryptionTypeNone,
		CompressionType: string(m.config.CompressionType),
		EncryptionType:  string(m.config.EncryptionType),
		NodeID:          m.config.NodeID,
		ClusterID:       m.config.ClusterID,
		Version:         m.config.Version,
		Status:          "in_progress",
	}

	// Create pipeline: data -> compress -> encrypt -> checksum -> upload
	reader := data

	// Apply compression
	if metadata.Compressed {
		compressedReader, err := m.compressor.Compress(reader)
		if err != nil {
			metadata.Status = "failed"
			metadata.Error = fmt.Sprintf("compression failed: %v", err)
			return metadata, fmt.Errorf("compression failed: %w", err)
		}
		reader = compressedReader
	}

	// Apply encryption
	if metadata.Encrypted {
		encryptedReader, err := m.encryptor.Encrypt(reader)
		if err != nil {
			metadata.Status = "failed"
			metadata.Error = fmt.Sprintf("encryption failed: %v", err)
			return metadata, fmt.Errorf("encryption failed: %w", err)
		}
		reader = encryptedReader
	}

	// Create checksum reader
	checksumReader := &checksumReader{
		reader: reader,
		hash:   sha256.New(),
	}

	// Upload backup data
	dataKey := m.catalog.DataKey(backupID)
	if err := m.provider.Upload(ctx, dataKey, checksumReader, metadata.ToMap()); err != nil {
		metadata.Status = "failed"
		metadata.Error = fmt.Sprintf("upload failed: %v", err)
		return metadata, fmt.Errorf("upload failed: %w", err)
	}

	// Finalize metadata
	metadata.Size = checksumReader.bytesRead
	metadata.CompressedSize = checksumReader.bytesRead
	metadata.Checksum = fmt.Sprintf("%x", checksumReader.hash.Sum(nil))
	metadata.Status = "complete"

	// Save metadata
	if err := m.catalog.SaveMetadata(ctx, metadata); err != nil {
		return metadata, fmt.Errorf("failed to save metadata: %w", err)
	}

	return metadata, nil
}

// ListBackups returns all available backups
func (m *BackupManager) ListBackups(ctx context.Context) ([]*BackupMetadata, error) {
	return m.catalog.ListBackups(ctx)
}

// GetBackup retrieves backup metadata
func (m *BackupManager) GetBackup(ctx context.Context, backupID string) (*BackupMetadata, error) {
	return m.catalog.GetMetadata(ctx, backupID)
}

// DeleteBackup removes a backup
func (m *BackupManager) DeleteBackup(ctx context.Context, backupID string) error {
	// Delete data
	dataKey := m.catalog.DataKey(backupID)
	if err := m.provider.Delete(ctx, dataKey); err != nil {
		return fmt.Errorf("failed to delete backup data: %w", err)
	}

	// Delete metadata
	if err := m.catalog.DeleteMetadata(ctx, backupID); err != nil {
		return fmt.Errorf("failed to delete metadata: %w", err)
	}

	return nil
}

// PruneOldBackups removes backups older than retention period
func (m *BackupManager) PruneOldBackups(ctx context.Context) (int, error) {
	if m.config.RetentionDays <= 0 {
		return 0, nil
	}

	backups, err := m.ListBackups(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list backups: %w", err)
	}

	cutoffTime := time.Now().AddDate(0, 0, -m.config.RetentionDays)
	deleted := 0

	for _, backup := range backups {
		if backup.Timestamp.Before(cutoffTime) {
			if err := m.DeleteBackup(ctx, backup.ID); err != nil {
				// Log error but continue
				continue
			}
			deleted++
		}
	}

	return deleted, nil
}

// PruneExcessBackups removes backups beyond max count
func (m *BackupManager) PruneExcessBackups(ctx context.Context) (int, error) {
	if m.config.MaxBackups <= 0 {
		return 0, nil
	}

	backups, err := m.ListBackups(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list backups: %w", err)
	}

	if len(backups) <= m.config.MaxBackups {
		return 0, nil
	}

	// Sort by timestamp (oldest first)
	sortBackupsByTimestamp(backups)

	deleted := 0
	excessCount := len(backups) - m.config.MaxBackups

	for i := 0; i < excessCount; i++ {
		if err := m.DeleteBackup(ctx, backups[i].ID); err != nil {
			// Log error but continue
			continue
		}
		deleted++
	}

	return deleted, nil
}

// VerifyBackup checks backup integrity
func (m *BackupManager) VerifyBackup(ctx context.Context, backupID string) error {
	metadata, err := m.GetBackup(ctx, backupID)
	if err != nil {
		return fmt.Errorf("failed to get backup metadata: %w", err)
	}

	// Download and verify checksum
	dataKey := m.catalog.DataKey(backupID)
	reader, err := m.provider.Download(ctx, dataKey)
	if err != nil {
		return fmt.Errorf("failed to download backup: %w", err)
	}
	defer reader.Close()

	// Calculate checksum
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return fmt.Errorf("failed to read backup: %w", err)
	}

	actualChecksum := fmt.Sprintf("%x", hash.Sum(nil))
	if actualChecksum != metadata.Checksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", metadata.Checksum, actualChecksum)
	}

	return nil
}

// Close releases resources
func (m *BackupManager) Close() error {
	return m.provider.Close()
}

// checksumReader wraps a reader and calculates checksum
type checksumReader struct {
	reader    io.Reader
	hash      interface {
		io.Writer
		Sum([]byte) []byte
	}
	bytesRead int64
}

func (r *checksumReader) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	if n > 0 {
		r.hash.Write(p[:n])
		r.bytesRead += int64(n)
	}
	return n, err
}

// sortBackupsByTimestamp sorts backups by timestamp (oldest first)
func sortBackupsByTimestamp(backups []*BackupMetadata) {
	// Simple bubble sort for small arrays
	n := len(backups)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if backups[j].Timestamp.After(backups[j+1].Timestamp) {
				backups[j], backups[j+1] = backups[j+1], backups[j]
			}
		}
	}
}
