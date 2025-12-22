package queue

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewMessage(t *testing.T) {
	queueName := "test-queue"
	body := []byte("test message")

	msg := NewMessage(queueName, body)

	assert.NotEmpty(t, msg.ID)
	assert.Equal(t, queueName, msg.QueueName)
	assert.Equal(t, body, msg.Body)
	assert.Equal(t, 0, msg.Priority)
	assert.NotNil(t, msg.Headers)
	assert.False(t, msg.CreatedAt.IsZero())
	assert.Equal(t, 0, msg.DeliveryCount)
}

func TestMessage_WithPriority(t *testing.T) {
	msg := NewMessage("test", []byte("test")).WithPriority(5)
	assert.Equal(t, 5, msg.Priority)
}

func TestMessage_WithScheduledAt(t *testing.T) {
	scheduledTime := time.Now().Add(1 * time.Hour)
	msg := NewMessage("test", []byte("test")).WithScheduledAt(scheduledTime)

	assert.NotNil(t, msg.ScheduledAt)
	assert.Equal(t, scheduledTime.Unix(), msg.ScheduledAt.Unix())
}

func TestMessage_WithExpiresAt(t *testing.T) {
	expiresTime := time.Now().Add(1 * time.Hour)
	msg := NewMessage("test", []byte("test")).WithExpiresAt(expiresTime)

	assert.NotNil(t, msg.ExpiresAt)
	assert.Equal(t, expiresTime.Unix(), msg.ExpiresAt.Unix())
}

func TestMessage_WithHeader(t *testing.T) {
	msg := NewMessage("test", []byte("test")).
		WithHeader("key1", "value1").
		WithHeader("key2", "value2")

	assert.Equal(t, "value1", msg.Headers["key1"])
	assert.Equal(t, "value2", msg.Headers["key2"])
}

func TestMessage_IsExpired(t *testing.T) {
	t.Run("NotExpired", func(t *testing.T) {
		msg := NewMessage("test", []byte("test"))
		assert.False(t, msg.IsExpired())
	})

	t.Run("NotExpiredYet", func(t *testing.T) {
		expiresAt := time.Now().Add(1 * time.Hour)
		msg := NewMessage("test", []byte("test")).WithExpiresAt(expiresAt)
		assert.False(t, msg.IsExpired())
	})

	t.Run("Expired", func(t *testing.T) {
		expiresAt := time.Now().Add(-1 * time.Hour)
		msg := NewMessage("test", []byte("test")).WithExpiresAt(expiresAt)
		assert.True(t, msg.IsExpired())
	})
}

func TestMessage_IsScheduled(t *testing.T) {
	t.Run("NotScheduled", func(t *testing.T) {
		msg := NewMessage("test", []byte("test"))
		assert.False(t, msg.IsScheduled())
	})

	t.Run("ScheduledForFuture", func(t *testing.T) {
		scheduledAt := time.Now().Add(1 * time.Hour)
		msg := NewMessage("test", []byte("test")).WithScheduledAt(scheduledAt)
		assert.True(t, msg.IsScheduled())
	})

	t.Run("ScheduledInPast", func(t *testing.T) {
		scheduledAt := time.Now().Add(-1 * time.Hour)
		msg := NewMessage("test", []byte("test")).WithScheduledAt(scheduledAt)
		assert.False(t, msg.IsScheduled())
	})
}

func TestMessage_IncrementDeliveryCount(t *testing.T) {
	msg := NewMessage("test", []byte("test"))

	assert.Equal(t, 0, msg.DeliveryCount)
	assert.Nil(t, msg.LastDelivered)

	msg.IncrementDeliveryCount()
	assert.Equal(t, 1, msg.DeliveryCount)
	assert.NotNil(t, msg.LastDelivered)

	time.Sleep(10 * time.Millisecond)
	firstDelivery := *msg.LastDelivered

	msg.IncrementDeliveryCount()
	assert.Equal(t, 2, msg.DeliveryCount)
	assert.True(t, msg.LastDelivered.After(firstDelivery))
}

