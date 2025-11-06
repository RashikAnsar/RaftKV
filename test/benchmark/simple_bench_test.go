package benchmark

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/RashikAnsar/raftkv/internal/storage"
	"github.com/stretchr/testify/require"
)

// BenchmarkStorageSequential measures sequential read/write performance
func BenchmarkStorageSequential(b *testing.B) {
	ctx := context.Background()

	b.Run("MemoryStore-Put", func(b *testing.B) {
		store := storage.NewMemoryStore()
		defer store.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i)
			value := []byte(fmt.Sprintf("value-%d", i))
			_ = store.Put(ctx, key, value)
		}
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
	})

	b.Run("MemoryStore-Get", func(b *testing.B) {
		store := storage.NewMemoryStore()
		defer store.Close()

		// Pre-populate
		for i := 0; i < 10000; i++ {
			key := fmt.Sprintf("key-%d", i)
			value := []byte(fmt.Sprintf("value-%d", i))
			store.Put(ctx, key, value)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i%10000)
			_, _ = store.Get(ctx, key)
		}
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
	})

	b.Run("DurableStore-Put-NoFsync", func(b *testing.B) {
		tmpDir := b.TempDir()
		store, err := storage.NewDurableStore(storage.DurableStoreConfig{
			DataDir:       tmpDir,
			SyncOnWrite:   false, // No fsync for faster writes
			SnapshotEvery: 10000,
		})
		require.NoError(b, err)
		defer store.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i)
			value := []byte(fmt.Sprintf("value-%d", i))
			_ = store.Put(ctx, key, value)
		}
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
	})

	b.Run("DurableStore-Put-WithFsync", func(b *testing.B) {
		tmpDir := b.TempDir()
		store, err := storage.NewDurableStore(storage.DurableStoreConfig{
			DataDir:       tmpDir,
			SyncOnWrite:   true, // Fsync on every write (durable)
			SnapshotEvery: 10000,
		})
		require.NoError(b, err)
		defer store.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i)
			value := []byte(fmt.Sprintf("value-%d", i))
			_ = store.Put(ctx, key, value)
		}
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
	})
}

// BenchmarkStorageConcurrent measures concurrent access performance
func BenchmarkStorageConcurrent(b *testing.B) {
	ctx := context.Background()
	workers := []int{1, 10, 50, 100}

	for _, numWorkers := range workers {
		b.Run(fmt.Sprintf("MemoryStore-%dWorkers", numWorkers), func(b *testing.B) {
			store := storage.NewMemoryStore()
			defer store.Close()

			// Pre-populate
			for i := 0; i < 10000; i++ {
				key := fmt.Sprintf("key-%d", i)
				value := []byte(fmt.Sprintf("value-%d", i))
				store.Put(ctx, key, value)
			}

			b.ResetTimer()

			var wg sync.WaitGroup
			opsPerWorker := b.N / numWorkers

			for w := 0; w < numWorkers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					rnd := rand.New(rand.NewSource(time.Now().UnixNano()))

					for i := 0; i < opsPerWorker; i++ {
						op := rnd.Intn(10)
						key := fmt.Sprintf("key-%d", rnd.Intn(10000))

						if op < 7 { // 70% reads
							store.Get(ctx, key)
						} else { // 30% writes
							value := []byte(fmt.Sprintf("value-%d", time.Now().UnixNano()))
							store.Put(ctx, key, value)
						}
					}
				}()
			}

			wg.Wait()
			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
		})
	}
}

// BenchmarkValueSizes tests performance with different value sizes
func BenchmarkValueSizes(b *testing.B) {
	ctx := context.Background()
	sizes := []int{64, 256, 1024, 4096, 16384} // 64B to 16KB

	for _, size := range sizes {
		value := make([]byte, size)
		for i := range value {
			value[i] = byte(i % 256)
		}

		b.Run(fmt.Sprintf("Size-%dB", size), func(b *testing.B) {
			store := storage.NewMemoryStore()
			defer store.Close()

			b.SetBytes(int64(size))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%d", i)
				_ = store.Put(ctx, key, value)
			}

			throughputMBps := float64(b.N*size) / b.Elapsed().Seconds() / 1024 / 1024
			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
			b.ReportMetric(throughputMBps, "MB/sec")
		})
	}
}

// BenchmarkRecovery tests recovery time with different dataset sizes
func BenchmarkRecovery(b *testing.B) {
	ctx := context.Background()
	sizes := []int{1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Keys-%d", size), func(b *testing.B) {
			tmpDir := b.TempDir()

			// Create and populate store
			store, err := storage.NewDurableStore(storage.DurableStoreConfig{
				DataDir:       tmpDir,
				SyncOnWrite:   false,
				SnapshotEvery: 10000,
			})
			require.NoError(b, err)

			for i := 0; i < size; i++ {
				key := fmt.Sprintf("key-%d", i)
				value := []byte(fmt.Sprintf("value-%d", i))
				_ = store.Put(ctx, key, value)
			}
			store.Close()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Measure recovery time
				recoveredStore, err := storage.NewDurableStore(storage.DurableStoreConfig{
					DataDir:       tmpDir,
					SyncOnWrite:   false,
					SnapshotEvery: 10000,
				})
				if err != nil {
					b.Fatal(err)
				}
				recoveredStore.Close()
			}
			recoveryMs := b.Elapsed().Seconds() / float64(b.N) * 1000
			b.ReportMetric(recoveryMs, "ms/recovery")
		})
	}
}

// BenchmarkSnapshot tests snapshot performance
func BenchmarkSnapshot(b *testing.B) {
	sizes := []int{1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Create-%dKeys", size), func(b *testing.B) {
			tmpDir := b.TempDir()
			mgr, err := storage.NewSnapshotManager(tmpDir)
			require.NoError(b, err)

			// Create test data
			data := make(map[string][]byte)
			for i := 0; i < size; i++ {
				key := fmt.Sprintf("key-%d", i)
				value := []byte(fmt.Sprintf("value-%d", i))
				data[key] = value
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = mgr.Create(uint64(i), data)
			}
			snapshotMs := b.Elapsed().Seconds() / float64(b.N) * 1000
			b.ReportMetric(snapshotMs, "ms/snapshot")
		})

		b.Run(fmt.Sprintf("Restore-%dKeys", size), func(b *testing.B) {
			tmpDir := b.TempDir()
			mgr, err := storage.NewSnapshotManager(tmpDir)
			require.NoError(b, err)

			// Create test data and snapshot
			data := make(map[string][]byte)
			for i := 0; i < size; i++ {
				key := fmt.Sprintf("key-%d", i)
				value := []byte(fmt.Sprintf("value-%d", i))
				data[key] = value
			}
			_ = mgr.Create(1, data)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = mgr.Restore()
			}
			restoreMs := b.Elapsed().Seconds() / float64(b.N) * 1000
			b.ReportMetric(restoreMs, "ms/restore")
		})
	}
}
