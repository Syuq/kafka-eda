package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"go-kafka-eda-demo/internal/models"
	"go-kafka-eda-demo/pkg/config"
	"go-kafka-eda-demo/pkg/logger"
)

type Client struct {
	client *redis.Client
}

type IdempotencyChecker interface {
	CheckIdempotency(ctx context.Context, eventID string) (*models.IdempotencyRecord, error)
	SetIdempotency(ctx context.Context, record *models.IdempotencyRecord, ttl time.Duration) error
	DeleteIdempotency(ctx context.Context, eventID string) error
}

func NewClient(cfg *config.RedisConfig) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &Client{
		client: rdb,
	}, nil
}

func (c *Client) CheckIdempotency(ctx context.Context, eventID string) (*models.IdempotencyRecord, error) {
	key := c.idempotencyKey(eventID)
	
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Not found
		}
		logger.Errorf(ctx, "Failed to check idempotency for event %s: %v", eventID, err)
		return nil, err
	}

	var record models.IdempotencyRecord
	if err := json.Unmarshal([]byte(val), &record); err != nil {
		logger.Errorf(ctx, "Failed to unmarshal idempotency record for event %s: %v", eventID, err)
		return nil, err
	}

	logger.Debugf(ctx, "Found idempotency record for event %s: %s", eventID, record.Status)
	return &record, nil
}

func (c *Client) SetIdempotency(ctx context.Context, record *models.IdempotencyRecord, ttl time.Duration) error {
	key := c.idempotencyKey(record.EventID)
	
	data, err := json.Marshal(record)
	if err != nil {
		logger.Errorf(ctx, "Failed to marshal idempotency record for event %s: %v", record.EventID, err)
		return err
	}

	err = c.client.Set(ctx, key, data, ttl).Err()
	if err != nil {
		logger.Errorf(ctx, "Failed to set idempotency record for event %s: %v", record.EventID, err)
		return err
	}

	logger.Debugf(ctx, "Set idempotency record for event %s with status %s", record.EventID, record.Status)
	return nil
}

func (c *Client) DeleteIdempotency(ctx context.Context, eventID string) error {
	key := c.idempotencyKey(eventID)
	
	err := c.client.Del(ctx, key).Err()
	if err != nil {
		logger.Errorf(ctx, "Failed to delete idempotency record for event %s: %v", eventID, err)
		return err
	}

	logger.Debugf(ctx, "Deleted idempotency record for event %s", eventID)
	return nil
}

func (c *Client) SetWithExpiry(ctx context.Context, key string, value interface{}, expiry time.Duration) error {
	return c.client.Set(ctx, key, value, expiry).Err()
}

func (c *Client) Get(ctx context.Context, key string) (string, error) {
	return c.client.Get(ctx, key).Result()
}

func (c *Client) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}

func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	count, err := c.client.Exists(ctx, key).Result()
	return count > 0, err
}

func (c *Client) Increment(ctx context.Context, key string) (int64, error) {
	return c.client.Incr(ctx, key).Result()
}

func (c *Client) IncrementWithExpiry(ctx context.Context, key string, expiry time.Duration) (int64, error) {
	pipe := c.client.TxPipeline()
	incrCmd := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, expiry)
	
	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}
	
	return incrCmd.Val(), nil
}

func (c *Client) Close() error {
	return c.client.Close()
}

func (c *Client) idempotencyKey(eventID string) string {
	return fmt.Sprintf("idempotency:%s", eventID)
}

// Circuit breaker state management
func (c *Client) GetCircuitBreakerState(ctx context.Context, name string) (string, error) {
	key := fmt.Sprintf("circuit_breaker:%s:state", name)
	return c.client.Get(ctx, key).Result()
}

func (c *Client) SetCircuitBreakerState(ctx context.Context, name, state string, ttl time.Duration) error {
	key := fmt.Sprintf("circuit_breaker:%s:state", name)
	return c.client.Set(ctx, key, state, ttl).Err()
}

func (c *Client) IncrementCircuitBreakerFailures(ctx context.Context, name string, ttl time.Duration) (int64, error) {
	key := fmt.Sprintf("circuit_breaker:%s:failures", name)
	return c.IncrementWithExpiry(ctx, key, ttl)
}

func (c *Client) ResetCircuitBreakerFailures(ctx context.Context, name string) error {
	key := fmt.Sprintf("circuit_breaker:%s:failures", name)
	return c.client.Del(ctx, key).Err()
}

