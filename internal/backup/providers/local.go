package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/RashikAnsar/raftkv/internal/backup"
)

// LocalProvider implements StorageProvider for local filesystem storage
// Useful for development, testing, and on-premise deployments
type LocalProvider struct {
	basePath string
}

// NewLocalProvider creates a new local filesystem storage provider
func NewLocalProvider(basePath string) (*LocalProvider, error) {
	if basePath == "" {
		return nil, fmt.Errorf("base path cannot be empty")
	}

	// Create base directory if it doesn't exist
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	return &LocalProvider{
		basePath: basePath,
	}, nil
}

// Upload stores data in the local filesystem
func (p *LocalProvider) Upload(ctx context.Context, key string, data io.Reader, metadata map[string]string) error {
	filePath := filepath.Join(p.basePath, key)

	// Create parent directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create the file
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Copy data to file
	if _, err := io.Copy(file, data); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	// Store metadata in a separate file
	if len(metadata) > 0 {
		metadataPath := filePath + ".meta"
		if err := p.saveMetadata(metadataPath, metadata); err != nil {
			return fmt.Errorf("failed to save metadata: %w", err)
		}
	}

	return nil
}

// Download retrieves data from the local filesystem
func (p *LocalProvider) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	filePath := filepath.Join(p.basePath, key)

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, backup.ErrObjectNotFound
		}
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	return file, nil
}

// Delete removes a file from the local filesystem
func (p *LocalProvider) Delete(ctx context.Context, key string) error {
	filePath := filepath.Join(p.basePath, key)

	// Delete the main file
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	// Delete metadata file if it exists
	metadataPath := filePath + ".meta"
	if err := os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
		// Non-fatal error, just log it
		return nil
	}

	return nil
}

// List returns all objects matching the prefix
func (p *LocalProvider) List(ctx context.Context, prefix string) ([]backup.ObjectInfo, error) {
	var objects []backup.ObjectInfo

	searchPath := filepath.Join(p.basePath, prefix)
	searchDir := searchPath
	if !strings.HasSuffix(searchPath, "/") {
		searchDir = filepath.Dir(searchPath)
	}

	err := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and metadata files
		if info.IsDir() || strings.HasSuffix(path, ".meta") {
			return nil
		}

		// Get relative path from base
		relPath, err := filepath.Rel(p.basePath, path)
		if err != nil {
			return err
		}

		// Check if path matches prefix
		if !strings.HasPrefix(relPath, prefix) {
			return nil
		}

		// Load metadata if exists
		metadata := make(map[string]string)
		metadataPath := path + ".meta"
		if _, err := os.Stat(metadataPath); err == nil {
			loadedMeta, _ := p.loadMetadata(metadataPath)
			metadata = loadedMeta
		}

		objects = append(objects, backup.ObjectInfo{
			Key:          relPath,
			Size:         info.Size(),
			LastModified: info.ModTime(),
			ETag:         fmt.Sprintf("%d-%d", info.Size(), info.ModTime().Unix()),
			Metadata:     metadata,
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	return objects, nil
}

// Exists checks if a file exists
func (p *LocalProvider) Exists(ctx context.Context, key string) (bool, error) {
	filePath := filepath.Join(p.basePath, key)
	_, err := os.Stat(filePath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// GetMetadata retrieves metadata for an object
func (p *LocalProvider) GetMetadata(ctx context.Context, key string) (map[string]string, error) {
	filePath := filepath.Join(p.basePath, key)
	metadataPath := filePath + ".meta"

	metadata, err := p.loadMetadata(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}

	return metadata, nil
}

// Close cleans up resources (no-op for local provider)
func (p *LocalProvider) Close() error {
	return nil
}

// Helper methods

func (p *LocalProvider) saveMetadata(path string, metadata map[string]string) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func (p *LocalProvider) loadMetadata(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var metadata map[string]string
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}

	return metadata, nil
}
