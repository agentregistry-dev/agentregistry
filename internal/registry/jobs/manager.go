package jobs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

const (
	// JobTTL is how long completed jobs are retained.
	JobTTL = 1 * time.Hour

	// BackfillJobType is the type for embedding backfill jobs.
	BackfillJobType = "embeddings-backfill"
)

var (
	// ErrJobNotFound is returned when a job is not found.
	ErrJobNotFound = errors.New("job not found")

	// ErrJobAlreadyRunning is returned when a job of the same type is already running.
	ErrJobAlreadyRunning = errors.New("job already running")
)

// Manager manages async jobs in memory.
type Manager struct {
	mu   sync.RWMutex
	jobs map[JobID]*Job
}

// NewManager creates a new job manager.
func NewManager() *Manager {
	m := &Manager{
		jobs: make(map[JobID]*Job),
	}
	go m.cleanupLoop()
	return m
}

// CreateJob creates a new job of the given type.
// Returns ErrJobAlreadyRunning if a job of the same type is already running.
func (m *Manager) CreateJob(jobType string) (*Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if a job of this type is already running
	for _, job := range m.jobs {
		if job.Type == jobType && !job.IsTerminal() {
			return nil, ErrJobAlreadyRunning
		}
	}

	id := generateJobID(jobType)
	now := time.Now().UTC()

	job := &Job{
		ID:        id,
		Type:      jobType,
		Status:    JobStatusPending,
		Progress:  JobProgress{},
		CreatedAt: now,
		UpdatedAt: now,
	}

	m.jobs[id] = job
	return job, nil
}

// GetJob retrieves a job by ID.
func (m *Manager) GetJob(id JobID) (*Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	job, ok := m.jobs[id]
	if !ok {
		return nil, ErrJobNotFound
	}
	// Return a copy to avoid race conditions
	jobCopy := *job
	return &jobCopy, nil
}

// GetRunningJob returns the currently running job of the given type, if any.
func (m *Manager) GetRunningJob(jobType string) *Job {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, job := range m.jobs {
		if job.Type == jobType && !job.IsTerminal() {
			jobCopy := *job
			return &jobCopy
		}
	}
	return nil
}

// StartJob transitions a job to running status.
func (m *Manager) StartJob(id JobID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[id]
	if !ok {
		return ErrJobNotFound
	}

	job.Status = JobStatusRunning
	job.UpdatedAt = time.Now().UTC()
	return nil
}

// UpdateProgress updates the progress of a job.
func (m *Manager) UpdateProgress(id JobID, progress JobProgress) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[id]
	if !ok {
		return ErrJobNotFound
	}

	job.Progress = progress
	job.UpdatedAt = time.Now().UTC()
	return nil
}

// CompleteJob marks a job as completed with a result.
func (m *Manager) CompleteJob(id JobID, result *JobResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[id]
	if !ok {
		return ErrJobNotFound
	}

	job.Status = JobStatusCompleted
	job.Result = result
	job.UpdatedAt = time.Now().UTC()
	return nil
}

// FailJob marks a job as failed with an error message.
func (m *Manager) FailJob(id JobID, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[id]
	if !ok {
		return ErrJobNotFound
	}

	job.Status = JobStatusFailed
	job.Result = &JobResult{Error: errMsg}
	job.UpdatedAt = time.Now().UTC()
	return nil
}

// cleanupLoop periodically removes old completed/failed jobs.
func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.cleanup()
	}
}

func (m *Manager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().UTC().Add(-JobTTL)
	for id, job := range m.jobs {
		if job.IsTerminal() && job.UpdatedAt.Before(cutoff) {
			delete(m.jobs, id)
		}
	}
}

func generateJobID(prefix string) JobID {
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		// Fall back to timestamp-based ID
		return JobID(prefix + "-" + time.Now().UTC().Format("20060102150405"))
	}
	return JobID(prefix + "-" + hex.EncodeToString(bytes))
}
