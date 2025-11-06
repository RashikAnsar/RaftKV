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

type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string, limit int) ([]string, error)
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
