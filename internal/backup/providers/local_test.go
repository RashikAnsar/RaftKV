package providers

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/RashikAnsar/raftkv/internal/backup"
)

func TestLocalProvider_Upload(t *testing.T) {
	tmpDir := t.TempDir()
	provider, err := NewLocalProvider(tmpDir)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	key := "test-backup.dat"
	data := []byte("test backup data")
	metadata := map[string]string{
		"node_id":    "node-1",
		"cluster_id": "test-cluster",
	}

	err = provider.Upload(ctx, key, bytes.NewReader(data), metadata)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	// Verify file exists
	filePath := filepath.Join(tmpDir, key)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatalf("file was not created: %s", filePath)
	}

	// Verify metadata file exists
	metadataPath := filePath + ".meta"
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		t.Fatalf("metadata file was not created: %s", metadataPath)
	}

	// Verify content
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read uploaded file: %v", err)
	}
	if !bytes.Equal(content, data) {
		t.Fatalf("content mismatch: got %s, want %s", content, data)
	}
}

func TestLocalProvider_Download(t *testing.T) {
	tmpDir := t.TempDir()
	provider, err := NewLocalProvider(tmpDir)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	key := "test-backup.dat"
	data := []byte("test backup data")

	// Upload first
	err = provider.Upload(ctx, key, bytes.NewReader(data), nil)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	// Download
	reader, err := provider.Download(ctx, key)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	defer reader.Close()

	// Verify content
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read downloaded data: %v", err)
	}
	if !bytes.Equal(content, data) {
		t.Fatalf("content mismatch: got %s, want %s", content, data)
	}
}

func TestLocalProvider_Download_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	provider, err := NewLocalProvider(tmpDir)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	_, err = provider.Download(ctx, "nonexistent.dat")
	if err != backup.ErrObjectNotFound {
		t.Fatalf("expected ErrObjectNotFound, got %v", err)
	}
}

func TestLocalProvider_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	provider, err := NewLocalProvider(tmpDir)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	key := "test-backup.dat"
	data := []byte("test backup data")

	// Upload first
	err = provider.Upload(ctx, key, bytes.NewReader(data), nil)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	// Delete
	err = provider.Delete(ctx, key)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	// Verify file deleted
	filePath := filepath.Join(tmpDir, key)
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("file still exists after delete: %s", filePath)
	}

	// Verify metadata deleted
	metadataPath := filePath + ".meta"
	if _, err := os.Stat(metadataPath); !os.IsNotExist(err) {
		t.Fatalf("metadata file still exists after delete: %s", metadataPath)
	}
}

func TestLocalProvider_Exists(t *testing.T) {
	tmpDir := t.TempDir()
	provider, err := NewLocalProvider(tmpDir)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	key := "test-backup.dat"
	data := []byte("test backup data")

	// Check non-existent file
	exists, err := provider.Exists(ctx, key)
	if err != nil {
		t.Fatalf("exists check failed: %v", err)
	}
	if exists {
		t.Fatalf("file should not exist yet")
	}

	// Upload
	err = provider.Upload(ctx, key, bytes.NewReader(data), nil)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	// Check existing file
	exists, err = provider.Exists(ctx, key)
	if err != nil {
		t.Fatalf("exists check failed: %v", err)
	}
	if !exists {
		t.Fatalf("file should exist")
	}
}

func TestLocalProvider_List(t *testing.T) {
	tmpDir := t.TempDir()
	provider, err := NewLocalProvider(tmpDir)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()

	// Upload multiple files
	files := []string{
		"backups/backup-1.dat",
		"backups/backup-2.dat",
		"backups/backup-3.dat",
		"other/file.dat",
	}

	for _, key := range files {
		err = provider.Upload(ctx, key, bytes.NewReader([]byte("data")), nil)
		if err != nil {
			t.Fatalf("upload failed for %s: %v", key, err)
		}
	}

	// List with prefix
	objects, err := provider.List(ctx, "backups/")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	if len(objects) != 3 {
		t.Fatalf("expected 3 objects, got %d", len(objects))
	}

	// Verify keys
	expectedKeys := map[string]bool{
		"backups/backup-1.dat": true,
		"backups/backup-2.dat": true,
		"backups/backup-3.dat": true,
	}

	for _, obj := range objects {
		if !expectedKeys[obj.Key] {
			t.Fatalf("unexpected key: %s", obj.Key)
		}
	}
}

func TestLocalProvider_GetMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	provider, err := NewLocalProvider(tmpDir)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	key := "test-backup.dat"
	data := []byte("test backup data")
	metadata := map[string]string{
		"node_id":    "node-1",
		"cluster_id": "test-cluster",
		"version":    "1.0.0",
	}

	// Upload with metadata
	err = provider.Upload(ctx, key, bytes.NewReader(data), metadata)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	// Get metadata
	retrievedMetadata, err := provider.GetMetadata(ctx, key)
	if err != nil {
		t.Fatalf("get metadata failed: %v", err)
	}

	// Verify metadata
	for k, v := range metadata {
		if retrievedMetadata[k] != v {
			t.Fatalf("metadata mismatch for key %s: got %s, want %s", k, retrievedMetadata[k], v)
		}
	}
}

func TestLocalProvider_GetMetadata_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	provider, err := NewLocalProvider(tmpDir)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	metadata, err := provider.GetMetadata(ctx, "nonexistent.dat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// LocalProvider returns empty map instead of error for non-existent files
	if len(metadata) != 0 {
		t.Fatalf("expected empty metadata, got %v", metadata)
	}
}

func TestLocalProvider_UploadLargeFile(t *testing.T) {
	tmpDir := t.TempDir()
	provider, err := NewLocalProvider(tmpDir)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	key := "large-backup.dat"

	// Create 10MB of data
	data := make([]byte, 10*1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	// Upload
	err = provider.Upload(ctx, key, bytes.NewReader(data), nil)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	// Download and verify
	reader, err := provider.Download(ctx, key)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	defer reader.Close()

	downloaded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read downloaded data: %v", err)
	}

	if !bytes.Equal(downloaded, data) {
		t.Fatalf("large file content mismatch")
	}
}
