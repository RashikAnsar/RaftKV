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
	Type ProviderType `json:"type" yaml:"type"`

	// S3/MinIO configuration
	S3Bucket    string `json:"s3_bucket,omitempty" yaml:"s3_bucket,omitempty"`
	S3Region    string `json:"s3_region,omitempty" yaml:"s3_region,omitempty"`
	S3Endpoint  string `json:"s3_endpoint,omitempty" yaml:"s3_endpoint,omitempty"` // Optional, for MinIO or S3-compatible storage
	S3AccessKey string `json:"s3_access_key,omitempty" yaml:"s3_access_key,omitempty"`
	S3SecretKey string `json:"s3_secret_key,omitempty" yaml:"s3_secret_key,omitempty"`

	// GCS configuration
	GCSBucket    string `json:"gcs_bucket,omitempty" yaml:"gcs_bucket,omitempty"`
	GCSProject   string `json:"gcs_project,omitempty" yaml:"gcs_project,omitempty"`
	GCSCredsFile string `json:"gcs_creds_file,omitempty" yaml:"gcs_creds_file,omitempty"` // Path to service account JSON

	// Azure configuration
	AzureContainer string `json:"azure_container,omitempty" yaml:"azure_container,omitempty"`
	AzureAccount   string `json:"azure_account,omitempty" yaml:"azure_account,omitempty"`
	AzureKey       string `json:"azure_key,omitempty" yaml:"azure_key,omitempty"`

	// Local filesystem configuration
	LocalPath string `json:"local_path,omitempty" yaml:"local_path,omitempty"`
}
