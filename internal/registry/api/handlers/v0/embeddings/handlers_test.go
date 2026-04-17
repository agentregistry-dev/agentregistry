package embeddings_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v0embeddings "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/embeddings"
	"github.com/agentregistry-dev/agentregistry/internal/registry/jobs"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
)

type mockIndexer struct {
	mu       sync.Mutex
	runFunc  func(ctx context.Context, opts service.IndexOptions, onProgress service.IndexProgressCallback) (*service.IndexResult, error)
	runCalls []service.IndexOptions
}

func (m *mockIndexer) Run(ctx context.Context, opts service.IndexOptions, onProgress service.IndexProgressCallback) (*service.IndexResult, error) {
	m.mu.Lock()
	m.runCalls = append(m.runCalls, opts)
	runFunc := m.runFunc
	m.mu.Unlock()

	if runFunc != nil {
		return runFunc(ctx, opts, onProgress)
	}
	return &service.IndexResult{}, nil
}

func TestStartIndex_Success(t *testing.T) {
	mockIdx := &mockIndexer{
		runFunc: func(ctx context.Context, opts service.IndexOptions, onProgress service.IndexProgressCallback) (*service.IndexResult, error) {
			return &service.IndexResult{
				Servers: service.IndexStats{Processed: 5, Updated: 3, Skipped: 2},
			}, nil
		},
	}

	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	v0embeddings.RegisterEmbeddingsEndpoints(api, "/v0", mockIdx, jobManager)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp v0embeddings.IndexJobResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.NotEmpty(t, resp.JobID)
	assert.Equal(t, "pending", resp.Status)
}

func TestStartIndex_WithOptions(t *testing.T) {
	captured := make(chan service.IndexOptions, 1)
	mockIdx := &mockIndexer{
		runFunc: func(ctx context.Context, opts service.IndexOptions, onProgress service.IndexProgressCallback) (*service.IndexResult, error) {
			captured <- opts
			return &service.IndexResult{}, nil
		},
	}

	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	v0embeddings.RegisterEmbeddingsEndpoints(api, "/v0", mockIdx, jobManager)

	body := strings.NewReader(`{"batchSize": 50, "force": true, "dryRun": true, "includeServers": true, "includeAgents": true}`)
	req := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	select {
	case capturedOpts := <-captured:
		assert.Equal(t, 50, capturedOpts.BatchSize)
		assert.True(t, capturedOpts.Force)
		assert.True(t, capturedOpts.DryRun)
		assert.True(t, capturedOpts.IncludeServers)
		assert.True(t, capturedOpts.IncludeAgents)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for indexer to be called")
	}
}

func TestStartIndex_IndexerNil(t *testing.T) {
	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	v0embeddings.RegisterEmbeddingsEndpoints(api, "/v0", nil, jobManager)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "not configured")
}

func TestStartIndex_StreamTrue(t *testing.T) {
	mockIdx := &mockIndexer{}

	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	v0embeddings.RegisterEmbeddingsEndpoints(api, "/v0", mockIdx, jobManager)

	body := strings.NewReader(`{"stream": true}`)
	req := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "SSE streaming")
}

func TestStartIndex_JobAlreadyRunning(t *testing.T) {
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
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	v0embeddings.RegisterEmbeddingsEndpoints(api, "/v0", mockIdx, jobManager)

	body1 := strings.NewReader(`{}`)
	req1 := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index", body1)
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()

	mux.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code)

	<-started

	body2 := strings.NewReader(`{}`)
	req2 := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index", body2)
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()

	mux.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusConflict, w2.Code)
	assert.Contains(t, w2.Body.String(), "already running")

	close(blockCh)
}

func TestStartIndex_DefaultsApplied(t *testing.T) {
	captured := make(chan service.IndexOptions, 1)
	mockIdx := &mockIndexer{
		runFunc: func(ctx context.Context, opts service.IndexOptions, onProgress service.IndexProgressCallback) (*service.IndexResult, error) {
			captured <- opts
			return &service.IndexResult{}, nil
		},
	}

	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	v0embeddings.RegisterEmbeddingsEndpoints(api, "/v0", mockIdx, jobManager)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	select {
	case capturedOpts := <-captured:
		assert.True(t, capturedOpts.IncludeServers)
		assert.True(t, capturedOpts.IncludeAgents)
		assert.Equal(t, 100, capturedOpts.BatchSize)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for indexer to be called")
	}
}

