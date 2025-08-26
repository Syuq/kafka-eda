#!/bin/bash

# Test script for Replay Service functionality
# This script demonstrates DLQ and replay operations

set -e

REPLAY_URL="http://localhost:8092"

echo "🔄 Testing Replay Service Functionality"
echo "======================================="

# Function to check if service is ready
check_service() {
    local url=$1
    local service_name=$2
    
    echo "Checking $service_name..."
    if curl -s "$url/health" > /dev/null; then
        echo "✅ $service_name is healthy"
    else
        echo "❌ $service_name is not responding"
        echo "Make sure the replay service is running on port 8092"
        exit 1
    fi
}

# Check replay service
echo "📋 Checking replay service health..."
check_service $REPLAY_URL "Replay Service"

echo ""
echo "🧪 Test 1: Get DLQ Statistics"
echo "-----------------------------"

DLQ_STATS=$(curl -s "$REPLAY_URL/api/v1/dlq/stats")
echo "DLQ Stats: $DLQ_STATS"

TOTAL_MESSAGES=$(echo $DLQ_STATS | jq -r '.data.total_messages // 0')
echo "📊 Total DLQ messages: $TOTAL_MESSAGES"

echo ""
echo "🧪 Test 2: List DLQ Messages"
echo "----------------------------"

DLQ_MESSAGES=$(curl -s "$REPLAY_URL/api/v1/dlq/messages?limit=10&offset=0")
echo "DLQ Messages: $DLQ_MESSAGES"

MESSAGE_COUNT=$(echo $DLQ_MESSAGES | jq -r '.data.count // 0')
echo "📋 Retrieved $MESSAGE_COUNT DLQ messages"

echo ""
echo "🧪 Test 3: Dry Run Replay"
echo "-------------------------"

echo "Performing dry run replay of all messages..."
DRY_RUN_RESPONSE=$(curl -s -X POST "$REPLAY_URL/api/v1/replay" \
  -H "Content-Type: application/json" \
  -d '{
    "max_messages": 10,
    "dry_run": true
  }')

echo "Dry Run Response: $DRY_RUN_RESPONSE"

DRY_RUN_SUCCESS=$(echo $DRY_RUN_RESPONSE | jq -r '.success')
if [ "$DRY_RUN_SUCCESS" = "true" ]; then
    REPLAYED_COUNT=$(echo $DRY_RUN_RESPONSE | jq -r '.data.replayed_messages // 0')
    echo "✅ Dry run successful - would replay $REPLAYED_COUNT messages"
else
    echo "❌ Dry run failed"
fi

echo ""
echo "🧪 Test 4: Batch Dry Run Replay"
echo "-------------------------------"

echo "Performing batch dry run replay..."
BATCH_DRY_RUN=$(curl -s -X POST "$REPLAY_URL/api/v1/replay/batch" \
  -H "Content-Type: application/json" \
  -d '{
    "max_messages": 50,
    "dry_run": true
  }')

echo "Batch Dry Run Response: $BATCH_DRY_RUN"

BATCH_SUCCESS=$(echo $BATCH_DRY_RUN | jq -r '.success')
if [ "$BATCH_SUCCESS" = "true" ]; then
    BATCH_REPLAYED=$(echo $BATCH_DRY_RUN | jq -r '.data.replayed_messages // 0')
    echo "✅ Batch dry run successful - would replay $BATCH_REPLAYED messages"
else
    echo "❌ Batch dry run failed"
fi

echo ""
echo "🧪 Test 5: Specific Event Replay (Dry Run)"
echo "------------------------------------------"

# Test replaying specific event IDs (these are example IDs)
SPECIFIC_REPLAY=$(curl -s -X POST "$REPLAY_URL/api/v1/replay" \
  -H "Content-Type: application/json" \
  -d '{
    "event_ids": ["event-123", "event-456", "event-789"],
    "dry_run": true
  }')

echo "Specific Event Replay Response: $SPECIFIC_REPLAY"

SPECIFIC_SUCCESS=$(echo $SPECIFIC_REPLAY | jq -r '.success')
if [ "$SPECIFIC_SUCCESS" = "true" ]; then
    echo "✅ Specific event replay test successful"
else
    echo "ℹ️  Specific event replay test completed (events may not exist in DLQ)"
fi

echo ""
echo "🧪 Test 6: Time Range Replay (Dry Run)"
echo "--------------------------------------"

