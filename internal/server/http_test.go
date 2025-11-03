package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/RashikAnsar/raftkv/internal/observability"
	"github.com/RashikAnsar/raftkv/internal/storage"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	testMetrics     *observability.Metrics
	testMetricsOnce sync.Once
)

func getTestMetrics() *observability.Metrics {
	testMetricsOnce.Do(func() {
		registry := prometheus.NewRegistry()

		testMetrics = &observability.Metrics{
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
					Help: "Current number of snapshots",
				},
			),
		}

		registry.MustRegister(
			testMetrics.HTTPRequestsTotal,
			testMetrics.HTTPRequestDuration,
			testMetrics.HTTPRequestSize,
			testMetrics.HTTPResponseSize,
			testMetrics.StorageOperationsTotal,
			testMetrics.StorageOperationDuration,
			testMetrics.StorageKeysTotal,
			testMetrics.StorageSnapshotsTotal,
		)
	})

	return testMetrics
}

func setupTestServer(t *testing.T) (*HTTPServer, storage.Store) {
	store, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       t.TempDir(),
		SyncOnWrite:   false,
		SnapshotEvery: 100,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	logger, err := observability.NewDevelopmentLogger()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	metrics := getTestMetrics()

	server := NewHTTPServer(HTTPServerConfig{
		Addr:    ":8080",
		Store:   store,
		Logger:  logger,
		Metrics: metrics,
	})

	return server, store
}

func TestHTTPServer_GetKey(t *testing.T) {
	server, store := setupTestServer(t)
	ctx := context.Background()

	store.Put(ctx, "test-key", []byte("test-value"))

	t.Run("Get existing key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/keys/test-key", nil)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}

		body := rec.Body.String()
		if body != "test-value" {
			t.Errorf("Expected 'test-value', got '%s'", body)
		}
	})

	t.Run("Get non-existent key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/keys/missing", nil)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rec.Code)
		}
	})
}

func TestHTTPServer_PutKey(t *testing.T) {
	server, store := setupTestServer(t)
	ctx := context.Background()

	t.Run("Put new key", func(t *testing.T) {
		body := bytes.NewBufferString("new-value")
		req := httptest.NewRequest(http.MethodPut, "/keys/new-key", body)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d", rec.Code)
		}

		value, err := store.Get(ctx, "new-key")
		if err != nil {
			t.Fatalf("Key not found: %v", err)
		}

		if string(value) != "new-value" {
			t.Errorf("Expected 'new-value', got '%s'", string(value))
		}
	})

	t.Run("Update existing key", func(t *testing.T) {
		store.Put(ctx, "update-key", []byte("old-value"))

		body := bytes.NewBufferString("updated-value")
		req := httptest.NewRequest(http.MethodPut, "/keys/update-key", body)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d", rec.Code)
		}

		value, _ := store.Get(ctx, "update-key")
		if string(value) != "updated-value" {
			t.Errorf("Expected 'updated-value', got '%s'", string(value))
		}
	})

	t.Run("Put with binary data", func(t *testing.T) {
		binaryData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}
		body := bytes.NewBuffer(binaryData)
		req := httptest.NewRequest(http.MethodPut, "/keys/binary-key", body)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d", rec.Code)
		}

		value, _ := store.Get(ctx, "binary-key")
		if !bytes.Equal(value, binaryData) {
			t.Error("Binary data mismatch")
		}
	})
}

func TestHTTPServer_DeleteKey(t *testing.T) {
	server, store := setupTestServer(t)
	ctx := context.Background()

	t.Run("Delete existing key", func(t *testing.T) {
		store.Put(ctx, "delete-me", []byte("value"))

		req := httptest.NewRequest(http.MethodDelete, "/keys/delete-me", nil)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("Expected status 204, got %d", rec.Code)
		}

		_, err := store.Get(ctx, "delete-me")
		if err != storage.ErrKeyNotFound {
			t.Error("Key should be deleted")
		}
	})

	t.Run("Delete non-existent key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/keys/not-there", nil)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("Expected status 204, got %d", rec.Code)
		}
	})
}

