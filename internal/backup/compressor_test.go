package backup

import (
	"bytes"
	"io"
	"testing"
)

func TestGzipCompressor_Compress(t *testing.T) {
	compressor, err := NewCompressor(CompressionTypeGzip, 6)
	if err != nil {
		t.Fatalf("failed to create compressor: %v", err)
	}

	data := []byte("This is test data that should be compressed. " +
		"Compression works best with repetitive data. " +
		"Repetitive data compresses well. " +
		"This is more test data for compression testing.")

	reader := bytes.NewReader(data)
	compressedReader, err := compressor.Compress(reader)
	if err != nil {
		t.Fatalf("compression failed: %v", err)
	}

	compressed, err := io.ReadAll(compressedReader)
	if err != nil {
		t.Fatalf("failed to read compressed data: %v", err)
	}

	// Compressed data should be smaller
	if len(compressed) >= len(data) {
		t.Logf("Warning: compressed size (%d) >= original size (%d)", len(compressed), len(data))
	}

	// Should not be equal to original
	if bytes.Equal(compressed, data) {
		t.Fatalf("compressed data equals original data")
	}
}

func TestGzipCompressor_Decompress(t *testing.T) {
	compressor, err := NewCompressor(CompressionTypeGzip, 6)
	if err != nil {
		t.Fatalf("failed to create compressor: %v", err)
	}

	originalData := []byte("This is test data that should be compressed and then decompressed. " +
		"The round-trip should preserve the original data exactly.")

	// Compress
	reader := bytes.NewReader(originalData)
	compressedReader, err := compressor.Compress(reader)
	if err != nil {
		t.Fatalf("compression failed: %v", err)
	}

	compressed, err := io.ReadAll(compressedReader)
	if err != nil {
		t.Fatalf("failed to read compressed data: %v", err)
	}

	// Decompress
	decompressedReader, err := compressor.Decompress(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("decompression failed: %v", err)
	}

	decompressed, err := io.ReadAll(decompressedReader)
	if err != nil {
		t.Fatalf("failed to read decompressed data: %v", err)
	}

	// Verify round-trip
	if !bytes.Equal(decompressed, originalData) {
		t.Fatalf("decompressed data doesn't match original.\nOriginal: %s\nDecompressed: %s",
			originalData, decompressed)
	}
}

func TestGzipCompressor_CompressionLevels(t *testing.T) {
	testData := bytes.Repeat([]byte("test data "), 1000)

	levels := []int{1, 6, 9}
	for _, level := range levels {
		t.Run(string(rune('0'+level)), func(t *testing.T) {
			compressor, err := NewCompressor(CompressionTypeGzip, level)
			if err != nil {
				t.Fatalf("failed to create compressor: %v", err)
			}

			compressedReader, err := compressor.Compress(bytes.NewReader(testData))
			if err != nil {
				t.Fatalf("compression failed: %v", err)
			}

			compressed, err := io.ReadAll(compressedReader)
			if err != nil {
				t.Fatalf("failed to read compressed data: %v", err)
			}

			t.Logf("Level %d: original=%d, compressed=%d, ratio=%.2f%%",
				level, len(testData), len(compressed),
				float64(len(compressed))/float64(len(testData))*100)

			// Verify decompression works
			decompressedReader, err := compressor.Decompress(bytes.NewReader(compressed))
			if err != nil {
				t.Fatalf("decompression failed: %v", err)
			}

			decompressed, err := io.ReadAll(decompressedReader)
			if err != nil {
				t.Fatalf("failed to read decompressed data: %v", err)
			}

			if !bytes.Equal(decompressed, testData) {
				t.Fatalf("decompressed data doesn't match original")
			}
		})
	}
}

func TestNoCompression(t *testing.T) {
	compressor, err := NewCompressor(CompressionTypeNone, 0)
	if err != nil {
		t.Fatalf("failed to create compressor: %v", err)
	}

	data := []byte("test data without compression")

	// Compress (should be no-op)
	compressedReader, err := compressor.Compress(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("compression failed: %v", err)
	}

	compressed, err := io.ReadAll(compressedReader)
	if err != nil {
		t.Fatalf("failed to read compressed data: %v", err)
	}

	// Should be identical
	if !bytes.Equal(compressed, data) {
		t.Fatalf("no-compression output doesn't match input")
	}

	// Decompress (should be no-op)
	decompressedReader, err := compressor.Decompress(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("decompression failed: %v", err)
	}

	decompressed, err := io.ReadAll(decompressedReader)
	if err != nil {
		t.Fatalf("failed to read decompressed data: %v", err)
	}

	if !bytes.Equal(decompressed, data) {
		t.Fatalf("no-decompression output doesn't match input")
	}
}

func TestGzipCompressor_LargeData(t *testing.T) {
	compressor, err := NewCompressor(CompressionTypeGzip, 6)
	if err != nil {
		t.Fatalf("failed to create compressor: %v", err)
	}

	// Create 10MB of compressible data
	pattern := []byte("This is a repeating pattern for compression testing. ")
	data := bytes.Repeat(pattern, 200000) // ~10MB

	// Compress
	compressedReader, err := compressor.Compress(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("compression failed: %v", err)
	}

	compressed, err := io.ReadAll(compressedReader)
	if err != nil {
		t.Fatalf("failed to read compressed data: %v", err)
	}

	t.Logf("Large data: original=%d, compressed=%d, ratio=%.2f%%",
		len(data), len(compressed),
		float64(len(compressed))/float64(len(data))*100)

	// Decompress
	decompressedReader, err := compressor.Decompress(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("decompression failed: %v", err)
	}

	decompressed, err := io.ReadAll(decompressedReader)
	if err != nil {
		t.Fatalf("failed to read decompressed data: %v", err)
	}

	// Verify
	if !bytes.Equal(decompressed, data) {
		t.Fatalf("large data round-trip failed")
	}
}

func TestGzipCompressor_EmptyData(t *testing.T) {
	compressor, err := NewCompressor(CompressionTypeGzip, 6)
	if err != nil {
		t.Fatalf("failed to create compressor: %v", err)
	}

	data := []byte{}

	// Compress
	compressedReader, err := compressor.Compress(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("compression failed: %v", err)
	}

	compressed, err := io.ReadAll(compressedReader)
	if err != nil {
		t.Fatalf("failed to read compressed data: %v", err)
	}

	// Decompress
	decompressedReader, err := compressor.Decompress(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("decompression failed: %v", err)
	}

	decompressed, err := io.ReadAll(decompressedReader)
	if err != nil {
		t.Fatalf("failed to read decompressed data: %v", err)
	}

	// Verify
	if !bytes.Equal(decompressed, data) {
		t.Fatalf("empty data round-trip failed")
	}
}

func TestInvalidCompressionType(t *testing.T) {
	_, err := NewCompressor(CompressionType("invalid"), 6)
	if err == nil {
		t.Fatalf("expected error for invalid compression type")
	}
}
