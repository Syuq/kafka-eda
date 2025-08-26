package logger

import (
	"context"
	"os"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

type contextKey string

const CorrelationIDKey contextKey = "correlation_id"

var log *logrus.Logger

func init() {
	log = logrus.New()
	log.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
	})
	log.SetOutput(os.Stdout)
}

func SetLevel(level string) {
	switch level {
	case "debug":
		log.SetLevel(logrus.DebugLevel)
	case "info":
		log.SetLevel(logrus.InfoLevel)
	case "warn":
		log.SetLevel(logrus.WarnLevel)
	case "error":
		log.SetLevel(logrus.ErrorLevel)
	default:
		log.SetLevel(logrus.InfoLevel)
	}
}

func WithCorrelationID(ctx context.Context) *logrus.Entry {
	correlationID := GetCorrelationID(ctx)
	return log.WithField("correlation_id", correlationID)
}

func GetCorrelationID(ctx context.Context) string {
	if ctx == nil {
		return generateCorrelationID()
	}
	
	if correlationID, ok := ctx.Value(CorrelationIDKey).(string); ok && correlationID != "" {
		return correlationID
	}
	
	return generateCorrelationID()
}

func SetCorrelationID(ctx context.Context, correlationID string) context.Context {
	if correlationID == "" {
		correlationID = generateCorrelationID()
	}
	return context.WithValue(ctx, CorrelationIDKey, correlationID)
}

func NewCorrelationID() string {
	return generateCorrelationID()
}

func generateCorrelationID() string {
	return uuid.New().String()
}

// Convenience methods
func Info(ctx context.Context, args ...interface{}) {
	WithCorrelationID(ctx).Info(args...)
}

func Infof(ctx context.Context, format string, args ...interface{}) {
	WithCorrelationID(ctx).Infof(format, args...)
}

func Debug(ctx context.Context, args ...interface{}) {
	WithCorrelationID(ctx).Debug(args...)
}

func Debugf(ctx context.Context, format string, args ...interface{}) {
	WithCorrelationID(ctx).Debugf(format, args...)
}

func Warn(ctx context.Context, args ...interface{}) {
	WithCorrelationID(ctx).Warn(args...)
}

func Warnf(ctx context.Context, format string, args ...interface{}) {
	WithCorrelationID(ctx).Warnf(format, args...)
}

func Error(ctx context.Context, args ...interface{}) {
	WithCorrelationID(ctx).Error(args...)
}

func Errorf(ctx context.Context, format string, args ...interface{}) {
	WithCorrelationID(ctx).Errorf(format, args...)
}

func Fatal(ctx context.Context, args ...interface{}) {
	WithCorrelationID(ctx).Fatal(args...)
}

func Fatalf(ctx context.Context, format string, args ...interface{}) {
	WithCorrelationID(ctx).Fatalf(format, args...)
}

func WithFields(ctx context.Context, fields logrus.Fields) *logrus.Entry {
	entry := WithCorrelationID(ctx)
	return entry.WithFields(fields)
}

