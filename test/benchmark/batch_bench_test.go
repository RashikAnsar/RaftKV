package benchmark

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/RashikAnsar/raftkv/internal/storage"
	"github.com/stretchr/testify/require"
)

// BenchmarkBatchedWrites_Sequential compares batched vs non-batched write performance (sequential)
func BenchmarkBatchedWrites_Sequential(b *testing.B) {
	ctx := context.Background()

	b.Run("NoBatch-Fsync", func(b *testing.B) {
		tmpDir := b.TempDir()
		store, err := storage.NewDurableStore(storage.DurableStoreConfig{
			DataDir:       tmpDir,
			SyncOnWrite:   true,  // Fsync on every write
			SnapshotEvery: 100000, // Don't snapshot during benchmark
			BatchConfig: storage.BatchConfig{
				Enabled: false, // No batching
			},
		})
		require.NoError(b, err)
		defer store.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i)
			value := []byte(fmt.Sprintf("value-%d", i))
			err := store.Put(ctx, key, value)
			if err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
	})

	b.Run("WithBatch-100ops", func(b *testing.B) {
		tmpDir := b.TempDir()
		store, err := storage.NewDurableStore(storage.DurableStoreConfig{
			DataDir:       tmpDir,
			SyncOnWrite:   true,
			SnapshotEvery: 100000,
			BatchConfig: storage.BatchConfig{
				Enabled:       true,
				MaxBatchSize:  100,
				MaxBatchBytes: 1024 * 1024,
				MaxWaitTime:   1 * time.Millisecond, // 1ms
			},
		})
		require.NoError(b, err)
		defer store.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%d", i)
			value := []byte(fmt.Sprintf("value-%d", i))
			err := store.Put(ctx, key, value)
			if err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
	})
}

// BenchmarkBatchedWrites_Concurrent demonstrates the real power of batching
func BenchmarkBatchedWrites_Concurrent(b *testing.B) {
	ctx := context.Background()
	workers := 10

	b.Run("NoBatch-Fsync-10Workers", func(b *testing.B) {
		tmpDir := b.TempDir()
		store, err := storage.NewDurableStore(storage.DurableStoreConfig{
			DataDir:       tmpDir,
			SyncOnWrite:   true,
			SnapshotEvery: 100000,
			BatchConfig: storage.BatchConfig{
				Enabled: false,
			},
		})
		require.NoError(b, err)
		defer store.Close()

		b.ResetTimer()

		var wg sync.WaitGroup
		opsPerWorker := b.N / workers

		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				for i := 0; i < opsPerWorker; i++ {
					key := fmt.Sprintf("key-w%d-%d", workerID, i)
					value := []byte(fmt.Sprintf("value-%d", i))
					_ = store.Put(ctx, key, value)
				}
			}(w)
		}

		wg.Wait()
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
	})

	b.Run("SmallBatch-10ops-10Workers", func(b *testing.B) {
		tmpDir := b.TempDir()
		store, err := storage.NewDurableStore(storage.DurableStoreConfig{
			DataDir:       tmpDir,
			SyncOnWrite:   true,
			SnapshotEvery: 100000,
			BatchConfig: storage.BatchConfig{
				Enabled:       true,
				MaxBatchSize:  10,
				MaxBatchBytes: 1024 * 1024,
				MaxWaitTime:   1 * time.Millisecond,
			},
		})
		require.NoError(b, err)
		defer store.Close()

		b.ResetTimer()

		var wg sync.WaitGroup
		opsPerWorker := b.N / workers

		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				for i := 0; i < opsPerWorker; i++ {
					key := fmt.Sprintf("key-w%d-%d", workerID, i)
					value := []byte(fmt.Sprintf("value-%d", i))
					_ = store.Put(ctx, key, value)
				}
			}(w)
		}

		wg.Wait()
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
	})

	b.Run("MediumBatch-100ops-10Workers", func(b *testing.B) {
		tmpDir := b.TempDir()
		store, err := storage.NewDurableStore(storage.DurableStoreConfig{
			DataDir:       tmpDir,
			SyncOnWrite:   true,
			SnapshotEvery: 100000,
			BatchConfig: storage.BatchConfig{
				Enabled:       true,
				MaxBatchSize:  100,
				MaxBatchBytes: 1024 * 1024,
				MaxWaitTime:   1 * time.Millisecond,
			},
		})
		require.NoError(b, err)
		defer store.Close()

		b.ResetTimer()

		var wg sync.WaitGroup
		opsPerWorker := b.N / workers

		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				for i := 0; i < opsPerWorker; i++ {
					key := fmt.Sprintf("key-w%d-%d", workerID, i)
					value := []byte(fmt.Sprintf("value-%d", i))
					_ = store.Put(ctx, key, value)
				}
			}(w)
		}

		wg.Wait()
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
	})

	b.Run("LargeBatch-1000ops-10Workers", func(b *testing.B) {
		tmpDir := b.TempDir()
		store, err := storage.NewDurableStore(storage.DurableStoreConfig{
			DataDir:       tmpDir,
			SyncOnWrite:   true,
			SnapshotEvery: 100000,
			BatchConfig: storage.BatchConfig{
				Enabled:       true,
				MaxBatchSize:  1000,
				MaxBatchBytes: 10 * 1024 * 1024,
				MaxWaitTime:   1 * time.Millisecond,
			},
		})
		require.NoError(b, err)
		defer store.Close()

		b.ResetTimer()

		var wg sync.WaitGroup
		opsPerWorker := b.N / workers

		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				for i := 0; i < opsPerWorker; i++ {
					key := fmt.Sprintf("key-w%d-%d", workerID, i)
					value := []byte(fmt.Sprintf("value-%d", i))
					_ = store.Put(ctx, key, value)
				}
			}(w)
		}

		wg.Wait()
		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "ops/sec")
	})
}
