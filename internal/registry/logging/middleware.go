package logging

import (
	"net/http"

	"go.uber.org/zap"
)

type ResponseRecorder struct {
	http.ResponseWriter
	StatusCode int
	written    bool
}

func NewResponseRecorder(w http.ResponseWriter) *ResponseRecorder {
	return &ResponseRecorder{ResponseWriter: w, StatusCode: http.StatusOK}
}

func (r *ResponseRecorder) WriteHeader(code int) {
	if !r.written {
		r.StatusCode = code
		r.written = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *ResponseRecorder) Write(b []byte) (int, error) {
	if !r.written {
		r.written = true
	}
	return r.ResponseWriter.Write(b)
}

func EventLoggingMiddleware(cfg *EventLoggingConfig) func(http.Handler) http.Handler {
	parsedCfg := parseEventLoggingConfig(cfg)
	excludeSet := parsedCfg.excludePaths

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			if excludeSet[path] {
				next.ServeHTTP(w, r)
				return
			}

			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = r.Header.Get("X-Correlation-ID")
			}

			var logger *EventLogger
			if requestID != "" {
				logger = NewEventLoggerWithID("api", path, requestID, cfg)
			} else {
				logger = NewEventLogger("api", path, cfg)
			}

			logger.AddFields(
				zap.String("method", r.Method),
				zap.String("user_agent", r.UserAgent()),
				zap.String("remote_addr", r.RemoteAddr),
			)

			w.Header().Set("X-Request-ID", logger.RequestID())

			// Create outcome holder for handler to populate
			outcomeHolder := &EventOutcomeHolder{}
			ctx := ContextWithEventLogger(r.Context(), logger)
			ctx = ContextWithEventOutcomeHolder(ctx, outcomeHolder)
			recorder := NewResponseRecorder(w)

			defer func() {
				if rec := recover(); rec != nil {
					logger.AddFields(zap.Any("panic", rec))
					logger.Finalize(EventOutcome{
						Level:      zap.ErrorLevel,
						StatusCode: http.StatusInternalServerError,
						Message:    "request panicked",
					})
					panic(rec)
				}
			}()

			next.ServeHTTP(recorder, r.WithContext(ctx))

			if outcomeHolder.Outcome != nil {
				outcomeHolder.Outcome.StatusCode = recorder.StatusCode
				logger.Finalize(*outcomeHolder.Outcome)
			} else {
				logger.Finalize(EventOutcome{
					Level:      EventLevelFromStatusCode(recorder.StatusCode),
					StatusCode: recorder.StatusCode,
					Message:    "request completed",
				})
			}
		})
	}
}
