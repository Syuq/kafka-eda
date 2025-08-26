package kafka

import (
	"context"
	"fmt"
	"time"

	"github.com/IBM/sarama"
	"go-kafka-eda-demo/pkg/config"
	"go-kafka-eda-demo/pkg/logger"
)

type Client struct {
	config   *config.KafkaConfig
	producer sarama.SyncProducer
	consumer sarama.ConsumerGroup
}

type Producer interface {
	SendMessage(ctx context.Context, topic string, key, value []byte) error
	SendMessageWithHeaders(ctx context.Context, topic string, key, value []byte, headers map[string]string) error
	Close() error
}

type Consumer interface {
	Subscribe(ctx context.Context, topics []string, handler sarama.ConsumerGroupHandler) error
	Close() error
}

func NewClient(cfg *config.KafkaConfig) (*Client, error) {
	client := &Client{
		config: cfg,
	}

	// Initialize producer
	producer, err := client.createProducer()
	if err != nil {
		return nil, fmt.Errorf("failed to create producer: %w", err)
	}
	client.producer = producer

	// Initialize consumer
	consumer, err := client.createConsumer()
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}
	client.consumer = consumer

	return client, nil
}

func (c *Client) createProducer() (sarama.SyncProducer, error) {
	config := sarama.NewConfig()
	config.Producer.RequiredAcks = sarama.WaitForAll
	config.Producer.Retry.Max = 3
	config.Producer.Return.Successes = true
	config.Producer.Compression = sarama.CompressionSnappy
	config.Producer.Flush.Frequency = 500 * time.Millisecond
	config.Producer.Idempotent = true
	config.Net.MaxOpenRequests = 1

	return sarama.NewSyncProducer(c.config.Brokers, config)
}

func (c *Client) createConsumer() (sarama.ConsumerGroup, error) {
	config := sarama.NewConfig()
	config.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategyRoundRobin
	config.Consumer.Offsets.Initial = sarama.OffsetOldest
	config.Consumer.Group.Session.Timeout = 10 * time.Second
	config.Consumer.Group.Heartbeat.Interval = 3 * time.Second
	config.Consumer.MaxProcessingTime = 30 * time.Second
	config.Consumer.Return.Errors = true

	return sarama.NewConsumerGroup(c.config.Brokers, c.config.ConsumerGroup, config)
}

func (c *Client) SendMessage(ctx context.Context, topic string, key, value []byte) error {
	return c.SendMessageWithHeaders(ctx, topic, key, value, nil)
}

func (c *Client) SendMessageWithHeaders(ctx context.Context, topic string, key, value []byte, headers map[string]string) error {
	correlationID := logger.GetCorrelationID(ctx)
	
	msg := &sarama.ProducerMessage{
		Topic: topic,
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(value),
		Headers: []sarama.RecordHeader{
			{
				Key:   []byte("correlation_id"),
				Value: []byte(correlationID),
			},
		},
		Timestamp: time.Now(),
	}

	// Add custom headers
	for k, v := range headers {
		msg.Headers = append(msg.Headers, sarama.RecordHeader{
			Key:   []byte(k),
			Value: []byte(v),
		})
	}

	partition, offset, err := c.producer.SendMessage(msg)
	if err != nil {
		logger.Errorf(ctx, "Failed to send message to topic %s: %v", topic, err)
		return err
	}

	logger.Infof(ctx, "Message sent to topic %s, partition %d, offset %d", topic, partition, offset)
	return nil
}

func (c *Client) Subscribe(ctx context.Context, topics []string, handler sarama.ConsumerGroupHandler) error {
	for {
		select {
		case <-ctx.Done():
			logger.Info(ctx, "Consumer context cancelled")
			return ctx.Err()
		default:
			err := c.consumer.Consume(ctx, topics, handler)
			if err != nil {
				logger.Errorf(ctx, "Error from consumer: %v", err)
				return err
			}
		}
	}
}

func (c *Client) Close() error {
	var errs []error

	if c.producer != nil {
		if err := c.producer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close producer: %w", err))
		}
	}

	if c.consumer != nil {
		if err := c.consumer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close consumer: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing kafka client: %v", errs)
	}

	return nil
}

// Helper function to extract correlation ID from message headers
func ExtractCorrelationID(msg *sarama.ConsumerMessage) string {
	for _, header := range msg.Headers {
		if string(header.Key) == "correlation_id" {
			return string(header.Value)
		}
	}
	return ""
}

// Helper function to extract all headers from message
func ExtractHeaders(msg *sarama.ConsumerMessage) map[string]string {
	headers := make(map[string]string)
	for _, header := range msg.Headers {
		headers[string(header.Key)] = string(header.Value)
	}
	return headers
}

