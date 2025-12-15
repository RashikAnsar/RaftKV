package queue

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewQueueManager(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()

	qm := NewQueueManager(store, logger)
	assert.NotNil(t, qm)
	assert.NotNil(t, qm.queues)

	qm.Close()
}

func TestQueueManager_CreateQueue(t *testing.T) {
	t.Run("CreateFIFOQueue", func(t *testing.T) {
		store := NewMockStore()
		logger := zap.NewNop()
		qm := NewQueueManager(store, logger)
		defer qm.Close()

		config := DefaultQueueConfig("fifo-queue")
		config.Type = "fifo"

		err := qm.CreateQueue(config)
		assert.NoError(t, err)

		queues := qm.ListQueues()
		assert.Contains(t, queues, "fifo-queue")
	})

	t.Run("CreatePriorityQueue", func(t *testing.T) {
		store := NewMockStore()
		logger := zap.NewNop()
		qm := NewQueueManager(store, logger)
		defer qm.Close()

		config := DefaultQueueConfig("priority-queue")
		config.Type = "priority"

		err := qm.CreateQueue(config)
		assert.NoError(t, err)

		queues := qm.ListQueues()
		assert.Contains(t, queues, "priority-queue")
	})

	t.Run("CreateDuplicateQueue", func(t *testing.T) {
		store := NewMockStore()
		logger := zap.NewNop()
		qm := NewQueueManager(store, logger)
		defer qm.Close()

		config := DefaultQueueConfig("test-queue")

		err := qm.CreateQueue(config)
		assert.NoError(t, err)

		// Try to create again
		err = qm.CreateQueue(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("CreateWithNilConfig", func(t *testing.T) {
		store := NewMockStore()
		logger := zap.NewNop()
		qm := NewQueueManager(store, logger)
		defer qm.Close()

		err := qm.CreateQueue(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config cannot be nil")
	})

	t.Run("CreateWithEmptyName", func(t *testing.T) {
		store := NewMockStore()
		logger := zap.NewNop()
		qm := NewQueueManager(store, logger)
		defer qm.Close()

		config := DefaultQueueConfig("")

		err := qm.CreateQueue(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "queue name cannot be empty")
	})

	t.Run("CreateUnsupportedType", func(t *testing.T) {
		store := NewMockStore()
		logger := zap.NewNop()
		qm := NewQueueManager(store, logger)
		defer qm.Close()

		config := DefaultQueueConfig("test-queue")
		config.Type = "unsupported"

		err := qm.CreateQueue(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported queue type")
	})
}

func TestQueueManager_DeleteQueue(t *testing.T) {
	t.Run("DeleteExistingQueue", func(t *testing.T) {
		store := NewMockStore()
		logger := zap.NewNop()
		qm := NewQueueManager(store, logger)
		defer qm.Close()

		config := DefaultQueueConfig("test-queue")
		err := qm.CreateQueue(config)
		require.NoError(t, err)

		err = qm.DeleteQueue("test-queue")
		assert.NoError(t, err)

		queues := qm.ListQueues()
		assert.NotContains(t, queues, "test-queue")
	})

	t.Run("DeleteNonExistentQueue", func(t *testing.T) {
		store := NewMockStore()
		logger := zap.NewNop()
		qm := NewQueueManager(store, logger)
		defer qm.Close()

		err := qm.DeleteQueue("non-existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "queue not found")
	})

	t.Run("DeleteQueueWithMessages", func(t *testing.T) {
		store := NewMockStore()
		logger := zap.NewNop()
		qm := NewQueueManager(store, logger)
		defer qm.Close()

		config := DefaultQueueConfig("test-queue")
		err := qm.CreateQueue(config)
		require.NoError(t, err)

		// Enqueue some messages
		msg := NewMessage("test-queue", []byte("test"))
		err = qm.Enqueue("test-queue", msg)
		require.NoError(t, err)

		// Delete should purge messages
		err = qm.DeleteQueue("test-queue")
		assert.NoError(t, err)
	})
}

func TestQueueManager_GetQueue(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	config := DefaultQueueConfig("test-queue")
	err := qm.CreateQueue(config)
	require.NoError(t, err)

	q, err := qm.GetQueue("test-queue")
	assert.NoError(t, err)
	assert.NotNil(t, q)

	_, err = qm.GetQueue("non-existent")
	assert.Error(t, err)
}

func TestQueueManager_ListQueues(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	// Create multiple queues
	for i := 0; i < 3; i++ {
		config := DefaultQueueConfig("queue-" + string(rune('A'+i)))
		err := qm.CreateQueue(config)
		require.NoError(t, err)
	}

	queues := qm.ListQueues()
	assert.Len(t, queues, 3)
	assert.Contains(t, queues, "queue-A")
	assert.Contains(t, queues, "queue-B")
	assert.Contains(t, queues, "queue-C")
}

func TestQueueManager_GetQueueStats(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	config1 := DefaultQueueConfig("queue-1")
	err := qm.CreateQueue(config1)
	require.NoError(t, err)

	config2 := DefaultQueueConfig("queue-2")
	err = qm.CreateQueue(config2)
	require.NoError(t, err)

	stats := qm.GetQueueStats()
	assert.Len(t, stats, 2)
	assert.NotNil(t, stats["queue-1"])
	assert.NotNil(t, stats["queue-2"])
}

func TestQueueManager_Enqueue(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	config := DefaultQueueConfig("test-queue")
	err := qm.CreateQueue(config)
	require.NoError(t, err)

	msg := NewMessage("test-queue", []byte("test"))
	err = qm.Enqueue("test-queue", msg)
	assert.NoError(t, err)

	// Try to enqueue to non-existent queue
	err = qm.Enqueue("non-existent", msg)
	assert.Error(t, err)
}

func TestQueueManager_Dequeue(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	config := DefaultQueueConfig("test-queue")
	err := qm.CreateQueue(config)
	require.NoError(t, err)

	original := NewMessage("test-queue", []byte("test"))
	err = qm.Enqueue("test-queue", original)
	require.NoError(t, err)

	msg, err := qm.Dequeue("test-queue")
	assert.NoError(t, err)
	assert.Equal(t, original.ID, msg.ID)

	// Try to dequeue from non-existent queue
	_, err = qm.Dequeue("non-existent")
	assert.Error(t, err)
}

func TestQueueManager_Peek(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	config := DefaultQueueConfig("test-queue")
	err := qm.CreateQueue(config)
	require.NoError(t, err)

	msg := NewMessage("test-queue", []byte("test"))
	err = qm.Enqueue("test-queue", msg)
	require.NoError(t, err)

	peeked, err := qm.Peek("test-queue")
	assert.NoError(t, err)
	assert.Equal(t, msg.ID, peeked.ID)
}

func TestQueueManager_Acknowledge(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	config := DefaultQueueConfig("test-queue")
	err := qm.CreateQueue(config)
	require.NoError(t, err)

	msg := NewMessage("test-queue", []byte("test"))
	err = qm.Enqueue("test-queue", msg)
	require.NoError(t, err)

	dequeuedMsg, err := qm.Dequeue("test-queue")
	require.NoError(t, err)

	err = qm.Acknowledge("test-queue", dequeuedMsg.ID)
	assert.NoError(t, err)
}

func TestQueueManager_Nack(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	config := DefaultQueueConfig("test-queue")
	err := qm.CreateQueue(config)
	require.NoError(t, err)

	msg := NewMessage("test-queue", []byte("test"))
	err = qm.Enqueue("test-queue", msg)
	require.NoError(t, err)

	dequeuedMsg, err := qm.Dequeue("test-queue")
	require.NoError(t, err)

	err = qm.Nack("test-queue", dequeuedMsg.ID, true)
	assert.NoError(t, err)
}

func TestQueueManager_Delete(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	config := DefaultQueueConfig("test-queue")
	err := qm.CreateQueue(config)
	require.NoError(t, err)

	msg := NewMessage("test-queue", []byte("test"))
	err = qm.Enqueue("test-queue", msg)
	require.NoError(t, err)

	err = qm.Delete("test-queue", msg.ID)
	assert.NoError(t, err)
}

func TestQueueManager_Get(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	config := DefaultQueueConfig("test-queue")
	err := qm.CreateQueue(config)
	require.NoError(t, err)

	msg := NewMessage("test-queue", []byte("test"))
	err = qm.Enqueue("test-queue", msg)
	require.NoError(t, err)

	retrieved, err := qm.Get("test-queue", msg.ID)
	assert.NoError(t, err)
	assert.Equal(t, msg.ID, retrieved.ID)
}

func TestQueueManager_Purge(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	config := DefaultQueueConfig("test-queue")
	err := qm.CreateQueue(config)
	require.NoError(t, err)

	// Enqueue multiple messages
	for i := 0; i < 5; i++ {
		msg := NewMessage("test-queue", []byte("test"))
		err = qm.Enqueue("test-queue", msg)
		require.NoError(t, err)
	}

	err = qm.Purge("test-queue")
	assert.NoError(t, err)

	q, _ := qm.GetQueue("test-queue")
	assert.Equal(t, 0, q.Len())
}

func TestQueueManager_BatchEnqueue(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	config := DefaultQueueConfig("test-queue")
	err := qm.CreateQueue(config)
	require.NoError(t, err)

	messages := make([]*Message, 3)
	for i := 0; i < 3; i++ {
		messages[i] = NewMessage("test-queue", []byte("test"))
	}

	errors, err := qm.BatchEnqueue("test-queue", messages)
	assert.NoError(t, err)
	assert.Len(t, errors, 3)

	// All should succeed
	for _, err := range errors {
		assert.NoError(t, err)
	}

	q, _ := qm.GetQueue("test-queue")
	assert.Equal(t, 3, q.Len())
}

func TestQueueManager_BatchDequeue(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	config := DefaultQueueConfig("test-queue")
	err := qm.CreateQueue(config)
	require.NoError(t, err)

	// Enqueue 5 messages
	for i := 0; i < 5; i++ {
		msg := NewMessage("test-queue", []byte("test"))
		err = qm.Enqueue("test-queue", msg)
		require.NoError(t, err)
	}

	// Dequeue 3 messages
	messages, err := qm.BatchDequeue("test-queue", 3)
	assert.NoError(t, err)
	assert.Len(t, messages, 3)

	q, _ := qm.GetQueue("test-queue")
	assert.Equal(t, 5, q.Len()) // Still 5 because not acknowledged
}

func TestQueueManager_BatchAcknowledge(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	config := DefaultQueueConfig("test-queue")
	err := qm.CreateQueue(config)
	require.NoError(t, err)

	// Enqueue and dequeue messages
	messageIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		msg := NewMessage("test-queue", []byte("test"))
		err = qm.Enqueue("test-queue", msg)
		require.NoError(t, err)

		dequeuedMsg, err := qm.Dequeue("test-queue")
		require.NoError(t, err)
		messageIDs[i] = dequeuedMsg.ID
	}

	// Batch acknowledge
	errors, err := qm.BatchAcknowledge("test-queue", messageIDs)
	assert.NoError(t, err)
	assert.Len(t, errors, 3)

	// All should succeed
	for _, err := range errors {
		assert.NoError(t, err)
	}

	q, _ := qm.GetQueue("test-queue")
	assert.Equal(t, 0, q.Len())
}

func TestQueueManager_GetQueueConfig(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	config := DefaultQueueConfig("test-queue")
	config.MaxLength = 1000
	config.MaxRetries = 5
	err := qm.CreateQueue(config)
	require.NoError(t, err)

	retrievedConfig, err := qm.GetQueueConfig("test-queue")
	assert.NoError(t, err)
	assert.Equal(t, "test-queue", retrievedConfig.Name)
	assert.Equal(t, 1000, retrievedConfig.MaxLength)
	assert.Equal(t, 5, retrievedConfig.MaxRetries)
}

func TestQueueManager_LoadQueues(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()

	// Create first manager and add queues
	qm1 := NewQueueManager(store, logger)

	config1 := DefaultQueueConfig("queue-1")
	err := qm1.CreateQueue(config1)
	require.NoError(t, err)

	config2 := DefaultQueueConfig("queue-2")
	config2.Type = "priority"
	err = qm1.CreateQueue(config2)
	require.NoError(t, err)

	qm1.Close()

	// Create second manager and load queues
	qm2 := NewQueueManager(store, logger)
	defer qm2.Close()

	err = qm2.LoadQueues()
	assert.NoError(t, err)

	queues := qm2.ListQueues()
	assert.Len(t, queues, 2)
	assert.Contains(t, queues, "queue-1")
	assert.Contains(t, queues, "queue-2")
}

func TestQueueManager_MultipleQueueTypes(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	// Create FIFO queue
	fifoConfig := DefaultQueueConfig("fifo-queue")
	fifoConfig.Type = "fifo"
	err := qm.CreateQueue(fifoConfig)
	require.NoError(t, err)

	// Create Priority queue
	priorityConfig := DefaultQueueConfig("priority-queue")
	priorityConfig.Type = "priority"
	err = qm.CreateQueue(priorityConfig)
	require.NoError(t, err)

	// Enqueue to both
	msg1 := NewMessage("fifo-queue", []byte("fifo"))
	err = qm.Enqueue("fifo-queue", msg1)
	assert.NoError(t, err)

	msg2 := NewMessage("priority-queue", []byte("priority")).WithPriority(10)
	err = qm.Enqueue("priority-queue", msg2)
	assert.NoError(t, err)

	// Dequeue from both
	dequeuedFifo, err := qm.Dequeue("fifo-queue")
	assert.NoError(t, err)
	assert.Equal(t, msg1.ID, dequeuedFifo.ID)

	dequeuedPriority, err := qm.Dequeue("priority-queue")
	assert.NoError(t, err)
	assert.Equal(t, msg2.ID, dequeuedPriority.ID)
}

func TestQueueManager_StartScheduledMessageProcessor(t *testing.T) {
	store := NewMockStore()
	logger := zap.NewNop()
	qm := NewQueueManager(store, logger)
	defer qm.Close()

	stopCh := qm.StartScheduledMessageProcessor(50 * time.Millisecond)

	// Let it run for a bit
	time.Sleep(100 * time.Millisecond)

	// Stop processor
	close(stopCh)

	// Give it time to stop
	time.Sleep(100 * time.Millisecond)
}
