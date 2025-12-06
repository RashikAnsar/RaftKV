package replication_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/RashikAnsar/raftkv/internal/cdc"
	"github.com/RashikAnsar/raftkv/internal/replication"
)

// TestBasicReplication tests basic replication from primary to replica
func TestBasicReplication(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	ctx := context.Background()

	// Setup primary datacenter
	primaryDC := "dc-primary"
	replicaDC := "dc-replica-1"

	// Create CDC publisher (simulates primary)
	publisher := cdc.NewPublisher(primaryDC, logger)
	defer publisher.Close()

	// Create subscriber (simulates replica receiving events)
	subscriber := publisher.Subscribe(replicaDC, 100, nil)
	defer subscriber.Close()

	// Publish events
	events := []*cdc.ChangeEvent{
		{
			Operation: "put",
			Key:       "key1",
			Value:     []byte("value1"),
			Timestamp: time.Now(),
		},
		{
			Operation: "put",
			Key:       "key2",
			Value:     []byte("value2"),
			Timestamp: time.Now(),
		},
		{
			Operation: "delete",
			Key:       "key1",
			Timestamp: time.Now(),
		},
	}

	// Publish all events
	for _, event := range events {
		err := publisher.Publish(ctx, event)
		require.NoError(t, err, "Failed to publish event")
	}

	// Verify all events received
	received := make([]*cdc.ChangeEvent, 0, len(events))
	timeout := time.After(5 * time.Second)
	for i := 0; i < len(events); i++ {
		select {
		case event := <-subscriber.Events():
			received = append(received, event)
		case <-timeout:
			t.Fatalf("Timeout waiting for event %d", i+1)
		}
	}

	assert.Equal(t, len(events), len(received), "Should receive all events")
	for i, event := range received {
		assert.Equal(t, events[i].Operation, event.Operation)
		assert.Equal(t, events[i].Key, event.Key)
		// For delete operations, value may be nil or empty
		if events[i].Operation != "delete" {
			assert.Equal(t, events[i].Value, event.Value)
		}
	}
}

// TestLagMonitor tests lag tracking functionality
func TestLagMonitor(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	datacenterID := "dc-primary"

	// Create lag monitor
	monitor := replication.NewLagMonitor(datacenterID, logger)

	replicaDC := "dc-replica-1"

	// Update lag with initial index
	monitor.UpdateLag(replicaDC, 100, time.Now())

	// Get lag info
	info := monitor.GetLag(replicaDC)
	assert.Equal(t, replicaDC, info.DatacenterID)
	assert.Equal(t, uint64(100), info.LastReplicatedIndex)

	// Calculate lag with leader index
	time.Sleep(100 * time.Millisecond)
	lagInfo := monitor.CalculateLag(replicaDC, 150)
	assert.Equal(t, uint64(50), lagInfo.LagEntries, "Should have 50 entries lag")
	assert.Greater(t, lagInfo.LagSeconds, 0.0, "Should have time-based lag")

	// Test threshold checking
	exceeded := monitor.CheckLagThreshold(0.05, 40)
	assert.Contains(t, exceeded, replicaDC, "Should exceed both thresholds")

	// Update to caught up
	monitor.UpdateLag(replicaDC, 150, time.Now())
	lagInfo = monitor.CalculateLag(replicaDC, 150)
	assert.Equal(t, uint64(0), lagInfo.LagEntries, "Should be caught up")
}

// TestVectorClockConflictDetection tests concurrent update detection
func TestVectorClockConflictDetection(t *testing.T) {
	t.Parallel()

	// Create vector clocks for two datacenters
	dc1Clock := replication.NewVectorClock()
	dc2Clock := replication.NewVectorClock()

	// Both start at same state
	dc1Clock.Increment("dc1")
	dc2Clock.Increment("dc1")

	// DC1 makes update
	dc1Clock.Increment("dc1")

	// DC2 makes concurrent update (hasn't seen DC1's update)
	dc2Clock.Increment("dc2")

	// Detect concurrent updates
	relation := dc1Clock.Compare(dc2Clock)
	assert.Equal(t, replication.ClockConcurrent, relation, "Should detect concurrent updates")

	// Merge clocks to resolve
	dc1Clock.Merge(dc2Clock)
	assert.Equal(t, uint64(2), dc1Clock.Get("dc1"))
	assert.Equal(t, uint64(1), dc1Clock.Get("dc2"))
}

