package queue

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Message represents a queue message
type Message struct {
	ID          string            `json:"id"`           // Unique message ID
	QueueName   string            `json:"queue_name"`   // Name of the queue
	Body        []byte            `json:"body"`         // Message payload
	Priority    int               `json:"priority"`     // Priority (higher = more important, 0 = default)
	Headers     map[string]string `json:"headers"`      // Optional metadata
	CreatedAt   time.Time         `json:"created_at"`   // Creation timestamp
	ScheduledAt *time.Time        `json:"scheduled_at"` // Scheduled delivery time (for delayed messages)
	ExpiresAt   *time.Time        `json:"expires_at"`   // Expiration time (nil = no expiration)

	// Consumer tracking
	DeliveryCount int        `json:"delivery_count"` // Number of times delivered
	LastDelivered *time.Time `json:"last_delivered"` // Last delivery time

	// Dead letter queue
	DLQReason string `json:"dlq_reason,omitempty"` // Reason for DLQ (if applicable)
}

// NewMessage creates a new message with a unique ID
func NewMessage(queueName string, body []byte) *Message {
	return &Message{
		ID:            uuid.New().String(),
		QueueName:     queueName,
		Body:          body,
		Priority:      0,
		Headers:       make(map[string]string),
		CreatedAt:     time.Now(),
		DeliveryCount: 0,
	}
}

// WithPriority sets the message priority
func (m *Message) WithPriority(priority int) *Message {
	m.Priority = priority
	return m
}

// WithScheduledAt sets the scheduled delivery time
func (m *Message) WithScheduledAt(t time.Time) *Message {
	m.ScheduledAt = &t
	return m
}

// WithExpiresAt sets the expiration time
func (m *Message) WithExpiresAt(t time.Time) *Message {
	m.ExpiresAt = &t
	return m
}

// WithHeader adds a header to the message
func (m *Message) WithHeader(key, value string) *Message {
	m.Headers[key] = value
	return m
}

// IsExpired checks if the message has expired
func (m *Message) IsExpired() bool {
	if m.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*m.ExpiresAt)
}

// IsScheduled checks if the message is scheduled for future delivery
func (m *Message) IsScheduled() bool {
	if m.ScheduledAt == nil {
		return false
	}
	return time.Now().Before(*m.ScheduledAt)
}

// IncrementDeliveryCount increments the delivery count
func (m *Message) IncrementDeliveryCount() {
	m.DeliveryCount++
	now := time.Now()
	m.LastDelivered = &now
}

// MarshalJSON custom JSON marshaling
func (m *Message) MarshalJSON() ([]byte, error) {
	type Alias Message
	return json.Marshal(&struct {
		*Alias
		CreatedAt     int64  `json:"created_at"`               // Unix nanoseconds for precision
		ScheduledAt   *int64 `json:"scheduled_at,omitempty"`   // Unix nanoseconds
		ExpiresAt     *int64 `json:"expires_at,omitempty"`     // Unix nanoseconds
		LastDelivered *int64 `json:"last_delivered,omitempty"` // Unix nanoseconds
	}{
		Alias:         (*Alias)(m),
		CreatedAt:     m.CreatedAt.UnixNano(),
		ScheduledAt:   formatTimePtrNano(m.ScheduledAt),
		ExpiresAt:     formatTimePtrNano(m.ExpiresAt),
		LastDelivered: formatTimePtrNano(m.LastDelivered),
	})
}

// UnmarshalJSON custom JSON unmarshaling
func (m *Message) UnmarshalJSON(data []byte) error {
	type Alias Message
	aux := &struct {
		*Alias
		CreatedAt     int64  `json:"created_at"`
		ScheduledAt   *int64 `json:"scheduled_at,omitempty"`
		ExpiresAt     *int64 `json:"expires_at,omitempty"`
		LastDelivered *int64 `json:"last_delivered,omitempty"`
	}{
		Alias: (*Alias)(m),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	m.CreatedAt = time.Unix(0, aux.CreatedAt)
	m.ScheduledAt = parseTimePtrNano(aux.ScheduledAt)
	m.ExpiresAt = parseTimePtrNano(aux.ExpiresAt)
	m.LastDelivered = parseTimePtrNano(aux.LastDelivered)

	return nil
}

// QueueStats represents statistics for a queue
type QueueStats struct {
	Name              string    `json:"name"`
	Length            int       `json:"length"`             // Total messages in queue
	Ready             int       `json:"ready"`              // Messages ready for delivery
	Scheduled         int       `json:"scheduled"`          // Scheduled/delayed messages
	DeliveredCount    int64     `json:"delivered_count"`    // Total messages delivered
	AcknowledgedCount int64     `json:"acknowledged_count"` // Total messages acknowledged
	FailedCount       int64     `json:"failed_count"`       // Total failed deliveries
	DLQCount          int       `json:"dlq_count"`          // Messages in dead letter queue
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// QueueConfig represents configuration for a queue
type QueueConfig struct {
	Name              string        `json:"name"`
	Type              string        `json:"type"`               // "fifo", "priority", "delayed"
	MaxLength         int           `json:"max_length"`         // Max queue length (0 = unlimited)
	MaxRetries        int           `json:"max_retries"`        // Max delivery retries (0 = unlimited)
	MessageTTL        time.Duration `json:"message_ttl"`        // Message TTL (0 = no expiration)
	VisibilityTimeout time.Duration `json:"visibility_timeout"` // Visibility timeout for consumers
	DLQEnabled        bool          `json:"dlq_enabled"`        // Enable dead letter queue
	DLQName           string        `json:"dlq_name"`           // Dead letter queue name
}

// DefaultQueueConfig returns default queue configuration
func DefaultQueueConfig(name string) *QueueConfig {
	return &QueueConfig{
		Name:              name,
		Type:              "fifo",
		MaxLength:         10000,
		MaxRetries:        3,
		MessageTTL:        0,
		VisibilityTimeout: 30 * time.Second,
		DLQEnabled:        true,
		DLQName:           name + "-dlq",
	}
}

// Queue interface defines queue operations
type Queue interface {
	// Enqueue adds a message to the queue
	Enqueue(msg *Message) error

	// Dequeue retrieves and removes the next message from the queue
	Dequeue() (*Message, error)

	// Peek retrieves the next message without removing it
	Peek() (*Message, error)

	// Acknowledge marks a message as processed
	Acknowledge(messageID string) error

	// Nack negatively acknowledges a message (requeue or send to DLQ)
	Nack(messageID string, requeue bool) error

	// Delete removes a specific message from the queue
	Delete(messageID string) error

	// Get retrieves a specific message by ID
	Get(messageID string) (*Message, error)

	// Len returns the number of messages in the queue
	Len() int

	// Stats returns queue statistics
	Stats() *QueueStats

	// Purge removes all messages from the queue
	Purge() error

	// Close closes the queue
	Close() error
}

// Helper functions

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

func parseTimePtr(s *string) *time.Time {
	if s == nil {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil
	}
	return &t
}

func formatTimePtrNano(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	nano := t.UnixNano()
	return &nano
}

func parseTimePtrNano(nano *int64) *time.Time {
	if nano == nil {
		return nil
	}
	t := time.Unix(0, *nano)
	return &t
}
