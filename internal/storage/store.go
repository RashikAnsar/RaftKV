package storage

import (
	"context"
	"errors"
)

var (
	ErrKeyNotFound = errors.New("key not found")
	ErrStoreClosed = errors.New("store is closed")
	ErrInvalidKey  = errors.New("invalid key")
)

// ListOptions configures the List operation
type ListOptions struct {
	// Prefix filters keys by prefix (optional)
	Prefix string

	// Start and End define an inclusive key range (optional)
	// If set, only keys >= Start and <= End are returned
	Start string
	End   string

	// Limit restricts the maximum number of keys returned (0 = unlimited)
	Limit int

	// Cursor for pagination - the last key from a previous List call
	// Results will start AFTER this key
	Cursor string

	// Reverse returns keys in descending order (default: ascending)
	Reverse bool
}

// ListResult contains the result of a List operation
type ListResult struct {
	// Keys matching the query
	Keys []string

	// NextCursor is the cursor value for the next page
	// Empty string means no more results
	NextCursor string

	// HasMore indicates if there are more results beyond this page
	HasMore bool
}

type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string, limit int) ([]string, error)
	ListWithOptions(ctx context.Context, opts ListOptions) (*ListResult, error)
	Snapshot(ctx context.Context) (string, error)
	Restore(ctx context.Context, snapshotPath string) error
	Stats() Stats
	Close() error
}

type Stats struct {
	Gets     int64
	Puts     int64
	Deletes  int64
	KeyCount int64

	// Cache statistics
	CacheHits       uint64
	CacheMisses     uint64
	CacheHitRate    float64
	CacheSize       int
	CacheEvictions  uint64
}
