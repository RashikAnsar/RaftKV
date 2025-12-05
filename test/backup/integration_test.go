package backup_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"path/filepath"
	"testing"

	"github.com/RashikAnsar/raftkv/internal/backup"
	"github.com/RashikAnsar/raftkv/internal/backup/providers"
)

func setupTest(t *testing.T) (*backup.BackupManager, *backup.RestoreManager, backup.StorageProvider) {
	tmpDir := t.TempDir()

	// Create local provider
	provider, err := providers.NewLocalProvider(filepath.Join(tmpDir, "backups"))
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Generate encryption key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// Create backup config
	config := backup.BackupConfig{
		CompressionType:  backup.CompressionTypeGzip,
		CompressionLevel: 6,
		EncryptionType:   backup.EncryptionTypeAES256GCM,
		EncryptionKey:    key,
		NodeID:           "node-1",
		ClusterID:        "test-cluster",
		Version:          "1.0.0",
		RetentionDays:    30,
		MaxBackups:       10,
		BackupPrefix:     "backups/",
	}

	// Create backup manager
	manager, err := backup.NewBackupManager(provider, config)
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}

	// Create compressor and encryptor for restore
	compressor, err := backup.NewCompressor(config.CompressionType, config.CompressionLevel)
	if err != nil {
		t.Fatalf("failed to create compressor: %v", err)
	}
	encryptor, err := backup.NewEncryptor(config.EncryptionType, config.EncryptionKey)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	// Create restore manager
	restorer := backup.NewRestoreManager(provider, compressor, encryptor)

	return manager, restorer, provider
}

func TestBackupRestore_EndToEnd(t *testing.T) {
	manager, restorer, provider := setupTest(t)
	defer manager.Close()
	defer provider.Close()

	ctx := context.Background()

	// Test data
	originalData := []byte("This is the original database snapshot that will be backed up and restored. " +
		"It contains important data that must be preserved through the backup and restore process.")

	// Create backup
	metadata, err := manager.CreateBackup(ctx, bytes.NewReader(originalData), 100, 5)
	if err != nil {
		t.Fatalf("backup creation failed: %v", err)
	}

	t.Logf("Backup created: ID=%s, Size=%d, Compressed=%v, Encrypted=%v",
		metadata.ID, metadata.Size, metadata.Compressed, metadata.Encrypted)

	// List backups
	backups, err := manager.ListBackups(ctx)
	if err != nil {
		t.Fatalf("list backups failed: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(backups))
	}

	// Verify backup
	err = manager.VerifyBackup(ctx, metadata.ID)
	if err != nil {
		t.Fatalf("backup verification failed: %v", err)
	}
	t.Logf("Backup verified successfully")

	// Restore backup
	var restored bytes.Buffer
	err = restorer.RestoreBackup(ctx, metadata.ID, &restored)
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}

	// Verify restored data matches original
	if !bytes.Equal(restored.Bytes(), originalData) {
		t.Fatalf("restored data doesn't match original.\nOriginal: %s\nRestored: %s",
			originalData, restored.Bytes())
	}

	t.Logf("Backup and restore successful: %d bytes", len(originalData))
}

func TestBackupRestore_WithoutEncryption(t *testing.T) {
	tmpDir := t.TempDir()

	provider, err := providers.NewLocalProvider(filepath.Join(tmpDir, "backups"))
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close()

	config := backup.BackupConfig{
		CompressionType:  backup.CompressionTypeGzip,
		CompressionLevel: 6,
		EncryptionType:   backup.EncryptionTypeNone,
		NodeID:           "node-1",
		ClusterID:        "test-cluster",
		Version:          "1.0.0",
		BackupPrefix:     "backups/",
	}

	manager, err := backup.NewBackupManager(provider, config)
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}
	defer manager.Close()

	compressor, _ := backup.NewCompressor(config.CompressionType, config.CompressionLevel)
	encryptor, _ := backup.NewEncryptor(config.EncryptionType, config.EncryptionKey)
	restorer := backup.NewRestoreManager(provider, compressor, encryptor)

	ctx := context.Background()
	originalData := []byte("Unencrypted backup data")

	metadata, err := manager.CreateBackup(ctx, bytes.NewReader(originalData), 50, 3)
	if err != nil {
		t.Fatalf("backup creation failed: %v", err)
	}

	if metadata.Encrypted {
		t.Fatalf("backup should not be encrypted")
	}

	var restored bytes.Buffer
	err = restorer.RestoreBackup(ctx, metadata.ID, &restored)
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}

	if !bytes.Equal(restored.Bytes(), originalData) {
		t.Fatalf("restored data doesn't match original")
	}
}

