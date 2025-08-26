.PHONY: help build run-infra stop-infra clean test lint

# Default target
help:
	@echo "Available targets:"
	@echo "  build        - Build all services"
	@echo "  run-infra    - Start infrastructure (Kafka, Redis, etc.)"
	@echo "  stop-infra   - Stop infrastructure"
	@echo "  clean        - Clean up containers and volumes"
	@echo "  test         - Run tests"
	@echo "  lint         - Run linter"
	@echo "  producer     - Run producer service"
	@echo "  consumer     - Run consumer service"
	@echo "  replay       - Run replay service"
	@echo "  topics       - Create Kafka topics"

# Build all services
build:
	@echo "Building services..."
	go build -o bin/producer ./cmd/producer
	go build -o bin/consumer ./cmd/consumer
	go build -o bin/replay ./cmd/replay

# Infrastructure management
run-infra:
	@echo "Starting infrastructure..."
	docker-compose up -d
	@echo "Waiting for services to be ready..."
	sleep 30
	@make topics

stop-infra:
	@echo "Stopping infrastructure..."
	docker-compose down

clean:
	@echo "Cleaning up..."
	docker-compose down -v
	docker system prune -f
	rm -rf bin/

# Create Kafka topics
topics:
	@echo "Creating Kafka topics..."
	docker exec kafka kafka-topics --create --topic orders --bootstrap-server localhost:9092 --partitions 3 --replication-factor 1 --if-not-exists
	docker exec kafka kafka-topics --create --topic orders_retry --bootstrap-server localhost:9092 --partitions 3 --replication-factor 1 --if-not-exists
	docker exec kafka kafka-topics --create --topic orders_dlq --bootstrap-server localhost:9092 --partitions 3 --replication-factor 1 --if-not-exists
	docker exec kafka kafka-topics --create --topic orders_idempotency --bootstrap-server localhost:9092 --partitions 3 --replication-factor 1 --config cleanup.policy=compact --if-not-exists

# Run services
producer: build
	@echo "Starting producer service..."
	./bin/producer

consumer: build
	@echo "Starting consumer service..."
	./bin/consumer

replay: build
	@echo "Starting replay service..."
	./bin/replay

# Development
test:
	go test -v ./...

lint:
	golangci-lint run

# Dependencies
deps:
	go mod tidy
	go mod download

# Generate protobuf
proto:
	protoc --go_out=. --go_opt=paths=source_relative schemas/protobuf/*.proto

