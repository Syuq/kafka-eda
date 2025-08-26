# Troubleshooting Guide

This guide helps you diagnose and resolve common issues with the Go + Kafka EDA Demo.

## 🚨 Common Issues

### 1. Services Won't Start

#### Kafka Connection Refused
```
Error: kafka: client has run out of available brokers to talk to
```

**Diagnosis:**
```bash
# Check if Kafka is running
docker ps | grep kafka

# Check Kafka logs
docker logs kafka

# Verify Kafka is listening on the correct port
docker exec kafka netstat -tlnp | grep 9092
```

**Solutions:**
- Ensure Docker Compose is running: `docker-compose up -d`
- Wait for Kafka to fully start (can take 30-60 seconds)
- Check if port 9092 is available: `netstat -tlnp | grep 9092`
- Restart Kafka: `docker-compose restart kafka`

#### Redis Connection Issues
```
Error: dial tcp 127.0.0.1:6379: connect: connection refused
```

**Diagnosis:**
```bash
# Check Redis status
docker ps | grep redis

# Test Redis connectivity
docker exec redis redis-cli ping

# Check Redis logs
docker logs redis
```

**Solutions:**
- Start Redis: `docker-compose up -d redis`
- Check Redis configuration in `.env` file
- Verify Redis port is not in use by another process

#### Schema Registry Not Available
```
Error: Get "http://localhost:8081/subjects": dial tcp 127.0.0.1:8081: connect: connection refused
```

**Diagnosis:**
```bash
# Check Schema Registry status
curl -s http://localhost:8081/subjects

# Check Schema Registry logs
docker logs schema-registry
```

**Solutions:**
- Ensure Schema Registry is running: `docker-compose up -d schema-registry`
- Wait for Schema Registry to start (depends on Kafka)
- Check if port 8081 is available

### 2. Message Processing Issues

#### Messages Not Being Consumed
**Symptoms:**
- Producer sends messages successfully
- Consumer shows no activity
- Messages accumulate in topics

**Diagnosis:**
```bash
# Check consumer group status
docker exec kafka kafka-consumer-groups --bootstrap-server localhost:9092 --describe --group orders-consumer-group

# Check topic messages
docker exec kafka kafka-console-consumer --bootstrap-server localhost:9092 --topic orders --from-beginning --max-messages 5

# Check consumer logs
./bin/consumer 2>&1 | grep -i error
```

**Solutions:**
- Verify consumer group configuration
- Check if consumer is subscribed to correct topics
- Restart consumer service
- Check for consumer lag

#### Idempotency Not Working
**Symptoms:**
- Duplicate messages being processed
- Same event ID processed multiple times

**Diagnosis:**
```bash
# Check Redis for idempotency records
docker exec redis redis-cli keys "idempotency:*"

# Check specific idempotency record
docker exec redis redis-cli get "idempotency:your-event-id"

# Check compact topic for idempotency
docker exec kafka kafka-console-consumer --bootstrap-server localhost:9092 --topic orders_idempotency --from-beginning
```

**Solutions:**
- Verify Redis connectivity
- Check TTL settings for idempotency records
- Ensure event IDs are unique
- Check compact topic configuration

#### Circuit Breaker Stuck Open
**Symptoms:**
- All messages failing immediately
- Circuit breaker status shows "open"

**Diagnosis:**
```bash
# Check circuit breaker status
curl http://localhost:8091/api/v1/circuit-breaker/status

# Check consumer metrics
curl http://localhost:8091/api/v1/metrics
```

**Solutions:**
- Wait for circuit breaker timeout period
- Reset circuit breaker: `curl -X POST http://localhost:8091/api/v1/circuit-breaker/reset`
- Fix underlying issue causing failures
- Adjust circuit breaker thresholds

### 3. Performance Issues

#### High Message Latency
**Symptoms:**
- Messages take long time to process
- High consumer lag

**Diagnosis:**
```bash
# Check consumer lag
docker exec kafka kafka-consumer-groups --bootstrap-server localhost:9092 --describe --group orders-consumer-group

# Check system resources
docker stats

# Check Jaeger traces for bottlenecks
# Visit http://localhost:16686
```

**Solutions:**
- Increase consumer instances
- Optimize message processing logic
- Tune Kafka consumer configuration
- Add more partitions to topics

#### Memory Issues
**Symptoms:**
- Services crashing with OOM errors
- High memory usage

**Diagnosis:**
```bash
# Check memory usage
docker stats

# Check Go memory profiling
go tool pprof http://localhost:8090/debug/pprof/heap
```

**Solutions:**
- Increase Docker memory limits
- Optimize data structures
- Implement proper connection pooling
- Tune garbage collection settings

### 4. Docker and Infrastructure Issues

#### Port Conflicts
```
Error: bind: address already in use
```

**Diagnosis:**
```bash
# Check what's using the port
netstat -tlnp | grep :8080
lsof -i :8080
```

**Solutions:**
- Stop conflicting services
- Change port configuration in `.env` file
- Use different ports in `docker-compose.yml`

#### Disk Space Issues
```
Error: no space left on device
```

**Diagnosis:**
```bash
# Check disk usage
df -h

# Check Docker disk usage
docker system df

# Check large log files
du -sh /var/lib/docker/containers/*
```

**Solutions:**
- Clean up Docker: `docker system prune -a`
- Remove old containers: `docker container prune`
- Remove unused volumes: `docker volume prune`
- Increase disk space

