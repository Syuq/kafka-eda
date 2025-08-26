# Go + Kafka Event-Driven Architecture (EDA) Demo

A comprehensive demonstration of Event-Driven Architecture using Go and Apache Kafka, featuring idempotency, Dead Letter Queue (DLQ), replay functionality, circuit breakers, and schema evolution.

## 🚀 Features

### Core Features
- **Event-Driven Architecture**: Producer/Consumer pattern with Kafka
- **Idempotency**: Duplicate event detection using Redis and Kafka compact topics
- **Retry Logic**: Exponential backoff with jitter for failed message processing
- **Dead Letter Queue (DLQ)**: Failed messages handling and replay capability
- **Circuit Breaker**: Fault tolerance and resilience patterns
- **Correlation ID**: End-to-end request tracing

### Advanced Features
- **OpenTelemetry Integration**: Distributed tracing with Jaeger
- **Schema Evolution**: Avro and Protobuf schema management
- **Schema Registry**: Confluent Schema Registry integration
- **Hybrid Idempotency**: Redis + Kafka compact topic combination
- **Monitoring**: Health checks, metrics, and observability

### Infrastructure
- **Docker Compose**: Complete development environment
- **Kafka UI**: Web interface for Kafka management
- **Jaeger**: Distributed tracing visualization
- **Redis**: Caching and idempotency storage

## 📋 Prerequisites

- Docker and Docker Compose
- Go 1.21 or later
- Make (optional, for convenience commands)

## 🏗️ Architecture

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Producer  │───▶│    Kafka    │───▶│  Consumer   │
│   Service   │    │   Topics    │    │   Service   │
└─────────────┘    └─────────────┘    └─────────────┘
                          │                   │
                          ▼                   ▼
                   ┌─────────────┐    ┌─────────────┐
                   │    Redis    │    │   Replay    │
                   │(Idempotency)│    │   Service   │
                   └─────────────┘    └─────────────┘
```

### Topics Structure
- `orders`: Main topic for order events
- `orders_retry`: Retry topic for failed messages
- `orders_dlq`: Dead Letter Queue for permanently failed messages
- `orders_idempotency`: Compact topic for idempotency records

## 🚀 Quick Start

### 1. Clone and Setup


```bash
git clone <repository-url>
cd go-kafka-eda-demo
```

### 2. Start Infrastructure

```bash
# Start all infrastructure services
make run-infra

# Or manually with docker-compose
docker-compose up -d

# Wait for services to be ready (about 30 seconds)
```

### 3. Build Services

```bash
# Build all services
make build

# Or build individually
go build -o bin/producer ./cmd/producer
go build -o bin/consumer ./cmd/consumer
go build -o bin/replay ./cmd/replay
```

### 4. Run Services

Open three terminal windows and run:

```bash
# Terminal 1: Start Consumer
make consumer
# Or: ./bin/consumer

# Terminal 2: Start Producer
make producer
# Or: ./bin/producer

# Terminal 3: Start Replay Service
make replay
# Or: ./bin/replay
```

### 5. Test the System

```bash
# Create a test order
curl -X POST http://localhost:8090/api/v1/orders \
  -H "Content-Type: application/json" \
  -d '{
    "customer_id": "customer-123",
    "product_id": "product-456",
    "quantity": 2,
    "price": 29.99
  }'

# Create multiple orders
curl -X POST http://localhost:8090/api/v1/orders/batch \
  -H "Content-Type: application/json" \
  -d '[
    {
      "customer_id": "customer-123",
      "product_id": "product-456",
      "quantity": 1,
      "price": 19.99
    },
    {
      "customer_id": "customer-456",
      "product_id": "product-789",
      "quantity": 3,
      "price": 39.99
    }
  ]'
```

## 🔧 Configuration

The system uses environment variables for configuration. Default values are provided in `.env` file:

```bash
# Kafka Configuration
KAFKA_BROKERS=localhost:9092
KAFKA_TOPIC_ORDERS=orders
KAFKA_TOPIC_ORDERS_RETRY=orders_retry
KAFKA_TOPIC_ORDERS_DLQ=orders_dlq
KAFKA_TOPIC_IDEMPOTENCY=orders_idempotency
KAFKA_CONSUMER_GROUP=orders-consumer-group

