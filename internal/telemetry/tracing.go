package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go-kafka-eda-demo/pkg/config"
	"go-kafka-eda-demo/pkg/logger"
)

var tracer oteltrace.Tracer

func InitTracing(cfg *config.TelemetryConfig) (func(), error) {
	// Create Jaeger exporter
	exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(cfg.JaegerEndpoint)))
	if err != nil {
		return nil, fmt.Errorf("failed to create Jaeger exporter: %w", err)
	}

	// Create resource
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create trace provider
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exp),
		trace.WithResource(res),
		trace.WithSampler(trace.AlwaysSample()),
	)

	// Set global trace provider
	otel.SetTracerProvider(tp)

	// Set global propagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Get tracer
	tracer = otel.Tracer(cfg.ServiceName)

	// Return cleanup function
	return func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			logger.Errorf(context.Background(), "Failed to shutdown tracer provider: %v", err)
		}
	}, nil
}

func GetTracer() oteltrace.Tracer {
	return tracer
}

func StartSpan(ctx context.Context, name string, opts ...oteltrace.SpanStartOption) (context.Context, oteltrace.Span) {
	return tracer.Start(ctx, name, opts...)
}

func StartSpanFromContext(ctx context.Context, name string) (context.Context, oteltrace.Span) {
	return tracer.Start(ctx, name)
}

// Helper functions for common span operations
func SetSpanError(span oteltrace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(oteltrace.StatusCodeError, err.Error())
	}
}

func SetSpanSuccess(span oteltrace.Span) {
	span.SetStatus(oteltrace.StatusCodeOk, "")
}

func AddSpanEvent(span oteltrace.Span, name string, attributes ...oteltrace.EventOption) {
	span.AddEvent(name, attributes...)
}

func SetSpanAttributes(span oteltrace.Span, attributes ...oteltrace.SpanStartOption) {
	// Note: This is a simplified version. In practice, you'd use span.SetAttributes()
	// with attribute.KeyValue pairs
}

// Kafka-specific tracing helpers
func StartKafkaProducerSpan(ctx context.Context, topic string) (context.Context, oteltrace.Span) {
	return tracer.Start(ctx, fmt.Sprintf("kafka.produce.%s", topic),
		oteltrace.WithSpanKind(oteltrace.SpanKindProducer),
	)
}

func StartKafkaConsumerSpan(ctx context.Context, topic string) (context.Context, oteltrace.Span) {
	return tracer.Start(ctx, fmt.Sprintf("kafka.consume.%s", topic),
		oteltrace.WithSpanKind(oteltrace.SpanKindConsumer),
	)
}

// HTTP-specific tracing helpers
func StartHTTPServerSpan(ctx context.Context, method, path string) (context.Context, oteltrace.Span) {
	return tracer.Start(ctx, fmt.Sprintf("%s %s", method, path),
		oteltrace.WithSpanKind(oteltrace.SpanKindServer),
	)
}

func StartHTTPClientSpan(ctx context.Context, method, url string) (context.Context, oteltrace.Span) {
	return tracer.Start(ctx, fmt.Sprintf("%s %s", method, url),
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
	)
}

// Database-specific tracing helpers
func StartDatabaseSpan(ctx context.Context, operation, table string) (context.Context, oteltrace.Span) {
	return tracer.Start(ctx, fmt.Sprintf("db.%s.%s", operation, table),
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
	)
}

// Redis-specific tracing helpers
func StartRedisSpan(ctx context.Context, operation string) (context.Context, oteltrace.Span) {
	return tracer.Start(ctx, fmt.Sprintf("redis.%s", operation),
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
	)
}