## 🔧 Debugging Tools

### 1. Kafka Tools

#### List Topics
```bash
docker exec kafka kafka-topics --list --bootstrap-server localhost:9092
```

#### Describe Topic
```bash
docker exec kafka kafka-topics --describe --topic orders --bootstrap-server localhost:9092
```

#### Consume Messages
```bash
docker exec kafka kafka-console-consumer --bootstrap-server localhost:9092 --topic orders --from-beginning
```

#### Produce Test Message
```bash
docker exec kafka kafka-console-producer --bootstrap-server localhost:9092 --topic orders
```

#### Check Consumer Groups
```bash
docker exec kafka kafka-consumer-groups --bootstrap-server localhost:9092 --list
docker exec kafka kafka-consumer-groups --bootstrap-server localhost:9092 --describe --group orders-consumer-group
```

### 2. Redis Tools

#### Connect to Redis CLI
```bash
docker exec -it redis redis-cli
```

#### Check All Keys
```bash
docker exec redis redis-cli keys "*"
```

#### Monitor Redis Commands
```bash
docker exec redis redis-cli monitor
```

#### Check Redis Info
```bash
docker exec redis redis-cli info
```

### 3. Application Debugging

#### Enable Debug Logging
```bash
# Set environment variable
export LOG_LEVEL=debug

# Or modify .env file
echo "LOG_LEVEL=debug" >> .env
```

#### Check Service Health
```bash
# Producer health
curl http://localhost:8090/health

# Consumer health (adjust port)
curl http://localhost:8091/health

# Replay service health (adjust port)
curl http://localhost:8092/health
```

#### Get Service Metrics
```bash
# Consumer metrics
curl http://localhost:8091/api/v1/metrics

# Circuit breaker status
curl http://localhost:8091/api/v1/circuit-breaker/status
```

## 📊 Monitoring and Observability

### 1. Jaeger Tracing

**Access:** http://localhost:16686

**Common Issues:**
- Traces not appearing: Check if services are sending traces
- Incomplete traces: Verify all services have tracing enabled
- Performance issues: Look for long-running spans

### 2. Kafka UI

**Access:** http://localhost:8080

**Useful Features:**
- Topic overview and message browsing
- Consumer group monitoring
- Broker and cluster health
- Schema registry integration

### 3. Application Logs

#### Structured Logging
All services use structured JSON logging:
```bash
# Filter by correlation ID
./bin/consumer 2>&1 | jq 'select(.correlation_id == "your-correlation-id")'

# Filter by log level
./bin/consumer 2>&1 | jq 'select(.level == "error")'

# Filter by time range
./bin/consumer 2>&1 | jq 'select(.time > "2024-01-01T00:00:00Z")'
```

## 🚀 Performance Tuning

### 1. Kafka Configuration

#### Producer Tuning
```bash
# In producer configuration
batch.size=16384
linger.ms=5
compression.type=snappy
acks=all
retries=2147483647
```

#### Consumer Tuning
```bash
# In consumer configuration
fetch.min.bytes=1
fetch.max.wait.ms=500
max.poll.records=500
session.timeout.ms=10000
heartbeat.interval.ms=3000
```

### 2. Application Tuning

#### Go Runtime
```bash
# Set GOMAXPROCS
export GOMAXPROCS=4

# Tune garbage collector
export GOGC=100
```

#### Connection Pooling
- Use connection pools for Redis
- Reuse Kafka producers
- Implement proper connection lifecycle management

### 3. Infrastructure Tuning

#### Docker Resources
```yaml
# In docker-compose.yml
services:
  kafka:
    mem_limit: 2g
    cpus: 2.0
```

#### OS Tuning
```bash
# Increase file descriptor limits
ulimit -n 65536

# Tune network settings
echo 'net.core.rmem_max = 134217728' >> /etc/sysctl.conf
echo 'net.core.wmem_max = 134217728' >> /etc/sysctl.conf
```

## 🔒 Security Considerations

### 1. Production Checklist

- [ ] Enable Kafka SASL/SSL
- [ ] Configure Redis AUTH
- [ ] Use TLS for all communications
- [ ] Implement proper authentication
- [ ] Set up network segmentation
- [ ] Configure firewall rules
- [ ] Use secrets management
- [ ] Enable audit logging

### 2. Common Security Issues

#### Exposed Services
- Ensure services are not exposed to public internet
- Use proper network policies
- Implement authentication and authorization

#### Credential Management
- Don't hardcode credentials
- Use environment variables or secrets management
- Rotate credentials regularly

## 📞 Getting Help

### 1. Log Analysis

When reporting issues, include:
- Service logs with timestamps
- Error messages and stack traces
- Configuration files
- Environment details

### 2. Useful Commands for Support

```bash
# Collect system information
docker version
docker-compose version
go version

# Collect service status
docker ps -a
docker-compose ps

# Collect logs
docker-compose logs > system-logs.txt

# Collect configuration
env | grep -E "(KAFKA|REDIS|LOG)" > config.txt
```

### 3. Community Resources

- [Apache Kafka Documentation](https://kafka.apache.org/documentation/)
- [Go Kafka Client (Sarama)](https://github.com/Shopify/sarama)
- [Redis Documentation](https://redis.io/documentation)
- [OpenTelemetry Go](https://opentelemetry.io/docs/instrumentation/go/)

Remember: When in doubt, check the logs first! Most issues can be diagnosed by examining the service logs and system status.