func TestMessage_JSONMarshaling(t *testing.T) {
	t.Run("WithAllFields", func(t *testing.T) {
		scheduledAt := time.Now().Add(1 * time.Hour)
		expiresAt := time.Now().Add(2 * time.Hour)

		original := NewMessage("test-queue", []byte("test body")).
			WithPriority(5).
			WithScheduledAt(scheduledAt).
			WithExpiresAt(expiresAt).
			WithHeader("key", "value")

		original.DeliveryCount = 3
		lastDelivered := time.Now()
		original.LastDelivered = &lastDelivered

		// Marshal
		data, err := json.Marshal(original)
		assert.NoError(t, err)

		// Unmarshal
		var decoded Message
		err = json.Unmarshal(data, &decoded)
		assert.NoError(t, err)

		// Verify
		assert.Equal(t, original.ID, decoded.ID)
		assert.Equal(t, original.QueueName, decoded.QueueName)
		assert.Equal(t, original.Body, decoded.Body)
		assert.Equal(t, original.Priority, decoded.Priority)
		assert.Equal(t, original.Headers, decoded.Headers)
		assert.Equal(t, original.DeliveryCount, decoded.DeliveryCount)

		// Time fields - compare with second precision due to RFC3339 format
		assert.Equal(t, original.CreatedAt.Unix(), decoded.CreatedAt.Unix())
		assert.NotNil(t, decoded.ScheduledAt)
		assert.Equal(t, original.ScheduledAt.Unix(), decoded.ScheduledAt.Unix())
		assert.NotNil(t, decoded.ExpiresAt)
		assert.Equal(t, original.ExpiresAt.Unix(), decoded.ExpiresAt.Unix())
		assert.NotNil(t, decoded.LastDelivered)
		assert.Equal(t, original.LastDelivered.Unix(), decoded.LastDelivered.Unix())
	})

	t.Run("WithMinimalFields", func(t *testing.T) {
		original := NewMessage("test-queue", []byte("test"))

		data, err := json.Marshal(original)
		assert.NoError(t, err)

		var decoded Message
		err = json.Unmarshal(data, &decoded)
		assert.NoError(t, err)

		assert.Equal(t, original.ID, decoded.ID)
		assert.Equal(t, original.QueueName, decoded.QueueName)
		assert.Equal(t, original.Body, decoded.Body)
		assert.Nil(t, decoded.ScheduledAt)
		assert.Nil(t, decoded.ExpiresAt)
		assert.Nil(t, decoded.LastDelivered)
	})
}

func TestDefaultQueueConfig(t *testing.T) {
	config := DefaultQueueConfig("test-queue")

	assert.Equal(t, "test-queue", config.Name)
	assert.Equal(t, "fifo", config.Type)
	assert.Equal(t, 10000, config.MaxLength)
	assert.Equal(t, 3, config.MaxRetries)
	assert.Equal(t, time.Duration(0), config.MessageTTL)
	assert.Equal(t, 30*time.Second, config.VisibilityTimeout)
	assert.True(t, config.DLQEnabled)
	assert.Equal(t, "test-queue-dlq", config.DLQName)
}

func TestQueueStats(t *testing.T) {
	stats := &QueueStats{
		Name:              "test-queue",
		Length:            100,
		Ready:             80,
		Scheduled:         20,
		DeliveredCount:    500,
		AcknowledgedCount: 450,
		FailedCount:       50,
		DLQCount:          10,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	// Marshal and unmarshal to ensure JSON serialization works
	data, err := json.Marshal(stats)
	assert.NoError(t, err)

	var decoded QueueStats
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, stats.Name, decoded.Name)
	assert.Equal(t, stats.Length, decoded.Length)
	assert.Equal(t, stats.Ready, decoded.Ready)
	assert.Equal(t, stats.Scheduled, decoded.Scheduled)
	assert.Equal(t, stats.DeliveredCount, decoded.DeliveredCount)
	assert.Equal(t, stats.AcknowledgedCount, decoded.AcknowledgedCount)
	assert.Equal(t, stats.FailedCount, decoded.FailedCount)
	assert.Equal(t, stats.DLQCount, decoded.DLQCount)
}

func TestMessage_BuilderPattern(t *testing.T) {
	// Test chaining builder methods
	msg := NewMessage("test-queue", []byte("test")).
		WithPriority(10).
		WithHeader("Content-Type", "application/json").
		WithHeader("X-Custom", "value").
		WithScheduledAt(time.Now().Add(1 * time.Hour)).
		WithExpiresAt(time.Now().Add(2 * time.Hour))

	assert.Equal(t, "test-queue", msg.QueueName)
	assert.Equal(t, []byte("test"), msg.Body)
	assert.Equal(t, 10, msg.Priority)
	assert.Equal(t, "application/json", msg.Headers["Content-Type"])
	assert.Equal(t, "value", msg.Headers["X-Custom"])
	assert.NotNil(t, msg.ScheduledAt)
	assert.NotNil(t, msg.ExpiresAt)
}
