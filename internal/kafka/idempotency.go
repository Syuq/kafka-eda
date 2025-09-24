package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go-kafka-eda-demo/internal/models"
	"go-kafka-eda-demo/internal/telemetry"
	"go-kafka-eda-demo/pkg/config"
	"go-kafka-eda-demo/pkg/logger"

	"github.com/IBM/sarama"
)

// CompactTopicIdempotencyChecker implements idempotency using Kafka compact topics
type CompactTopicIdempotencyChecker struct {
	kafkaClient       *Client
	config            *config.KafkaConfig
	cache             map[string]*models.IdempotencyRecord
	cacheMutex        sync.RWMutex
	lastOffset        int64
	consumer          sarama.Consumer
	partitionConsumer sarama.PartitionConsumer
}

func NewCompactTopicIdempotencyChecker(kafkaClient *Client, cfg *config.KafkaConfig) (*CompactTopicIdempotencyChecker, error) {
	checker := &CompactTopicIdempotencyChecker{
		kafkaClient: kafkaClient,
		config:      cfg,
		cache:       make(map[string]*models.IdempotencyRecord),
		lastOffset:  -1,
	}

	// Initialize consumer for the idempotency topic
	consumer, err := sarama.NewConsumer(cfg.Brokers, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}
	checker.consumer = consumer

	// Start consuming from the idempotency topic to build cache
	if err := checker.initializeCache(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize cache: %w", err)
	}

	return checker, nil
}

func (c *CompactTopicIdempotencyChecker) initializeCache(ctx context.Context) error {
	ctx, span := telemetry.StartSpan(ctx, "initialize_idempotency_cache")
	defer span.End()

	logger.Info(ctx, "Initializing idempotency cache from compact topic")

	// Get partition consumer for partition 0 (assuming single partition for simplicity)
	partitionConsumer, err := c.consumer.ConsumePartition(c.config.TopicIdempotency, 0, sarama.OffsetOldest)
	if err != nil {
		return fmt.Errorf("failed to create partition consumer: %w", err)
	}
	c.partitionConsumer = partitionConsumer

	// Start background goroutine to keep cache updated
	go c.maintainCache(ctx)

	// Initial cache load
	timeout := time.After(10 * time.Second)
	messageCount := 0

	for {
		select {
		case message := <-partitionConsumer.Messages():
			if message == nil {
				continue
			}

			if err := c.processIdempotencyMessage(ctx, message); err != nil {
				logger.Errorf(ctx, "Failed to process idempotency message: %v", err)
			}
			messageCount++
			c.lastOffset = message.Offset

		case err := <-partitionConsumer.Errors():
			logger.Errorf(ctx, "Partition consumer error: %v", err)

		case <-timeout:
			logger.Infof(ctx, "Initial cache load completed: %d records loaded", messageCount)
			return nil
		}
	}
}

func (c *CompactTopicIdempotencyChecker) maintainCache(ctx context.Context) {
	logger.Info(ctx, "Starting idempotency cache maintenance")

	for {
		select {
		case message := <-c.partitionConsumer.Messages():
			if message == nil {
				continue
			}

			if err := c.processIdempotencyMessage(ctx, message); err != nil {
				logger.Errorf(ctx, "Failed to process idempotency message: %v", err)
			}
			c.lastOffset = message.Offset

		case err := <-c.partitionConsumer.Errors():
			logger.Errorf(ctx, "Partition consumer error: %v", err)

		case <-ctx.Done():
			logger.Info(ctx, "Stopping idempotency cache maintenance")
			return
		}
	}
}

func (c *CompactTopicIdempotencyChecker) processIdempotencyMessage(ctx context.Context, message *sarama.ConsumerMessage) error {
	if len(message.Value) == 0 {
		// Tombstone record - delete from cache
		eventID := string(message.Key)
		c.cacheMutex.Lock()
		delete(c.cache, eventID)
		c.cacheMutex.Unlock()
		logger.Debugf(ctx, "Deleted idempotency record for event %s", eventID)
		return nil
	}

	var record models.IdempotencyRecord
	if err := json.Unmarshal(message.Value, &record); err != nil {
		return fmt.Errorf("failed to unmarshal idempotency record: %w", err)
	}

	c.cacheMutex.Lock()
	c.cache[record.EventID] = &record
	c.cacheMutex.Unlock()

	logger.Debugf(ctx, "Updated idempotency record for event %s: %s", record.EventID, record.Status)
	return nil
}

func (c *CompactTopicIdempotencyChecker) CheckIdempotency(ctx context.Context, eventID string) (*models.IdempotencyRecord, error) {
	ctx, span := telemetry.StartSpan(ctx, "check_idempotency_compact_topic")
	defer span.End()

	c.cacheMutex.RLock()
	record, exists := c.cache[eventID]
	c.cacheMutex.RUnlock()

	if !exists {
		return nil, nil
	}

	// Return a copy to avoid race conditions
	recordCopy := *record
	return &recordCopy, nil
}

