package v0

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/agentregistry-dev/agentregistry/internal/registry/jobs"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
)

// SSEEvent represents a server-sent event.
type SSEEvent struct {
	Type     string `json:"type"`
	JobID    string `json:"jobId,omitempty"`
	Resource string `json:"resource,omitempty"`
	Stats    any    `json:"stats,omitempty"`
	Result   any    `json:"result,omitempty"`
	Error    string `json:"error,omitempty"`
}

// RegisterEmbeddingsSSEHandler registers the SSE streaming endpoint for indexing.
// This is registered separately as it uses raw HTTP handlers instead of huma.
func RegisterEmbeddingsSSEHandler(
	mux *http.ServeMux,
	pathPrefix string,
	indexer service.Indexer,
	jobManager *jobs.Manager,
) {
	// Use POST to accept JSON body with options, and method-specific pattern
	// to avoid conflict with Huma's {jobId} route
	path := "POST " + pathPrefix + "/embeddings/index/stream"
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		handleSSEIndex(w, r, indexer, jobManager)
	})
}

func handleSSEIndex(
	w http.ResponseWriter,
	r *http.Request,
	indexer service.Indexer,
	jobManager *jobs.Manager,
) {
	if indexer == nil {
		http.Error(w, "Embeddings service is not configured", http.StatusServiceUnavailable)
		return
	}

	// Check for SSE support
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Parse JSON body
	var req IndexRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Apply defaults
	if req.BatchSize <= 0 {
		req.BatchSize = 100
	}
	if !req.IncludeServers && !req.IncludeAgents {
		req.IncludeServers = true
		req.IncludeAgents = true
	}

	// Create a job for tracking
	job, err := jobManager.CreateJob(jobs.IndexJobType)
	if err != nil {
		if err == jobs.ErrJobAlreadyRunning {
			http.Error(w, "Indexing job already running", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create job: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Helper to send SSE events
	sendEvent := func(event SSEEvent) {
		data, err := json.Marshal(event)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Send started event
	sendEvent(SSEEvent{
		Type:  "started",
		JobID: string(job.ID),
	})

	ctx := r.Context()
	// Use system context for database operations
	dbCtx := auth.WithSystemContext(context.Background())

	if err := jobManager.StartJob(job.ID); err != nil {
		sendEvent(SSEEvent{
			Type:  "error",
			JobID: string(job.ID),
			Error: "Failed to start job: " + err.Error(),
		})
		return
	}

	opts := service.IndexOptions{
		BatchSize:      req.BatchSize,
		Force:          req.Force,
		DryRun:         req.DryRun,
		IncludeServers: req.IncludeServers,
		IncludeAgents:  req.IncludeAgents,
	}

	// Create a context that cancels when client disconnects
	runCtx, cancel := context.WithCancel(dbCtx)
	defer cancel()

	go func() {
		<-ctx.Done()
		cancel()
	}()

	result, err := indexer.Run(runCtx, opts, func(resource string, stats service.IndexStats) {
		sendEvent(SSEEvent{
			Type:     "progress",
			JobID:    string(job.ID),
			Resource: resource,
			Stats:    stats,
		})

		// Also update job progress
		progress := jobs.JobProgress{
			Processed: stats.Processed,
			Updated:   stats.Updated,
			Skipped:   stats.Skipped,
			Failures:  stats.Failures,
		}
		_ = jobManager.UpdateProgress(job.ID, progress)
	})

	if err != nil {
		_ = jobManager.FailJob(job.ID, err.Error())
		sendEvent(SSEEvent{
			Type:  "error",
			JobID: string(job.ID),
			Error: err.Error(),
		})
		return
	}

	jobResult := &jobs.JobResult{
		ServersProcessed: result.Servers.Processed,
		ServersUpdated:   result.Servers.Updated,
		ServersSkipped:   result.Servers.Skipped,
		ServerFailures:   result.Servers.Failures,
		AgentsProcessed:  result.Agents.Processed,
		AgentsUpdated:    result.Agents.Updated,
		AgentsSkipped:    result.Agents.Skipped,
		AgentFailures:    result.Agents.Failures,
	}

	_ = jobManager.CompleteJob(job.ID, jobResult)

	sendEvent(SSEEvent{
		Type:   "completed",
		JobID:  string(job.ID),
		Result: jobResult,
	})
}
