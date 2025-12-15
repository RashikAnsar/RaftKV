package queue

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPriorityQueue(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-priority-queue")
		config.Type = "priority"

		q, err := NewPriorityQueue(config, store)
		assert.NoError(t, err)
		assert.NotNil(t, q)

		q.Close()
	})

	t.Run("NilConfig", func(t *testing.T) {
		store := NewMockStore()

		q, err := NewPriorityQueue(nil, store)
		assert.Error(t, err)
		assert.Nil(t, q)
		assert.Contains(t, err.Error(), "config cannot be nil")
	})

	t.Run("NilStore", func(t *testing.T) {
		config := DefaultQueueConfig("test-priority-queue")
		config.Type = "priority"

		q, err := NewPriorityQueue(config, nil)
		assert.Error(t, err)
		assert.Nil(t, q)
		assert.Contains(t, err.Error(), "store cannot be nil")
	})
}

func TestPriorityQueue_Enqueue(t *testing.T) {
	t.Run("BasicEnqueue", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")
		config.Type = "priority"

		q, err := NewPriorityQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		msg := NewMessage("test-queue", []byte("test message")).WithPriority(5)
		err = q.Enqueue(msg)
		assert.NoError(t, err)

		assert.Equal(t, 1, q.Len())
	})

	t.Run("EnqueueMultiplePriorities", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")
		config.Type = "priority"

		q, err := NewPriorityQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		priorities := []int{1, 5, 3, 10, 2}
		for _, priority := range priorities {
			msg := NewMessage("test-queue", []byte(fmt.Sprintf("priority-%d", priority))).
				WithPriority(priority)
			err = q.Enqueue(msg)
			assert.NoError(t, err)
		}

		assert.Equal(t, 5, q.Len())
	})
}