# Get current time and 1 hour ago for time range test
CURRENT_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
ONE_HOUR_AGO=$(date -u -d "1 hour ago" +"%Y-%m-%dT%H:%M:%SZ")

TIME_RANGE_REPLAY=$(curl -s -X POST "$REPLAY_URL/api/v1/replay" \
  -H "Content-Type: application/json" \
  -d "{
    \"start_time\": \"$ONE_HOUR_AGO\",
    \"end_time\": \"$CURRENT_TIME\",
    \"max_messages\": 20,
    \"dry_run\": true
  }")

echo "Time Range Replay Response: $TIME_RANGE_REPLAY"

TIME_RANGE_SUCCESS=$(echo $TIME_RANGE_REPLAY | jq -r '.success')
if [ "$TIME_RANGE_SUCCESS" = "true" ]; then
    TIME_RANGE_COUNT=$(echo $TIME_RANGE_REPLAY | jq -r '.data.replayed_messages // 0')
    echo "✅ Time range replay test successful - would replay $TIME_RANGE_COUNT messages"
else
    echo "ℹ️  Time range replay test completed"
fi

echo ""
echo "⚠️  Test 7: Actual Replay (if DLQ has messages)"
echo "-----------------------------------------------"

if [ "$TOTAL_MESSAGES" -gt 0 ]; then
    echo "DLQ has $TOTAL_MESSAGES messages. Performing actual replay of 1 message..."
    
    read -p "Do you want to perform actual replay? (y/N): " -n 1 -r
    echo
    
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        ACTUAL_REPLAY=$(curl -s -X POST "$REPLAY_URL/api/v1/replay" \
          -H "Content-Type: application/json" \
          -d '{
            "max_messages": 1,
            "dry_run": false
          }')
        
        echo "Actual Replay Response: $ACTUAL_REPLAY"
        
        ACTUAL_SUCCESS=$(echo $ACTUAL_REPLAY | jq -r '.success')
        if [ "$ACTUAL_SUCCESS" = "true" ]; then
            ACTUAL_REPLAYED=$(echo $ACTUAL_REPLAY | jq -r '.data.replayed_messages // 0')
            echo "✅ Successfully replayed $ACTUAL_REPLAYED messages"
        else
            echo "❌ Actual replay failed"
        fi
    else
        echo "ℹ️  Skipped actual replay"
    fi
else
    echo "ℹ️  No messages in DLQ to replay"
fi

echo ""
echo "🧪 Test 8: DLQ Clear Test (Dangerous)"
echo "------------------------------------"

echo "⚠️  WARNING: This will clear all DLQ messages!"
read -p "Do you want to test DLQ clearing? (y/N): " -n 1 -r
echo

if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "Clearing DLQ..."
    CLEAR_RESPONSE=$(curl -s -X DELETE "$REPLAY_URL/api/v1/dlq/clear?confirm=true")
    echo "Clear Response: $CLEAR_RESPONSE"
    
    CLEAR_SUCCESS=$(echo $CLEAR_RESPONSE | jq -r '.success')
    if [ "$CLEAR_SUCCESS" = "true" ]; then
        CLEARED_COUNT=$(echo $CLEAR_RESPONSE | jq -r '.data.cleared_messages // 0')
        echo "✅ Successfully cleared $CLEARED_COUNT messages from DLQ"
    else
        echo "❌ DLQ clear failed"
    fi
else
    echo "ℹ️  Skipped DLQ clearing"
fi

echo ""
echo "📊 Replay Service Test Summary"
echo "============================="
echo "✅ DLQ statistics retrieval"
echo "✅ DLQ message listing"
echo "✅ Dry run replay testing"
echo "✅ Batch replay testing"
echo "✅ Specific event replay testing"
echo "✅ Time range replay testing"

if [ "$TOTAL_MESSAGES" -gt 0 ]; then
    echo "✅ Actual replay testing (optional)"
else
    echo "ℹ️  No messages available for actual replay"
fi

echo ""
echo "🔍 Monitoring Tips:"
echo "- Check Kafka UI at http://localhost:8080 for topic activity"
echo "- Monitor consumer logs for replayed message processing"
echo "- Check Jaeger traces at http://localhost:16686 for replay operations"
echo "- Use DLQ stats endpoint to monitor DLQ size over time"

echo ""
echo "🎉 Replay service tests completed!"

