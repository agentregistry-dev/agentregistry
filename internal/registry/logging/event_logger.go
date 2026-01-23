package logging

import (
	"context"
	"hash/fnv"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type eventLoggerKeyType struct{}
type outcomeHolderKeyType struct{}

var eventLoggerKey = eventLoggerKeyType{}
var outcomeHolderKey = outcomeHolderKeyType{}

func ContextWithEventLogger(ctx context.Context, logger *EventLogger) context.Context {
	return context.WithValue(ctx, eventLoggerKey, logger)
}

func EventLoggerFromContext(ctx context.Context) *EventLogger {
	if logger, ok := ctx.Value(eventLoggerKey).(*EventLogger); ok {
		return logger
	}
	return newNoOpEventLogger()
}

// OutcomeHolder is a mutable container for Outcome that handlers can update.
// This allows handlers to set custom error messages/levels that middleware will use.
type EventOutcomeHolder struct {
	Outcome *EventOutcome
}

func ContextWithEventOutcomeHolder(ctx context.Context, holder *EventOutcomeHolder) context.Context {
	return context.WithValue(ctx, outcomeHolderKey, holder)
}

// SetOutcome allows handlers to set a custom outcome (message, error, level).
// The middleware will use this when finalizing the log.
func SetEventOutcome(ctx context.Context, outcome EventOutcome) {
	if holder, ok := ctx.Value(outcomeHolderKey).(*EventOutcomeHolder); ok && holder != nil {
		holder.Outcome = &outcome
	}
}

// OutcomeFromContext retrieves the outcome from the holder (if set by handler).
func EventOutcomeFromContext(ctx context.Context) *EventOutcome {
	if holder, ok := ctx.Value(outcomeHolderKey).(*EventOutcomeHolder); ok && holder != nil {
		return holder.Outcome
	}
	return nil
}

// LoggingConfig holds sampling and filtering configuration.
type EventLoggingConfig struct {
	SuccessSampleRate float64 `env:"LOG_SUCCESS_SAMPLE_RATE" envDefault:"0.1"`
	ExcludePaths      string  `env:"LOG_EXCLUDE_PATHS" envDefault:"/health,/ready,/live,/metrics"`
	ErrorOnlyPaths    string  `env:"LOG_ERROR_ONLY_PATHS" envDefault:"/healthz,/livez"`
	RedactPatterns    string  `env:"LOG_REDACT_PATTERNS" envDefault:"password,token,secret,key,authorization,credential,bearer,api_key,apikey,private"`
}

type parsedEventLoggingConfig struct {
	successSampleRate float64
	excludePaths      map[string]bool
	errorOnlyPaths    map[string]bool
	redactRegex       *regexp.Regexp
}

func parseEventLoggingConfig(cfg *EventLoggingConfig) *parsedEventLoggingConfig {
	parsed := &parsedEventLoggingConfig{
		successSampleRate: cfg.SuccessSampleRate,
		excludePaths:      make(map[string]bool),
		errorOnlyPaths:    make(map[string]bool),
	}

	for _, p := range strings.Split(cfg.ExcludePaths, ",") {
		if p = strings.TrimSpace(p); p != "" {
			parsed.excludePaths[p] = true
		}
	}

	for _, p := range strings.Split(cfg.ErrorOnlyPaths, ",") {
		if p = strings.TrimSpace(p); p != "" {
			parsed.errorOnlyPaths[p] = true
		}
	}

	var regexParts []string
	for _, p := range strings.Split(cfg.RedactPatterns, ",") {
		if p = strings.TrimSpace(p); p != "" {
			regexParts = append(regexParts, regexp.QuoteMeta(p))
		}
	}
	if len(regexParts) > 0 {
		parsed.redactRegex = regexp.MustCompile("(?i)(" + strings.Join(regexParts, "|") + ")")
	}

	return parsed
}

func DefaultEventLoggingConfig() *EventLoggingConfig {
	return &EventLoggingConfig{
		SuccessSampleRate: 0.1,
		ExcludePaths:      "/health,/ready,/live,/metrics",
		ErrorOnlyPaths:    "/healthz,/livez",
		RedactPatterns:    "password,token,secret,key,authorization,credential,bearer,api_key,apikey,private",
	}
}

type EventOutcome struct {
	Level      zapcore.Level
	StatusCode int
	Error      error
	Message    string
}

// RequestLogger accumulates fields throughout a request lifecycle
// and emits a single "wide" log entry via Finalize().
// Fields can be added globally or under namespaces (e.g., "handler", "service", "db")
// for clear ownership in the final log output.
type EventLogger struct {
	baseLogger *zap.Logger
	config     *parsedEventLoggingConfig
	requestID  string
	path       string
	startTime  time.Time
	fields     []zap.Field            // top-level fields (request_id, path, etc.)
	namespaces map[string][]zap.Field // namespaced fields (handler.*, service.*, db.*)
	skipLog    bool
	errorOnly  bool
	finalized  bool
	noop       bool
}

func NewEventLogger(name string, path string, cfg *EventLoggingConfig) *EventLogger {
	baseLogger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}

	var parsedCfg *parsedEventLoggingConfig
	if cfg != nil {
		parsedCfg = parseEventLoggingConfig(cfg)
	} else {
		parsedCfg = parseEventLoggingConfig(DefaultEventLoggingConfig())
	}

	requestID := ulid.Make().String()

	return &EventLogger{
		baseLogger: baseLogger.Named(name),
		config:     parsedCfg,
		requestID:  requestID,
		path:       path,
		startTime:  time.Now(),
		fields:     []zap.Field{zap.String("request_id", requestID)},
		namespaces: make(map[string][]zap.Field),
	}
}