func TestHTTPServer_ListKeys(t *testing.T) {
	server, store := setupTestServer(t)
	ctx := context.Background()

	store.Put(ctx, "user:1", []byte("alice"))
	store.Put(ctx, "user:2", []byte("bob"))
	store.Put(ctx, "post:1", []byte("hello"))
	store.Put(ctx, "post:2", []byte("world"))

	t.Run("List all keys", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/keys", nil)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}

		var response map[string]interface{}
		json.NewDecoder(rec.Body).Decode(&response)

		count := int(response["count"].(float64))
		if count != 4 {
			t.Errorf("Expected 4 keys, got %d", count)
		}
	})

	t.Run("List keys with prefix", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/keys?prefix=user:", nil)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}

		var response map[string]interface{}
		json.NewDecoder(rec.Body).Decode(&response)

		keys := response["keys"].([]interface{})
		if len(keys) != 2 {
			t.Errorf("Expected 2 keys with prefix 'user:', got %d", len(keys))
		}
	})

	t.Run("List with limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/keys?limit=2", nil)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}

		var response map[string]interface{}
		json.NewDecoder(rec.Body).Decode(&response)

		keys := response["keys"].([]interface{})
		if len(keys) > 2 {
			t.Errorf("Expected max 2 keys, got %d", len(keys))
		}
	})
}

func TestHTTPServer_Health(t *testing.T) {
	server, _ := setupTestServer(t)

	t.Run("Health check", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}

		var response map[string]interface{}
		json.NewDecoder(rec.Body).Decode(&response)

		if response["status"] != "healthy" {
			t.Error("Expected healthy status")
		}
	})

	t.Run("Readiness check", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}
	})
}

func TestHTTPServer_Stats(t *testing.T) {
	server, store := setupTestServer(t)
	ctx := context.Background()

	store.Put(ctx, "key1", []byte("value1"))
	store.Put(ctx, "key2", []byte("value2"))
	store.Get(ctx, "key1")
	store.Delete(ctx, "key1")

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rec := httptest.NewRecorder()

	server.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	var response map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&response)

	if response["puts"].(float64) != 2 {
		t.Errorf("Expected 2 puts, got %v", response["puts"])
	}
	if response["gets"].(float64) != 1 {
		t.Errorf("Expected 1 get, got %v", response["gets"])
	}
	if response["deletes"].(float64) != 1 {
		t.Errorf("Expected 1 delete, got %v", response["deletes"])
	}
}

func TestHTTPServer_Root(t *testing.T) {
	server, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	server.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	var response map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&response)

	if response["service"] != "RaftKV" {
		t.Error("Expected service name 'RaftKV'")
	}
}

func TestHTTPServer_CORS(t *testing.T) {
	server, _ := setupTestServer(t)

	t.Run("CORS headers present", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Error("CORS header missing")
		}
	})

	t.Run("OPTIONS preflight", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/keys/test", nil)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200 for OPTIONS, got %d", rec.Code)
		}
	})
}

func TestHTTPServer_RequestIDHeader(t *testing.T) {
	server, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.router.ServeHTTP(rec, req)

	requestID := rec.Header().Get("X-Request-ID")
	if requestID == "" {
		t.Error("Expected X-Request-ID header")
	}
}

func TestHTTPServer_LargeValue(t *testing.T) {
	server, store := setupTestServer(t)
	ctx := context.Background()

	// Create 1KB value
	largeValue := bytes.Repeat([]byte("x"), 1024)

	t.Run("Put large value", func(t *testing.T) {
		body := bytes.NewBuffer(largeValue)
		req := httptest.NewRequest(http.MethodPut, "/keys/large", body)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d", rec.Code)
		}
	})

	t.Run("Get large value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/keys/large", nil)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rec.Code)
		}

		body, _ := io.ReadAll(rec.Body)
		if !bytes.Equal(body, largeValue) {
			t.Error("Large value mismatch")
		}
	})

	value, _ := store.Get(ctx, "large")
	if len(value) != 1024 {
		t.Errorf("Expected 1024 bytes, got %d", len(value))
	}
}

// Benchmark
func BenchmarkHTTPServer_Get(b *testing.B) {
	server, store := setupTestServer(&testing.T{})
	ctx := context.Background()

	store.Put(ctx, "bench-key", []byte("bench-value"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/keys/bench-key", nil)
		rec := httptest.NewRecorder()
		server.router.ServeHTTP(rec, req)
	}
}

func BenchmarkHTTPServer_Put(b *testing.B) {
	server, _ := setupTestServer(&testing.T{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body := bytes.NewBufferString("bench-value")
		req := httptest.NewRequest(http.MethodPut, "/keys/bench-key", body)
		rec := httptest.NewRecorder()
		server.router.ServeHTTP(rec, req)
	}
}