# Redis Configuration
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
REDIS_DB=0

# Schema Registry
SCHEMA_REGISTRY_URL=http://localhost:8081

# OpenTelemetry
JAEGER_ENDPOINT=http://localhost:14268/api/traces
SERVICE_NAME=kafka-eda-demo

# Application Configuration
LOG_LEVEL=info
HTTP_PORT=8090
RETRY_MAX_ATTEMPTS=3
RETRY_INITIAL_INTERVAL=1s
RETRY_MAX_INTERVAL=30s
CIRCUIT_BREAKER_MAX_REQUESTS=3
CIRCUIT_BREAKER_INTERVAL=60s
CIRCUIT_BREAKER_TIMEOUT=30s
```

## 📊 Monitoring and Observability

### Web Interfaces

- **Kafka UI**: http://localhost:8080 - Kafka topics, messages, and consumer groups
- **Jaeger UI**: http://localhost:16686 - Distributed tracing
- **Producer API**: http://localhost:8090 - Producer service endpoints
- **Consumer API**: http://localhost:8090 - Consumer service metrics (different port in practice)
- **Replay API**: http://localhost:8090 - Replay service endpoints (different port in practice)

### Health Checks

```bash
# Check service health
curl http://localhost:8090/health

# Check service readiness
curl http://localhost:8090/ready

# Get consumer metrics
curl http://localhost:8091/api/v1/metrics

# Get circuit breaker status
curl http://localhost:8091/api/v1/circuit-breaker/status
```

## 🔄 Idempotency

The system implements idempotency using two approaches:

### 1. Redis-based Idempotency (Default)
- Fast lookup using Redis cache
- TTL-based cleanup
- Suitable for high-throughput scenarios

### 2. Kafka Compact Topic Idempotency
- Persistent storage using Kafka compact topics
- No external dependencies
- Better for long-term idempotency guarantees

### 3. Hybrid Approach
- Combines both Redis and compact topic
- Redis for fast access, compact topic for persistence
- Best of both worlds

## 🔁 Retry and DLQ Mechanism

### Retry Flow
1. Message processing fails
2. Message sent to `orders_retry` topic with exponential backoff
3. Retry consumer processes after delay
4. If max retries exceeded, message goes to DLQ

### DLQ Management
```bash
# List DLQ messages
curl http://localhost:8092/api/v1/dlq/messages

# Get DLQ statistics
curl http://localhost:8092/api/v1/dlq/stats

# Replay specific messages
curl -X POST http://localhost:8092/api/v1/replay \
  -H "Content-Type: application/json" \
  -d '{
    "event_ids": ["event-123", "event-456"],
    "dry_run": false
  }'

# Replay all messages in time range
curl -X POST http://localhost:8092/api/v1/replay/batch \
  -H "Content-Type: application/json" \
  -d '{
    "start_time": "2024-01-01T00:00:00Z",
    "end_time": "2024-01-02T00:00:00Z",
    "max_messages": 100,
    "dry_run": true
  }'
```

## 🔧 Circuit Breaker

The circuit breaker protects against cascading failures:

- **Closed**: Normal operation
- **Open**: Failing fast, not processing messages
- **Half-Open**: Testing if service recovered

```bash
# Check circuit breaker status
curl http://localhost:8091/api/v1/circuit-breaker/status

# Reset circuit breaker
curl -X POST http://localhost:8091/api/v1/circuit-breaker/reset
```

## 📋 Schema Evolution

The project demonstrates schema evolution using Avro and Protobuf:

### Avro Schemas
- `schemas/avro/order_event.avsc` - Version 1
- `schemas/avro/order_event_v2.avsc` - Version 2 with new fields

### Protobuf Schemas
- `schemas/protobuf/order_event.proto` - Complete schema definition

### Schema Registry Operations
```bash
# Register a schema
curl -X POST http://localhost:8081/subjects/orders-value/versions \
  -H "Content-Type: application/vnd.schemaregistry.v1+json" \
  -d '{"schema": "..."}'

# Get latest schema
curl http://localhost:8081/subjects/orders-value/versions/latest

