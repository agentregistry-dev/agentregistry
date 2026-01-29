package logging

import (
	"context"

	"go.uber.org/zap"
)

type requestIDKeyType struct{}
type shouldLogKeyType struct{}

var requestIDKey = requestIDKeyType{}
var shouldLogKey = shouldLogKeyType{}

// Base loggers for each layer
var (
	SystemLog  = newBaseLogger("system")
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
// This is a helper used internally by Log().
// For application code, prefer using Log() which handles sampling automatically.
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

// SetShouldLog stores the sampling decision in context (all layers check this).
func SetShouldLog(ctx context.Context, shouldLog bool) context.Context {
	return context.WithValue(ctx, shouldLogKey, shouldLog)
}

// ShouldLog retrieves the sampling decision from context.
// Returns true if not set (default to logging for safety).
func ShouldLog(ctx context.Context) bool {
	if shouldLog, ok := ctx.Value(shouldLogKey).(bool); ok {
		return shouldLog
	}
	return true // Default to logging if not set
}