func TestPriorityQueue_DequeuePriorityOrder(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")
	config.Type = "priority"

	q, err := NewPriorityQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	// Enqueue messages with different priorities
	priorities := []int{3, 10, 1, 7, 5}
	expectedOrder := []int{10, 7, 5, 3, 1} // Highest priority first

	for _, priority := range priorities {
		msg := NewMessage("test-queue", []byte(fmt.Sprintf("priority-%d", priority))).
			WithPriority(priority)
		err = q.Enqueue(msg)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	// Dequeue should return messages in priority order
	for _, expectedPriority := range expectedOrder {
		msg, err := q.Dequeue()
		assert.NoError(t, err)
		assert.Equal(t, expectedPriority, msg.Priority)
	}
}

func TestPriorityQueue_SamePriorityFIFOOrder(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")
	config.Type = "priority"

	q, err := NewPriorityQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	// Enqueue multiple messages with same priority
	messages := make([]*Message, 3)
	for i := 0; i < 3; i++ {
		msg := NewMessage("test-queue", []byte(fmt.Sprintf("msg-%d", i))).
			WithPriority(5) // Same priority
		messages[i] = msg
		err = q.Enqueue(msg)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	// Within same priority, should be FIFO
	for i := 0; i < 3; i++ {
		msg, err := q.Dequeue()
		assert.NoError(t, err)
		assert.Equal(t, messages[i].ID, msg.ID)
	}
}

func TestPriorityQueue_MixedPriorityAndFIFO(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")
	config.Type = "priority"

	q, err := NewPriorityQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	// Enqueue messages:
	// Priority 1: msg1, msg2
	// Priority 5: msg3, msg4
	// Priority 1: msg5

	msg1 := NewMessage("test-queue", []byte("msg1")).WithPriority(1)
	msg2 := NewMessage("test-queue", []byte("msg2")).WithPriority(1)
	msg3 := NewMessage("test-queue", []byte("msg3")).WithPriority(5)
	msg4 := NewMessage("test-queue", []byte("msg4")).WithPriority(5)
	msg5 := NewMessage("test-queue", []byte("msg5")).WithPriority(1)

	for _, msg := range []*Message{msg1, msg2, msg3, msg4, msg5} {
		err = q.Enqueue(msg)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond)
	}

	// Expected dequeue order: msg3, msg4 (priority 5), then msg1, msg2, msg5 (priority 1)
	expectedIDs := []string{msg3.ID, msg4.ID, msg1.ID, msg2.ID, msg5.ID}

	for _, expectedID := range expectedIDs {
		msg, err := q.Dequeue()
		assert.NoError(t, err)
		assert.Equal(t, expectedID, msg.ID)
	}
}

func TestPriorityQueue_Peek(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")
	config.Type = "priority"

	q, err := NewPriorityQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	lowPriorityMsg := NewMessage("test-queue", []byte("low")).WithPriority(1)
	highPriorityMsg := NewMessage("test-queue", []byte("high")).WithPriority(10)

	err = q.Enqueue(lowPriorityMsg)
	require.NoError(t, err)
	err = q.Enqueue(highPriorityMsg)
	require.NoError(t, err)

	// Peek should return highest priority message
	peeked, err := q.Peek()
	assert.NoError(t, err)
	assert.Equal(t, highPriorityMsg.ID, peeked.ID)
	assert.Equal(t, 10, peeked.Priority)

	// Queue length should not change
	assert.Equal(t, 2, q.Len())
}

func TestPriorityQueue_Acknowledge(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")
	config.Type = "priority"

	q, err := NewPriorityQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	msg := NewMessage("test-queue", []byte("test")).WithPriority(5)
	err = q.Enqueue(msg)
	require.NoError(t, err)

	dequeuedMsg, err := q.Dequeue()
	require.NoError(t, err)

	err = q.Acknowledge(dequeuedMsg.ID)
	assert.NoError(t, err)
	assert.Equal(t, 0, q.Len())
}

func TestPriorityQueue_Nack(t *testing.T) {
	t.Run("NackWithRequeue", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")
		config.Type = "priority"

		q, err := NewPriorityQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		msg := NewMessage("test-queue", []byte("test")).WithPriority(5)
		err = q.Enqueue(msg)
		require.NoError(t, err)

		dequeuedMsg, err := q.Dequeue()
		require.NoError(t, err)

		err = q.Nack(dequeuedMsg.ID, true)
		assert.NoError(t, err)
		assert.Equal(t, 1, q.Len())
	})

	t.Run("NackWithoutRequeue", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")
		config.Type = "priority"

		q, err := NewPriorityQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		msg := NewMessage("test-queue", []byte("test")).WithPriority(5)
		err = q.Enqueue(msg)
		require.NoError(t, err)

		dequeuedMsg, err := q.Dequeue()
		require.NoError(t, err)

		err = q.Nack(dequeuedMsg.ID, false)
		assert.NoError(t, err)
		assert.Equal(t, 0, q.Len())
	})
}

func TestPriorityQueue_Delete(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")
	config.Type = "priority"

	q, err := NewPriorityQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	msg := NewMessage("test-queue", []byte("test")).WithPriority(5)
	err = q.Enqueue(msg)
	require.NoError(t, err)

	err = q.Delete(msg.ID)
	assert.NoError(t, err)
	assert.Equal(t, 0, q.Len())
}

func TestPriorityQueue_Get(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")
	config.Type = "priority"

	q, err := NewPriorityQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	msg := NewMessage("test-queue", []byte("test")).WithPriority(5)
	err = q.Enqueue(msg)
	require.NoError(t, err)

	retrievedMsg, err := q.Get(msg.ID)
	assert.NoError(t, err)
	assert.Equal(t, msg.ID, retrievedMsg.ID)
	assert.Equal(t, msg.Priority, retrievedMsg.Priority)
}

func TestPriorityQueue_Stats(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")
	config.Type = "priority"

	q, err := NewPriorityQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	for i := 0; i < 3; i++ {
		msg := NewMessage("test-queue", []byte(fmt.Sprintf("msg-%d", i))).WithPriority(i)
		err = q.Enqueue(msg)
		require.NoError(t, err)
	}

	msg, err := q.Dequeue()
	require.NoError(t, err)
	err = q.Acknowledge(msg.ID)
	require.NoError(t, err)

	stats := q.Stats()
	assert.Equal(t, "test-queue", stats.Name)
	assert.Equal(t, 2, stats.Length)
	assert.Equal(t, int64(1), stats.DeliveredCount)
	assert.Equal(t, int64(1), stats.AcknowledgedCount)
}

