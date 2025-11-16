//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/RashikAnsar/raftkv/internal/observability"
	"github.com/RashikAnsar/raftkv/internal/server"
	"github.com/RashikAnsar/raftkv/internal/storage"
	"github.com/prometheus/client_golang/prometheus"
)

// Helper function to create test metrics
func createTestMetrics() *observability.Metrics {
	return &observability.Metrics{
		HTTPRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "endpoint", "status"},
		),
		HTTPRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "test_http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "endpoint"},
		),
		HTTPRequestSize: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "test_http_request_size_bytes",
				Help:    "HTTP request size in bytes",
				Buckets: prometheus.ExponentialBuckets(100, 10, 6),
			},
			[]string{"method", "endpoint"},
		),
		HTTPResponseSize: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "test_http_response_size_bytes",
				Help:    "HTTP response size in bytes",
				Buckets: prometheus.ExponentialBuckets(100, 10, 6),
			},
			[]string{"method", "endpoint"},
		),
		StorageOperationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_storage_operations_total",
				Help: "Total number of storage operations",
			},
			[]string{"operation", "status"},
		),
		StorageOperationDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "test_storage_operation_duration_seconds",
				Help:    "Storage operation duration in seconds",
				Buckets: []float64{.00001, .00005, .0001, .0005, .001, .005, .01, .05, .1, .5, 1},
			},
			[]string{"operation"},
		),
		StorageKeysTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "test_storage_keys_total",
				Help: "Current number of keys in storage",
			},
		),
		StorageSnapshotsTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "test_storage_snapshots_total",
				Help: "Total number of snapshots",
			},
		),
	}
}

// TestSingleNode_HTTPBasicOperations tests basic HTTP operations on a single standalone node
func TestSingleNode_HTTPBasicOperations(t *testing.T) {
	// Create a simple in-memory store (no Raft)
	store, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       t.TempDir(),
		SyncOnWrite:   false,
		SnapshotEvery: 100,
	})
	require.NoError(t, err)
	defer store.Close()

	// Create logger
	logger, err := observability.NewDevelopmentLogger()
	require.NoError(t, err)

	// Create metrics
	metrics := createTestMetrics()

	// Create HTTP server
	httpServer := server.NewHTTPServer(server.HTTPServerConfig{
		Addr:    ":8080",
		Store:   store,
		Logger:  logger,
		Metrics: metrics,
	})

	ctx := context.Background()

	// Test 1: PUT a key
	t.Run("PUT key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/keys/test-key", bytes.NewReader([]byte("test-value")))
		rec := httptest.NewRecorder()

		httpServer.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	// Test 2: GET the key
	t.Run("GET key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/keys/test-key", nil)
		rec := httptest.NewRecorder()

		httpServer.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.Equal(t, "test-value", body)
	})

	// Test 3: DELETE the key
	t.Run("DELETE key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/keys/test-key", nil)
		rec := httptest.NewRecorder()

		httpServer.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	// Test 4: GET non-existent key
	t.Run("GET non-existent key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/keys/test-key", nil)
		rec := httptest.NewRecorder()

		httpServer.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	// Test 5: Health check
	t.Run("Health check", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		httpServer.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	// Test 6: Stats
	t.Run("Stats", func(t *testing.T) {
		// Put some data first
		store.Put(ctx, "stat-key-1", []byte("value1"))
		store.Put(ctx, "stat-key-2", []byte("value2"))

		req := httptest.NewRequest(http.MethodGet, "/stats", nil)
		rec := httptest.NewRecorder()

		httpServer.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var stats map[string]interface{}
		err := json.NewDecoder(rec.Body).Decode(&stats)
		require.NoError(t, err)
		assert.Contains(t, stats, "key_count")
	})
}