func (c *CompactTopicIdempotencyChecker) SetIdempotency(ctx context.Context, record *models.IdempotencyRecord, ttl time.Duration) error {
	ctx, span := telemetry.StartSpan(ctx, "set_idempotency_compact_topic")
	defer span.End()

	// Serialize the record
	recordBytes, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal idempotency record: %w", err)
	}

	// Send to compact topic
	err = c.kafkaClient.SendMessage(ctx, c.config.TopicIdempotency, []byte(record.EventID), recordBytes)
	if err != nil {
		return fmt.Errorf("failed to send idempotency record: %w", err)
	}

	// Update local cache
	c.cacheMutex.Lock()
	c.cache[record.EventID] = record
	c.cacheMutex.Unlock()

	logger.Debugf(ctx, "Set idempotency record for event %s with status %s", record.EventID, record.Status)
	return nil
}

func (c *CompactTopicIdempotencyChecker) DeleteIdempotency(ctx context.Context, eventID string) error {
	ctx, span := telemetry.StartSpan(ctx, "delete_idempotency_compact_topic")
	defer span.End()

	// Send tombstone record (null value) to compact topic
	err := c.kafkaClient.SendMessage(ctx, c.config.TopicIdempotency, []byte(eventID), nil)
	if err != nil {
		return fmt.Errorf("failed to send tombstone record: %w", err)
	}

	// Update local cache
	c.cacheMutex.Lock()
	delete(c.cache, eventID)
	c.cacheMutex.Unlock()

	logger.Debugf(ctx, "Deleted idempotency record for event %s", eventID)
	return nil
}

func (c *CompactTopicIdempotencyChecker) GetCacheStats() map[string]interface{} {
	c.cacheMutex.RLock()
	defer c.cacheMutex.RUnlock()

	stats := map[string]interface{}{
		"cache_size":  len(c.cache),
		"last_offset": c.lastOffset,
		"timestamp":   time.Now().UTC(),
	}

	// Count by status
	statusCounts := make(map[string]int)
	for _, record := range c.cache {
		statusCounts[record.Status]++
	}
	stats["status_counts"] = statusCounts

	return stats
}

func (c *CompactTopicIdempotencyChecker) Close() error {
	var errs []error

	if c.partitionConsumer != nil {
		if err := c.partitionConsumer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close partition consumer: %w", err))
		}
	}

	if c.consumer != nil {
		if err := c.consumer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close consumer: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing compact topic idempotency checker: %v", errs)
	}

	return nil
}

// RedisIdempotencyChecker defines the interface for Redis-based idempotency checking
type RedisIdempotencyChecker interface {
	CheckIdempotency(ctx context.Context, eventID string) (*models.IdempotencyRecord, error)
	SetIdempotency(ctx context.Context, record *models.IdempotencyRecord, ttl time.Duration) error
	DeleteIdempotency(ctx context.Context, eventID string) error
}

// HybridIdempotencyChecker combines Redis and compact topic for better performance
type HybridIdempotencyChecker struct {
	redisChecker        RedisIdempotencyChecker // Changed from *RedisIdempotencyChecker to RedisIdempotencyChecker
	compactTopicChecker *CompactTopicIdempotencyChecker
	useRedisFirst       bool
}

func NewHybridIdempotencyChecker(redisChecker RedisIdempotencyChecker, compactTopicChecker *CompactTopicIdempotencyChecker) *HybridIdempotencyChecker {
	return &HybridIdempotencyChecker{
		redisChecker:        redisChecker,
		compactTopicChecker: compactTopicChecker,
		useRedisFirst:       true,
	}
}

func (h *HybridIdempotencyChecker) CheckIdempotency(ctx context.Context, eventID string) (*models.IdempotencyRecord, error) {
	ctx, span := telemetry.StartSpan(ctx, "check_idempotency_hybrid")
	defer span.End()

	if h.useRedisFirst {
		// Try Redis first for faster access
		record, err := h.redisChecker.CheckIdempotency(ctx, eventID)
		if err == nil && record != nil {
			return record, nil
		}

		// Fallback to compact topic
		return h.compactTopicChecker.CheckIdempotency(ctx, eventID)
	}

	// Use compact topic first
	record, err := h.compactTopicChecker.CheckIdempotency(ctx, eventID)
	if err == nil && record != nil {
		return record, nil
	}

	// Fallback to Redis
	return h.redisChecker.CheckIdempotency(ctx, eventID)
}

func (h *HybridIdempotencyChecker) SetIdempotency(ctx context.Context, record *models.IdempotencyRecord, ttl time.Duration) error {
	ctx, span := telemetry.StartSpan(ctx, "set_idempotency_hybrid")
	defer span.End()

	// Write to both Redis and compact topic
	var errs []error

	if err := h.redisChecker.SetIdempotency(ctx, record, ttl); err != nil {
		errs = append(errs, fmt.Errorf("redis error: %w", err))
	}

	if err := h.compactTopicChecker.SetIdempotency(ctx, record, ttl); err != nil {
		errs = append(errs, fmt.Errorf("compact topic error: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("hybrid idempotency set errors: %v", errs)
	}

	return nil
}

func (h *HybridIdempotencyChecker) DeleteIdempotency(ctx context.Context, eventID string) error {
	ctx, span := telemetry.StartSpan(ctx, "delete_idempotency_hybrid")
	defer span.End()

	// Delete from both Redis and compact topic
	var errs []error

	if err := h.redisChecker.DeleteIdempotency(ctx, eventID); err != nil {
		errs = append(errs, fmt.Errorf("redis error: %w", err))
	}

	if err := h.compactTopicChecker.DeleteIdempotency(ctx, eventID); err != nil {
		errs = append(errs, fmt.Errorf("compact topic error: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("hybrid idempotency delete errors: %v", errs)
	}

	return nil
}
