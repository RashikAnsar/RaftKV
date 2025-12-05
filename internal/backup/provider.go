package backup

import (
	"context"
	"io"
	"time"
)

// StorageProvider defines the interface for backup storage backends
// This allows RaftKV to support multiple storage providers (S3, GCS, Azure, MinIO, Local)
type StorageProvider interface {
	// Upload uploads data to the storage provider
	Upload(ctx context.Context, key string, data io.Reader, metadata map[string]string) error

	// Download retrieves data from the storage provider
	Download(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes an object from storage
	Delete(ctx context.Context, key string) error

	// List returns objects matching the prefix
	List(ctx context.Context, prefix string) ([]ObjectInfo, error)

	// Exists checks if an object exists
	Exists(ctx context.Context, key string) (bool, error)

	// GetMetadata retrieves metadata for an object
	GetMetadata(ctx context.Context, key string) (map[string]string, error)

	// Close cleans up any resources used by the provider
	Close() error
}

// ObjectInfo contains information about a stored object
type ObjectInfo struct {
	Key          string            // Object key/path
	Size         int64             // Size in bytes
	LastModified time.Time         // Last modification time
	ETag         string            // ETag for versioning
	Metadata     map[string]string // Custom metadata
}

// ProviderType represents the type of storage provider
type ProviderType string

const (
	ProviderTypeS3    ProviderType = "s3"
	ProviderTypeGCS   ProviderType = "gcs"
	ProviderTypeAzure ProviderType = "azure"
	ProviderTypeMinIO ProviderType = "minio"
	ProviderTypeLocal ProviderType = "local"
)

// ProviderConfig contains configuration for creating a storage provider
type ProviderConfig struct {
	Type ProviderType

	// S3/MinIO configuration
	S3Bucket   string
	S3Region   string
	S3Endpoint string // Optional, for MinIO or S3-compatible storage
	S3AccessKey string
	S3SecretKey string

	// GCS configuration
	GCSBucket     string
	GCSProject    string
	GCSCredsFile  string // Path to service account JSON

	// Azure configuration
	AzureContainer string
	AzureAccount   string
	AzureKey       string

	// Local filesystem configuration
	LocalPath string
}
