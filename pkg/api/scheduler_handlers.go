package api

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/schardosin/astonish/pkg/scheduler"
)

// schedulerInstance holds a reference to the active Scheduler.
// Set by the daemon during startup via SetScheduler.
var (
	schedulerMu       sync.RWMutex
	schedulerInstance *scheduler.Scheduler
)

// SetScheduler registers the active scheduler for API/tool access.
// Called by the daemon run loop after scheduler initialization.
func SetScheduler(s *scheduler.Scheduler) {
	schedulerMu.Lock()
	defer schedulerMu.Unlock()
	schedulerInstance = s
}

// GetScheduler returns the active scheduler, or nil if not set.
func GetScheduler() *scheduler.Scheduler {
	schedulerMu.RLock()
	defer schedulerMu.RUnlock()
	return schedulerInstance
}

// SchedulerJobsHandler handles listing and creating scheduled jobs.
//
// GET  /api/scheduler/jobs — list all jobs
// POST /api/scheduler/jobs — create a new job
func SchedulerJobsHandler(w http.ResponseWriter, r *http.Request) {
	s := GetScheduler()

	switch r.Method {
	case http.MethodGet:
		handleListJobs(w, s)
	case http.MethodPost:
		handleCreateJob(w, r, s)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// SchedulerJobHandler handles operations on a single job.
//
// GET    /api/scheduler/jobs/{id} — get job details
// PUT    /api/scheduler/jobs/{id} — update job
// DELETE /api/scheduler/jobs/{id} — remove job
func SchedulerJobHandler(w http.ResponseWriter, r *http.Request) {
	s := GetScheduler()
	if s == nil {
		http.Error(w, "scheduler not enabled", http.StatusServiceUnavailable)
		return
	}

	// Extract job ID from URL path
	// Expected: /api/scheduler/jobs/{id}
	parts := splitPath(r.URL.Path)
	if len(parts) < 4 {
		http.Error(w, "missing job ID", http.StatusBadRequest)
		return
	}
	jobID := parts[len(parts)-1]

	switch r.Method {
	case http.MethodGet:
		job := s.Store().Get(jobID)
		if job == nil {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(job)

	case http.MethodPut:
		// The request body uses the flat tools.SchedulerJob JSON format
		// (with top-level "cron", "flow", "instructions" fields), not
		// the nested scheduler.Job format. Decode into a flat struct
		// and selectively merge into the existing job.
		var update struct {
			Name         string            `json:"name"`
			Mode         string            `json:"mode"`
			Cron         string            `json:"cron"`
			Timezone     string            `json:"timezone"`
			Flow         string            `json:"flow"`
			Params       map[string]string `json:"params"`
			Instructions string            `json:"instructions"`
			Channel      string            `json:"channel"`
			Target       string            `json:"target"`
			Enabled      *bool             `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		existing := s.Store().Get(jobID)
		if existing == nil {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}

		// Apply non-zero fields from the update
		if update.Name != "" {
			existing.Name = update.Name
		}
		if update.Mode != "" {
			existing.Mode = scheduler.JobMode(update.Mode)
		}
		if update.Cron != "" {
			existing.Schedule.Cron = update.Cron
		}
		if update.Timezone != "" {
			existing.Schedule.Timezone = update.Timezone
		}
		if update.Flow != "" {
			existing.Payload.Flow = update.Flow
		}
		if update.Params != nil {
			existing.Payload.Params = update.Params
		}
		if update.Instructions != "" {
			existing.Payload.Instructions = update.Instructions
		}
		if update.Channel != "" {
			existing.Delivery.Channel = update.Channel
		}
		if update.Target != "" {
			existing.Delivery.Target = update.Target
		}
		if update.Enabled != nil {
			existing.Enabled = *update.Enabled
		}

		if err := s.Store().Update(existing); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		// Recompute NextRun from now so schedule changes take effect immediately
		s.RefreshNextRun(jobID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "updated"})

	case http.MethodDelete:
		if err := s.Store().Remove(jobID); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "removed"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// SchedulerJobRunHandler triggers immediate execution of a job.
//
// POST /api/scheduler/jobs/{id}/run
func SchedulerJobRunHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s := GetScheduler()
	if s == nil {
		http.Error(w, "scheduler not enabled", http.StatusServiceUnavailable)
		return
	}

	// Extract job ID: /api/scheduler/jobs/{id}/run
	parts := splitPath(r.URL.Path)
	if len(parts) < 5 {
		http.Error(w, "missing job ID", http.StatusBadRequest)
		return
	}
	jobID := parts[len(parts)-2] // {id} is second to last, "run" is last

	result, err := s.RunNow(r.Context(), jobID)
	resp := map[string]any{
		"job_id": jobID,
		"result": result,
	}
	if err != nil {
		resp["error"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleListJobs returns all scheduled jobs.
func handleListJobs(w http.ResponseWriter, s *scheduler.Scheduler) {
	var jobs []*scheduler.Job
	if s != nil {
		jobs = s.Store().List()
	}
	if jobs == nil {
		jobs = []*scheduler.Job{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"jobs": jobs})
}

// handleCreateJob creates a new scheduled job.
func handleCreateJob(w http.ResponseWriter, r *http.Request, s *scheduler.Scheduler) {
	if s == nil {
		http.Error(w, "scheduler not enabled", http.StatusServiceUnavailable)
		return
	}

	var job scheduler.Job
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.Store().Add(&job); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Compute initial NextRun
	s.RefreshNextRun(job.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(job)
}

// splitPath splits a URL path into segments, filtering empty strings.
func splitPath(path string) []string {
	var parts []string
	for _, p := range split(path, '/') {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// split is a simple byte-based string splitter.
func split(s string, sep byte) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}
