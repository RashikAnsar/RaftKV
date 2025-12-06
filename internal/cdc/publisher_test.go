package cdc

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

func TestPublisher_PublishAndSubscribe(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pub := NewPublisher("dc1", logger)
	defer pub.Close()

	// Subscribe
	sub := pub.Subscribe("test-sub", 10, nil)
	defer sub.Close()

	// Publish event
	event := &ChangeEvent{
		RaftIndex: 1,
		RaftTerm:  1,
		Operation: "put",
		Key:       "test-key",
		Value:     []byte("test-value"),
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	err := pub.Publish(ctx, event)
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	// Receive event
	received, err := sub.RecvWithTimeout(time.Second)
	if err != nil {
		t.Fatalf("Recv failed: %v", err)
	}

	// Verify event
	if received.RaftIndex != event.RaftIndex {
		t.Errorf("Expected index %d, got %d", event.RaftIndex, received.RaftIndex)
	}
	if received.Key != event.Key {
		t.Errorf("Expected key %s, got %s", event.Key, received.Key)
	}
	if string(received.Value) != string(event.Value) {
		t.Errorf("Expected value %s, got %s", event.Value, received.Value)
	}
	if received.DatacenterID != "dc1" {
		t.Errorf("Expected datacenter_id dc1, got %s", received.DatacenterID)
	}
	if received.SequenceNum != 1 {
		t.Errorf("Expected sequence_num 1, got %d", received.SequenceNum)
	}
}

func TestPublisher_MultipleSubscribers(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pub := NewPublisher("dc1", logger)
	defer pub.Close()

	// Create 3 subscribers
	sub1 := pub.Subscribe("sub1", 10, nil)
	sub2 := pub.Subscribe("sub2", 10, nil)
	sub3 := pub.Subscribe("sub3", 10, nil)
	defer sub1.Close()
	defer sub2.Close()
	defer sub3.Close()

	// Publish event
	event := &ChangeEvent{
		RaftIndex: 1,
		RaftTerm:  1,
		Operation: "put",
		Key:       "key1",
		Value:     []byte("value1"),
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	pub.Publish(ctx, event)

	// All subscribers should receive the event
	for i, sub := range []*Subscriber{sub1, sub2, sub3} {
		received, err := sub.RecvWithTimeout(time.Second)
		if err != nil {
			t.Fatalf("Subscriber %d failed to receive: %v", i, err)
		}
		if received.Key != event.Key {
			t.Errorf("Subscriber %d got wrong key", i)
		}
	}
}

func TestPublisher_WithFilter(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pub := NewPublisher("dc1", logger)
	defer pub.Close()

	// Subscribe with prefix filter
	filter := KeyPrefixFilter("user:")
	sub := pub.Subscribe("filtered-sub", 10, filter)
	defer sub.Close()

	ctx := context.Background()

	// Publish event that matches filter
	event1 := &ChangeEvent{
		RaftIndex: 1,
		Operation: "put",
		Key:       "user:123",
		Value:     []byte("data"),
		Timestamp: time.Now(),
	}
	pub.Publish(ctx, event1)

	// Publish event that doesn't match filter
	event2 := &ChangeEvent{
		RaftIndex: 2,
		Operation: "put",
		Key:       "product:456",
		Value:     []byte("data"),
		Timestamp: time.Now(),
	}
	pub.Publish(ctx, event2)

	// Should only receive the first event
	received, err := sub.RecvWithTimeout(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to receive matching event: %v", err)
	}
	if received.Key != "user:123" {
		t.Errorf("Expected key user:123, got %s", received.Key)
	}

	// Should not receive the second event
	received, err = sub.RecvWithTimeout(100 * time.Millisecond)
	if err == nil {
		t.Errorf("Should not have received filtered event, but got: %s", received.Key)
	}
}

func TestPublisher_Unsubscribe(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pub := NewPublisher("dc1", logger)
	defer pub.Close()

	sub := pub.Subscribe("test-sub", 10, nil)

	if pub.SubscriberCount() != 1 {
		t.Errorf("Expected 1 subscriber, got %d", pub.SubscriberCount())
	}

	pub.Unsubscribe("test-sub")

	if pub.SubscriberCount() != 0 {
		t.Errorf("Expected 0 subscribers after unsubscribe, got %d", pub.SubscriberCount())
	}

	if !sub.IsClosed() {
		t.Error("Subscriber should be closed after unsubscribe")
	}
}

func TestPublisher_Close(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pub := NewPublisher("dc1", logger)

	sub1 := pub.Subscribe("sub1", 10, nil)
	sub2 := pub.Subscribe("sub2", 10, nil)

	// Close publisher
	pub.Close()

	// All subscribers should be closed
	if !sub1.IsClosed() {
		t.Error("sub1 should be closed")
	}
	if !sub2.IsClosed() {
		t.Error("sub2 should be closed")
	}

	// Publishing after close should fail
	event := &ChangeEvent{Key: "test"}
	err := pub.Publish(context.Background(), event)
	if err != ErrPublisherClosed {
		t.Errorf("Expected ErrPublisherClosed, got %v", err)
	}
}

func TestSubscriber_TryRecv(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pub := NewPublisher("dc1", logger)
	defer pub.Close()

	sub := pub.Subscribe("test-sub", 10, nil)
	defer sub.Close()

	// Try to receive when no events
	event := sub.TryRecv()
	if event != nil {
		t.Error("Should return nil when no events available")
	}

	// Publish an event
	pub.Publish(context.Background(), &ChangeEvent{
		Key:   "test",
		Value: []byte("data"),
	})

	// Give it a moment to propagate
	time.Sleep(10 * time.Millisecond)

	// Should now receive the event
	event = sub.TryRecv()
	if event == nil {
		t.Error("Should have received event")
	}
	if event.Key != "test" {
		t.Errorf("Expected key test, got %s", event.Key)
	}
}

func TestChangeEvent_Clone(t *testing.T) {
	original := &ChangeEvent{
		RaftIndex:    1,
		RaftTerm:     2,
		Operation:    "put",
		Key:          "key1",
		Value:        []byte("value1"),
		Timestamp:    time.Now(),
		DatacenterID: "dc1",
		SequenceNum:  100,
		VectorClock:  map[string]uint64{"dc1": 1, "dc2": 2},
	}

	clone := original.Clone()

	// Verify all fields are copied
	if clone.RaftIndex != original.RaftIndex {
		t.Error("RaftIndex not cloned")
	}
	if clone.Key != original.Key {
		t.Error("Key not cloned")
	}
	if string(clone.Value) != string(original.Value) {
		t.Error("Value not cloned")
	}

	// Verify deep copy (modifying clone doesn't affect original)
	clone.Value[0] = 'X'
	if original.Value[0] == 'X' {
		t.Error("Value was not deep copied")
	}

	clone.VectorClock["dc3"] = 3
	if _, exists := original.VectorClock["dc3"]; exists {
		t.Error("VectorClock was not deep copied")
	}
}

func TestEventFilter_CompositeFilter(t *testing.T) {
	filter := CompositeFilter(
		KeyPrefixFilter("user:"),
		OperationFilter("put"),
	)

	// Should match
	event1 := &ChangeEvent{
		Key:       "user:123",
		Operation: "put",
	}
	if !filter(event1) {
		t.Error("Should match both filters")
	}

	// Should not match (wrong operation)
	event2 := &ChangeEvent{
		Key:       "user:123",
		Operation: "delete",
	}
	if filter(event2) {
		t.Error("Should not match (wrong operation)")
	}

	// Should not match (wrong prefix)
	event3 := &ChangeEvent{
		Key:       "product:456",
		Operation: "put",
	}
	if filter(event3) {
		t.Error("Should not match (wrong prefix)")
	}
}
