package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"go-kafka-eda-demo/internal/kafka"
	"go-kafka-eda-demo/internal/models"
	"go-kafka-eda-demo/internal/redis"
	"go-kafka-eda-demo/internal/telemetry"
	"go-kafka-eda-demo/pkg/config"
	"go-kafka-eda-demo/pkg/logger"

	"github.com/gorilla/mux"
)

type ReplayService struct {
	kafkaClient   *kafka.Client
	redisClient   *redis.Client
	config        *config.Config
	shutdownTrace func()
}

type ReplayRequest struct {
	EventIDs    []string `json:"event_ids,omitempty"`
	StartTime   string   `json:"start_time,omitempty"`
	EndTime     string   `json:"end_time,omitempty"`
	MaxMessages int      `json:"max_messages,omitempty"`
	DryRun      bool     `json:"dry_run,omitempty"`
}

type ReplayResponse struct {
	Success       bool        `json:"success"`
	Message       string      `json:"message"`
	Data          interface{} `json:"data,omitempty"`
	CorrelationID string      `json:"correlation_id"`
}

type ReplayResult struct {
	TotalMessages    int      `json:"total_messages"`
	ReplayedMessages int      `json:"replayed_messages"`
	FailedMessages   int      `json:"failed_messages"`
	SkippedMessages  int      `json:"skipped_messages"`
	ReplayedEventIDs []string `json:"replayed_event_ids"`
	FailedEventIDs   []string `json:"failed_event_ids"`
	SkippedEventIDs  []string `json:"skipped_event_ids"`
	Duration         string   `json:"duration"`
}

func main() {
	// Load configuration
	cfg := config.Load()
	logger.SetLevel(cfg.App.LogLevel)

	ctx := context.Background()
	logger.Info(ctx, "Starting replay service...")

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

	// Create replay service
	service := &ReplayService{
		kafkaClient:   kafkaClient,
		redisClient:   redisClient,
		config:        cfg,
		shutdownTrace: shutdownTrace,
	}

	// Setup HTTP server
	router := mux.NewRouter()
	service.setupRoutes(router)

	port, err := strconv.Atoi(cfg.App.HTTPPort)
	if err != nil {
		logger.Fatalf(ctx, "Invalid HTTP port: %v", err)
	}

	server := &http.Server{
		Addr:         ":" + strconv.Itoa(port+2),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		logger.Infof(ctx, "Replay service listening on port %s", strconv.Itoa(port+2))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf(ctx, "Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info(ctx, "Shutting down replay service...")

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Errorf(ctx, "Server forced to shutdown: %v", err)
	}

	logger.Info(ctx, "Replay service stopped")
}

func (s *ReplayService) setupRoutes(router *mux.Router) {
	// Add correlation ID middleware
	router.Use(s.correlationIDMiddleware)
	router.Use(s.loggingMiddleware)

	// API routes
	api := router.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/replay", s.replayMessages).Methods("POST")
	api.HandleFunc("/replay/batch", s.replayBatch).Methods("POST")
	api.HandleFunc("/dlq/messages", s.listDLQMessages).Methods("GET")
	api.HandleFunc("/dlq/stats", s.getDLQStats).Methods("GET")
	api.HandleFunc("/dlq/clear", s.clearDLQ).Methods("DELETE")

	// Health check
	router.HandleFunc("/health", s.healthCheck).Methods("GET")
	router.HandleFunc("/ready", s.readinessCheck).Methods("GET")
}

func (s *ReplayService) correlationIDMiddleware(next http.Handler) http.Handler {
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

func (s *ReplayService) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		logger.Infof(r.Context(), "Request started: %s %s", r.Method, r.URL.Path)

		next.ServeHTTP(w, r)

		duration := time.Since(start)
		logger.Infof(r.Context(), "Request completed: %s %s in %v", r.Method, r.URL.Path, duration)
	})
}

