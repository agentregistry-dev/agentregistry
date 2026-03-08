package apitypes

import (
	"github.com/agentregistry-dev/agentregistry/internal/registry/jobs"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

// VersionBody represents API version information.
type VersionBody struct {
	Version   string `json:"version" example:"v1.0.0" doc:"Application version"`
	GitCommit string `json:"git_commit" example:"abc123d" doc:"Git commit SHA"`
	BuildTime string `json:"build_time" example:"2025-10-14T12:00:00Z" doc:"Build timestamp"`
}

// DeploymentRequest represents the input for deploying a resource.
type DeploymentRequest struct {
	ServerName     string            `json:"serverName" doc:"Server name to deploy" example:"io.github.user/weather"`
	Version        string            `json:"version" doc:"Version to deploy (use 'latest' for latest version)" default:"latest" example:"1.0.0"`
	Env            map[string]string `json:"env,omitempty" doc:"Deployment environment variables."`
	ProviderConfig map[string]any    `json:"providerConfig,omitempty" doc:"Optional provider-specific deployment settings (not env vars)."`
	PreferRemote   bool              `json:"preferRemote,omitempty" doc:"Prefer remote deployment over local" default:"false"`
	ResourceType   string            `json:"resourceType,omitempty" doc:"Type of resource to deploy (mcp, agent)" default:"mcp" example:"mcp" enum:"mcp,agent"`
	ProviderID     string            `json:"providerId" doc:"Concrete provider instance ID." required:"true"`
}

// IndexRequest is the request body for embeddings indexing.
type IndexRequest struct {
	BatchSize      int  `json:"batchSize,omitempty" doc:"Number of items to process per batch" default:"100" minimum:"1" maximum:"1000"`
	Force          bool `json:"force,omitempty" doc:"Regenerate embeddings even when checksum matches" default:"false"`
	DryRun         bool `json:"dryRun,omitempty" doc:"Preview changes without writing to database" default:"false"`
	IncludeServers bool `json:"includeServers,omitempty" doc:"Include MCP servers" default:"true"`
	IncludeAgents  bool `json:"includeAgents,omitempty" doc:"Include agents" default:"true"`
	Stream         bool `json:"stream,omitempty" doc:"Use SSE streaming for progress updates" default:"false"`
}

// IndexJobResponse is the response body returned when creating an index job.
type IndexJobResponse struct {
	JobID  string `json:"jobId" doc:"Unique job identifier"`
	Status string `json:"status" doc:"Current job status"`
}

// JobProgress contains job progress counters for index jobs.
type JobProgress = jobs.JobProgress

// JobResult contains the final result for an index job.
type JobResult = jobs.JobResult

// JobStatusResponse is the status payload for an index job.
type JobStatusResponse struct {
	JobID     string      `json:"jobId" doc:"Unique job identifier"`
	Type      string      `json:"type" doc:"Job type"`
	Status    string      `json:"status" doc:"Current job status (pending, running, completed, failed)"`
	Progress  JobProgress `json:"progress" doc:"Current progress"`
	Result    *JobResult  `json:"result,omitempty" doc:"Final result (when completed or failed)"`
	CreatedAt string      `json:"createdAt" doc:"Job creation timestamp"`
	UpdatedAt string      `json:"updatedAt" doc:"Last update timestamp"`
}

// DeploymentsListResponse is the deployment list response body.
type DeploymentsListResponse struct {
	Deployments []models.Deployment `json:"deployments" doc:"List of deployed servers"`
}

// DeploymentLogsResponse is the deployment logs response body.
type DeploymentLogsResponse struct {
	DeploymentID string   `json:"deploymentId"`
	Logs         []string `json:"logs"`
}
