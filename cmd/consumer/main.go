package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"go-kafka-eda-demo/internal/handler"
	"go-kafka-eda-demo/internal/kafka"
	"go-kafka-eda-demo/internal/redis"
	"go-kafka-eda-demo/internal/telemetry"
	"go-kafka-eda-demo/pkg/config"
	"go-kafka-eda-demo/pkg/logger"
)

type ConsumerService struct {
	kafkaClient   *kafka.Client
	redisClient   *redis.Client
	orderHandler  *handler.OrderHandler
	config        *config.Config
	shutdownTrace func()
}

type Response struct {
	Success       bool        `json:"success"`
	Message       string      `json:"message"`
	Data          interface{} `json:"data,omitempty"`
	CorrelationID string      `json:"correlation_id"`
}

func main() {
	// Load configuration
	cfg := config.Load()
	logger.SetLevel(cfg.App.LogLevel)

	ctx := context.Background()
	logger.Info(ctx, "Starting consumer service...")

	// Initialize tracing
	shutdownTrace, err := telemetry.InitTracing(&cfg.Telemetry)
	if err != nil {
		logger.Fatalf(ctx, "Failed to initialize tracing: %v", err)
	}
	defer shutdownTrace()

	// Initialize Redis client
	redisClient, err := redis.NewClient(&cfg.Redis)
	if err != nil {
		logger.Fatalf(ctx, "Failed to create Redis client: %v", err)
	}
	defer redisClient.Close()

	// Initialize Kafka client
	kafkaClient, err := kafka.NewClient(&cfg.Kafka)
	if err != nil {
		logger.Fatalf(ctx, "Failed to create Kafka client: %v", err)
	}
	defer kafkaClient.Close()

	// Create order handler
	orderHandler := handler.NewOrderHandler(kafkaClient, redisClient, cfg)

	// Create consumer service
	service := &ConsumerService{
		kafkaClient:   kafkaClient,
		redisClient:   redisClient,
		orderHandler:  orderHandler,
		config:        cfg,
		shutdownTrace: shutdownTrace,
	}

	// Setup HTTP server for health checks and metrics
	router := mux.NewRouter()
	service.setupRoutes(router)

	server := &http.Server{
		Addr:         ":" + cfg.App.HTTPPort,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start HTTP server in a goroutine
	go func() {
		logger.Infof(ctx, "Consumer HTTP server listening on port %s", cfg.App.HTTPPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Errorf(ctx, "HTTP server error: %v", err)
		}
	}()

	// Start consuming messages
	consumerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		topics := []string{cfg.Kafka.TopicOrders}
		logger.Infof(ctx, "Starting to consume from topics: %v", topics)
		
		if err := kafkaClient.Subscribe(consumerCtx, topics, orderHandler); err != nil {
			logger.Errorf(ctx, "Consumer error: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info(ctx, "Shutting down consumer service...")

	// Cancel consumer context
	cancel()

	// Graceful shutdown of HTTP server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Errorf(ctx, "Server forced to shutdown: %v", err)
	}

	logger.Info(ctx, "Consumer service stopped")
}

func (s *ConsumerService) setupRoutes(router *mux.Router) {
	// Add correlation ID middleware
	router.Use(s.correlationIDMiddleware)
	router.Use(s.loggingMiddleware)

	// API routes
	api := router.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/metrics", s.getMetrics).Methods("GET")
	api.HandleFunc("/circuit-breaker/status", s.getCircuitBreakerStatus).Methods("GET")
	api.HandleFunc("/circuit-breaker/reset", s.resetCircuitBreaker).Methods("POST")

	// Health check
	router.HandleFunc("/health", s.healthCheck).Methods("GET")
	router.HandleFunc("/ready", s.readinessCheck).Methods("GET")
}

func (s *ConsumerService) correlationIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correlationID := r.Header.Get("X-Correlation-ID")
		if correlationID == "" {
			correlationID = logger.NewCorrelationID()
		}

		ctx := logger.SetCorrelationID(r.Context(), correlationID)
		r = r.WithContext(ctx)

		w.Header().Set("X-Correlation-ID", correlationID)
		next.ServeHTTP(w, r)
	})
}

func (s *ConsumerService) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		logger.Infof(r.Context(), "Request started: %s %s", r.Method, r.URL.Path)
		
		next.ServeHTTP(w, r)
		
		duration := time.Since(start)
		logger.Infof(r.Context(), "Request completed: %s %s in %v", r.Method, r.URL.Path, duration)
	})
}

func (s *ConsumerService) getMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	// Get circuit breaker metrics
	cbCounts := s.orderHandler.GetCircuitBreakerCounts()
	cbState := s.orderHandler.GetCircuitBreakerState()

	metrics := map[string]interface{}{
		"circuit_breaker": map[string]interface{}{
			"state":                 cbState.String(),
			"requests":              cbCounts.Requests,
			"total_successes":       cbCounts.TotalSuccesses,
			"total_failures":        cbCounts.TotalFailures,
			"consecutive_successes": cbCounts.ConsecutiveSuccesses,
			"consecutive_failures":  cbCounts.ConsecutiveFailures,
		},
		"timestamp": time.Now().UTC(),
	}

	s.sendSuccessResponse(w, ctx, "Metrics retrieved", metrics)
}

func (s *ConsumerService) getCircuitBreakerStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	state := s.orderHandler.GetCircuitBreakerState()
	counts := s.orderHandler.GetCircuitBreakerCounts()

	status := map[string]interface{}{
		"state":                 state.String(),
		"counts":                counts,
		"timestamp":             time.Now().UTC(),
	}

	s.sendSuccessResponse(w, ctx, "Circuit breaker status", status)
}

func (s *ConsumerService) resetCircuitBreaker(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	// Note: This would require adding a reset method to the circuit breaker
	// For now, we'll just return a success response
	logger.Infof(ctx, "Circuit breaker reset requested")
	
	s.sendSuccessResponse(w, ctx, "Circuit breaker reset requested", map[string]interface{}{
		"timestamp": time.Now().UTC(),
	})
}

func (s *ConsumerService) healthCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s.sendSuccessResponse(w, ctx, "Service is healthy", map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"service":   "consumer",
	})
}

func (s *ConsumerService) readinessCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	// Check Redis connectivity
	_, err := s.redisClient.Get(ctx, "health_check")
	if err != nil && err.Error() != "redis: nil" {
		s.sendErrorResponse(w, ctx, http.StatusServiceUnavailable, "Redis not ready", err)
		return
	}

	s.sendSuccessResponse(w, ctx, "Service is ready", map[string]interface{}{
		"status":    "ready",
		"timestamp": time.Now().UTC(),
		"service":   "consumer",
	})
}

func (s *ConsumerService) sendSuccessResponse(w http.ResponseWriter, ctx context.Context, message string, data interface{}) {
	response := Response{
		Success:       true,
		Message:       message,
		Data:          data,
		CorrelationID: logger.GetCorrelationID(ctx),
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (s *ConsumerService) sendErrorResponse(w http.ResponseWriter, ctx context.Context, statusCode int, message string, err error) {
	logger.Errorf(ctx, "%s: %v", message, err)
	
	response := Response{
		Success:       false,
		Message:       message,
		CorrelationID: logger.GetCorrelationID(ctx),
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}

