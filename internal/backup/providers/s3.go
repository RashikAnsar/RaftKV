package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/RashikAnsar/raftkv/internal/backup"
)

// S3Provider implements StorageProvider for AWS S3
type S3Provider struct {
	client *s3.Client
	bucket string
}

// NewS3Provider creates a new AWS S3 storage provider
func NewS3Provider(ctx context.Context, cfg backup.ProviderConfig) (*S3Provider, error) {
	if cfg.S3Bucket == "" {
		return nil, fmt.Errorf("bucket name cannot be empty")
	}

	if cfg.S3Region == "" {
		return nil, fmt.Errorf("region cannot be empty")
	}

	// Load AWS configuration
	var awsCfg aws.Config
	var err error

	if cfg.S3AccessKey != "" && cfg.S3SecretKey != "" {
		// Use explicit credentials
		awsCfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(cfg.S3Region),
			config.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(
					cfg.S3AccessKey,
					cfg.S3SecretKey,
					"",
				),
			),
		)
	} else {
		// Use default credential chain (IAM role, env vars, etc.)
		awsCfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(cfg.S3Region),
		)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with custom options if needed
	var clientOptions []func(*s3.Options)

	if cfg.S3Endpoint != "" {
		clientOptions = append(clientOptions, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
			o.UsePathStyle = true // Always use path style for custom endpoints
		})
	}

	client := s3.NewFromConfig(awsCfg, clientOptions...)

	return &S3Provider{
		client: client,
		bucket: cfg.S3Bucket,
	}, nil
}

// Upload stores data in S3
func (p *S3Provider) Upload(ctx context.Context, key string, data io.Reader, metadata map[string]string) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(key),
		Body:   data,
	}

	// Add metadata
	if len(metadata) > 0 {
		input.Metadata = metadata
	}

	_, err := p.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	return nil
}

// Download retrieves data from S3
func (p *S3Provider) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(key),
	}

	result, err := p.client.GetObject(ctx, input)
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, backup.ErrObjectNotFound
		}
		return nil, fmt.Errorf("failed to download from S3: %w", err)
	}

	return result.Body, nil
}

// Delete removes an object from S3
func (p *S3Provider) Delete(ctx context.Context, key string) error {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(key),
	}

	_, err := p.client.DeleteObject(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to delete from S3: %w", err)
	}

	return nil
}

// List returns all objects matching the prefix
func (p *S3Provider) List(ctx context.Context, prefix string) ([]backup.ObjectInfo, error) {
	var objects []backup.ObjectInfo

	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(p.bucket),
		Prefix: aws.String(prefix),
	}

	paginator := s3.NewListObjectsV2Paginator(p.client, input)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list S3 objects: %w", err)
		}

		for _, obj := range page.Contents {
			// Get metadata if needed
			metadata := make(map[string]string)
			if obj.Key != nil {
				headInput := &s3.HeadObjectInput{
					Bucket: aws.String(p.bucket),
					Key:    obj.Key,
				}
				headResult, err := p.client.HeadObject(ctx, headInput)
				if err == nil && headResult.Metadata != nil {
					metadata = headResult.Metadata
				}
			}

			objects = append(objects, backup.ObjectInfo{
				Key:          aws.ToString(obj.Key),
				Size:         aws.ToInt64(obj.Size),
				LastModified: aws.ToTime(obj.LastModified),
				ETag:         strings.Trim(aws.ToString(obj.ETag), "\""),
				Metadata:     metadata,
			})
		}
	}

	return objects, nil
}

// Exists checks if an object exists in S3
func (p *S3Provider) Exists(ctx context.Context, key string) (bool, error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(key),
	}

	_, err := p.client.HeadObject(ctx, input)
	if err != nil {
		var nsk *types.NoSuchKey
		var notFound *types.NotFound
		if errors.As(err, &nsk) || errors.As(err, &notFound) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check S3 object existence: %w", err)
	}

	return true, nil
}

// GetMetadata retrieves metadata for an object
func (p *S3Provider) GetMetadata(ctx context.Context, key string) (map[string]string, error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(key),
	}

	result, err := p.client.HeadObject(ctx, input)
	if err != nil {
		var nsk *types.NoSuchKey
		var notFound *types.NotFound
		if errors.As(err, &nsk) || errors.As(err, &notFound) {
			return nil, backup.ErrObjectNotFound
		}
		return nil, fmt.Errorf("failed to get S3 object metadata: %w", err)
	}

	if result.Metadata == nil {
		return make(map[string]string), nil
	}

	return result.Metadata, nil
}

// Close cleans up resources
func (p *S3Provider) Close() error {
	// S3 client doesn't need explicit cleanup
	return nil
}
