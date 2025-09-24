package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go-kafka-eda-demo/internal/circuit"
	"go-kafka-eda-demo/internal/kafka"
	"go-kafka-eda-demo/internal/models"
	"go-kafka-eda-demo/internal/redis"
	"go-kafka-eda-demo/internal/telemetry"
	"go-kafka-eda-demo/pkg/config"
	"go-kafka-eda-demo/pkg/logger"

	"github.com/IBM/sarama"
)

type OrderHandler struct {
	kafkaClient        *kafka.Client
	redisClient        *redis.Client
	config             *config.Config
	circuitBreaker     *circuit.CircuitBreaker
	exponentialBackoff *circuit.ExponentialBackoff
}

func NewOrderHandler(kafkaClient *kafka.Client, redisClient *redis.Client, cfg *config.Config) *OrderHandler {
	// Create circuit breaker
	cb := circuit.NewCircuitBreaker(circuit.Settings{
		Name:        "order-processor",
		MaxRequests: cfg.App.CircuitBreakerMaxRequests,
		Interval:    cfg.App.CircuitBreakerInterval,
		Timeout:     cfg.App.CircuitBreakerTimeout,
		ReadyToTrip: func(counts circuit.Counts) bool {
			return counts.ConsecutiveFailures >= 3
		},
		OnStateChange: func(name string, from circuit.State, to circuit.State) {
			logger.Infof(context.Background(), "Circuit breaker %s changed from %s to %s", name, from, to)
		},
	})

	// Create exponential backoff
	backoff := circuit.NewExponentialBackoff(
		cfg.App.RetryInitialInterval,
		cfg.App.RetryMaxInterval,
		cfg.App.RetryMaxAttempts,
	)

	return &OrderHandler{
		kafkaClient:        kafkaClient,
		redisClient:        redisClient,
		config:             cfg,
		circuitBreaker:     cb,
		exponentialBackoff: backoff,
	}
}

// Setup implements sarama.ConsumerGroupHandler
func (h *OrderHandler) Setup(sarama.ConsumerGroupSession) error {
	return nil
}

// Cleanup implements sarama.ConsumerGroupHandler
func (h *OrderHandler) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

// ConsumeClaim implements sarama.ConsumerGroupHandler
func (h *OrderHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case message := <-claim.Messages():
			if message == nil {
				return nil
			}

			// Extract correlation ID from message headers
			correlationID := kafka.ExtractCorrelationID(message)
			ctx := logger.SetCorrelationID(context.Background(), correlationID)

			// Start tracing span
			ctx, span := telemetry.StartKafkaConsumerSpan(ctx, message.Topic)
			defer span.End()

			logger.Infof(ctx, "Processing message from topic %s, partition %d, offset %d",
				message.Topic, message.Partition, message.Offset)

			// Process the message
			err := h.processMessage(ctx, message)
			if err != nil {
				logger.Errorf(ctx, "Failed to process message: %v", err)
				telemetry.SetSpanError(span, err)
				// Don't mark message as processed if it failed
				continue
			}

			// Mark message as processed
			session.MarkMessage(message, "")
			telemetry.SetSpanSuccess(span)
			logger.Infof(ctx, "Message processed successfully")

		case <-session.Context().Done():
			return nil
		}
	}
}

