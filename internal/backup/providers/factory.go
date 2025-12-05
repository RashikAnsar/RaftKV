package providers

import (
	"context"

	"github.com/RashikAnsar/raftkv/internal/backup"
)

// NewStorageProvider creates a new storage provider based on the configuration
func NewStorageProvider(ctx context.Context, config backup.ProviderConfig) (backup.StorageProvider, error) {
	switch config.Type {
	case backup.ProviderTypeLocal:
		return NewLocalProvider(config.LocalPath)
	case backup.ProviderTypeS3:
		return NewS3Provider(ctx, config)
	case backup.ProviderTypeMinIO:
		// MinIO uses the same S3 provider with custom endpoint
		return NewS3Provider(ctx, config)
	case backup.ProviderTypeGCS:
		// TODO: Implement GCSProvider
		return nil, backup.ErrUnsupportedProvider
	case backup.ProviderTypeAzure:
		// TODO: Implement AzureProvider
		return nil, backup.ErrUnsupportedProvider
	default:
		return nil, backup.ErrUnsupportedProvider
	}
}