func TestGetJobStatus_Success(t *testing.T) {
	mockIdx := &mockIndexer{
		runFunc: func(ctx context.Context, opts service.IndexOptions, onProgress service.IndexProgressCallback) (*service.IndexResult, error) {
			return &service.IndexResult{}, nil
		},
	}

	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	v0embeddings.RegisterEmbeddingsEndpoints(api, "/v0", mockIdx, jobManager)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var createResp v0embeddings.IndexJobResponse
	err := json.Unmarshal(w.Body.Bytes(), &createResp)
	require.NoError(t, err)

	req2 := httptest.NewRequest(http.MethodGet, "/v0/embeddings/index/"+createResp.JobID, nil)
	w2 := httptest.NewRecorder()

	mux.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)

	var statusResp v0embeddings.JobStatusResponse
	err = json.Unmarshal(w2.Body.Bytes(), &statusResp)
	require.NoError(t, err)

	assert.Equal(t, createResp.JobID, statusResp.JobID)
	assert.Equal(t, "embeddings-index", statusResp.Type)
}

func TestGetJobStatus_NotFound(t *testing.T) {
	mockIdx := &mockIndexer{}

	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	v0embeddings.RegisterEmbeddingsEndpoints(api, "/v0", mockIdx, jobManager)

	req := httptest.NewRequest(http.MethodGet, "/v0/embeddings/index/non-existent-job-id", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not found")
}

func TestGetJobStatus_Completed(t *testing.T) {
	mockIdx := &mockIndexer{
		runFunc: func(ctx context.Context, opts service.IndexOptions, onProgress service.IndexProgressCallback) (*service.IndexResult, error) {
			return &service.IndexResult{
				Servers: service.IndexStats{Processed: 10, Updated: 5, Skipped: 3, Failures: 2},
				Agents:  service.IndexStats{Processed: 8, Updated: 4, Skipped: 2, Failures: 2},
			}, nil
		},
	}

	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	v0embeddings.RegisterEmbeddingsEndpoints(api, "/v0", mockIdx, jobManager)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var createResp v0embeddings.IndexJobResponse
	err := json.Unmarshal(w.Body.Bytes(), &createResp)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	req2 := httptest.NewRequest(http.MethodGet, "/v0/embeddings/index/"+createResp.JobID, nil)
	w2 := httptest.NewRecorder()

	mux.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)

	var statusResp v0embeddings.JobStatusResponse
	err = json.Unmarshal(w2.Body.Bytes(), &statusResp)
	require.NoError(t, err)

	assert.Equal(t, "completed", statusResp.Status)
	require.NotNil(t, statusResp.Result)
	assert.Equal(t, 10, statusResp.Result.ServersProcessed)
	assert.Equal(t, 5, statusResp.Result.ServersUpdated)
	assert.Equal(t, 8, statusResp.Result.AgentsProcessed)
	assert.Equal(t, 4, statusResp.Result.AgentsUpdated)
}

func TestGetJobStatus_Failed(t *testing.T) {
	mockIdx := &mockIndexer{
		runFunc: func(ctx context.Context, opts service.IndexOptions, onProgress service.IndexProgressCallback) (*service.IndexResult, error) {
			return nil, assert.AnError
		},
	}

	jobManager := jobs.NewManager()
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	v0embeddings.RegisterEmbeddingsEndpoints(api, "/v0", mockIdx, jobManager)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/v0/embeddings/index", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var createResp v0embeddings.IndexJobResponse
	err := json.Unmarshal(w.Body.Bytes(), &createResp)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	req2 := httptest.NewRequest(http.MethodGet, "/v0/embeddings/index/"+createResp.JobID, nil)
	w2 := httptest.NewRecorder()

	mux.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)

	var statusResp v0embeddings.JobStatusResponse
	err = json.Unmarshal(w2.Body.Bytes(), &statusResp)
	require.NoError(t, err)

	assert.Equal(t, "failed", statusResp.Status)
	require.NotNil(t, statusResp.Result)
	assert.NotEmpty(t, statusResp.Result.Error)
}
