package queue

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/RashikAnsar/raftkv/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockStore implements storage.Store for testing
type MockStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func NewMockStore() *MockStore {
	return &MockStore{
		data: make(map[string][]byte),
	}
}

func (m *MockStore) Get(ctx context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	value, exists := m.data[key]
	if !exists {
		return nil, storage.ErrKeyNotFound
	}
	return value, nil
}

func (m *MockStore) Put(ctx context.Context, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data[key] = value
	return nil
}

func (m *MockStore) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.data, key)
	return nil
}

func (m *MockStore) List(ctx context.Context, prefix string, limit int) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keys := make([]string, 0)
	for key := range m.data {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}

	// Sort keys lexicographically for correct FIFO/priority ordering
	sort.Strings(keys)

	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}

	return keys, nil
}

func (m *MockStore) CompareAndSwap(ctx context.Context, key string, expectedVersion uint64, newValue []byte) (bool, uint64, error) {
	return false, 0, fmt.Errorf("not implemented")
}

func (m *MockStore) GetWithVersion(ctx context.Context, key string) ([]byte, uint64, error) {
	return nil, 0, fmt.Errorf("not implemented")
}

func (m *MockStore) ListWithOptions(ctx context.Context, opts storage.ListOptions) (*storage.ListResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockStore) Snapshot(ctx context.Context) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (m *MockStore) Restore(ctx context.Context, snapshotPath string) error {
	return fmt.Errorf("not implemented")
}

func (m *MockStore) Stats() storage.Stats {
	return storage.Stats{}
}

func (m *MockStore) Close() error {
	return nil
}

func TestNewFIFOQueue(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")

		q, err := NewFIFOQueue(config, store)
		assert.NoError(t, err)
		assert.NotNil(t, q)

		// Cleanup
		q.Close()
	})

	t.Run("NilConfig", func(t *testing.T) {
		store := NewMockStore()

		q, err := NewFIFOQueue(nil, store)
		assert.Error(t, err)
		assert.Nil(t, q)
		assert.Contains(t, err.Error(), "config cannot be nil")
	})

	t.Run("NilStore", func(t *testing.T) {
		config := DefaultQueueConfig("test-queue")

		q, err := NewFIFOQueue(config, nil)
		assert.Error(t, err)
		assert.Nil(t, q)
		assert.Contains(t, err.Error(), "store cannot be nil")
	})
}

func TestFIFOQueue_Enqueue(t *testing.T) {
	t.Run("BasicEnqueue", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")
		config.MaxLength = 0 // Unlimited

		q, err := NewFIFOQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		msg := NewMessage("test-queue", []byte("test message"))
		err = q.Enqueue(msg)
		assert.NoError(t, err)

		// Verify queue length
		assert.Equal(t, 1, q.Len())
	})

	t.Run("EnqueueMultiple", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")

		q, err := NewFIFOQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		for i := 0; i < 5; i++ {
			msg := NewMessage("test-queue", []byte(fmt.Sprintf("message-%d", i)))
			err = q.Enqueue(msg)
			assert.NoError(t, err)
		}

		assert.Equal(t, 5, q.Len())
	})

	t.Run("EnqueueWithMaxLength", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")
		config.MaxLength = 2

		q, err := NewFIFOQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		// Enqueue up to max length
		msg1 := NewMessage("test-queue", []byte("msg1"))
		err = q.Enqueue(msg1)
		assert.NoError(t, err)

		msg2 := NewMessage("test-queue", []byte("msg2"))
		err = q.Enqueue(msg2)
		assert.NoError(t, err)

		// Try to exceed max length
		msg3 := NewMessage("test-queue", []byte("msg3"))
		err = q.Enqueue(msg3)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "queue is full")
	})

	t.Run("EnqueueWithTTL", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")
		config.MessageTTL = 100 * time.Millisecond

		q, err := NewFIFOQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		msg := NewMessage("test-queue", []byte("test"))
		err = q.Enqueue(msg)
		assert.NoError(t, err)

		// Message should have expiration set
		retrievedMsg, err := q.Peek()
		assert.NoError(t, err)
		assert.NotNil(t, retrievedMsg.ExpiresAt)
	})
}

