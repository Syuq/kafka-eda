package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-kafka-eda-demo/internal/kafka"
	"go-kafka-eda-demo/internal/models"
	"go-kafka-eda-demo/pkg/config"
	"go-kafka-eda-demo/pkg/logger"

	"github.com/gorilla/mux"
)

type ProducerService struct {
	kafkaClient *kafka.Client
	config      *config.Config
}

type OrderRequest struct {
	CustomerID string  `json:"customer_id"`
	ProductID  string  `json:"product_id"`
	Quantity   int     `json:"quantity"`
	Price      float64 `json:"price"`
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
	logger.Info(ctx, "Starting producer service...")

	// Initialize Kafka client
	kafkaClient, err := kafka.NewClient(&cfg.Kafka)
	if err != nil {
		logger.Fatalf(ctx, "Failed to create Kafka client: %v", err)
	}
	defer kafkaClient.Close()

	// Create producer service
	service := &ProducerService{
		kafkaClient: kafkaClient,
		config:      cfg,
	}

	// Setup HTTP server
	router := mux.NewRouter()
	service.setupRoutes(router)

	server := &http.Server{
		Addr:         ":" + cfg.App.HTTPPort,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		logger.Infof(ctx, "Producer service listening on port %s", cfg.App.HTTPPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf(ctx, "Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info(ctx, "Shutting down producer service...")

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Errorf(ctx, "Server forced to shutdown: %v", err)
	}

	logger.Info(ctx, "Producer service stopped")
}

func (s *ProducerService) setupRoutes(router *mux.Router) {
	// Add correlation ID middleware
	router.Use(s.correlationIDMiddleware)
	router.Use(s.loggingMiddleware)

	// API routes
	api := router.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/orders", s.createOrder).Methods("POST")
	api.HandleFunc("/orders/batch", s.createOrdersBatch).Methods("POST")

	// Health check
	router.HandleFunc("/health", s.healthCheck).Methods("GET")
	router.HandleFunc("/ready", s.readinessCheck).Methods("GET")
}

func (s *ProducerService) correlationIDMiddleware(next http.Handler) http.Handler {
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

func (s *ProducerService) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		logger.Infof(r.Context(), "Request started: %s %s", r.Method, r.URL.Path)

		next.ServeHTTP(w, r)

		duration := time.Since(start)
		logger.Infof(r.Context(), "Request completed: %s %s in %v", r.Method, r.URL.Path, duration)
	})
}

func (s *ProducerService) createOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var orderReq OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&orderReq); err != nil {
		s.sendErrorResponse(w, ctx, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// Validate request
	if err := s.validateOrderRequest(&orderReq); err != nil {
		s.sendErrorResponse(w, ctx, http.StatusBadRequest, "Validation failed", err)
		return
	}

	// Create order
	order := &models.Order{
		ID:         generateOrderID(),
		CustomerID: orderReq.CustomerID,
		ProductID:  orderReq.ProductID,
		Quantity:   orderReq.Quantity,
		Price:      orderReq.Price,
		Status:     models.OrderStatusPending,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	// Create order event
	correlationID := logger.GetCorrelationID(ctx)
	eventData := map[string]interface{}{
		"order": order,
	}

	event := models.NewOrderEvent(
		models.EventTypeOrderCreated,
		order.ID,
		correlationID,
		eventData,
	)

	// Send to Kafka
	eventBytes, err := event.ToJSON()
	if err != nil {
		s.sendErrorResponse(w, ctx, http.StatusInternalServerError, "Failed to serialize event", err)
		return
	}

	err = s.kafkaClient.SendMessage(ctx, s.config.Kafka.TopicOrders, []byte(order.ID), eventBytes)
	if err != nil {
		s.sendErrorResponse(w, ctx, http.StatusInternalServerError, "Failed to send message", err)
		return
	}

	logger.Infof(ctx, "Order created successfully: %s", order.ID)

	s.sendSuccessResponse(w, ctx, "Order created successfully", map[string]interface{}{
		"order_id": order.ID,
		"event_id": event.EventID,
	})
}

func (s *ProducerService) createOrdersBatch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var orderReqs []OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&orderReqs); err != nil {
		s.sendErrorResponse(w, ctx, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if len(orderReqs) == 0 {
		s.sendErrorResponse(w, ctx, http.StatusBadRequest, "Empty batch", nil)
		return
	}

	if len(orderReqs) > 100 {
		s.sendErrorResponse(w, ctx, http.StatusBadRequest, "Batch too large (max 100)", nil)
		return
	}

	var results []map[string]interface{}
	var errors []string

	for i, orderReq := range orderReqs {
		// Validate request
		if err := s.validateOrderRequest(&orderReq); err != nil {
			errors = append(errors, fmt.Sprintf("Order %d: %v", i, err))
			continue
		}

		// Create order
		order := &models.Order{
			ID:         generateOrderID(),
			CustomerID: orderReq.CustomerID,
			ProductID:  orderReq.ProductID,
			Quantity:   orderReq.Quantity,
			Price:      orderReq.Price,
			Status:     models.OrderStatusPending,
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}

		// Create order event
		correlationID := logger.GetCorrelationID(ctx)
		eventData := map[string]interface{}{
			"order": order,
		}

		event := models.NewOrderEvent(
			models.EventTypeOrderCreated,
			order.ID,
			correlationID,
			eventData,
		)

		// Send to Kafka
		eventBytes, err := event.ToJSON()
		if err != nil {
			errors = append(errors, fmt.Sprintf("Order %d: Failed to serialize event: %v", i, err))
			continue
		}

		err = s.kafkaClient.SendMessage(ctx, s.config.Kafka.TopicOrders, []byte(order.ID), eventBytes)
		if err != nil {
			errors = append(errors, fmt.Sprintf("Order %d: Failed to send message: %v", i, err))
			continue
		}

		results = append(results, map[string]interface{}{
			"order_id": order.ID,
			"event_id": event.EventID,
		})
	}

	response := map[string]interface{}{
		"processed": len(results),
		"failed":    len(errors),
		"results":   results,
	}

	if len(errors) > 0 {
		response["errors"] = errors
	}

	logger.Infof(ctx, "Batch processed: %d successful, %d failed", len(results), len(errors))

	s.sendSuccessResponse(w, ctx, "Batch processed", response)
}

func (s *ProducerService) healthCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s.sendSuccessResponse(w, ctx, "Service is healthy", map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"service":   "producer",
	})
}

func (s *ProducerService) readinessCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Here you could add checks for Kafka connectivity, etc.
	s.sendSuccessResponse(w, ctx, "Service is ready", map[string]interface{}{
		"status":    "ready",
		"timestamp": time.Now().UTC(),
		"service":   "producer",
	})
}

func (s *ProducerService) validateOrderRequest(req *OrderRequest) error {
	if req.CustomerID == "" {
		return fmt.Errorf("customer_id is required")
	}
	if req.ProductID == "" {
		return fmt.Errorf("product_id is required")
	}
	if req.Quantity <= 0 {
		return fmt.Errorf("quantity must be positive")
	}
	if req.Price <= 0 {
		return fmt.Errorf("price must be positive")
	}
	return nil
}

func (s *ProducerService) sendSuccessResponse(w http.ResponseWriter, ctx context.Context, message string, data interface{}) {
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

func (s *ProducerService) sendErrorResponse(w http.ResponseWriter, ctx context.Context, statusCode int, message string, err error) {
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

func generateOrderID() string {
	return fmt.Sprintf("order_%d_%d", time.Now().Unix(), time.Now().Nanosecond())
}