// TestSingleNode_ConcurrentOperations tests concurrent operations on a single node
func TestSingleNode_ConcurrentOperations(t *testing.T) {
	// Create a simple in-memory store (no Raft)
	store, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       t.TempDir(),
		SyncOnWrite:   false,
		SnapshotEvery: 100,
	})
	require.NoError(t, err)
	// Don't defer close here - close it after verification
	defer func() {
		// Give time for all requests to finish before closing
		time.Sleep(100 * time.Millisecond)
		store.Close()
	}()

	// Create logger
	logger, err := observability.NewDevelopmentLogger()
	require.NoError(t, err)

	// Create metrics
	metrics := createTestMetrics()

	// Create HTTP server
	httpServer := server.NewHTTPServer(server.HTTPServerConfig{
		Addr:    ":8080",
		Store:   store,
		Logger:  logger,
		Metrics: metrics,
	})

	ctx := context.Background()

	// Test concurrent writes
	t.Run("Concurrent writes", func(t *testing.T) {
		const numGoroutines = 50
		const numOpsPerGoroutine = 10

		errCh := make(chan error, numGoroutines)
		done := make(chan bool)

		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				for j := 0; j < numOpsPerGoroutine; j++ {
					key := fmt.Sprintf("concurrent-key-%d-%d", id, j)
					value := fmt.Sprintf("value-%d-%d", id, j)

					req := httptest.NewRequest(http.MethodPut, "/keys/"+key, bytes.NewReader([]byte(value)))
					rec := httptest.NewRecorder()

					httpServer.Handler().ServeHTTP(rec, req)

					if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
						errCh <- fmt.Errorf("unexpected status code: %d", rec.Code)
						return
					}
				}
				done <- true
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < numGoroutines; i++ {
			select {
			case <-done:
				// Success
			case err := <-errCh:
				t.Fatalf("Concurrent write error: %v", err)
			case <-time.After(30 * time.Second):
				t.Fatal("Timeout waiting for concurrent writes")
			}
		}
	})

	// Verify all keys were written
	t.Run("Verify concurrent writes", func(t *testing.T) {
		const numGoroutines = 50
		const numOpsPerGoroutine = 10

		for i := 0; i < numGoroutines; i++ {
			for j := 0; j < numOpsPerGoroutine; j++ {
				key := fmt.Sprintf("concurrent-key-%d-%d", i, j)
				expectedValue := fmt.Sprintf("value-%d-%d", i, j)

				value, err := store.Get(ctx, key)
				require.NoError(t, err)
				assert.Equal(t, expectedValue, string(value))
			}
		}
	})
}

// TestSingleNode_Persistence tests data persistence with WAL
func TestSingleNode_Persistence(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Phase 1: Create durable store, write data, and close
	t.Run("Write data with WAL", func(t *testing.T) {
		store, err := storage.NewDurableStore(storage.DurableStoreConfig{
			DataDir:       tmpDir,
			SyncOnWrite:   true, // Force sync for durability
			SnapshotEvery: 100,
		})
		require.NoError(t, err)

		// Write some data
		require.NoError(t, store.Put(ctx, "persistent-key-1", []byte("persistent-value-1")))
		require.NoError(t, store.Put(ctx, "persistent-key-2", []byte("persistent-value-2")))
		require.NoError(t, store.Put(ctx, "persistent-key-3", []byte("persistent-value-3")))

		// Close the store (simulating shutdown)
		require.NoError(t, store.Close())
	})

	// Phase 2: Reopen store and verify data was persisted
	t.Run("Verify data after restart", func(t *testing.T) {
		store, err := storage.NewDurableStore(storage.DurableStoreConfig{
			DataDir:       tmpDir,
			SyncOnWrite:   false,
			SnapshotEvery: 100,
		})
		require.NoError(t, err)
		defer store.Close()

		// Verify data was recovered
		value, err := store.Get(ctx, "persistent-key-1")
		require.NoError(t, err)
		assert.Equal(t, []byte("persistent-value-1"), value)

		value, err = store.Get(ctx, "persistent-key-2")
		require.NoError(t, err)
		assert.Equal(t, []byte("persistent-value-2"), value)

		value, err = store.Get(ctx, "persistent-key-3")
		require.NoError(t, err)
		assert.Equal(t, []byte("persistent-value-3"), value)
	})
}

// TestSingleNode_LargeValues tests handling of large values
func TestSingleNode_LargeValues(t *testing.T) {
	ctx := context.Background()

	store, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       t.TempDir(),
		SyncOnWrite:   false,
		SnapshotEvery: 100,
	})
	require.NoError(t, err)
	defer store.Close()

	// Create logger
	logger, err := observability.NewDevelopmentLogger()
	require.NoError(t, err)

	// Create metrics
	metrics := createTestMetrics()

	// Create HTTP server
	httpServer := server.NewHTTPServer(server.HTTPServerConfig{
		Addr:    ":8080",
		Store:   store,
		Logger:  logger,
		Metrics: metrics,
	})

	// Test 1MB value
	t.Run("1MB value", func(t *testing.T) {
		largeValue := bytes.Repeat([]byte("x"), 1024*1024) // 1MB

		req := httptest.NewRequest(http.MethodPut, "/keys/large-key", bytes.NewReader(largeValue))
		rec := httptest.NewRecorder()

		httpServer.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)

		// Verify the value
		value, err := store.Get(ctx, "large-key")
		require.NoError(t, err)
		assert.Equal(t, largeValue, value)
	})
}
