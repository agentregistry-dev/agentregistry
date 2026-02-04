package v0

import (
	"context"
	"net/http"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/jobs"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/danielgtaylor/huma/v2"
)

// BackfillRequest is the request body for starting a backfill job.
type BackfillRequest struct {
	BatchSize      int  `json:"batchSize,omitempty" doc:"Number of items to process per batch" default:"100" minimum:"1" maximum:"1000"`
	Force          bool `json:"force,omitempty" doc:"Regenerate embeddings even when checksum matches" default:"false"`
	DryRun         bool `json:"dryRun,omitempty" doc:"Preview changes without writing to database" default:"false"`
	IncludeServers bool `json:"includeServers,omitempty" doc:"Include MCP servers" default:"true"`
	IncludeAgents  bool `json:"includeAgents,omitempty" doc:"Include agents" default:"true"`
	Stream         bool `json:"stream,omitempty" doc:"Use SSE streaming for progress updates" default:"false"`
}

// BackfillInput is the input for starting a backfill.
type BackfillInput struct {
	Body BackfillRequest
}

// BackfillJobResponse is the response for job creation.
type BackfillJobResponse struct {
	JobID  string `json:"jobId" doc:"Unique job identifier"`
	Status string `json:"status" doc:"Current job status"`
}

// JobStatusInput is the input for getting job status.
type JobStatusInput struct {
	JobID string `path:"jobId" doc:"Job identifier"`
}

// JobStatusResponse is the response for job status.
type JobStatusResponse struct {
	JobID     string             `json:"jobId" doc:"Unique job identifier"`
	Type      string             `json:"type" doc:"Job type"`
	Status    string             `json:"status" doc:"Current job status (pending, running, completed, failed)"`
	Progress  jobs.JobProgress   `json:"progress" doc:"Current progress"`
	Result    *jobs.JobResult    `json:"result,omitempty" doc:"Final result (when completed or failed)"`
	CreatedAt string             `json:"createdAt" doc:"Job creation timestamp"`
	UpdatedAt string             `json:"updatedAt" doc:"Last update timestamp"`
}

// RegisterEmbeddingsEndpoints registers the embeddings admin endpoints.
func RegisterEmbeddingsEndpoints(
	api huma.API,
	pathPrefix string,
	backfillService *service.BackfillService,
	jobManager *jobs.Manager,
) {
	registerBackfillEndpoint(api, pathPrefix, backfillService, jobManager)
	registerJobStatusEndpoint(api, pathPrefix, jobManager)
}

func registerBackfillEndpoint(
	api huma.API,
	pathPrefix string,
	backfillService *service.BackfillService,
	jobManager *jobs.Manager,
) {
	huma.Register(api, huma.Operation{
		OperationID: "start-embeddings-backfill" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/embeddings/backfill",
		Summary:     "Start embeddings backfill",
		Description: "Start a background job to generate embeddings for servers and/or agents. Use stream=true for SSE progress updates.",
		Tags:        []string{"embeddings"},
	}, func(ctx context.Context, input *BackfillInput) (*Response[BackfillJobResponse], error) {
		if backfillService == nil {
			return nil, huma.Error503ServiceUnavailable("embeddings service is not configured")
		}

		req := input.Body

		// Default to including both if neither specified
		if !req.IncludeServers && !req.IncludeAgents {
			req.IncludeServers = true
			req.IncludeAgents = true
		}

		if req.BatchSize <= 0 {
			req.BatchSize = 100
		}

		// SSE streaming is handled by a different endpoint
		if req.Stream {
			return nil, huma.Error400BadRequest("SSE streaming should use GET /embeddings/backfill/stream with query parameters")
		}

		// Create a new job
		job, err := jobManager.CreateJob(jobs.BackfillJobType)
		if err != nil {
			if err == jobs.ErrJobAlreadyRunning {
				existingJob := jobManager.GetRunningJob(jobs.BackfillJobType)
				if existingJob != nil {
					return nil, huma.Error409Conflict("backfill job already running: " + string(existingJob.ID))
				}
				return nil, huma.Error409Conflict("backfill job already running")
			}
			return nil, huma.Error500InternalServerError("failed to create job: " + err.Error())
		}

		// Run backfill in background
		go runBackfillJob(backfillService, jobManager, job.ID, req)

		return &Response[BackfillJobResponse]{
			Body: BackfillJobResponse{
				JobID:  string(job.ID),
				Status: string(job.Status),
			},
		}, nil
	})
}

func runBackfillJob(
	backfillService *service.BackfillService,
	jobManager *jobs.Manager,
	jobID jobs.JobID,
	req BackfillRequest,
) {
	ctx := auth.WithSystemContext(context.Background())

	if err := jobManager.StartJob(jobID); err != nil {
		_ = jobManager.FailJob(jobID, "failed to start job: "+err.Error())
		return
	}

	opts := service.BackfillOptions{
		BatchSize:      req.BatchSize,
		Force:          req.Force,
		DryRun:         req.DryRun,
		IncludeServers: req.IncludeServers,
		IncludeAgents:  req.IncludeAgents,
	}

	var serverStats, agentStats service.BackfillStats

	result, err := backfillService.Run(ctx, opts, func(resource string, stats service.BackfillStats) {
		switch resource {
		case "servers":
			serverStats = stats
		case "agents":
			agentStats = stats
		}

		progress := jobs.JobProgress{
			Processed: serverStats.Processed + agentStats.Processed,
			Updated:   serverStats.Updated + agentStats.Updated,
			Skipped:   serverStats.Skipped + agentStats.Skipped,
			Failures:  serverStats.Failures + agentStats.Failures,
		}
		_ = jobManager.UpdateProgress(jobID, progress)
	})

	if err != nil {
		_ = jobManager.FailJob(jobID, err.Error())
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

	_ = jobManager.CompleteJob(jobID, jobResult)
}

func registerJobStatusEndpoint(
	api huma.API,
	pathPrefix string,
	jobManager *jobs.Manager,
) {
	huma.Register(api, huma.Operation{
		OperationID: "get-embeddings-backfill-status" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/embeddings/backfill/{jobId}",
		Summary:     "Get backfill job status",
		Description: "Get the status and progress of a backfill job.",
		Tags:        []string{"embeddings"},
	}, func(ctx context.Context, input *JobStatusInput) (*Response[JobStatusResponse], error) {
		job, err := jobManager.GetJob(jobs.JobID(input.JobID))
		if err != nil {
			if err == jobs.ErrJobNotFound {
				return nil, huma.Error404NotFound("job not found: " + input.JobID)
			}
			return nil, huma.Error500InternalServerError("failed to get job: " + err.Error())
		}

		return &Response[JobStatusResponse]{
			Body: JobStatusResponse{
				JobID:     string(job.ID),
				Type:      job.Type,
				Status:    string(job.Status),
				Progress:  job.Progress,
				Result:    job.Result,
				CreatedAt: job.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
				UpdatedAt: job.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			},
		}, nil
	})
}
