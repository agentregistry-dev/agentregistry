package logging

import (
	"context"

	"go.uber.org/zap"
)

type requestIDKeyType struct{}

var requestIDKey = requestIDKeyType{}

// NewLogger creates a named zap production logger.
func NewLogger(name string) *zap.Logger {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	return logger.Named(name)
}

// WithRequestID returns a logger with request_id from context.
func WithRequestID(ctx context.Context, logger *zap.Logger) *zap.Logger {
	if reqID, ok := ctx.Value(requestIDKey).(string); ok && reqID != "" {
		return logger.With(zap.String("request_id", reqID))
	}
	return logger
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
