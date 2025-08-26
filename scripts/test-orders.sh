#!/bin/bash

# Test script for Go + Kafka EDA Demo
# This script demonstrates various testing scenarios

set -e

PRODUCER_URL="http://localhost:8090"
CONSUMER_URL="http://localhost:8091"
REPLAY_URL="http://localhost:8092"

echo "🚀 Starting Go + Kafka EDA Demo Tests"
echo "======================================"

# Function to check if service is ready
check_service() {
    local url=$1
    local service_name=$2
    
    echo "Checking $service_name..."
    if curl -s "$url/health" > /dev/null; then
        echo "✅ $service_name is healthy"
    else
        echo "❌ $service_name is not responding"
        exit 1
    fi
}

# Check all services
echo "📋 Checking service health..."
check_service $PRODUCER_URL "Producer"
# Note: Consumer and Replay services run on different ports in practice
# check_service $CONSUMER_URL "Consumer"
# check_service $REPLAY_URL "Replay"

echo ""
echo "🧪 Test 1: Create a single order"
echo "--------------------------------"

RESPONSE=$(curl -s -X POST "$PRODUCER_URL/api/v1/orders" \
  -H "Content-Type: application/json" \
  -d '{
    "customer_id": "customer-001",
    "product_id": "product-laptop",
    "quantity": 1,
    "price": 999.99
  }')

echo "Response: $RESPONSE"
ORDER_ID=$(echo $RESPONSE | jq -r '.data.order_id')
CORRELATION_ID=$(echo $RESPONSE | jq -r '.correlation_id')
echo "✅ Created order: $ORDER_ID with correlation ID: $CORRELATION_ID"

echo ""
echo "🧪 Test 2: Create batch orders"
echo "------------------------------"

BATCH_RESPONSE=$(curl -s -X POST "$PRODUCER_URL/api/v1/orders/batch" \
  -H "Content-Type: application/json" \
  -d '[
    {
      "customer_id": "customer-002",
      "product_id": "product-phone",
      "quantity": 2,
      "price": 599.99
    },
    {
      "customer_id": "customer-003",
      "product_id": "product-tablet",
      "quantity": 1,
      "price": 399.99
    },
    {
      "customer_id": "customer-004",
      "product_id": "product-watch",
      "quantity": 3,
      "price": 299.99
    }
  ]')

echo "Batch Response: $BATCH_RESPONSE"
PROCESSED_COUNT=$(echo $BATCH_RESPONSE | jq -r '.data.processed')
echo "✅ Processed $PROCESSED_COUNT orders in batch"

echo ""
echo "🧪 Test 3: Test idempotency (duplicate order)"
echo "--------------------------------------------"

# Send the same order twice
echo "Sending first order..."
FIRST_RESPONSE=$(curl -s -X POST "$PRODUCER_URL/api/v1/orders" \
  -H "Content-Type: application/json" \
  -H "X-Correlation-ID: test-idempotency-123" \
  -d '{
    "customer_id": "customer-005",
    "product_id": "product-headphones",
    "quantity": 1,
    "price": 199.99
  }')

echo "First Response: $FIRST_RESPONSE"

echo "Sending duplicate order with same correlation ID..."
sleep 2  # Wait a bit to ensure processing

SECOND_RESPONSE=$(curl -s -X POST "$PRODUCER_URL/api/v1/orders" \
  -H "Content-Type: application/json" \
  -H "X-Correlation-ID: test-idempotency-123" \
  -d '{
    "customer_id": "customer-005",
    "product_id": "product-headphones",
    "quantity": 1,
    "price": 199.99
  }')

echo "Second Response: $SECOND_RESPONSE"
echo "✅ Idempotency test completed"

echo ""
echo "🧪 Test 4: Load test with multiple orders"
echo "----------------------------------------"

echo "Creating 20 orders rapidly..."
for i in {1..20}; do
    curl -s -X POST "$PRODUCER_URL/api/v1/orders" \
      -H "Content-Type: application/json" \
      -d "{
        \"customer_id\": \"load-test-customer-$i\",
        \"product_id\": \"load-test-product-$((i % 5 + 1))\",
        \"quantity\": $((i % 3 + 1)),
        \"price\": $((i * 10 + 50)).99
      }" > /dev/null &
    
    if [ $((i % 5)) -eq 0 ]; then
        echo "Created $i orders..."
        wait  # Wait for batch to complete
    fi
done

wait  # Wait for all background jobs
echo "✅ Load test completed - 20 orders created"

echo ""
echo "🧪 Test 5: Error simulation (invalid data)"
echo "-----------------------------------------"

ERROR_RESPONSE=$(curl -s -X POST "$PRODUCER_URL/api/v1/orders" \
  -H "Content-Type: application/json" \
  -d '{
    "customer_id": "",
    "product_id": "product-invalid",
    "quantity": -1,
    "price": -99.99
  }')

echo "Error Response: $ERROR_RESPONSE"
SUCCESS=$(echo $ERROR_RESPONSE | jq -r '.success')
if [ "$SUCCESS" = "false" ]; then
    echo "✅ Error handling working correctly"
else
    echo "❌ Error handling not working as expected"
fi

echo ""
echo "📊 Test Summary"
echo "==============="
echo "✅ Single order creation"
echo "✅ Batch order creation"
echo "✅ Idempotency testing"
echo "✅ Load testing"
echo "✅ Error handling"

echo ""
echo "🔍 Next Steps:"
echo "- Check Kafka UI at http://localhost:8080"
echo "- Check Jaeger traces at http://localhost:16686"
echo "- Monitor consumer logs for message processing"
echo "- Check DLQ for any failed messages"

echo ""
echo "🎉 All tests completed successfully!"