func (h *OrderHandler) processMessage(ctx context.Context, message *sarama.ConsumerMessage) error {
	// Parse the order event
	var event models.OrderEvent
	if err := event.FromJSON(message.Value); err != nil {
		return fmt.Errorf("failed to parse order event: %w", err)
	}

	// Check idempotency
	idempotencyRecord, err := h.redisClient.CheckIdempotency(ctx, event.EventID)
	if err != nil {
		return fmt.Errorf("failed to check idempotency: %w", err)
	}

	if idempotencyRecord != nil {
		if idempotencyRecord.Status == models.IdempotencyStatusProcessed {
			logger.Infof(ctx, "Event %s already processed, skipping", event.EventID)
			return nil
		}
		if idempotencyRecord.Status == models.IdempotencyStatusProcessing {
			logger.Warnf(ctx, "Event %s is currently being processed, skipping", event.EventID)
			return nil
		}
	}

	// Mark as processing
	processingRecord := &models.IdempotencyRecord{
		EventID:       event.EventID,
		CorrelationID: event.CorrelationID,
		ProcessedAt:   time.Now().UTC(),
		Status:        models.IdempotencyStatusProcessing,
	}

	if err := h.redisClient.SetIdempotency(ctx, processingRecord, 24*time.Hour); err != nil {
		return fmt.Errorf("failed to set processing status: %w", err)
	}

	// Process with circuit breaker
	_, err = h.circuitBreaker.Execute(ctx, func() (interface{}, error) {
		return nil, h.processOrderEvent(ctx, &event)
	})

	if err != nil {
		// Mark as failed
		failedRecord := &models.IdempotencyRecord{
			EventID:       event.EventID,
			CorrelationID: event.CorrelationID,
			ProcessedAt:   time.Now().UTC(),
			Status:        models.IdempotencyStatusFailed,
			Result:        err.Error(),
		}

		if setErr := h.redisClient.SetIdempotency(ctx, failedRecord, 24*time.Hour); setErr != nil {
			logger.Errorf(ctx, "Failed to set failed status: %v", setErr)
		}

		// Send to retry topic or DLQ
		return h.handleProcessingError(ctx, &event, err)
	}

	// Mark as processed
	processedRecord := &models.IdempotencyRecord{
		EventID:       event.EventID,
		CorrelationID: event.CorrelationID,
		ProcessedAt:   time.Now().UTC(),
		Status:        models.IdempotencyStatusProcessed,
		Result:        "success",
	}

	if err := h.redisClient.SetIdempotency(ctx, processedRecord, 24*time.Hour); err != nil {
		logger.Errorf(ctx, "Failed to set processed status: %v", err)
	}

	return nil
}

func (h *OrderHandler) processOrderEvent(ctx context.Context, event *models.OrderEvent) error {
	ctx, span := telemetry.StartSpan(ctx, "process_order_event")
	defer span.End()

	logger.Infof(ctx, "Processing order event: %s for aggregate %s", event.EventType, event.AggregateID)

	// Simulate processing time
	time.Sleep(100 * time.Millisecond)

	// Extract order data
	orderData, ok := event.Data["order"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid order data in event")
	}

	// Simulate business logic based on event type
	switch event.EventType {
	case models.EventTypeOrderCreated:
		return h.handleOrderCreated(ctx, orderData)
	case models.EventTypeOrderUpdated:
		return h.handleOrderUpdated(ctx, orderData)
	case models.EventTypeOrderCancelled:
		return h.handleOrderCancelled(ctx, orderData)
	case models.EventTypeOrderCompleted:
		return h.handleOrderCompleted(ctx, orderData)
	default:
		return fmt.Errorf("unknown event type: %s", event.EventType)
	}
}

func (h *OrderHandler) handleOrderCreated(ctx context.Context, orderData map[string]interface{}) error {
	ctx, span := telemetry.StartSpan(ctx, "handle_order_created")
	defer span.End()

	orderID, _ := orderData["id"].(string)
	logger.Infof(ctx, "Handling order created: %s", orderID)

	// Simulate business logic
	// - Validate inventory
	// - Reserve stock
	// - Calculate pricing
	// - Update order status

	// Simulate potential failure (10% chance)
	if time.Now().UnixNano()%10 == 0 {
		return fmt.Errorf("simulated processing failure for order %s", orderID)
	}

	logger.Infof(ctx, "Order created successfully: %s", orderID)
	return nil
}

func (h *OrderHandler) handleOrderUpdated(ctx context.Context, orderData map[string]interface{}) error {
	ctx, span := telemetry.StartSpan(ctx, "handle_order_updated")
	defer span.End()

	orderID, _ := orderData["id"].(string)
	logger.Infof(ctx, "Handling order updated: %s", orderID)

	// Simulate business logic
	return nil
}

func (h *OrderHandler) handleOrderCancelled(ctx context.Context, orderData map[string]interface{}) error {
	ctx, span := telemetry.StartSpan(ctx, "handle_order_cancelled")
	defer span.End()

	orderID, _ := orderData["id"].(string)
	logger.Infof(ctx, "Handling order cancelled: %s", orderID)

	// Simulate business logic
	return nil
}

func (h *OrderHandler) handleOrderCompleted(ctx context.Context, orderData map[string]interface{}) error {
	ctx, span := telemetry.StartSpan(ctx, "handle_order_completed")
	defer span.End()

	orderID, _ := orderData["id"].(string)
	logger.Infof(ctx, "Handling order completed: %s", orderID)

	// Simulate business logic
	return nil
}