// TestMultipleSubscribers tests multiple replicas receiving same events
func TestMultipleSubscribers(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	ctx := context.Background()

	primaryDC := "dc-primary"
	publisher := cdc.NewPublisher(primaryDC, logger)
	defer publisher.Close()

	// Create 3 subscribers (simulating 3 replica datacenters)
	subscribers := make([]*cdc.Subscriber, 3)
	for i := 0; i < 3; i++ {
		sub := publisher.Subscribe("dc-replica-"+string(rune('1'+i)), 100, nil)
		defer sub.Close()
		subscribers[i] = sub
	}

	// Publish event
	event := &cdc.ChangeEvent{
		Operation: "put",
		Key:       "key1",
		Value:     []byte("value1"),
		Timestamp: time.Now(),
	}

	err := publisher.Publish(ctx, event)
	require.NoError(t, err)

	// Verify all subscribers receive the event
	timeout := time.After(2 * time.Second)
	for i, sub := range subscribers {
		select {
		case received := <-sub.Events():
			assert.Equal(t, event.Key, received.Key)
			assert.Equal(t, event.Value, received.Value)
		case <-timeout:
			t.Fatalf("Subscriber %d did not receive event", i)
		}
	}
}

// TestEventFiltering tests filtering events by key prefix
func TestEventFiltering(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	ctx := context.Background()

	primaryDC := "dc-primary"
	publisher := cdc.NewPublisher(primaryDC, logger)
	defer publisher.Close()

	// Create subscriber with key prefix filter
	filter := func(event *cdc.ChangeEvent) bool {
		return len(event.Key) >= 5 && event.Key[:5] == "user:"
	}
	subscriber := publisher.Subscribe("dc-replica-1", 100, filter)
	defer subscriber.Close()

	// Publish events
	events := []*cdc.ChangeEvent{
		{
			Operation: "put",
			Key:       "user:1",
			Value:     []byte("value1"),
			Timestamp: time.Now(),
		},
		{
			Operation: "put",
			Key:       "system:config",
			Value:     []byte("value2"),
			Timestamp: time.Now(),
		},
		{
			Operation: "put",
			Key:       "user:2",
			Value:     []byte("value3"),
			Timestamp: time.Now(),
		},
	}

	for _, event := range events {
		err := publisher.Publish(ctx, event)
		require.NoError(t, err)
	}

	// Should only receive events with "user:" prefix
	received := make([]*cdc.ChangeEvent, 0)
	timeout := time.After(2 * time.Second)

	for {
		select {
		case event := <-subscriber.Events():
			received = append(received, event)
			if len(received) == 2 {
				goto done
			}
		case <-timeout:
			goto done
		}
	}

done:
	assert.Equal(t, 2, len(received), "Should receive only filtered events")
	assert.Equal(t, "user:1", received[0].Key)
	assert.Equal(t, "user:2", received[1].Key)
}

// TestBufferOverflow tests event dropping when buffer is full
func TestBufferOverflow(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	ctx := context.Background()

	primaryDC := "dc-primary"
	publisher := cdc.NewPublisher(primaryDC, logger)
	defer publisher.Close()

	// Create subscriber with small buffer
	subscriber := publisher.Subscribe("dc-replica-1", 5, nil) // Small buffer
	defer subscriber.Close()

	// Publish more events than buffer size
	numEvents := 20
	for i := 0; i < numEvents; i++ {
		event := &cdc.ChangeEvent{
			Operation: "put",
			Key:       "key" + string(rune('0'+i)),
			Value:     []byte("value"),
			Timestamp: time.Now(),
		}
		err := publisher.Publish(ctx, event)
		require.NoError(t, err)
	}

	// Should receive at most buffer size events
	time.Sleep(100 * time.Millisecond)
	received := 0
	for {
		select {
		case <-subscriber.Events():
			received++
		default:
			goto done
		}
	}

done:
	assert.LessOrEqual(t, received, 5, "Should not receive more than buffer size")
}

// TestPublisherClose tests proper cleanup on publisher close
func TestPublisherClose(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	ctx := context.Background()

	primaryDC := "dc-primary"
	publisher := cdc.NewPublisher(primaryDC, logger)

	subscriber := publisher.Subscribe("dc-replica-1", 10, nil)

	// Close publisher
	publisher.Close()

	// Publishing after close should return error
	event := &cdc.ChangeEvent{
		Operation: "put",
		Key:       "key1",
		Value:     []byte("value1"),
		Timestamp: time.Now(),
	}

	err := publisher.Publish(ctx, event)
	assert.Error(t, err, "Should error when publishing to closed publisher")

	// Subscriber should be closed too
	select {
	case _, ok := <-subscriber.Events():
		assert.False(t, ok, "Subscriber channel should be closed")
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for subscriber channel to close")
	}
}

// TestVectorClockMerge tests vector clock merging
func TestVectorClockMerge(t *testing.T) {
	t.Parallel()

	clock1 := replication.NewVectorClock()
	clock2 := replication.NewVectorClock()

	// DC1 increments
	clock1.Increment("dc1")
	clock1.Increment("dc1")

	// DC2 increments
	clock2.Increment("dc2")
	clock2.Increment("dc2")
	clock2.Increment("dc2")

	// Merge clock2 into clock1
	clock1.Merge(clock2)

	// clock1 should have max of both
	assert.Equal(t, uint64(2), clock1.Get("dc1"))
	assert.Equal(t, uint64(3), clock1.Get("dc2"))
}
