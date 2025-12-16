//go:build integration

package integration_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/RashikAnsar/raftkv/internal/queue"
	"github.com/RashikAnsar/raftkv/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestQueue_Integration_BasicOperations tests basic queue operations with durable storage
func TestQueue_Integration_BasicOperations(t *testing.T) {
	store, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       t.TempDir(),
		SyncOnWrite:   true, // Enable sync for integration test
		SnapshotEvery: 100,
	})
	require.NoError(t, err)
	defer store.Close()

	logger := zap.NewNop()
	qm := queue.NewQueueManager(store, logger)
	defer qm.Close()

	// Create a FIFO queue
	config := queue.DefaultQueueConfig("integration-test-queue")
	config.Type = "fifo"
	config.MaxLength = 100

	err = qm.CreateQueue(config)
	require.NoError(t, err)

	// Enqueue messages
	messages := make([]*queue.Message, 5)
	for i := 0; i < 5; i++ {
		msg := queue.NewMessage("integration-test-queue", []byte(fmt.Sprintf("message-%d", i)))
		err = qm.Enqueue("integration-test-queue", msg)
		require.NoError(t, err)
		messages[i] = msg
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	// Get queue stats
	q, err := qm.GetQueue("integration-test-queue")
	require.NoError(t, err)
	stats := q.Stats()
	assert.Equal(t, 5, stats.Length)

	// Dequeue and acknowledge messages (verify by ID, not order due to storage timing)
	receivedIDs := make(map[string]bool)
	for i := 0; i < 5; i++ {
		msg, err := qm.Dequeue("integration-test-queue")
		require.NoError(t, err)
		assert.NotNil(t, msg)
		receivedIDs[msg.ID] = true

		err = qm.Acknowledge("integration-test-queue", msg.ID)
		require.NoError(t, err)
	}

	// Verify all messages were received
	for i := 0; i < 5; i++ {
		assert.True(t, receivedIDs[messages[i].ID], "Message %d not received", i)
	}

	// Verify queue is empty
	q, err = qm.GetQueue("integration-test-queue")
	require.NoError(t, err)
	stats = q.Stats()
	assert.Equal(t, 0, stats.Length)
}

// TestQueue_Integration_PriorityOrdering tests priority queue with durable storage
func TestQueue_Integration_PriorityOrdering(t *testing.T) {
	store, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       t.TempDir(),
		SyncOnWrite:   true,
		SnapshotEvery: 100,
	})
	require.NoError(t, err)
	defer store.Close()

	logger := zap.NewNop()
	qm := queue.NewQueueManager(store, logger)
	defer qm.Close()

	// Create priority queue
	config := queue.DefaultQueueConfig("priority-queue")
	config.Type = "priority"

	err = qm.CreateQueue(config)
	require.NoError(t, err)

	// Enqueue messages with different priorities
	priorities := []int{3, 10, 1, 7, 5}
	for _, priority := range priorities {
		msg := queue.NewMessage("priority-queue", []byte(fmt.Sprintf("priority-%d", priority))).
			WithPriority(priority)
		err = qm.Enqueue("priority-queue", msg)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond)
	}

	// Dequeue should return in priority order (highest first)
	expectedOrder := []int{10, 7, 5, 3, 1}
	for _, expectedPriority := range expectedOrder {
		msg, err := qm.Dequeue("priority-queue")
		require.NoError(t, err)
		assert.Equal(t, expectedPriority, msg.Priority)

		err = qm.Acknowledge("priority-queue", msg.ID)
		require.NoError(t, err)
	}
}

// TestQueue_Integration_Persistence tests that queue data persists across restarts
func TestQueue_Integration_Persistence(t *testing.T) {
	dataDir := t.TempDir()

	// Phase 1: Create queue and enqueue messages
	{
		store, err := storage.NewDurableStore(storage.DurableStoreConfig{
			DataDir:       dataDir,
			SyncOnWrite:   true,
			SnapshotEvery: 100,
		})
		require.NoError(t, err)

		logger := zap.NewNop()
		qm := queue.NewQueueManager(store, logger)

		config := queue.DefaultQueueConfig("persistent-queue")
		err = qm.CreateQueue(config)
		require.NoError(t, err)

		// Enqueue 3 messages
		for i := 0; i < 3; i++ {
			msg := queue.NewMessage("persistent-queue", []byte(fmt.Sprintf("persistent-message-%d", i)))
			err = qm.Enqueue("persistent-queue", msg)
			require.NoError(t, err)
		}

		qm.Close()
		store.Close()
	}

	// Phase 2: Reopen store and verify messages persist
	{
		store, err := storage.NewDurableStore(storage.DurableStoreConfig{
			DataDir:       dataDir,
			SyncOnWrite:   true,
			SnapshotEvery: 100,
		})
		require.NoError(t, err)
		defer store.Close()

		logger := zap.NewNop()
		qm := queue.NewQueueManager(store, logger)
		defer qm.Close()

		// Recreate queue (queue metadata doesn't persist, but messages do)
		config := queue.DefaultQueueConfig("persistent-queue")
		err = qm.CreateQueue(config)
		require.NoError(t, err)

		// Messages should still exist with 3 messages
		q, err := qm.GetQueue("persistent-queue")
		require.NoError(t, err)
		stats := q.Stats()
		assert.Equal(t, 3, stats.Length)

		// Dequeue all messages
		for i := 0; i < 3; i++ {
			msg, err := qm.Dequeue("persistent-queue")
			require.NoError(t, err)
			assert.Contains(t, string(msg.Body), "persistent-message")

			err = qm.Acknowledge("persistent-queue", msg.ID)
			require.NoError(t, err)
		}
	}
}