func TestBackupRestore_LargeData(t *testing.T) {
	manager, restorer, provider := setupTest(t)
	defer manager.Close()
	defer provider.Close()

	ctx := context.Background()

	// Create 10MB of compressible data
	pattern := []byte("This is a repeating pattern for compression testing. ")
	originalData := bytes.Repeat(pattern, 200000) // ~10MB

	metadata, err := manager.CreateBackup(ctx, bytes.NewReader(originalData), 1000, 10)
	if err != nil {
		t.Fatalf("backup creation failed: %v", err)
	}

	t.Logf("Large backup: original=%d, compressed=%d, ratio=%.2f%%",
		len(originalData), metadata.CompressedSize,
		float64(metadata.CompressedSize)/float64(len(originalData))*100)

	var restored bytes.Buffer
	err = restorer.RestoreBackup(ctx, metadata.ID, &restored)
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}

	if !bytes.Equal(restored.Bytes(), originalData) {
		t.Fatalf("restored data doesn't match original")
	}
}

func TestBackupManager_MultipleBackups(t *testing.T) {
	manager, restorer, provider := setupTest(t)
	defer manager.Close()
	defer provider.Close()

	ctx := context.Background()

	// Create multiple backups
	backupData := map[string][]byte{
		"backup1": []byte("First backup snapshot"),
		"backup2": []byte("Second backup snapshot with more data"),
		"backup3": []byte("Third backup snapshot with even more data here"),
	}

	createdBackups := make(map[string]string) // name -> backupID

	for name, data := range backupData {
		metadata, err := manager.CreateBackup(ctx, bytes.NewReader(data), 100, 5)
		if err != nil {
			t.Fatalf("backup creation failed for %s: %v", name, err)
		}
		createdBackups[name] = metadata.ID
	}

	// List backups
	backups, err := manager.ListBackups(ctx)
	if err != nil {
		t.Fatalf("list backups failed: %v", err)
	}
	if len(backups) != 3 {
		t.Fatalf("expected 3 backups, got %d", len(backups))
	}

	// Restore each backup and verify
	for name, backupID := range createdBackups {
		var restored bytes.Buffer
		err = restorer.RestoreBackup(ctx, backupID, &restored)
		if err != nil {
			t.Fatalf("restore failed for %s: %v", name, err)
		}

		if !bytes.Equal(restored.Bytes(), backupData[name]) {
			t.Fatalf("restored data doesn't match original for %s", name)
		}
	}

	t.Logf("Successfully created and restored %d backups", len(backupData))
}

func TestBackupManager_DeleteBackup(t *testing.T) {
	manager, _, provider := setupTest(t)
	defer manager.Close()
	defer provider.Close()

	ctx := context.Background()

	// Create backup
	data := []byte("backup to be deleted")
	metadata, err := manager.CreateBackup(ctx, bytes.NewReader(data), 10, 1)
	if err != nil {
		t.Fatalf("backup creation failed: %v", err)
	}

	// Verify backup exists
	backups, err := manager.ListBackups(ctx)
	if err != nil {
		t.Fatalf("list backups failed: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(backups))
	}

	// Delete backup
	err = manager.DeleteBackup(ctx, metadata.ID)
	if err != nil {
		t.Fatalf("delete backup failed: %v", err)
	}

	// Verify backup deleted
	backups, err = manager.ListBackups(ctx)
	if err != nil {
		t.Fatalf("list backups failed: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("expected 0 backups after deletion, got %d", len(backups))
	}
}

func TestBackupManager_PruneOldBackups(t *testing.T) {
	tmpDir := t.TempDir()

	provider, err := providers.NewLocalProvider(filepath.Join(tmpDir, "backups"))
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close()

	config := backup.BackupConfig{
		CompressionType: backup.CompressionTypeNone,
		EncryptionType:  backup.EncryptionTypeNone,
		NodeID:          "node-1",
		ClusterID:       "test-cluster",
		Version:         "1.0.0",
		RetentionDays:   0, // Delete immediately for testing
		MaxBackups:      2, // Keep only 2
		BackupPrefix:    "backups/",
	}

	manager, err := backup.NewBackupManager(provider, config)
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// Create 5 backups
	for i := 0; i < 5; i++ {
		data := []byte("backup data")
		_, err := manager.CreateBackup(ctx, bytes.NewReader(data), uint64(i*10), 1)
		if err != nil {
			t.Fatalf("backup creation failed: %v", err)
		}
	}

	// Verify 5 backups exist
	backups, err := manager.ListBackups(ctx)
	if err != nil {
		t.Fatalf("list backups failed: %v", err)
	}
	if len(backups) != 5 {
		t.Fatalf("expected 5 backups, got %d", len(backups))
	}

	// Prune excess backups (keep only 2)
	deleted, err := manager.PruneExcessBackups(ctx)
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}

	if deleted != 3 {
		t.Fatalf("expected 3 deleted backups, got %d", deleted)
	}

	// Verify only 2 backups remain
	backups, err = manager.ListBackups(ctx)
	if err != nil {
		t.Fatalf("list backups failed: %v", err)
	}
	if len(backups) != 2 {
		t.Fatalf("expected 2 backups after pruning, got %d", len(backups))
	}

	t.Logf("Pruned %d backups, %d remaining", deleted, len(backups))
}