func (h *OrderHandler) handleProcessingError(ctx context.Context, event *models.OrderEvent, processingErr error) error {
	// Check if this is a retry event
	retryCountKey := fmt.Sprintf("retry_count:%s", event.EventID)
	retryCount, err := h.redisClient.Get(ctx, retryCountKey)
	if err != nil && err.Error() != "redis: nil" {
		logger.Errorf(ctx, "Failed to get retry count: %v", err)
	}

	currentRetryCount := 0
	if retryCount != "" {
		if parseErr := json.Unmarshal([]byte(retryCount), &currentRetryCount); parseErr != nil {
			logger.Errorf(ctx, "Failed to parse retry count: %v", parseErr)
		}
	}

	// Check if we should retry
	if currentRetryCount < h.config.App.RetryMaxAttempts {
		return h.sendToRetryTopic(ctx, event, processingErr, currentRetryCount)
	}

	// Send to DLQ
	return h.sendToDLQ(ctx, event, processingErr, currentRetryCount)
}

func (h *OrderHandler) sendToRetryTopic(ctx context.Context, event *models.OrderEvent, processingErr error, retryCount int) error {
	ctx, span := telemetry.StartSpan(ctx, "send_to_retry_topic")
	defer span.End()

	nextRetryAt := time.Now().Add(h.exponentialBackoff.NextBackoff(retryCount))

	retryEvent := &models.RetryEvent{
		OriginalEvent: *event,
		RetryCount:    retryCount + 1,
		LastError:     processingErr.Error(),
		NextRetryAt:   nextRetryAt,
		CreatedAt:     time.Now().UTC(),
	}

	retryEventBytes, err := retryEvent.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize retry event: %w", err)
	}

	// Update retry count in Redis
	retryCountKey := fmt.Sprintf("retry_count:%s", event.EventID)
	retryCountBytes, _ := json.Marshal(retryCount + 1)
	if err := h.redisClient.SetWithExpiry(ctx, retryCountKey, string(retryCountBytes), 24*time.Hour); err != nil {
		logger.Errorf(ctx, "Failed to update retry count: %v", err)
	}

	// Send to retry topic
	err = h.kafkaClient.SendMessage(ctx, h.config.Kafka.TopicOrdersRetry, []byte(event.EventID), retryEventBytes)
	if err != nil {
		return fmt.Errorf("failed to send to retry topic: %w", err)
	}

	logger.Infof(ctx, "Sent event %s to retry topic (attempt %d/%d), next retry at %v",
		event.EventID, retryCount+1, h.config.App.RetryMaxAttempts, nextRetryAt)

	return nil
}

func (h *OrderHandler) sendToDLQ(ctx context.Context, event *models.OrderEvent, processingErr error, retryCount int) error {
	ctx, span := telemetry.StartSpan(ctx, "send_to_dlq")
	defer span.End()

	// Create retry history
	retryHistory := make([]models.RetryAttempt, retryCount)
	for i := 0; i < retryCount; i++ {
		retryHistory[i] = models.RetryAttempt{
			AttemptNumber: i + 1,
			Timestamp:     time.Now().Add(-time.Duration(retryCount-i) * time.Hour), // Approximate
			Error:         processingErr.Error(),
			Duration:      "unknown",
		}
	}

	dlqEvent := &models.DLQEvent{
		OriginalEvent: *event,
		RetryHistory:  retryHistory,
		FinalError:    processingErr.Error(),
		CreatedAt:     time.Now().UTC(),
		Reason:        fmt.Sprintf("Max retry attempts (%d) exceeded", h.config.App.RetryMaxAttempts),
	}

	dlqEventBytes, err := dlqEvent.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize DLQ event: %w", err)
	}

	// Send to DLQ topic
	err = h.kafkaClient.SendMessage(ctx, h.config.Kafka.TopicOrdersDLQ, []byte(event.EventID), dlqEventBytes)
	if err != nil {
		return fmt.Errorf("failed to send to DLQ topic: %w", err)
	}

	logger.Errorf(ctx, "Sent event %s to DLQ after %d failed attempts: %v",
		event.EventID, retryCount, processingErr)

	return nil
}

// GetCircuitBreakerState returns the current state of the circuit breaker
func (h *OrderHandler) GetCircuitBreakerState() circuit.State {
	return h.circuitBreaker.State()
}

// GetCircuitBreakerCounts returns the current counts of the circuit breaker
func (h *OrderHandler) GetCircuitBreakerCounts() circuit.Counts {
	return h.circuitBreaker.Counts()
}
