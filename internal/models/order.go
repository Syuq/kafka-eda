package models

import (
	"encoding/json"
	"time"
)

// Order represents the main order entity
type Order struct {
	ID           string    `json:"id"`
	CustomerID   string    `json:"customer_id"`
	ProductID    string    `json:"product_id"`
	Quantity     int       `json:"quantity"`
	Price        float64   `json:"price"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// OrderEvent represents an event in the system
type OrderEvent struct {
	EventID       string                 `json:"event_id"`
	CorrelationID string                 `json:"correlation_id"`
	EventType     string                 `json:"event_type"`
	AggregateID   string                 `json:"aggregate_id"`
	AggregateType string                 `json:"aggregate_type"`
	Version       int                    `json:"version"`
	Timestamp     time.Time              `json:"timestamp"`
	Data          map[string]interface{} `json:"data"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// RetryEvent represents an event that needs to be retried
type RetryEvent struct {
	OriginalEvent OrderEvent `json:"original_event"`
	RetryCount    int        `json:"retry_count"`
	LastError     string     `json:"last_error"`
	NextRetryAt   time.Time  `json:"next_retry_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

// DLQEvent represents an event in the Dead Letter Queue
type DLQEvent struct {
	OriginalEvent OrderEvent `json:"original_event"`
	RetryHistory  []RetryAttempt `json:"retry_history"`
	FinalError    string     `json:"final_error"`
	CreatedAt     time.Time  `json:"created_at"`
	Reason        string     `json:"reason"`
}

// RetryAttempt represents a single retry attempt
type RetryAttempt struct {
	AttemptNumber int       `json:"attempt_number"`
	Timestamp     time.Time `json:"timestamp"`
	Error         string    `json:"error"`
	Duration      string    `json:"duration"`
}

// IdempotencyRecord represents an idempotency check record
type IdempotencyRecord struct {
	EventID       string    `json:"event_id"`
	CorrelationID string    `json:"correlation_id"`
	ProcessedAt   time.Time `json:"processed_at"`
	Status        string    `json:"status"`
	Result        string    `json:"result,omitempty"`
}

// Event types
const (
	EventTypeOrderCreated   = "order.created"
	EventTypeOrderUpdated   = "order.updated"
	EventTypeOrderCancelled = "order.cancelled"
	EventTypeOrderCompleted = "order.completed"
)

// Order statuses
const (
	OrderStatusPending   = "pending"
	OrderStatusProcessing = "processing"
	OrderStatusCompleted = "completed"
	OrderStatusCancelled = "cancelled"
	OrderStatusFailed    = "failed"
)

// Idempotency statuses
const (
	IdempotencyStatusProcessed = "processed"
	IdempotencyStatusFailed    = "failed"
	IdempotencyStatusProcessing = "processing"
)

// NewOrderEvent creates a new order event
func NewOrderEvent(eventType, aggregateID, correlationID string, data map[string]interface{}) *OrderEvent {
	return &OrderEvent{
		EventID:       generateEventID(),
		CorrelationID: correlationID,
		EventType:     eventType,
		AggregateID:   aggregateID,
		AggregateType: "order",
		Version:       1,
		Timestamp:     time.Now().UTC(),
		Data:          data,
		Metadata:      make(map[string]interface{}),
	}
}

// ToJSON converts the event to JSON bytes
func (e *OrderEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// FromJSON creates an OrderEvent from JSON bytes
func (e *OrderEvent) FromJSON(data []byte) error {
	return json.Unmarshal(data, e)
}

// ToJSON converts the retry event to JSON bytes
func (r *RetryEvent) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

// FromJSON creates a RetryEvent from JSON bytes
func (r *RetryEvent) FromJSON(data []byte) error {
	return json.Unmarshal(data, r)
}

// ToJSON converts the DLQ event to JSON bytes
func (d *DLQEvent) ToJSON() ([]byte, error) {
	return json.Marshal(d)
}

// FromJSON creates a DLQEvent from JSON bytes
func (d *DLQEvent) FromJSON(data []byte) error {
	return json.Unmarshal(data, d)
}

func generateEventID() string {
	// This would typically use a UUID library
	// For now, using a simple timestamp-based ID
	return time.Now().Format("20060102150405") + "-" + time.Now().Format("000000")
}

