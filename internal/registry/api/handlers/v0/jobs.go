package v0

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// JobStatus represents the status of an async job
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
)

// Job represents an async job
type Job struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Status     JobStatus              `json:"status"`
	Progress   int                    `json:"progress"` // 0-100
	Message    string                 `json:"message,omitempty"`
	Result     map[string]interface{} `json:"result,omitempty"`
	Error      string                 `json:"error,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
	StartedAt  *time.Time             `json:"started_at,omitempty"`
	FinishedAt *time.Time             `json:"finished_at,omitempty"`
}

// JobStore manages async jobs (in-memory implementation)
type JobStore struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

// NewJobStore creates a new job store
func NewJobStore() *JobStore {
	return &JobStore{
		jobs: make(map[string]*Job),
	}
}

// CreateJob creates a new job
func (s *JobStore) CreateJob(jobType string) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()

	job := &Job{
		ID:        uuid.New().String(),
		Type:      jobType,
		Status:    JobStatusPending,
		Progress:  0,
		CreatedAt: time.Now(),
	}

	s.jobs[job.ID] = job
	return job
}

// GetJob retrieves a job by ID
func (s *JobStore) GetJob(id string) (*Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, exists := s.jobs[id]
	return job, exists
}

// UpdateJob updates a job's status and details
func (s *JobStore) UpdateJob(id string, update func(*Job)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, exists := s.jobs[id]
	if !exists {
		return nil
	}

	update(job)
	return nil
}

// ListJobs returns all jobs (for debugging/admin purposes)
func (s *JobStore) ListJobs() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

// CleanupOldJobs removes jobs older than the specified duration
func (s *JobStore) CleanupOldJobs(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for id, job := range s.jobs {
		if job.CreatedAt.Before(cutoff) {
			delete(s.jobs, id)
		}
	}
}

// Global job store instance
var globalJobStore = NewJobStore()

// GetJobStore returns the global job store
func GetJobStore() *JobStore {
	return globalJobStore
}

