package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Kafka         KafkaConfig
	Redis         RedisConfig
	SchemaRegistry SchemaRegistryConfig
	Telemetry     TelemetryConfig
	App           AppConfig
}

type KafkaConfig struct {
	Brokers           []string
	TopicOrders       string
	TopicOrdersRetry  string
	TopicOrdersDLQ    string
	TopicIdempotency  string
	ConsumerGroup     string
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type SchemaRegistryConfig struct {
	URL string
}

type TelemetryConfig struct {
	JaegerEndpoint string
	ServiceName    string
}

type AppConfig struct {
	LogLevel                  string
	HTTPPort                  string
	RetryMaxAttempts          int
	RetryInitialInterval      time.Duration
	RetryMaxInterval          time.Duration
	CircuitBreakerMaxRequests uint32
	CircuitBreakerInterval    time.Duration
	CircuitBreakerTimeout     time.Duration
}

func Load() *Config {
	return &Config{
		Kafka: KafkaConfig{
			Brokers:           strings.Split(getEnv("KAFKA_BROKERS", "localhost:9092"), ","),
			TopicOrders:       getEnv("KAFKA_TOPIC_ORDERS", "orders"),
			TopicOrdersRetry:  getEnv("KAFKA_TOPIC_ORDERS_RETRY", "orders_retry"),
			TopicOrdersDLQ:    getEnv("KAFKA_TOPIC_ORDERS_DLQ", "orders_dlq"),
			TopicIdempotency:  getEnv("KAFKA_TOPIC_IDEMPOTENCY", "orders_idempotency"),
			ConsumerGroup:     getEnv("KAFKA_CONSUMER_GROUP", "orders-consumer-group"),
		},
		Redis: RedisConfig{
			Addr:     getEnv("REDIS_ADDR", "localhost:6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getEnvAsInt("REDIS_DB", 0),
		},
		SchemaRegistry: SchemaRegistryConfig{
			URL: getEnv("SCHEMA_REGISTRY_URL", "http://localhost:8081"),
		},
		Telemetry: TelemetryConfig{
			JaegerEndpoint: getEnv("JAEGER_ENDPOINT", "http://localhost:14268/api/traces"),
			ServiceName:    getEnv("SERVICE_NAME", "kafka-eda-demo"),
		},
		App: AppConfig{
			LogLevel:                  getEnv("LOG_LEVEL", "info"),
			HTTPPort:                  getEnv("HTTP_PORT", "8090"),
			RetryMaxAttempts:          getEnvAsInt("RETRY_MAX_ATTEMPTS", 3),
			RetryInitialInterval:      getEnvAsDuration("RETRY_INITIAL_INTERVAL", time.Second),
			RetryMaxInterval:          getEnvAsDuration("RETRY_MAX_INTERVAL", 30*time.Second),
			CircuitBreakerMaxRequests: uint32(getEnvAsInt("CIRCUIT_BREAKER_MAX_REQUESTS", 3)),
			CircuitBreakerInterval:    getEnvAsDuration("CIRCUIT_BREAKER_INTERVAL", 60*time.Second),
			CircuitBreakerTimeout:     getEnvAsDuration("CIRCUIT_BREAKER_TIMEOUT", 30*time.Second),
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