# Check compatibility
curl -X POST http://localhost:8081/compatibility/subjects/orders-value/versions/latest \
  -H "Content-Type: application/vnd.schemaregistry.v1+json" \
  -d '{"schema": "..."}'
```

## 🧪 Testing

### Unit Tests
```bash
# Run all tests
make test

# Run tests with coverage
go test -v -cover ./...
```

### Integration Tests
```bash
# Start infrastructure first
make run-infra

# Run integration tests
go test -v -tags=integration ./tests/integration/...
```

### Load Testing
```bash
# Generate load using the batch endpoint
for i in {1..100}; do
  curl -X POST http://localhost:8090/api/v1/orders/batch \
    -H "Content-Type: application/json" \
    -d '[{"customer_id":"load-test-'$i'","product_id":"product-1","quantity":1,"price":10.00}]'
done
```

## 📁 Project Structure

```
go-kafka-eda-demo/
├── cmd/                    # Application entry points
│   ├── producer/          # Producer service
│   ├── consumer/          # Consumer service
│   └── replay/            # Replay service
├── internal/              # Private application code
│   ├── kafka/            # Kafka client and utilities
│   ├── redis/            # Redis client
│   ├── handler/          # Message handlers
│   ├── models/           # Data models
│   ├── telemetry/        # OpenTelemetry setup
│   └── circuit/          # Circuit breaker implementation
├── pkg/                   # Public library code
│   ├── config/           # Configuration management
│   └── logger/           # Logging utilities
├── schemas/               # Schema definitions
│   ├── avro/             # Avro schemas
│   └── protobuf/         # Protobuf schemas
├── scripts/               # Utility scripts
├── docs/                  # Documentation
├── docker-compose.yml     # Infrastructure setup
├── Makefile              # Build and run commands
├── go.mod                # Go module definition
└── README.md             # This file
```

## 🐛 Troubleshooting

### Common Issues

#### 1. Kafka Connection Issues
```bash
# Check if Kafka is running
docker ps | grep kafka

# Check Kafka logs
docker logs kafka

# Verify topics exist
docker exec kafka kafka-topics --list --bootstrap-server localhost:9092
```

#### 2. Redis Connection Issues
```bash
# Check Redis connectivity
docker exec redis redis-cli ping

# Check Redis logs
docker logs redis
```

#### 3. Schema Registry Issues
```bash
# Check Schema Registry health
curl http://localhost:8081/subjects

# Check Schema Registry logs
docker logs schema-registry
```

#### 4. Service Not Starting
```bash
# Check service logs
./bin/producer 2>&1 | tee producer.log

# Verify environment variables
env | grep KAFKA
env | grep REDIS
```

### Performance Tuning

#### Kafka Producer
- Adjust `batch.size` and `linger.ms` for throughput
- Use compression (snappy/lz4)
- Configure `acks=all` for durability

#### Kafka Consumer
- Tune `fetch.min.bytes` and `fetch.max.wait.ms`
- Adjust `max.poll.records` for batch processing
- Configure proper `session.timeout.ms`

#### Redis
- Use connection pooling
- Configure appropriate TTL values
- Monitor memory usage

## 🔒 Security Considerations

### Production Deployment
- Enable Kafka SASL/SSL authentication
- Use Redis AUTH and SSL
- Implement proper network segmentation
- Add rate limiting and input validation
- Use secrets management for credentials

### Monitoring
- Set up alerts for DLQ message count
- Monitor circuit breaker state changes
- Track message processing latency
- Monitor resource usage (CPU, memory, disk)

## 📚 Additional Resources

- [Apache Kafka Documentation](https://kafka.apache.org/documentation/)
- [Confluent Schema Registry](https://docs.confluent.io/platform/current/schema-registry/index.html)
- [OpenTelemetry Go](https://opentelemetry.io/docs/instrumentation/go/)
- [Circuit Breaker Pattern](https://martinfowler.com/bliki/CircuitBreaker.html)
- [Event-Driven Architecture](https://martinfowler.com/articles/201701-event-driven.html)

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## 📄 License

This project is licensed under the MIT License - see the LICENSE file for details.

## 🙏 Acknowledgments

- Apache Kafka community
- Confluent for Schema Registry
- OpenTelemetry project
- Go community for excellent libraries