func TestFIFOQueue_Dequeue(t *testing.T) {
	t.Run("DequeueBasic", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")

		q, err := NewFIFOQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		// Enqueue a message
		original := NewMessage("test-queue", []byte("test message"))
		err = q.Enqueue(original)
		require.NoError(t, err)

		// Dequeue the message
		msg, err := q.Dequeue()
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, original.ID, msg.ID)
		assert.Equal(t, original.Body, msg.Body)
		assert.Equal(t, 1, msg.DeliveryCount)
	})

	t.Run("DequeueFIFOOrder", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")

		q, err := NewFIFOQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		// Enqueue multiple messages
		messages := make([]*Message, 3)
		for i := 0; i < 3; i++ {
			msg := NewMessage("test-queue", []byte(fmt.Sprintf("msg-%d", i)))
			messages[i] = msg
			err = q.Enqueue(msg)
			require.NoError(t, err)
			time.Sleep(10 * time.Millisecond) // Ensure different timestamps
		}

		// Dequeue should return messages in FIFO order
		// We need to acknowledge each message to remove it from in-flight
		for i := 0; i < 3; i++ {
			msg, err := q.Dequeue()
			assert.NoError(t, err)
			assert.NotNil(t, msg)
			assert.Equal(t, messages[i].ID, msg.ID)
			// Acknowledge to remove from queue
			err = q.Acknowledge(msg.ID)
			assert.NoError(t, err)
		}
	})

	t.Run("DequeueEmptyQueue", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")

		q, err := NewFIFOQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		msg, err := q.Dequeue()
		assert.Error(t, err)
		assert.Nil(t, msg)
		assert.Contains(t, err.Error(), "queue is empty")
	})

	t.Run("DequeueExpiredMessage", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")

		q, err := NewFIFOQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		// Enqueue a message with past expiration
		msg := NewMessage("test-queue", []byte("test"))
		expiresAt := time.Now().Add(-1 * time.Second)
		msg.ExpiresAt = &expiresAt
		err = q.Enqueue(msg)
		require.NoError(t, err)

		// Dequeue should fail due to expiration
		dequeuedMsg, err := q.Dequeue()
		assert.Error(t, err)
		assert.Nil(t, dequeuedMsg)
		assert.Contains(t, err.Error(), "expired")
	})

	t.Run("DequeueScheduledMessage", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")

		q, err := NewFIFOQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		// Enqueue a message scheduled for future
		msg := NewMessage("test-queue", []byte("test"))
		scheduledAt := time.Now().Add(1 * time.Hour)
		msg.ScheduledAt = &scheduledAt
		err = q.Enqueue(msg)
		require.NoError(t, err)

		// Dequeue should fail as message is not ready
		dequeuedMsg, err := q.Dequeue()
		assert.Error(t, err)
		assert.Nil(t, dequeuedMsg)
		assert.Contains(t, err.Error(), "no messages ready")
	})
}

func TestFIFOQueue_Peek(t *testing.T) {
	t.Run("PeekDoesNotRemove", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")

		q, err := NewFIFOQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		msg := NewMessage("test-queue", []byte("test"))
		err = q.Enqueue(msg)
		require.NoError(t, err)

		// Peek should return message without removing it
		peeked1, err := q.Peek()
		assert.NoError(t, err)
		assert.Equal(t, msg.ID, peeked1.ID)
		assert.Equal(t, 1, q.Len())

		// Peek again should return same message
		peeked2, err := q.Peek()
		assert.NoError(t, err)
		assert.Equal(t, msg.ID, peeked2.ID)
		assert.Equal(t, 1, q.Len())
	})
}

func TestFIFOQueue_Acknowledge(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")

	q, err := NewFIFOQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	// Enqueue and dequeue a message
	msg := NewMessage("test-queue", []byte("test"))
	err = q.Enqueue(msg)
	require.NoError(t, err)

	dequeuedMsg, err := q.Dequeue()
	require.NoError(t, err)

	// Acknowledge should remove the message
	err = q.Acknowledge(dequeuedMsg.ID)
	assert.NoError(t, err)
	assert.Equal(t, 0, q.Len())
}

func TestFIFOQueue_Nack(t *testing.T) {
	t.Run("NackWithRequeue", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")

		q, err := NewFIFOQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		msg := NewMessage("test-queue", []byte("test"))
		err = q.Enqueue(msg)
		require.NoError(t, err)

		dequeuedMsg, err := q.Dequeue()
		require.NoError(t, err)

		// Nack with requeue should keep message in queue
		err = q.Nack(dequeuedMsg.ID, true)
		assert.NoError(t, err)
		assert.Equal(t, 1, q.Len())
	})

	t.Run("NackWithoutRequeue", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")

		q, err := NewFIFOQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		msg := NewMessage("test-queue", []byte("test"))
		err = q.Enqueue(msg)
		require.NoError(t, err)

		dequeuedMsg, err := q.Dequeue()
		require.NoError(t, err)

		// Nack without requeue should remove message
		err = q.Nack(dequeuedMsg.ID, false)
		assert.NoError(t, err)
		assert.Equal(t, 0, q.Len())
	})

	t.Run("NackExceedMaxRetries", func(t *testing.T) {
		store := NewMockStore()
		config := DefaultQueueConfig("test-queue")
		config.MaxRetries = 2
		config.DLQEnabled = true

		q, err := NewFIFOQueue(config, store)
		require.NoError(t, err)
		defer q.Close()

		msg := NewMessage("test-queue", []byte("test"))
		err = q.Enqueue(msg)
		require.NoError(t, err)

		// Dequeue and nack multiple times to exceed max retries
		for i := 0; i < 3; i++ {
			dequeuedMsg, err := q.Dequeue()
			if err != nil {
				break
			}
			err = q.Nack(dequeuedMsg.ID, true)
			assert.NoError(t, err)
		}

		// Message should be removed (sent to DLQ)
		assert.Equal(t, 0, q.Len())
	})
}

