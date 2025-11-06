package benchmark

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/RashikAnsar/raftkv/internal/observability"
	"github.com/RashikAnsar/raftkv/internal/server"
	"github.com/RashikAnsar/raftkv/internal/storage"
)

// BenchmarkHTTPServer_Sequential tests HTTP API performance with sequential requests
func BenchmarkHTTPServer_Sequential(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	logger, _ := observability.NewLogger("error") // Quiet logging for benchmarks
	// Pass nil for metrics to avoid duplicate registration issues in benchmarks
	var metrics *observability.Metrics

	srv := server.NewHTTPServer(server.HTTPServerConfig{
		Addr:           ":8080",
		Store:          store,
		Logger:         logger,
		Metrics:        metrics,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxRequestSize: 1024 * 1024,
	})

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	b.Run("PUT", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i)
			value := []byte(fmt.Sprintf("value-%d", i))

			req, _ := http.NewRequest("PUT", ts.URL+"/keys/"+key, bytes.NewReader(value))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				b.Fatal(err)
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusCreated {
				b.Fatalf("expected 201, got %d", resp.StatusCode)
			}
		}
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "req/sec")
	})

	b.Run("GET", func(b *testing.B) {
		// Pre-populate
		for i := 0; i < 10000; i++ {
			key := fmt.Sprintf("key-%d", i)
			value := []byte(fmt.Sprintf("value-%d", i))
			req, _ := http.NewRequest("PUT", ts.URL+"/keys/"+key, bytes.NewReader(value))
			resp, _ := http.DefaultClient.Do(req)
			resp.Body.Close()
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i%10000)
			resp, err := http.Get(ts.URL + "/keys/" + key)
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "req/sec")
	})

	b.Run("DELETE", func(b *testing.B) {
		// Pre-populate
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i)
			value := []byte(fmt.Sprintf("value-%d", i))
			req, _ := http.NewRequest("PUT", ts.URL+"/keys/"+key, bytes.NewReader(value))
			resp, _ := http.DefaultClient.Do(req)
			resp.Body.Close()
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i)
			req, _ := http.NewRequest("DELETE", ts.URL+"/keys/"+key, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				b.Fatal(err)
			}
			resp.Body.Close()
		}
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "req/sec")
	})
}

// BenchmarkHTTPServer_Concurrent tests concurrent HTTP requests
func BenchmarkHTTPServer_Concurrent(b *testing.B) {
	store := storage.NewMemoryStore()
	defer store.Close()

	logger, _ := observability.NewLogger("error")
	var metrics *observability.Metrics

	srv := server.NewHTTPServer(server.HTTPServerConfig{
		Addr:           ":8080",
		Store:          store,
		Logger:         logger,
		Metrics:        metrics,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxRequestSize: 1024 * 1024,
	})

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Pre-populate
	ctx := context.Background()
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("key-%d", i)
		value := []byte(fmt.Sprintf("value-%d", i))
		store.Put(ctx, key, value)
	}

	// Reduce workers to avoid port exhaustion
	workers := []int{1, 10, 25}

	for _, numWorkers := range workers {
		b.Run(fmt.Sprintf("Workers-%d", numWorkers), func(b *testing.B) {
			// Create shared HTTP client with connection pooling
			transport := &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			}
			client := &http.Client{
				Transport: transport,
				Timeout:   30 * time.Second,
			}
			defer transport.CloseIdleConnections()

			b.ResetTimer()

			var wg sync.WaitGroup
			opsPerWorker := b.N / numWorkers

			for w := 0; w < numWorkers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					for i := 0; i < opsPerWorker; i++ {
						key := fmt.Sprintf("key-%d", i%10000)
						resp, err := client.Get(ts.URL + "/keys/" + key)
						if err != nil {
							b.Error(err)
							return
						}
						io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
					}
				}()
			}

			wg.Wait()
			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "req/sec")
		})
	}
}