func (s *ReplayService) replayMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var replayReq ReplayRequest
	if err := json.NewDecoder(r.Body).Decode(&replayReq); err != nil {
		s.sendErrorResponse(w, ctx, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// Set defaults
	if replayReq.MaxMessages == 0 {
		replayReq.MaxMessages = 100
	}

	result, err := s.performReplay(ctx, &replayReq)
	if err != nil {
		s.sendErrorResponse(w, ctx, http.StatusInternalServerError, "Replay failed", err)
		return
	}

	message := "Replay completed successfully"
	if replayReq.DryRun {
		message = "Dry run completed successfully"
	}

	s.sendSuccessResponse(w, ctx, message, result)
}

func (s *ReplayService) replayBatch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var replayReq ReplayRequest
	if err := json.NewDecoder(r.Body).Decode(&replayReq); err != nil {
		s.sendErrorResponse(w, ctx, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// Set defaults for batch processing
	if replayReq.MaxMessages == 0 {
		replayReq.MaxMessages = 1000
	}

	result, err := s.performBatchReplay(ctx, &replayReq)
	if err != nil {
		s.sendErrorResponse(w, ctx, http.StatusInternalServerError, "Batch replay failed", err)
		return
	}

	message := "Batch replay completed successfully"
	if replayReq.DryRun {
		message = "Batch dry run completed successfully"
	}

	s.sendSuccessResponse(w, ctx, message, result)
}

func (s *ReplayService) listDLQMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	offsetStr := r.URL.Query().Get("offset")
	offset := 0
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	messages, err := s.fetchDLQMessages(ctx, limit, offset)
	if err != nil {
		s.sendErrorResponse(w, ctx, http.StatusInternalServerError, "Failed to fetch DLQ messages", err)
		return
	}

	s.sendSuccessResponse(w, ctx, "DLQ messages retrieved", map[string]interface{}{
		"messages": messages,
		"limit":    limit,
		"offset":   offset,
		"count":    len(messages),
	})
}

func (s *ReplayService) getDLQStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	stats, err := s.calculateDLQStats(ctx)
	if err != nil {
		s.sendErrorResponse(w, ctx, http.StatusInternalServerError, "Failed to get DLQ stats", err)
		return
	}

	s.sendSuccessResponse(w, ctx, "DLQ stats retrieved", stats)
}

func (s *ReplayService) clearDLQ(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// This is a dangerous operation, so we require confirmation
	confirm := r.URL.Query().Get("confirm")
	if confirm != "true" {
		s.sendErrorResponse(w, ctx, http.StatusBadRequest, "Confirmation required",
			fmt.Errorf("add ?confirm=true to confirm DLQ clearing"))
		return
	}

	count, err := s.clearDLQMessages(ctx)
	if err != nil {
		s.sendErrorResponse(w, ctx, http.StatusInternalServerError, "Failed to clear DLQ", err)
		return
	}

	s.sendSuccessResponse(w, ctx, "DLQ cleared successfully", map[string]interface{}{
		"cleared_messages": count,
		"timestamp":        time.Now().UTC(),
	})
}

func (s *ReplayService) performReplay(ctx context.Context, req *ReplayRequest) (*ReplayResult, error) {
	ctx, span := telemetry.StartSpan(ctx, "perform_replay")
	defer span.End()

	start := time.Now()
	result := &ReplayResult{
		ReplayedEventIDs: make([]string, 0),
		FailedEventIDs:   make([]string, 0),
		SkippedEventIDs:  make([]string, 0),
	}

	logger.Infof(ctx, "Starting replay operation (dry_run: %v)", req.DryRun)

	// Fetch messages from DLQ
	messages, err := s.fetchDLQMessagesForReplay(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch DLQ messages: %w", err)
	}

	result.TotalMessages = len(messages)
	logger.Infof(ctx, "Found %d messages to replay", result.TotalMessages)

	// Process each message
	for _, dlqEvent := range messages {
		if req.DryRun {
			result.ReplayedEventIDs = append(result.ReplayedEventIDs, dlqEvent.OriginalEvent.EventID)
			result.ReplayedMessages++
			continue
		}

		err := s.replayMessage(ctx, &dlqEvent.OriginalEvent)
		if err != nil {
			logger.Errorf(ctx, "Failed to replay message %s: %v", dlqEvent.OriginalEvent.EventID, err)
			result.FailedEventIDs = append(result.FailedEventIDs, dlqEvent.OriginalEvent.EventID)
			result.FailedMessages++
		} else {
			logger.Infof(ctx, "Successfully replayed message %s", dlqEvent.OriginalEvent.EventID)
			result.ReplayedEventIDs = append(result.ReplayedEventIDs, dlqEvent.OriginalEvent.EventID)
			result.ReplayedMessages++
		}
	}

	result.Duration = time.Since(start).String()
	logger.Infof(ctx, "Replay completed: %d replayed, %d failed, %d skipped in %s",
		result.ReplayedMessages, result.FailedMessages, result.SkippedMessages, result.Duration)

	return result, nil
}

