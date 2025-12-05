package backup

import (
	"compress/gzip"
	"fmt"
	"io"
)

// CompressionType represents the type of compression
type CompressionType string

const (
	CompressionTypeNone CompressionType = "none"
	CompressionTypeGzip CompressionType = "gzip"
)

// Compressor provides compression functionality
type Compressor interface {
	// Compress wraps a reader with compression
	Compress(r io.Reader) (io.ReadCloser, error)

	// Decompress wraps a reader with decompression
	Decompress(r io.Reader) (io.ReadCloser, error)

	// Type returns the compression type
	Type() CompressionType
}

// NoCompression provides a no-op compressor
type NoCompression struct{}

func (c *NoCompression) Compress(r io.Reader) (io.ReadCloser, error) {
	return io.NopCloser(r), nil
}

func (c *NoCompression) Decompress(r io.Reader) (io.ReadCloser, error) {
	return io.NopCloser(r), nil
}

func (c *NoCompression) Type() CompressionType {
	return CompressionTypeNone
}

// GzipCompressor provides gzip compression
type GzipCompressor struct {
	level int // compression level (1-9, default 6)
}

// NewGzipCompressor creates a new gzip compressor with the specified level
func NewGzipCompressor(level int) *GzipCompressor {
	if level < 1 || level > 9 {
		level = gzip.DefaultCompression
	}
	return &GzipCompressor{level: level}
}

func (c *GzipCompressor) Compress(r io.Reader) (io.ReadCloser, error) {
	pr, pw := io.Pipe()

	go func() {
		gw, err := gzip.NewWriterLevel(pw, c.level)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("failed to create gzip writer: %w", err))
			return
		}

		if _, err := io.Copy(gw, r); err != nil {
			gw.Close()
			pw.CloseWithError(fmt.Errorf("compression failed: %w", err))
			return
		}

		if err := gw.Close(); err != nil {
			pw.CloseWithError(fmt.Errorf("failed to close gzip writer: %w", err))
			return
		}

		pw.Close()
	}()

	return pr, nil
}

func (c *GzipCompressor) Decompress(r io.Reader) (io.ReadCloser, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	return gr, nil
}

func (c *GzipCompressor) Type() CompressionType {
	return CompressionTypeGzip
}

// NewCompressor creates a compressor based on the type
func NewCompressor(cType CompressionType, level int) (Compressor, error) {
	switch cType {
	case CompressionTypeNone:
		return &NoCompression{}, nil
	case CompressionTypeGzip:
		return NewGzipCompressor(level), nil
	default:
		return nil, fmt.Errorf("unsupported compression type: %s", cType)
	}
}
