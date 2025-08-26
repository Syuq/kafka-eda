#!/bin/bash

# Setup script for Go + Kafka EDA Demo
# This script helps you get started quickly

set -e

echo "🚀 Go + Kafka EDA Demo Setup"
echo "============================"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

print_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

# Check prerequisites
echo ""
echo "📋 Checking Prerequisites..."

# Check Docker
if command -v docker &> /dev/null; then
    DOCKER_VERSION=$(docker --version)
    print_status "Docker found: $DOCKER_VERSION"
else
    print_error "Docker not found. Please install Docker first."
    exit 1
fi

# Check Docker Compose
if command -v docker-compose &> /dev/null; then
    COMPOSE_VERSION=$(docker-compose --version)
    print_status "Docker Compose found: $COMPOSE_VERSION"
else
    print_error "Docker Compose not found. Please install Docker Compose first."
    exit 1
fi

# Check Go
if command -v go &> /dev/null; then
    GO_VERSION=$(go version)
    print_status "Go found: $GO_VERSION"
else
    print_error "Go not found. Please install Go 1.21 or later."
    exit 1
fi

# Check Make (optional)
if command -v make &> /dev/null; then
    print_status "Make found (optional)"
    HAS_MAKE=true
else
    print_warning "Make not found (optional, but recommended)"
    HAS_MAKE=false
fi

# Check jq (for testing scripts)
if command -v jq &> /dev/null; then
    print_status "jq found (for testing scripts)"
else
    print_warning "jq not found (recommended for testing scripts)"
    echo "Install with: sudo apt-get install jq (Ubuntu/Debian) or brew install jq (macOS)"
fi

echo ""
echo "🔧 Setting up the project..."

# Make scripts executable
print_info "Making scripts executable..."
chmod +x scripts/*.sh

# Check if .env file exists
if [ ! -f .env ]; then
    print_info "Creating .env file with default configuration..."
    cat > .env << 'EOF'
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
EOF
    print_status ".env file created"
else
    print_status ".env file already exists"
fi

# Download Go dependencies
print_info "Downloading Go dependencies..."
go mod tidy
go mod download
print_status "Go dependencies downloaded"

# Check if Docker is running
if ! docker info &> /dev/null; then
    print_error "Docker daemon is not running. Please start Docker first."
    exit 1
fi

# Start infrastructure
echo ""
echo "🐳 Starting Infrastructure..."
print_info "This may take a few minutes on first run..."

docker-compose up -d

# Wait for services to be ready
print_info "Waiting for services to be ready..."
sleep 10

# Check if Kafka is ready
KAFKA_READY=false
for i in {1..30}; do
    if docker exec kafka kafka-topics --list --bootstrap-server localhost:9092 &> /dev/null; then
        KAFKA_READY=true
        break
    fi
    echo -n "."
    sleep 2
done

if [ "$KAFKA_READY" = true ]; then
    print_status "Kafka is ready"
else
    print_error "Kafka failed to start properly"
    echo "Check logs with: docker logs kafka"
    exit 1
fi

# Create Kafka topics
print_info "Creating Kafka topics..."
if [ "$HAS_MAKE" = true ]; then
    make topics
else
    docker exec kafka kafka-topics --create --topic orders --bootstrap-server localhost:9092 --partitions 3 --replication-factor 1 --if-not-exists
    docker exec kafka kafka-topics --create --topic orders_retry --bootstrap-server localhost:9092 --partitions 3 --replication-factor 1 --if-not-exists
    docker exec kafka kafka-topics --create --topic orders_dlq --bootstrap-server localhost:9092 --partitions 3 --replication-factor 1 --if-not-exists
    docker exec kafka kafka-topics --create --topic orders_idempotency --bootstrap-server localhost:9092 --partitions 3 --replication-factor 1 --config cleanup.policy=compact --if-not-exists
fi
print_status "Kafka topics created"

# Check Redis
if docker exec redis redis-cli ping &> /dev/null; then
    print_status "Redis is ready"
else
    print_error "Redis failed to start properly"
    echo "Check logs with: docker logs redis"
    exit 1
fi

# Build services
echo ""
echo "🔨 Building Services..."
if [ "$HAS_MAKE" = true ]; then
    make build
else
    go build -o bin/producer ./cmd/producer
    go build -o bin/consumer ./cmd/consumer
    go build -o bin/replay ./cmd/replay
fi
print_status "Services built successfully"

# Final status check
echo ""
echo "🔍 Final Status Check..."

# Check all containers
CONTAINERS_STATUS=$(docker-compose ps --services --filter "status=running" | wc -l)
TOTAL_CONTAINERS=$(docker-compose ps --services | wc -l)

if [ "$CONTAINERS_STATUS" -eq "$TOTAL_CONTAINERS" ]; then
    print_status "All containers are running ($CONTAINERS_STATUS/$TOTAL_CONTAINERS)"
else
    print_warning "Some containers may not be running ($CONTAINERS_STATUS/$TOTAL_CONTAINERS)"
    echo "Check with: docker-compose ps"
fi

# Display service URLs
echo ""
echo "🌐 Service URLs:"
echo "  Kafka UI:        http://localhost:8080"
echo "  Jaeger UI:       http://localhost:16686"
echo "  Schema Registry: http://localhost:8081"
echo "  Producer API:    http://localhost:8090"

echo ""
echo "🚀 Setup Complete!"
echo "=================="
print_status "Infrastructure is running"
print_status "Services are built and ready"
print_status "Kafka topics are created"

echo ""
echo "📖 Next Steps:"
echo ""
echo "1. Start the services in separate terminals:"
echo "   Terminal 1: ./bin/consumer (or make consumer)"
echo "   Terminal 2: ./bin/producer (or make producer)"  
echo "   Terminal 3: ./bin/replay (or make replay)"
echo ""
echo "2. Run tests:"
echo "   ./scripts/test-orders.sh"
echo "   ./scripts/test-replay.sh"
echo ""
echo "3. Monitor the system:"
echo "   - Kafka UI: http://localhost:8080"
echo "   - Jaeger traces: http://localhost:16686"
echo ""
echo "4. Read the documentation:"
echo "   - README.md for detailed usage"
echo "   - docs/TROUBLESHOOTING.md for common issues"

echo ""
print_info "Happy coding! 🎉"

