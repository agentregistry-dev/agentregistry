// Package jobs provides job management for async operations.
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

// JobProgress tracks the progress of a job.
type JobProgress struct {
	Total     int `json:"total"`
	Processed int `json:"processed"`
	Updated   int `json:"updated"`
	Skipped   int `json:"skipped"`
	Failures  int `json:"failures"`
}

// JobResult contains the final outcome of a job.
type JobResult struct {
	ServersProcessed int    `json:"serversProcessed,omitempty"`
	ServersUpdated   int    `json:"serversUpdated,omitempty"`
	ServersSkipped   int    `json:"serversSkipped,omitempty"`
	ServerFailures   int    `json:"serverFailures,omitempty"`
	AgentsProcessed  int    `json:"agentsProcessed,omitempty"`
	AgentsUpdated    int    `json:"agentsUpdated,omitempty"`
	AgentsSkipped    int    `json:"agentsSkipped,omitempty"`
	AgentFailures    int    `json:"agentFailures,omitempty"`
	Error            string `json:"error,omitempty"`
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