func TestFIFOQueue_Delete(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")

	q, err := NewFIFOQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	msg := NewMessage("test-queue", []byte("test"))
	err = q.Enqueue(msg)
	require.NoError(t, err)

	err = q.Delete(msg.ID)
	assert.NoError(t, err)
	assert.Equal(t, 0, q.Len())
}

func TestFIFOQueue_Get(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")

	q, err := NewFIFOQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	msg := NewMessage("test-queue", []byte("test"))
	err = q.Enqueue(msg)
	require.NoError(t, err)

	retrievedMsg, err := q.Get(msg.ID)
	assert.NoError(t, err)
	assert.Equal(t, msg.ID, retrievedMsg.ID)
	assert.Equal(t, msg.Body, retrievedMsg.Body)
}

func TestFIFOQueue_Stats(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")

	q, err := NewFIFOQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	// Enqueue some messages
	for i := 0; i < 3; i++ {
		msg := NewMessage("test-queue", []byte(fmt.Sprintf("msg-%d", i)))
		err = q.Enqueue(msg)
		require.NoError(t, err)
	}

	// Dequeue and acknowledge one message
	msg, err := q.Dequeue()
	require.NoError(t, err)
	err = q.Acknowledge(msg.ID)
	require.NoError(t, err)

	stats := q.Stats()
	assert.Equal(t, "test-queue", stats.Name)
	assert.Equal(t, 2, stats.Length) // 2 remaining messages
	assert.Equal(t, int64(1), stats.DeliveredCount)
	assert.Equal(t, int64(1), stats.AcknowledgedCount)
}

func TestFIFOQueue_Purge(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")

	q, err := NewFIFOQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	// Enqueue some messages
	for i := 0; i < 5; i++ {
		msg := NewMessage("test-queue", []byte(fmt.Sprintf("msg-%d", i)))
		err = q.Enqueue(msg)
		require.NoError(t, err)
	}

	assert.Equal(t, 5, q.Len())

	err = q.Purge()
	assert.NoError(t, err)
	assert.Equal(t, 0, q.Len())
}

func TestFIFOQueue_VisibilityTimeout(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")
	config.VisibilityTimeout = 100 * time.Millisecond

	q, err := NewFIFOQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	msg := NewMessage("test-queue", []byte("test"))
	err = q.Enqueue(msg)
	require.NoError(t, err)

	// Dequeue message
	dequeuedMsg, err := q.Dequeue()
	require.NoError(t, err)
	assert.Equal(t, msg.ID, dequeuedMsg.ID)

	// Try to dequeue again immediately - should not return the message (in-flight)
	msg2, err := q.Dequeue()
	assert.Error(t, err)
	assert.Nil(t, msg2)

	// Wait for visibility timeout to expire
	time.Sleep(150 * time.Millisecond)

	// Now message should be available again
	msg3, err := q.Dequeue()
	assert.NoError(t, err)
	assert.NotNil(t, msg3)
	assert.Equal(t, msg.ID, msg3.ID)
	assert.Equal(t, 2, msg3.DeliveryCount)
}

func TestFIFOQueue_ConcurrentOperations(t *testing.T) {
	store := NewMockStore()
	config := DefaultQueueConfig("test-queue")

	q, err := NewFIFOQueue(config, store)
	require.NoError(t, err)
	defer q.Close()

	var wg sync.WaitGroup
	numGoroutines := 10
	messagesPerGoroutine := 10

	// Concurrent enqueues
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < messagesPerGoroutine; j++ {
				msg := NewMessage("test-queue", []byte(fmt.Sprintf("msg-%d-%d", id, j)))
				q.Enqueue(msg)
			}
		}(i)
	}
	wg.Wait()

	expectedCount := numGoroutines * messagesPerGoroutine
	assert.Equal(t, expectedCount, q.Len())

	// Concurrent dequeues and acknowledges
	// Workers keep trying until all messages are processed
	var mu sync.Mutex
	dequeuedCount := 0
	targetReached := false

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for {
				// Check if target reached
				mu.Lock()
				if targetReached {
					mu.Unlock()
					return
				}
				mu.Unlock()

				// Try to dequeue
				msg, err := q.Dequeue()
				if err == nil {
					q.Acknowledge(msg.ID)
					mu.Lock()
					dequeuedCount++
					if dequeuedCount >= expectedCount {
						targetReached = true
					}
					mu.Unlock()
				} else {
					// Small backoff if queue appears empty or messages in-flight
					time.Sleep(1 * time.Millisecond)
				}
			}
		}()
	}
	wg.Wait()

	// Should have dequeued all messages
	assert.Equal(t, expectedCount, dequeuedCount)
	assert.Equal(t, 0, q.Len())
}
