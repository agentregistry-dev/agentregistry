package logging

import (
	"context"
	"hash/fnv"
	"regexp"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// EventLoggingConfig holds sampling and filtering configuration.
type EventLoggingConfig struct {
	SuccessSampleRate float64 `env:"LOG_SUCCESS_SAMPLE_RATE" envDefault:"0.1"`
	ExcludePaths      string  `env:"LOG_EXCLUDE_PATHS" envDefault:"/health,/metrics,/ping"`
	ErrorOnlyPaths    string  `env:"LOG_ERROR_ONLY_PATHS" envDefault:"/healthz"`
	RedactPatterns    string  `env:"LOG_REDACT_PATTERNS" envDefault:"password,token,secret,key,authorization,credential,bearer,api_key,apikey,private"`
}

// ParsedEventLoggingConfig is the parsed version of EventLoggingConfig for efficient use.
type ParsedEventLoggingConfig struct {
	SuccessSampleRate float64
	ExcludePaths      map[string]bool
	ErrorOnlyPaths    map[string]bool
	RedactRegex       *regexp.Regexp
}

// ParseEventLoggingConfig parses the config into an efficient structure.
func ParseEventLoggingConfig(cfg *EventLoggingConfig) *ParsedEventLoggingConfig {
	parsed := &ParsedEventLoggingConfig{
		SuccessSampleRate: cfg.SuccessSampleRate,
		ExcludePaths:      make(map[string]bool),
		ErrorOnlyPaths:    make(map[string]bool),
	}

	for _, p := range strings.Split(cfg.ExcludePaths, ",") {
		if p = strings.TrimSpace(p); p != "" {
			parsed.ExcludePaths[p] = true
		}
	}

	for _, p := range strings.Split(cfg.ErrorOnlyPaths, ",") {
		if p = strings.TrimSpace(p); p != "" {
			parsed.ErrorOnlyPaths[p] = true
		}
	}

	var regexParts []string
	for _, p := range strings.Split(cfg.RedactPatterns, ",") {
		if p = strings.TrimSpace(p); p != "" {
			regexParts = append(regexParts, regexp.QuoteMeta(p))
		}
	}
	if len(regexParts) > 0 {
		parsed.RedactRegex = regexp.MustCompile("(?i)(" + strings.Join(regexParts, "|") + ")")
	}

	return parsed
}

func DefaultEventLoggingConfig() *EventLoggingConfig {
	return &EventLoggingConfig{
		SuccessSampleRate: 0.1,
		ExcludePaths:      "/health,/metrics,/ping",
		ErrorOnlyPaths:    "/healthz",
		RedactPatterns:    "password,token,secret,key,authorization,credential,bearer,api_key,apikey,private",
	}
}

// Base event loggers for each layer (reused across all requests, thread-safe)
var (
	APIEventLog = newBaseEventLogger("api")
)

func newBaseEventLogger(layer string) *zap.Logger {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	return logger.Named(layer)
}

// Global config for redaction (can be set via DefaultEventLoggingConfig if needed)
var globalRedactRegex *regexp.Regexp

func init() {
	cfg := DefaultEventLoggingConfig()
	parsed := ParseEventLoggingConfig(cfg)
	globalRedactRegex = parsed.RedactRegex
}

const redactedValue = "***"

// RedactFields redacts sensitive fields based on configured patterns.
func RedactFields(fields ...zap.Field) []zap.Field {
	if globalRedactRegex == nil {
		return fields
	}
	redacted := make([]zap.Field, len(fields))
	for i, f := range fields {
		if globalRedactRegex.MatchString(f.Key) {
			redacted[i] = zap.String(f.Key, redactedValue)
		} else {
			redacted[i] = f
		}
	}
	return redacted
}

// shouldLogForLevel determines if we should log based on sampling decision and log level.
// Errors and warnings are always logged regardless of sampling.
func shouldLogForLevel(ctx context.Context, level zapcore.Level) bool {
	// Always log errors and warnings
	if level >= zapcore.WarnLevel {
		return true
	}
	// For info/debug, check sampling decision
	return ShouldLog(ctx)
}

// Log logs an event using the logger from context with tail-based sampling.
// Errors and warnings are always logged; Info/Debug are sampled based on request_id.
// Usage:
//
//	logging.Log(ctx, logging.HandlerLog, zapcore.InfoLevel, "message", fields...)
//	logging.Log(ctx, logging.HandlerLog, zapcore.ErrorLevel, "error", zap.Error(err))
//	logging.Log(ctx, logging.ServiceLog, zapcore.InfoLevel, "completed", zap.Duration("duration", duration))
func Log(ctx context.Context, base *zap.Logger, level zapcore.Level, message string, fields ...zap.Field) {
	if !shouldLogForLevel(ctx, level) {
		return
	}

	logger := L(ctx, base)
	allFields := RedactFields(fields...)

	// Use zap's Log method directly - no need for switch statement
	logger.Log(level, message, allFields...)
}

// HashRequestIDToFloat returns a deterministic float between 0 and 1 based on request ID.
// This is used for tail-based sampling - same request_id always gets same hash value.
func HashRequestIDToFloat(requestID string) float64 {
	h := fnv.New64a()
	h.Write([]byte(requestID))
	return float64(h.Sum64()) / float64(^uint64(0))
}

func EventLevelFromStatusCode(statusCode int) zapcore.Level {
	switch {
	case statusCode >= 500:
		return zapcore.ErrorLevel
	case statusCode >= 400:
		return zapcore.WarnLevel
	default:
		return zapcore.InfoLevel
	}
}
