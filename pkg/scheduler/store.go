// Package scheduler implements a job scheduler for Astonish.
// It supports two execution modes: "routine" (run a flow with fixed params)
// and "adaptive" (LLM-driven agentic chat turn with stored instructions).
package scheduler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// JobMode defines how a scheduled job executes.
type JobMode string

const (
	// ModeRoutine runs a flow YAML with fixed parameters through the flow engine.
	// Same inputs, same execution path, every time.
	ModeRoutine JobMode = "routine"

	// ModeAdaptive runs a free-form agentic chat turn. The LLM receives
	// stored instructions and can reason, adapt, and use tools dynamically.
	ModeAdaptive JobMode = "adaptive"
)

// JobSchedule defines when a job runs.
type JobSchedule struct {
	// Cron is a standard 5-field cron expression (e.g., "0 9 * * *").
	Cron string `json:"cron"`
	// Timezone is an IANA timezone name (e.g., "America/Sao_Paulo").
	// Empty defaults to the system's local timezone.
	Timezone string `json:"timezone,omitempty"`
}

// JobPayload holds mode-specific execution data.
type JobPayload struct {
	// Flow is the flow name for routine mode (resolved at runtime).
	Flow string `json:"flow,omitempty"`
	// Params are the flow parameters for routine mode.
	Params map[string]string `json:"params,omitempty"`
	// Instructions is the prompt/task description for adaptive mode.
	Instructions string `json:"instructions,omitempty"`
}

// JobDelivery defines where to send job results.
type JobDelivery struct {
	// Channel is the channel adapter ID (e.g., "telegram").
	Channel string `json:"channel"`
	// Target is the chat/conversation ID to deliver to.
	Target string `json:"target"`
}

// JobStatus represents the outcome of the last execution.
type JobStatus string

const (
	StatusPending JobStatus = "pending" // Never run yet
	StatusSuccess JobStatus = "success"
	StatusFailed  JobStatus = "failed"
)

// Job is a scheduled task with all configuration and runtime state.
type Job struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Mode      JobMode     `json:"mode"`
	Schedule  JobSchedule `json:"schedule"`
	Payload   JobPayload  `json:"payload"`
	Delivery  JobDelivery `json:"delivery"`
	Enabled   bool        `json:"enabled"`
	CreatedAt time.Time   `json:"created_at"`

	// Runtime state (updated by the scheduler after each run)
	LastRun             *time.Time `json:"last_run,omitempty"`
	LastStatus          JobStatus  `json:"last_status"`
	LastError           string     `json:"last_error,omitempty"`
	NextRun             *time.Time `json:"next_run,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
}

// storeData is the JSON file root structure.
type storeData struct {
	Jobs []*Job `json:"jobs"`
}

// Store persists scheduled jobs to a JSON file with atomic writes.
type Store struct {
	path string
	mu   sync.RWMutex
	jobs map[string]*Job // keyed by ID
}

// NewStore creates or loads a job store at the given file path.
// If the file doesn't exist, an empty store is created.
func NewStore(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create store directory: %w", err)
	}

	s := &Store{
		path: path,
		jobs: make(map[string]*Job),
	}

	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// DefaultStorePath returns the default job store path:
// ~/.config/astonish/scheduler/jobs.json
func DefaultStorePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "astonish", "scheduler", "jobs.json"), nil
}

// List returns a copy of all jobs.
func (s *Store) List() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		cp := *j
		jobs = append(jobs, &cp)
	}
	return jobs
}

// Get returns a job by ID. Returns nil if not found.
func (s *Store) Get(id string) *Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	j, ok := s.jobs[id]
	if !ok {
		return nil
	}
	cp := *j
	return &cp
}

// GetByName returns a job by name (case-insensitive). Returns nil if not found.
func (s *Store) GetByName(name string) *Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, j := range s.jobs {
		if equalFold(j.Name, name) {
			cp := *j
			return &cp
		}
	}
	return nil
}

// Add persists a new job. The job ID is generated if empty.
func (s *Store) Add(job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job.ID == "" {
		job.ID = uuid.New().String()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	if job.LastStatus == "" {
		job.LastStatus = StatusPending
	}

	s.jobs[job.ID] = job
	return s.save()
}

// Update replaces an existing job. Returns error if not found.
func (s *Store) Update(job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.jobs[job.ID]; !ok {
		return fmt.Errorf("job %s not found", job.ID)
	}
	s.jobs[job.ID] = job
	return s.save()
}

// Remove deletes a job by ID. Returns error if not found.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.jobs[id]; !ok {
		return fmt.Errorf("job %s not found", id)
	}
	delete(s.jobs, id)
	return s.save()
}

// load reads the JSON file into memory.
func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil // Empty store
	}
	if err != nil {
		return fmt.Errorf("read store: %w", err)
	}

	// Handle empty file
	if len(data) == 0 {
		return nil
	}

	var sd storeData
	if err := json.Unmarshal(data, &sd); err != nil {
		return fmt.Errorf("parse store: %w", err)
	}

	for _, j := range sd.Jobs {
		s.jobs[j.ID] = j
	}
	return nil
}

// save writes the store to disk atomically (write temp + rename).
func (s *Store) save() error {
	sd := storeData{Jobs: make([]*Job, 0, len(s.jobs))}
	for _, j := range s.jobs {
		sd.Jobs = append(sd.Jobs, j)
	}

	data, err := json.MarshalIndent(sd, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal store: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write temp store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp) // Cleanup on failure
		return fmt.Errorf("rename store: %w", err)
	}
	return nil
}

// equalFold is a simple case-insensitive string comparison.
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
