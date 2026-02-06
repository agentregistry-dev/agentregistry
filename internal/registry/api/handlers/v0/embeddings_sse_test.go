package v0_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v0 "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0"
	"github.com/agentregistry-dev/agentregistry/internal/registry/jobs"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
)

// sseEvent represents a parsed SSE event.
type sseEvent struct {
	Type     string          `json:"type"`
	JobID    string          `json:"jobId,omitempty"`
	Resource string          `json:"resource,omitempty"`
	Stats    json.RawMessage `json:"stats,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// parseSSEEvents parses SSE events from a response body.
func parseSSEEvents(t *testing.T, body string) []sseEvent {
	t.Helper()
	var events []sseEvent
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if data, found := strings.CutPrefix(line, "data: "); found {
			var event sseEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				t.Logf("Failed to parse SSE event: %v", err)
				continue
			}
			events = append(events, event)
		}
	}
	return events
}

func TestSSEIndex_Success(t *testing.T) {
	mockIdx := &mockIndexer{
		runFunc: func(ctx context.Context, opts service.IndexOptions, onProgress service.IndexProgressCallback) (*service.IndexResult, error) {
			onProgress("servers", service.IndexStats{Processed: 1, Updated: 1})
			return &service.IndexResult{Servers: service.IndexStats{Processed: 1, Updated: 1}}, nil
		},
	}

	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	v0.RegisterEmbeddingsSSEHandler(mux, "/v0", mockIdx, jobManager)

	body := strings.NewReader(`{"includeServers": true}`)
	req := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index/stream", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))

	events := parseSSEEvents(t, w.Body.String())
	require.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, "started", events[0].Type)
	assert.Equal(t, "completed", events[len(events)-1].Type)
}

func TestSSEIndex_IndexerNil(t *testing.T) {
	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	v0.RegisterEmbeddingsSSEHandler(mux, "/v0", nil, jobManager)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index/stream", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "not configured")
}

func TestSSEIndex_InvalidJSON(t *testing.T) {
	mockIdx := &mockIndexer{}

	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	v0.RegisterEmbeddingsSSEHandler(mux, "/v0", mockIdx, jobManager)

	body := strings.NewReader(`{invalid json`)
	req := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index/stream", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid JSON")
}

func TestSSEIndex_JobAlreadyRunning(t *testing.T) {
	// Create a blocking indexer
	started := make(chan struct{})
	blockCh := make(chan struct{})
	mockIdx := &mockIndexer{
		runFunc: func(ctx context.Context, opts service.IndexOptions, onProgress service.IndexProgressCallback) (*service.IndexResult, error) {
			close(started)
			<-blockCh
			return &service.IndexResult{}, nil
		},
	}

	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	v0.RegisterEmbeddingsSSEHandler(mux, "/v0", mockIdx, jobManager)

	// Start first request in a goroutine
	go func() {
		body := strings.NewReader(`{}`)
		req := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index/stream", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
	}()

	// Wait for first job to start
	<-started

	// Try second request
	body2 := strings.NewReader(`{}`)
	req2 := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index/stream", body2)
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()

	mux.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusConflict, w2.Code)
	assert.Contains(t, w2.Body.String(), "already running")

	// Cleanup
	close(blockCh)
}

func TestSSEIndex_IndexerError(t *testing.T) {
	mockIdx := &mockIndexer{
		runFunc: func(ctx context.Context, opts service.IndexOptions, onProgress service.IndexProgressCallback) (*service.IndexResult, error) {
			return nil, errors.New("indexer failed")
		},
	}

	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	v0.RegisterEmbeddingsSSEHandler(mux, "/v0", mockIdx, jobManager)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index/stream", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	// SSE endpoint returns 200 and sends error as event
	assert.Equal(t, http.StatusOK, w.Code)

	events := parseSSEEvents(t, w.Body.String())
	require.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, "started", events[0].Type)

	// Find error event
	var foundError bool
	for _, e := range events {
		if e.Type == "error" {
			foundError = true
			assert.Contains(t, e.Error, "indexer failed")
		}
	}
	assert.True(t, foundError, "Expected to find an error event")
}

func TestSSEIndex_Headers(t *testing.T) {
	mockIdx := &mockIndexer{
		runFunc: func(ctx context.Context, opts service.IndexOptions, onProgress service.IndexProgressCallback) (*service.IndexResult, error) {
			return &service.IndexResult{}, nil
		},
	}

	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	v0.RegisterEmbeddingsSSEHandler(mux, "/v0", mockIdx, jobManager)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index/stream", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", w.Header().Get("Connection"))
	assert.Equal(t, "no", w.Header().Get("X-Accel-Buffering"))
}

func TestSSEIndex_ProgressEvents(t *testing.T) {
	mockIdx := &mockIndexer{
		runFunc: func(ctx context.Context, opts service.IndexOptions, onProgress service.IndexProgressCallback) (*service.IndexResult, error) {
			onProgress("servers", service.IndexStats{Processed: 50, Updated: 25, Skipped: 20, Failures: 5})
			onProgress("servers", service.IndexStats{Processed: 100, Updated: 50, Skipped: 40, Failures: 10})
			onProgress("agents", service.IndexStats{Processed: 30, Updated: 15, Skipped: 10, Failures: 5})
			return &service.IndexResult{
				Servers: service.IndexStats{Processed: 100, Updated: 50, Skipped: 40, Failures: 10},
				Agents:  service.IndexStats{Processed: 30, Updated: 15, Skipped: 10, Failures: 5},
			}, nil
		},
	}

	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	v0.RegisterEmbeddingsSSEHandler(mux, "/v0", mockIdx, jobManager)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index/stream", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	events := parseSSEEvents(t, w.Body.String())

	// Count event types
	var startedCount, progressCount, completedCount int
	for _, e := range events {
		switch e.Type {
		case "started":
			startedCount++
		case "progress":
			progressCount++
		case "completed":
			completedCount++
		}
	}

	assert.Equal(t, 1, startedCount)
	assert.Equal(t, 3, progressCount)
	assert.Equal(t, 1, completedCount)
}

func TestSSEIndex_DefaultsApplied(t *testing.T) {
	var capturedOpts service.IndexOptions
	mockIdx := &mockIndexer{
		runFunc: func(ctx context.Context, opts service.IndexOptions, onProgress service.IndexProgressCallback) (*service.IndexResult, error) {
			capturedOpts = opts
			return &service.IndexResult{}, nil
		},
	}

	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	v0.RegisterEmbeddingsSSEHandler(mux, "/v0", mockIdx, jobManager)

	// Empty body - should apply defaults
	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index/stream", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, capturedOpts.IncludeServers)
	assert.True(t, capturedOpts.IncludeAgents)
	assert.Equal(t, 100, capturedOpts.BatchSize)
}
