// Package jobs provides job management for async operations. Currently
// used only by the embeddings indexer but kind-agnostic: any subsystem
// needing "kick off a long-running operation + poll status / stream
// progress" can reuse Manager.
package jobs

import (
	"time"
)

// JobID uniquely identifies a job.
type JobID string

// JobStatus represents the current state of a job.
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
)

// JobProgress tracks the progress of a job. The indexer emits one of
// these per Kind it's processing (agents / mcp_servers / skills /
// prompts) and an optional overall rollup.
type JobProgress struct {
	Total     int `json:"total"`
	Processed int `json:"processed"`
	Updated   int `json:"updated"`
	Skipped   int `json:"skipped"`
	Failures  int `json:"failures"`
}

// JobResult contains the final outcome of a job. Counts are keyed by
// Kind so the indexer can report "agents: 12 updated, mcp_servers: 5
// updated" without the jobs package knowing what those Kinds are.
// Error is populated when the job entered JobStatusFailed.
type JobResult struct {
	PerKind map[string]JobProgress `json:"perKind,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

// Job represents an async job with progress tracking.
type Job struct {
	ID        JobID       `json:"id"`
	Type      string      `json:"type"`
	Status    JobStatus   `json:"status"`
	Progress  JobProgress `json:"progress"`
	Result    *JobResult  `json:"result,omitempty"`
	CreatedAt time.Time   `json:"createdAt"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

// IsTerminal returns true if the job is in a terminal state.
func (j *Job) IsTerminal() bool {
	return j.Status == JobStatusCompleted || j.Status == JobStatusFailed
}