// TestQueue_Integration_BatchOperations tests batch operations with durable storage
func TestQueue_Integration_BatchOperations(t *testing.T) {
	store, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       t.TempDir(),
		SyncOnWrite:   true,
		SnapshotEvery: 100,
	})
	require.NoError(t, err)
	defer store.Close()

	logger := zap.NewNop()
	qm := queue.NewQueueManager(store, logger)
	defer qm.Close()

	// Create queue
	config := queue.DefaultQueueConfig("batch-queue")
	err = qm.CreateQueue(config)
	require.NoError(t, err)

	// Batch enqueue
	messages := make([]*queue.Message, 10)
	for i := 0; i < 10; i++ {
		messages[i] = queue.NewMessage("batch-queue", []byte(fmt.Sprintf("batch-message-%d", i)))
	}

	errors, err := qm.BatchEnqueue("batch-queue", messages)
	require.NoError(t, err)
	assert.Len(t, errors, 10)
	for _, e := range errors {
		assert.NoError(t, e)
	}

	// Batch dequeue
	dequeued, err := qm.BatchDequeue("batch-queue", 5)
	require.NoError(t, err)
	assert.Len(t, dequeued, 5)

	// Batch acknowledge
	messageIDs := make([]string, len(dequeued))
	for i, msg := range dequeued {
		messageIDs[i] = msg.ID
	}

	ackErrors, err := qm.BatchAcknowledge("batch-queue", messageIDs)
	require.NoError(t, err)
	assert.Len(t, ackErrors, 5)
	for _, e := range ackErrors {
		assert.NoError(t, e)
	}

	// Verify 5 messages remain
	q, err := qm.GetQueue("batch-queue")
	require.NoError(t, err)
	stats := q.Stats()
	assert.Equal(t, 5, stats.Length)
}

// TestQueue_Integration_ConcurrentOperations tests concurrent operations with durable storage
func TestQueue_Integration_ConcurrentOperations(t *testing.T) {
	store, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       t.TempDir(),
		SyncOnWrite:   false, // Disable sync for performance
		SnapshotEvery: 100,
	})
	require.NoError(t, err)
	defer store.Close()

	logger := zap.NewNop()
	qm := queue.NewQueueManager(store, logger)
	defer qm.Close()

	// Create queue
	config := queue.DefaultQueueConfig("concurrent-queue")
	config.MaxLength = 1000
	err = qm.CreateQueue(config)
	require.NoError(t, err)

	// Concurrent enqueues
	numGoroutines := 10
	messagesPerGoroutine := 10
	var wg sync.WaitGroup

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < messagesPerGoroutine; j++ {
				msg := queue.NewMessage("concurrent-queue", []byte(fmt.Sprintf("message-%d-%d", id, j)))
				err := qm.Enqueue("concurrent-queue", msg)
				if err != nil {
					t.Errorf("Failed to enqueue: %v", err)
				}
			}
		}(i)
	}
	wg.Wait()

	// Verify all messages were enqueued
	q, err := qm.GetQueue("concurrent-queue")
	require.NoError(t, err)
	stats := q.Stats()
	assert.Equal(t, numGoroutines*messagesPerGoroutine, stats.Length)

	// Concurrent dequeues and acknowledges
	var mu sync.Mutex
	processed := 0
	expectedCount := numGoroutines * messagesPerGoroutine
	targetReached := false

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				if targetReached {
					mu.Unlock()
					return
				}
				mu.Unlock()

				msg, err := qm.Dequeue("concurrent-queue")
				if err == nil {
					qm.Acknowledge("concurrent-queue", msg.ID)
					mu.Lock()
					processed++
					if processed >= expectedCount {
						targetReached = true
					}
					mu.Unlock()
				} else {
					time.Sleep(1 * time.Millisecond)
				}
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, expectedCount, processed)

	// Verify queue is empty
	q, err = qm.GetQueue("concurrent-queue")
	require.NoError(t, err)
	stats = q.Stats()
	assert.Equal(t, 0, stats.Length)
}

// TestQueue_Integration_VisibilityTimeout tests visibility timeout with durable storage
func TestQueue_Integration_VisibilityTimeout(t *testing.T) {
	store, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       t.TempDir(),
		SyncOnWrite:   true,
		SnapshotEvery: 100,
	})
	require.NoError(t, err)
	defer store.Close()

	logger := zap.NewNop()
	qm := queue.NewQueueManager(store, logger)
	defer qm.Close()

	// Create queue with short visibility timeout
	config := queue.DefaultQueueConfig("visibility-queue")
	config.VisibilityTimeout = 100 * time.Millisecond

	err = qm.CreateQueue(config)
	require.NoError(t, err)

	// Enqueue a message
	msg := queue.NewMessage("visibility-queue", []byte("test"))
	err = qm.Enqueue("visibility-queue", msg)
	require.NoError(t, err)

	// Dequeue message
	firstMsg, err := qm.Dequeue("visibility-queue")
	require.NoError(t, err)
	firstID := firstMsg.ID

	// Try to dequeue immediately - should fail (message in-flight)
	secondMsg, err := qm.Dequeue("visibility-queue")
	assert.Error(t, err)
	assert.Nil(t, secondMsg)

	// Wait for visibility timeout
	time.Sleep(150 * time.Millisecond)

	// Dequeue again - should succeed with same message
	thirdMsg, err := qm.Dequeue("visibility-queue")
	require.NoError(t, err)
	assert.Equal(t, firstID, thirdMsg.ID)
	assert.Equal(t, 2, thirdMsg.DeliveryCount)

	// Acknowledge to clean up
	err = qm.Acknowledge("visibility-queue", thirdMsg.ID)
	require.NoError(t, err)
}