func TestPriorityQueue_Purge(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")
	config.Type = "priority"

	q, err := NewPriorityQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	for i := 0; i < 5; i++ {
		msg := NewMessage("test-queue", []byte(fmt.Sprintf("msg-%d", i))).WithPriority(i)
		err = q.Enqueue(msg)
		require.NoError(t, err)
	}

	assert.Equal(t, 5, q.Len())

	err = q.Purge()
	assert.NoError(t, err)
	assert.Equal(t, 0, q.Len())
}

func TestPriorityQueue_VisibilityTimeout(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")
	config.Type = "priority"
	config.VisibilityTimeout = 100 * time.Millisecond

	q, err := NewPriorityQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	msg := NewMessage("test-queue", []byte("test")).WithPriority(5)
	err = q.Enqueue(msg)
	require.NoError(t, err)

	// Dequeue message
	dequeuedMsg, err := q.Dequeue()
	require.NoError(t, err)
	assert.Equal(t, msg.ID, dequeuedMsg.ID)

	// Try to dequeue again immediately - should not return the message
	msg2, err := q.Dequeue()
	assert.Error(t, err)
	assert.Nil(t, msg2)

	// Wait for visibility timeout
	time.Sleep(150 * time.Millisecond)

	// Message should be available again
	msg3, err := q.Dequeue()
	assert.NoError(t, err)
	assert.NotNil(t, msg3)
	assert.Equal(t, msg.ID, msg3.ID)
	assert.Equal(t, 2, msg3.DeliveryCount)
}

func TestPriorityQueue_NegativePriority(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")
	config.Type = "priority"

	q, err := NewPriorityQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	// Enqueue messages with negative, zero, and positive priorities
	priorities := []int{-5, 0, 5, -10, 10}
	expectedOrder := []int{10, 5, 0, -5, -10} // Highest to lowest

	for _, priority := range priorities {
		msg := NewMessage("test-queue", []byte(fmt.Sprintf("priority-%d", priority))).
			WithPriority(priority)
		err = q.Enqueue(msg)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond)
	}

	for _, expectedPriority := range expectedOrder {
		msg, err := q.Dequeue()
		assert.NoError(t, err)
		assert.Equal(t, expectedPriority, msg.Priority)
	}
}

func TestPriorityQueue_ExpiredMessage(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")
	config.Type = "priority"

	q, err := NewPriorityQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	// Enqueue expired message with high priority
	expiredMsg := NewMessage("test-queue", []byte("expired")).
		WithPriority(10).
		WithExpiresAt(time.Now().Add(-1 * time.Second))
	err = q.Enqueue(expiredMsg)
	require.NoError(t, err)

	// Enqueue valid message with lower priority
	validMsg := NewMessage("test-queue", []byte("valid")).WithPriority(5)
	err = q.Enqueue(validMsg)
	require.NoError(t, err)

	// Dequeue should skip expired message and return valid one
	msg, err := q.Dequeue()
	assert.NoError(t, err)
	assert.Equal(t, validMsg.ID, msg.ID)
}

func TestPriorityQueue_ScheduledMessage(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")
	config.Type = "priority"

	q, err := NewPriorityQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	// Enqueue scheduled message with high priority
	scheduledMsg := NewMessage("test-queue", []byte("scheduled")).
		WithPriority(10).
		WithScheduledAt(time.Now().Add(1 * time.Hour))
	err = q.Enqueue(scheduledMsg)
	require.NoError(t, err)

	// Enqueue ready message with lower priority
	readyMsg := NewMessage("test-queue", []byte("ready")).WithPriority(5)
	err = q.Enqueue(readyMsg)
	require.NoError(t, err)

	// Dequeue should skip scheduled message and return ready one
	msg, err := q.Dequeue()
	assert.NoError(t, err)
	assert.Equal(t, readyMsg.ID, msg.ID)
}