func NewEventLoggerWithID(name string, path string, requestID string, cfg *EventLoggingConfig) *EventLogger {
	logger := NewEventLogger(name, path, cfg)
	logger.requestID = requestID
	logger.fields[0] = zap.String("request_id", requestID)
	return logger
}

func newNoOpEventLogger() *EventLogger {
	return &EventLogger{noop: true, fields: []zap.Field{}, namespaces: make(map[string][]zap.Field)}
}

func (l *EventLogger) RequestID() string {
	return l.requestID
}

func (l *EventLogger) AddFields(fields ...zap.Field) {
	if l.noop {
		return
	}
	for _, f := range fields {
		l.fields = append(l.fields, l.redactField(f))
	}
}

// AddNamespacedFields adds fields under a specific namespace (e.g., "handler", "service", "db").
// In the final log output, these will appear as nested objects:
//
//	{"request_id": "abc", "handler": {"input": {...}}, "service": {"filter": {...}}, "db": {"duration_ms": 12}}
func (l *EventLogger) AddNamespacedFields(namespace string, fields ...zap.Field) {
	if l.noop {
		return
	}
	for _, f := range fields {
		l.namespaces[namespace] = append(l.namespaces[namespace], l.redactField(f))
	}
}

func (l *EventLogger) Skip() {
	l.skipLog = true
}

func (l *EventLogger) SetErrorOnly() {
	l.errorOnly = true
}

func (l *EventLogger) Finalize(outcome EventOutcome) {
	if l.noop || l.finalized {
		return
	}
	l.finalized = true

	if !l.shouldLog(outcome) {
		return
	}

	duration := time.Since(l.startTime)

	finalFields := make([]zap.Field, 0, len(l.fields)+len(l.namespaces)+4)
	finalFields = append(finalFields, l.fields...)

	// Add namespaced fields as nested objects
	for ns, nsFields := range l.namespaces {
		// Capture loop variables for closure
		fields := nsFields
		finalFields = append(finalFields, zap.Object(ns, zapcore.ObjectMarshalerFunc(func(enc zapcore.ObjectEncoder) error {
			for _, f := range fields {
				f.AddTo(enc)
			}
			return nil
		})))
	}

	finalFields = append(finalFields,
		zap.String("path", l.path),
		zap.Int("status_code", outcome.StatusCode),
		zap.Duration("duration", duration),
		zap.Int64("duration_ms", duration.Milliseconds()),
	)

	if outcome.Error != nil {
		finalFields = append(finalFields, zap.Error(outcome.Error))
	}

	msg := outcome.Message
	if msg == "" {
		msg = "request completed"
	}

	switch outcome.Level {
	case zapcore.DebugLevel:
		l.baseLogger.Debug(msg, finalFields...)
	case zapcore.InfoLevel:
		l.baseLogger.Info(msg, finalFields...)
	case zapcore.WarnLevel:
		l.baseLogger.Warn(msg, finalFields...)
	case zapcore.ErrorLevel:
		l.baseLogger.Error(msg, finalFields...)
	case zapcore.FatalLevel:
		l.baseLogger.Fatal(msg, finalFields...)
	default:
		l.baseLogger.Info(msg, finalFields...)
	}
}

func (l *EventLogger) shouldLog(outcome EventOutcome) bool {
	if l.config.excludePaths[l.path] {
		return false
	}

	if l.skipLog {
		return false
	}

	isError := outcome.Level >= zapcore.WarnLevel || outcome.Error != nil
	if isError {
		return true
	}

	if l.errorOnly || l.config.errorOnlyPaths[l.path] {
		return false
	}

	return l.hashToFloat() < l.config.successSampleRate
}

func (l *EventLogger) hashToFloat() float64 {
	h := fnv.New64a()
	h.Write([]byte(l.requestID))
	return float64(h.Sum64()) / float64(^uint64(0))
}

const redactedValue = "***"

func (l *EventLogger) redactField(f zap.Field) zap.Field {
	if l.config.redactRegex == nil {
		return f
	}
	if l.config.redactRegex.MatchString(f.Key) {
		return zap.String(f.Key, redactedValue)
	}
	return f
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