func (s *ReplayService) performBatchReplay(ctx context.Context, req *ReplayRequest) (*ReplayResult, error) {
	ctx, span := telemetry.StartSpan(ctx, "perform_batch_replay")
	defer span.End()

	start := time.Now()
	result := &ReplayResult{
		ReplayedEventIDs: make([]string, 0),
		FailedEventIDs:   make([]string, 0),
		SkippedEventIDs:  make([]string, 0),
	}

	logger.Infof(ctx, "Starting batch replay operation (dry_run: %v)", req.DryRun)

	// Fetch messages from DLQ in batches
	batchSize := 50
	offset := 0

	for {
		messages, err := s.fetchDLQMessagesBatch(ctx, batchSize, offset)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch DLQ messages batch: %w", err)
		}

		if len(messages) == 0 {
			break
		}

		result.TotalMessages += len(messages)

		// Process batch
		for _, dlqEvent := range messages {
			if result.TotalMessages >= req.MaxMessages {
				break
			}

			if req.DryRun {
				result.ReplayedEventIDs = append(result.ReplayedEventIDs, dlqEvent.OriginalEvent.EventID)
				result.ReplayedMessages++
				continue
			}

			err := s.replayMessage(ctx, &dlqEvent.OriginalEvent)
			if err != nil {
				logger.Errorf(ctx, "Failed to replay message %s: %v", dlqEvent.OriginalEvent.EventID, err)
				result.FailedEventIDs = append(result.FailedEventIDs, dlqEvent.OriginalEvent.EventID)
				result.FailedMessages++
			} else {
				logger.Debugf(ctx, "Successfully replayed message %s", dlqEvent.OriginalEvent.EventID)
				result.ReplayedEventIDs = append(result.ReplayedEventIDs, dlqEvent.OriginalEvent.EventID)
				result.ReplayedMessages++
			}
		}

		if result.TotalMessages >= req.MaxMessages {
			break
		}

		offset += batchSize
	}

	result.Duration = time.Since(start).String()
	logger.Infof(ctx, "Batch replay completed: %d replayed, %d failed, %d skipped in %s",
		result.ReplayedMessages, result.FailedMessages, result.SkippedMessages, result.Duration)

	return result, nil
}

func (s *ReplayService) replayMessage(ctx context.Context, event *models.OrderEvent) error {
	ctx, span := telemetry.StartSpan(ctx, "replay_message")
	defer span.End()

	// Clear idempotency record to allow reprocessing
	if err := s.redisClient.DeleteIdempotency(ctx, event.EventID); err != nil {
		logger.Warnf(ctx, "Failed to clear idempotency record for event %s: %v", event.EventID, err)
	}

	// Clear retry count
	retryCountKey := fmt.Sprintf("retry_count:%s", event.EventID)
	if err := s.redisClient.Delete(ctx, retryCountKey); err != nil {
		logger.Warnf(ctx, "Failed to clear retry count for event %s: %v", event.EventID, err)
	}

	// Send message back to main topic
	eventBytes, err := event.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize event: %w", err)
	}

	headers := map[string]string{
		"replayed":    "true",
		"replay_time": time.Now().UTC().Format(time.RFC3339),
	}

	err = s.kafkaClient.SendMessageWithHeaders(ctx, s.config.Kafka.TopicOrders,
		[]byte(event.AggregateID), eventBytes, headers)
	if err != nil {
		return fmt.Errorf("failed to send message to orders topic: %w", err)
	}

	return nil
}

func (s *ReplayService) fetchDLQMessages(ctx context.Context, limit, offset int) ([]models.DLQEvent, error) {
	// This is a simplified implementation
	// In a real scenario, you'd use Kafka consumer to read from DLQ topic
	return []models.DLQEvent{}, nil
}

func (s *ReplayService) fetchDLQMessagesForReplay(ctx context.Context, req *ReplayRequest) ([]models.DLQEvent, error) {
	// This is a simplified implementation
	// In a real scenario, you'd filter messages based on the request criteria
	return s.fetchDLQMessages(ctx, req.MaxMessages, 0)
}

func (s *ReplayService) fetchDLQMessagesBatch(ctx context.Context, batchSize, offset int) ([]models.DLQEvent, error) {
	return s.fetchDLQMessages(ctx, batchSize, offset)
}

func (s *ReplayService) calculateDLQStats(ctx context.Context) (map[string]interface{}, error) {
	// This is a simplified implementation
	// In a real scenario, you'd calculate actual stats from the DLQ topic
	return map[string]interface{}{
		"total_messages":   0,
		"oldest_message":   nil,
		"newest_message":   nil,
		"event_types":      map[string]int{},
		"error_categories": map[string]int{},
		"timestamp":        time.Now().UTC(),
	}, nil
}

func (s *ReplayService) clearDLQMessages(ctx context.Context) (int, error) {
	// This is a simplified implementation
	// In a real scenario, you'd actually clear the DLQ topic
	logger.Warnf(ctx, "DLQ clear operation requested - this is a destructive operation")
	return 0, nil
}

func (s *ReplayService) healthCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s.sendSuccessResponse(w, ctx, "Service is healthy", map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"service":   "replay",
	})
}

func (s *ReplayService) readinessCheck(w http.ResponseWriter, r *http.Request) {
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
		"service":   "replay",
	})
}

func (s *ReplayService) sendSuccessResponse(w http.ResponseWriter, ctx context.Context, message string, data interface{}) {
	response := ReplayResponse{
		Success:       true,
		Message:       message,
		Data:          data,
		CorrelationID: logger.GetCorrelationID(ctx),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (s *ReplayService) sendErrorResponse(w http.ResponseWriter, ctx context.Context, statusCode int, message string, err error) {
	logger.Errorf(ctx, "%s: %v", message, err)

	response := ReplayResponse{
		Success:       false,
		Message:       message,
		CorrelationID: logger.GetCorrelationID(ctx),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}
