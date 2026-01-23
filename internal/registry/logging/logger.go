package logging

import (
	"context"

	"go.uber.org/zap"
)

type requestIDKeyType struct{}

var requestIDKey = requestIDKeyType{}

// Base loggers for each layer
var (
	HandlerLog = newBaseLogger("handler")
	ServiceLog = newBaseLogger("service")
	DBLog      = newBaseLogger("db")
)

func newBaseLogger(name string) *zap.Logger {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	return logger.Named(name)
}

// NewLogger creates a named zap production logger (use sparingly, prefer base loggers).
func NewLogger(name string) *zap.Logger {
	return newBaseLogger(name)
}

// L returns a logger with request_id from context.
// Usage: logging.L(ctx, logging.HandlerLog).Info("something", zap.Any("data", data))
func L(ctx context.Context, base *zap.Logger) *zap.Logger {
	if reqID := GetRequestID(ctx); reqID != "" {
		return base.With(zap.String("request_id", reqID))
	}
	return base
}

// SetRequestID stores request_id in context (call once in middleware).
func SetRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// GetRequestID retrieves request_id from context.
func GetRequestID(ctx context.Context) string {
	if reqID, ok := ctx.Value(requestIDKey).(string); ok {
		return reqID
	}
	return ""
}