// BenchmarkCachedVsUncached compares cached vs uncached read performance
func BenchmarkCachedVsUncached(b *testing.B) {
	logger, _ := observability.NewLogger("error")
	var metrics *observability.Metrics

	// Populate data
	populateData := func(store storage.Store) {
		ctx := context.Background()
		for i := 0; i < 10000; i++ {
			key := fmt.Sprintf("key-%d", i)
			value := []byte(fmt.Sprintf("value-%d", i))
			store.Put(ctx, key, value)
		}
	}

	b.Run("Uncached", func(b *testing.B) {
		baseStore := storage.NewMemoryStore()
		defer baseStore.Close()

		srv := server.NewHTTPServer(server.HTTPServerConfig{
			Addr:           ":8080",
			Store:          baseStore,
			Logger:         logger,
			Metrics:        metrics,
			ReadTimeout:    10 * time.Second,
			WriteTimeout:   10 * time.Second,
			MaxRequestSize: 1024 * 1024,
		})

		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()

		populateData(baseStore)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i%10000)
			resp, err := http.Get(ts.URL + "/keys/" + key)
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "req/sec")
	})

	b.Run("Cached", func(b *testing.B) {
		baseStore := storage.NewMemoryStore()
		defer baseStore.Close()

		cachedStore := storage.NewCachedStore(baseStore, storage.CacheConfig{
			MaxSize: 10000,
			TTL:     0, // No expiration
		})

		srv := server.NewHTTPServer(server.HTTPServerConfig{
			Addr:           ":8080",
			Store:          cachedStore,
			Logger:         logger,
			Metrics:        metrics,
			ReadTimeout:    10 * time.Second,
			WriteTimeout:   10 * time.Second,
			MaxRequestSize: 1024 * 1024,
		})

		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()

		populateData(cachedStore)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i%10000)
			resp, err := http.Get(ts.URL + "/keys/" + key)
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "req/sec")
	})
}

// BenchmarkStorage_Operations tests raw storage performance
func BenchmarkStorage_Operations(b *testing.B) {
	ctx := context.Background()

	b.Run("MemoryStore", func(b *testing.B) {
		store := storage.NewMemoryStore()
		defer store.Close()

		b.Run("Put", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i)
				value := []byte(fmt.Sprintf("value-%d", i))
				store.Put(ctx, key, value)
			}
		})

		b.Run("Get", func(b *testing.B) {
			// Pre-populate
			for i := 0; i < 10000; i++ {
				key := fmt.Sprintf("key-%d", i)
				value := []byte(fmt.Sprintf("value-%d", i))
				store.Put(ctx, key, value)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i%10000)
				store.Get(ctx, key)
			}
		})

		b.Run("Delete", func(b *testing.B) {
			// Pre-populate
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i)
				value := []byte(fmt.Sprintf("value-%d", i))
				store.Put(ctx, key, value)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i)
				store.Delete(ctx, key)
			}
		})
	})

	b.Run("CachedStore", func(b *testing.B) {
		baseStore := storage.NewMemoryStore()
		defer baseStore.Close()

		cachedStore := storage.NewCachedStore(baseStore, storage.CacheConfig{
			MaxSize: 10000,
			TTL:     0,
		})

		b.Run("Put", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i)
				value := []byte(fmt.Sprintf("value-%d", i))
				cachedStore.Put(ctx, key, value)
			}
		})

		b.Run("Get-Hit", func(b *testing.B) {
			// Pre-populate
			for i := 0; i < 10000; i++ {
				key := fmt.Sprintf("key-%d", i)
				value := []byte(fmt.Sprintf("value-%d", i))
				cachedStore.Put(ctx, key, value)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i%10000)
				cachedStore.Get(ctx, key)
			}
		})

		b.Run("Delete", func(b *testing.B) {
			// Pre-populate
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i)
				value := []byte(fmt.Sprintf("value-%d", i))
				cachedStore.Put(ctx, key, value)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i)
				cachedStore.Delete(ctx, key)
			}
		})
	})
}
