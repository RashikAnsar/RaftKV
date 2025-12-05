package backup

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
)

// RestoreManager handles restore operations
type RestoreManager struct {
	provider   StorageProvider
	catalog    *BackupCatalog
	compressor Compressor
	encryptor  Encryptor
}

// NewRestoreManager creates a new restore manager
func NewRestoreManager(provider StorageProvider, compressor Compressor, encryptor Encryptor) *RestoreManager {
	return &RestoreManager{
		provider:   provider,
		catalog:    NewBackupCatalog(provider, "backups/"),
		compressor: compressor,
		encryptor:  encryptor,
	}
}

// RestoreBackup restores a backup to a writer
func (r *RestoreManager) RestoreBackup(ctx context.Context, backupID string, writer io.Writer) error {
	// Get backup metadata
	metadata, err := r.catalog.GetMetadata(ctx, backupID)
	if err != nil {
		return fmt.Errorf("failed to get backup metadata: %w", err)
	}

	if metadata.Status != "complete" {
		return fmt.Errorf("backup is not complete: %s", metadata.Status)
	}

	// Download backup data
	dataKey := r.catalog.DataKey(backupID)
	reader, err := r.provider.Download(ctx, dataKey)
	if err != nil {
		return fmt.Errorf("failed to download backup: %w", err)
	}
	defer reader.Close()

	// Create verification reader
	verifyReader := &verifyChecksumReader{
		reader:           reader,
		hash:             sha256.New(),
		expectedChecksum: metadata.Checksum,
	}

	// Create pipeline: download -> verify -> decrypt -> decompress -> write
	pipelineReader := io.Reader(verifyReader)

	// Apply decryption
	if metadata.Encrypted {
		decryptedReader, err := r.encryptor.Decrypt(pipelineReader)
		if err != nil {
			return fmt.Errorf("decryption failed: %w", err)
		}
		defer decryptedReader.Close()
		pipelineReader = decryptedReader
	}

	// Apply decompression
	if metadata.Compressed {
		decompressedReader, err := r.compressor.Decompress(pipelineReader)
		if err != nil {
			return fmt.Errorf("decompression failed: %w", err)
		}
		defer decompressedReader.Close()
		pipelineReader = decompressedReader
	}

	// Write restored data
	if _, err := io.Copy(writer, pipelineReader); err != nil {
		return fmt.Errorf("failed to write restored data: %w", err)
	}

	// Verify checksum
	if err := verifyReader.Verify(); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	return nil
}

// RestoreToReader returns a reader for the restored backup data
func (r *RestoreManager) RestoreToReader(ctx context.Context, backupID string) (io.ReadCloser, *BackupMetadata, error) {
	// Get backup metadata
	metadata, err := r.catalog.GetMetadata(ctx, backupID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get backup metadata: %w", err)
	}

	if metadata.Status != "complete" {
		return nil, nil, fmt.Errorf("backup is not complete: %s", metadata.Status)
	}

	// Download backup data
	dataKey := r.catalog.DataKey(backupID)
	reader, err := r.provider.Download(ctx, dataKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to download backup: %w", err)
	}

	// Create pipeline
	pipelineReader := io.ReadCloser(reader)

	// Apply decryption
	if metadata.Encrypted {
		decryptedReader, err := r.encryptor.Decrypt(pipelineReader)
		if err != nil {
			pipelineReader.Close()
			return nil, nil, fmt.Errorf("decryption failed: %w", err)
		}
		pipelineReader = decryptedReader
	}

	// Apply decompression
	if metadata.Compressed {
		decompressedReader, err := r.compressor.Decompress(pipelineReader)
		if err != nil {
			pipelineReader.Close()
			return nil, nil, fmt.Errorf("decompression failed: %w", err)
		}
		pipelineReader = decompressedReader
	}

	return pipelineReader, metadata, nil
}

// ListAvailableBackups returns all backups that can be restored
func (r *RestoreManager) ListAvailableBackups(ctx context.Context) ([]*BackupMetadata, error) {
	backups, err := r.catalog.ListBackups(ctx)
	if err != nil {
		return nil, err
	}

	// Filter only complete backups
	var available []*BackupMetadata
	for _, backup := range backups {
		if backup.Status == "complete" {
			available = append(available, backup)
		}
	}

	return available, nil
}

// VerifyBackupIntegrity verifies a backup can be restored successfully
func (r *RestoreManager) VerifyBackupIntegrity(ctx context.Context, backupID string) error {
	// Get metadata
	metadata, err := r.catalog.GetMetadata(ctx, backupID)
	if err != nil {
		return fmt.Errorf("failed to get backup metadata: %w", err)
	}

	// Download and verify
	dataKey := r.catalog.DataKey(backupID)
	reader, err := r.provider.Download(ctx, dataKey)
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

// GetLatestBackup returns the most recent complete backup
func (r *RestoreManager) GetLatestBackup(ctx context.Context) (*BackupMetadata, error) {
	backups, err := r.ListAvailableBackups(ctx)
	if err != nil {
		return nil, err
	}

	if len(backups) == 0 {
		return nil, ErrBackupNotFound
	}

	// Find latest by timestamp
	latest := backups[0]
	for _, backup := range backups[1:] {
		if backup.Timestamp.After(latest.Timestamp) {
			latest = backup
		}
	}

	return latest, nil
}

// GetBackupByRaftIndex finds the backup closest to a specific Raft index
func (r *RestoreManager) GetBackupByRaftIndex(ctx context.Context, raftIndex uint64) (*BackupMetadata, error) {
	backups, err := r.ListAvailableBackups(ctx)
	if err != nil {
		return nil, err
	}

	if len(backups) == 0 {
		return nil, ErrBackupNotFound
	}

	// Find closest backup not exceeding the target index
	var closest *BackupMetadata
	var minDiff uint64 = ^uint64(0) // Max uint64

	for _, backup := range backups {
		if backup.RaftIndex <= raftIndex {
			diff := raftIndex - backup.RaftIndex
			if diff < minDiff {
				minDiff = diff
				closest = backup
			}
		}
	}

	if closest == nil {
		return nil, fmt.Errorf("no backup found at or before Raft index %d", raftIndex)
	}

	return closest, nil
}

// verifyChecksumReader wraps a reader and verifies checksum after reading
type verifyChecksumReader struct {
	reader           io.Reader
	hash             io.Writer
	expectedChecksum string
	verified         bool
}

func (r *verifyChecksumReader) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	if n > 0 {
		r.hash.Write(p[:n])
	}
	return n, err
}

func (r *verifyChecksumReader) Verify() error {
	if r.verified {
		return nil
	}

	actualChecksum := fmt.Sprintf("%x", r.hash.(interface{ Sum([]byte) []byte }).Sum(nil))
	if actualChecksum != r.expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", r.expectedChecksum, actualChecksum)
	}

	r.verified = true
	return nil
}
