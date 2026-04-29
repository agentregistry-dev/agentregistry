// Package embeddings hosts the HTTP endpoints that drive the semantic
// embeddings indexer + expose job status. Endpoints register only when
// the server has an Indexer + jobs.Manager wired (see RouteOptions in
// internal/registry/api/router/v0.go).
//
// Endpoints:
//
//	POST  /v0/embeddings/index              — kick off a background indexing job
//	GET   /v0/embeddings/index/{jobId}      — read job status / progress / result
package embeddings

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/embeddings/jobs"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
)

// IndexRequest is the POST /v0/embeddings/index body.
type IndexRequest struct {
	// BatchSize overrides the indexer's default page size.
	BatchSize int `json:"batchSize,omitempty"`
	// Force bypasses the checksum skip — every row gets a fresh
	// embedding.
	Force bool `json:"force,omitempty"`
	// DryRun exercises the indexer without persisting.
	DryRun bool `json:"dryRun,omitempty"`
	// Kinds narrows the pass to specific Kinds (empty ⇒ all).
	Kinds []string `json:"kinds,omitempty"`
	// Namespace narrows the pass to a single namespace (empty ⇒
	// cross-namespace).
	Namespace string `json:"namespace,omitempty"`
}

// IndexJobResponse is returned from POST /v0/embeddings/index.
type IndexJobResponse struct {
	JobID  string `json:"jobId"`
	Status string `json:"status"`
}

// JobStatusResponse is returned from GET /v0/embeddings/index/{jobId}.
type JobStatusResponse struct {
	JobID    string           `json:"jobId"`
	Status   string           `json:"status"`
	Type     string           `json:"type"`
	Progress jobs.JobProgress `json:"progress"`
	Result   *jobs.JobResult  `json:"result,omitempty"`
}

// Config bundles the dependencies the handler needs. The job manager is
// owned internally — there's only ever one in-process job tracker for
// indexing. Authz gates admin-only operations (indexing + job status);
// zero-valued Authz falls through to the public provider which allows
// every check.
type Config struct {
	BasePrefix string
	Indexer    *embeddings.Indexer
	Authz      auth.Authorizer
}

// runtimeConfig is the internal Config the registered handlers close
// over. Adds the per-Register-call jobs.Manager that callers don't (and
// shouldn't) supply — the manager is an embeddings-only concern.
type runtimeConfig struct {
	Config
	Manager *jobs.Manager
}

// Register wires POST + GET under {BasePrefix}/embeddings/index.
// Callers pass BasePrefix="/v0". Authz defaults to a permissive public
// provider when zero-valued so existing OSS deployments keep working.
//
// Caller is responsible for not invoking Register unless cfg.Indexer is
// wired — the router gates on that already.
func Register(api huma.API, cfg Config) {
	if cfg.Authz.Authz == nil {
		cfg.Authz = auth.Authorizer{Authz: auth.NewPublicAuthzProvider(nil)}
	}
	rt := runtimeConfig{Config: cfg, Manager: jobs.NewManager()}
	registerIndex(api, rt)
	registerStatus(api, rt)
}

type indexInput struct {
	Body IndexRequest
}

type indexOutput struct {
	Body IndexJobResponse
}

func registerIndex(api huma.API, cfg runtimeConfig) {
	huma.Register(api, huma.Operation{
		OperationID: "start-embeddings-index",
		Method:      http.MethodPost,
		Path:        cfg.BasePrefix + "/embeddings/index",
		Summary:     "Start a background indexing pass",
		Tags:        []string{"embeddings"},
	}, func(ctx context.Context, in *indexInput) (*indexOutput, error) {
		// Only registry admins may trigger an indexing pass — the
		// operation touches every row and is a resource hog.
		if !cfg.Authz.IsRegistryAdmin(ctx) {
			return nil, huma.Error403Forbidden("Forbidden")
		}
		job, err := cfg.Manager.CreateJob(jobs.IndexJobType)
		if err != nil {
			if errors.Is(err, jobs.ErrJobAlreadyRunning) {
				existing := cfg.Manager.GetRunningJob(jobs.IndexJobType)
				return nil, huma.Error409Conflict("indexing job already running: " + string(existing.ID))
			}
			return nil, huma.Error500InternalServerError("create job: " + err.Error())
		}

		opts := embeddings.IndexOptions{
			BatchSize: in.Body.BatchSize,
			Force:     in.Body.Force,
			DryRun:    in.Body.DryRun,
			Kinds:     in.Body.Kinds,
			Namespace: in.Body.Namespace,
		}

		// Run the indexer asynchronously so the HTTP request returns
		// immediately. The background context is disconnected from the
		// request — long-running indexing shouldn't be cancelled by a
		// client disconnect.
		go runIndex(context.Background(), cfg, job.ID, opts)

		return &indexOutput{Body: IndexJobResponse{
			JobID:  string(job.ID),
			Status: string(job.Status),
		}}, nil
	})
}

// runIndex drives the indexer + feeds progress updates back to the
// jobs.Manager. Always marks the job terminal (completed or failed)
// before returning so callers polling the status endpoint eventually
// see a final state.
func runIndex(ctx context.Context, cfg runtimeConfig, jobID jobs.JobID, opts embeddings.IndexOptions) {
	_ = cfg.Manager.StartJob(jobID)

	res, err := cfg.Indexer.Run(ctx, opts, func(kind string, stats embeddings.IndexStats) {
		_ = cfg.Manager.UpdateProgress(jobID, jobs.JobProgress{
			Processed: stats.Processed,
			Updated:   stats.Updated,
			Skipped:   stats.Skipped,
			Failures:  stats.Failures,
		})
	})

	result := &jobs.JobResult{
		PerKind: map[string]jobs.JobProgress{},
	}
	if res != nil {
		for kind, stats := range res.Stats {
			result.PerKind[kind] = jobs.JobProgress{
				Processed: stats.Processed,
				Updated:   stats.Updated,
				Skipped:   stats.Skipped,
				Failures:  stats.Failures,
			}
		}
	}

	if err != nil {
		result.Error = err.Error()
		_ = cfg.Manager.FailJob(jobID, err.Error())
		return
	}
	_ = cfg.Manager.CompleteJob(jobID, result)
}

type statusInput struct {
	JobID string `path:"jobId" doc:"Job identifier"`
}

type statusOutput struct {
	Body JobStatusResponse
}

func registerStatus(api huma.API, cfg runtimeConfig) {
	huma.Register(api, huma.Operation{
		OperationID: "get-embeddings-index-job",
		Method:      http.MethodGet,
		Path:        cfg.BasePrefix + "/embeddings/index/{jobId}",
		Summary:     "Get an embeddings indexing job's status",
		Tags:        []string{"embeddings"},
	}, func(ctx context.Context, in *statusInput) (*statusOutput, error) {
		// Same gate as job creation to avoid leaking job existence.
		if !cfg.Authz.IsRegistryAdmin(ctx) {
			return nil, huma.Error403Forbidden("Forbidden")
		}
		job, err := cfg.Manager.GetJob(jobs.JobID(in.JobID))
		if err != nil {
			if errors.Is(err, jobs.ErrJobNotFound) {
				return nil, huma.Error404NotFound("job not found")
			}
			return nil, huma.Error500InternalServerError("get job: " + err.Error())
		}
		return &statusOutput{Body: JobStatusResponse{
			JobID:    string(job.ID),
			Status:   string(job.Status),
			Type:     job.Type,
			Progress: job.Progress,
			Result:   job.Result,
		}}, nil
	})
}
