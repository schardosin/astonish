package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/schardosin/astonish/pkg/scheduler"
	"github.com/schardosin/astonish/pkg/store"
)

// executorInstance holds a reference to the active scheduler Executor.
// Set by the daemon during startup via SetExecutor.
// In platform mode this is used for RunNow (manual trigger) and test execution.
// CRUD operations go through the request-scoped store.SchedulerStore.
var (
	executorMu       sync.RWMutex
	executorInstance *scheduler.Executor
)

// SetExecutor registers the active scheduler executor for API/tool access.
// Called by the daemon run loop after scheduler initialization.
func SetExecutor(e *scheduler.Executor) {
	executorMu.Lock()
	defer executorMu.Unlock()
	executorInstance = e
}

// GetExecutor returns the active scheduler executor, or nil if not set.
func GetExecutor() *scheduler.Executor {
	executorMu.RLock()
	defer executorMu.RUnlock()
	return executorInstance
}

// Legacy globals — kept for personal mode backward compatibility where
// the single-instance Scheduler engine is still used for the tick loop.
var (
	schedulerMu       sync.RWMutex
	schedulerInstance *scheduler.Scheduler
)

// SetScheduler registers the active scheduler for personal mode tick loop.
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
	svc := store.FromRequest(r)
	if svc == nil || svc.Scheduler == nil {
		respondError(w, http.StatusServiceUnavailable, "scheduler not available")
		return
	}

	switch r.Method {
	case http.MethodGet:
		handleListJobs(w, svc.Scheduler)
	case http.MethodPost:
		if !RequireTeamAdmin(w, r) {
			return
		}
		handleCreateJob(w, r, svc.Scheduler)
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
	svc := store.FromRequest(r)
	if svc == nil || svc.Scheduler == nil {
		respondError(w, http.StatusServiceUnavailable, "scheduler not available")
		return
	}

	// Extract job ID from URL path
	// Expected: /api/scheduler/jobs/{id}
	parts := splitPath(r.URL.Path)
	if len(parts) < 4 {
		respondError(w, http.StatusBadRequest, "missing job ID")
		return
	}
	jobID := parts[len(parts)-1]

	switch r.Method {
	case http.MethodGet:
		job := svc.Scheduler.Get(r.Context(), jobID)
		if job == nil {
			respondError(w, http.StatusNotFound, "job not found")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(storeJobToAPIJob(job))

	case http.MethodPut:
		if !RequireTeamAdmin(w, r) {
			return
		}
		var update struct {
			Name           string              `json:"name"`
			Mode           string              `json:"mode"`
			Cron           string              `json:"cron"`
			Timezone       string              `json:"timezone"`
			Flow           string              `json:"flow"`
			Params         map[string]string   `json:"params"`
			Instructions   string              `json:"instructions"`
			Channel        string              `json:"channel"`
			Target         string              `json:"target"`
			Enabled        *bool               `json:"enabled"`
			Delivery       *store.JobDelivery  `json:"delivery"`
			ChannelFilter  []string            `json:"channel_filter"`
			MemberChannels map[string][]string `json:"member_channels"`
		}
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		existing := svc.Scheduler.Get(r.Context(), jobID)
		if existing == nil {
			respondError(w, http.StatusNotFound, "job not found")
			return
		}

		// Apply non-zero fields from the update
		if update.Name != "" {
			existing.Name = update.Name
		}
		if update.Mode != "" {
			existing.Mode = update.Mode
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
		// Apply delivery object if provided (full replace)
		if update.Delivery != nil {
			existing.Delivery = *update.Delivery
		}
		// Apply channel filter (can be set to empty to clear)
		if update.ChannelFilter != nil {
			existing.Delivery.ChannelFilter = update.ChannelFilter
		}
		// Apply per-member channel overrides (can be set to empty map to clear)
		if update.MemberChannels != nil {
			existing.Delivery.MemberChannels = update.MemberChannels
		}

		if err := svc.Scheduler.Update(r.Context(), existing); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Recompute NextRun so schedule changes take effect immediately
		if existing.Enabled {
			existing.NextRun = scheduler.ComputeNextRun(existing.Schedule.Cron, existing.Schedule.Timezone)
			_ = svc.Scheduler.Update(r.Context(), existing)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "updated"})

	case http.MethodDelete:
		if !RequireTeamAdmin(w, r) {
			return
		}
		if err := svc.Scheduler.Remove(r.Context(), jobID); err != nil {
			respondError(w, http.StatusNotFound, err.Error())
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

	svc := store.FromRequest(r)
	if svc == nil || svc.Scheduler == nil {
		respondError(w, http.StatusServiceUnavailable, "scheduler not available")
		return
	}

	exec := GetExecutor()
	if exec == nil {
		respondError(w, http.StatusServiceUnavailable, "scheduler executor not available")
		return
	}

	// Extract job ID: /api/scheduler/jobs/{id}/run
	parts := splitPath(r.URL.Path)
	if len(parts) < 5 {
		respondError(w, http.StatusBadRequest, "missing job ID")
		return
	}
	jobID := parts[len(parts)-2] // {id} is second to last, "run" is last

	// Load job from team-scoped store
	storeJob := svc.Scheduler.Get(r.Context(), jobID)
	if storeJob == nil {
		respondError(w, http.StatusNotFound, "job not found")
		return
	}

	// Convert to scheduler.Job for the executor
	job := storeJobToSchedulerJob(storeJob)

	// Build team-scoped execution context.
	// Inject stores so tools (credential_tool, skill_lookup, etc.) can read
	// from the correct team during execution.
	execCtx := r.Context()
	if svc.Credentials != nil {
		execCtx = store.WithCredentialStore(execCtx, svc.Credentials)
	}
	if svc.Flows != nil {
		execCtx = store.WithFlowStore(execCtx, svc.Flows)
	}
	if svc.DrillReports != nil {
		execCtx = store.WithDrillReportStore(execCtx, svc.DrillReports)
	}
	if svc.Skills != nil || svc.TeamSkills != nil {
		execCtx = store.WithSkillStores(execCtx, &store.SkillStores{
			Org:  svc.Skills,
			Team: svc.TeamSkills,
		})
	}
	if svc.Memory != nil {
		execCtx = store.WithMemoryStore(execCtx, svc.Memory)
	}

	// Execute the job
	result, err := exec.Execute(execCtx, job)

	// Update runtime state in team store
	now := time.Now()
	storeJob.LastRun = &now
	if err != nil {
		storeJob.LastStatus = "failed"
		storeJob.LastError = err.Error()
		storeJob.ConsecutiveFailures++
	} else {
		storeJob.LastStatus = "success"
		storeJob.LastError = ""
		storeJob.ConsecutiveFailures = 0
	}
	storeJob.NextRun = scheduler.ComputeNextRun(storeJob.Schedule.Cron, storeJob.Schedule.Timezone)
	_ = svc.Scheduler.Update(r.Context(), storeJob)

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

// handleListJobs returns all scheduled jobs from the team-scoped store.
func handleListJobs(w http.ResponseWriter, ss store.SchedulerStore) {
	storeJobs := ss.List(context.TODO())
	jobs := make([]apiJob, 0, len(storeJobs))
	for _, sj := range storeJobs {
		jobs = append(jobs, storeJobToAPIJob(sj))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"jobs": jobs})
}

// handleCreateJob creates a new scheduled job in the team-scoped store.
func handleCreateJob(w http.ResponseWriter, r *http.Request, ss store.SchedulerStore) {
	if ss == nil {
		respondError(w, http.StatusServiceUnavailable, "scheduler not available")
		return
	}

	// Decode the flat format that SchedulerHTTPAccess sends.
	var flat struct {
		ID           string            `json:"id"`
		Name         string            `json:"name"`
		Mode         string            `json:"mode"`
		Cron         string            `json:"cron"`
		Timezone     string            `json:"timezone"`
		Flow         string            `json:"flow"`
		Params       map[string]string `json:"params"`
		Instructions string            `json:"instructions"`
		Channel      string            `json:"channel"`
		Target       string            `json:"target"`
		Enabled      bool              `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&flat); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Convert flat fields to the store.ScheduledJob structure.
	job := &store.ScheduledJob{
		ID:   flat.ID,
		Name: flat.Name,
		Mode: flat.Mode,
		Schedule: store.JobSchedule{
			Cron:     flat.Cron,
			Timezone: flat.Timezone,
		},
		Payload: store.JobPayload{
			Flow:         flat.Flow,
			Params:       flat.Params,
			Instructions: flat.Instructions,
		},
		Delivery: store.JobDelivery{
			Channel: flat.Channel,
			Target:  flat.Target,
		},
		Enabled:    flat.Enabled,
		CreatedAt:  time.Now(),
		LastStatus: "pending",
	}

	if err := ss.Add(r.Context(), job); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Compute initial NextRun
	if job.Enabled {
		job.NextRun = scheduler.ComputeNextRun(job.Schedule.Cron, job.Schedule.Timezone)
		_ = ss.Update(r.Context(), job)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(storeJobToAPIJob(job))
}

// --- Type conversion helpers ---

// apiJob is the JSON representation returned to the frontend.
// It uses the flat format expected by SchedulerSettings.tsx.
type apiJob struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Mode                string            `json:"mode"`
	Schedule            store.JobSchedule `json:"schedule"`
	Payload             store.JobPayload  `json:"payload"`
	Delivery            store.JobDelivery `json:"delivery"`
	Enabled             bool              `json:"enabled"`
	CreatedAt           time.Time         `json:"created_at"`
	LastRun             *time.Time        `json:"last_run,omitempty"`
	LastStatus          string            `json:"last_status"`
	LastError           string            `json:"last_error,omitempty"`
	NextRun             *time.Time        `json:"next_run,omitempty"`
	ConsecutiveFailures int               `json:"consecutive_failures"`
}

func storeJobToAPIJob(sj *store.ScheduledJob) apiJob {
	return apiJob{
		ID:                  sj.ID,
		Name:                sj.Name,
		Mode:                sj.Mode,
		Schedule:            sj.Schedule,
		Payload:             sj.Payload,
		Delivery:            sj.Delivery,
		Enabled:             sj.Enabled,
		CreatedAt:           sj.CreatedAt,
		LastRun:             sj.LastRun,
		LastStatus:          sj.LastStatus,
		LastError:           sj.LastError,
		NextRun:             sj.NextRun,
		ConsecutiveFailures: sj.ConsecutiveFailures,
	}
}

// storeJobToSchedulerJob converts a store.ScheduledJob to a scheduler.Job
// for use with the executor's Execute method.
func storeJobToSchedulerJob(sj *store.ScheduledJob) *scheduler.Job {
	return &scheduler.Job{
		ID:   sj.ID,
		Name: sj.Name,
		Mode: scheduler.JobMode(sj.Mode),
		Schedule: scheduler.JobSchedule{
			Cron:     sj.Schedule.Cron,
			Timezone: sj.Schedule.Timezone,
		},
		Payload: scheduler.JobPayload{
			Flow:         sj.Payload.Flow,
			Params:       sj.Payload.Params,
			Instructions: sj.Payload.Instructions,
		},
		Delivery: scheduler.JobDelivery{
			Channel:        sj.Delivery.Channel,
			Target:         sj.Delivery.Target,
			Mode:           scheduler.DeliveryMode(sj.Delivery.Mode),
			MemberIDs:      sj.Delivery.MemberIDs,
			ChannelFilter:  sj.Delivery.ChannelFilter,
			MemberChannels: sj.Delivery.MemberChannels,
		},
		Enabled:             sj.Enabled,
		OwnerID:             sj.OwnerID,
		CreatedAt:           sj.CreatedAt,
		LastRun:             sj.LastRun,
		LastStatus:          scheduler.JobStatus(sj.LastStatus),
		LastError:           sj.LastError,
		NextRun:             sj.NextRun,
		ConsecutiveFailures: sj.ConsecutiveFailures,
	}
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

// --- Team members with channels endpoint ---

// TeamMemberChannelsHandler handles GET /api/team/members/channels.
// Returns all team members with their linked (enabled+verified) channel types.
// Used by the Scheduler Settings UI to populate the member picker and
// per-member channel selection.
func TeamMemberChannelsHandler(pa *PlatformAuth) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		user := GetPlatformUser(r)
		if user == nil {
			respondError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		ctx := r.Context()
		orgDataStore, err := pa.pgStore.ForOrg(user.OrgSlug)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to access organization")
			return
		}

		teamSlug := effectiveTeamSlug(r)
		if teamSlug == "" {
			respondError(w, http.StatusBadRequest, "team context required")
			return
		}

		team, err := orgDataStore.Teams().GetTeamBySlug(ctx, teamSlug)
		if err != nil || team == nil {
			respondError(w, http.StatusNotFound, "team not found")
			return
		}

		members, err := orgDataStore.Teams().ListMembers(ctx, team.ID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to list members")
			return
		}

		// Enrich with user details and channel links
		type memberChannelInfo struct {
			UserID         string   `json:"user_id"`
			DisplayName    string   `json:"display_name"`
			Email          string   `json:"email"`
			Role           string   `json:"role"`
			LinkedChannels []string `json:"linked_channels"`
		}

		userStore := pa.pgStore.Users()
		channelStore := pa.pgStore.UserChannels()
		result := make([]memberChannelInfo, 0, len(members))

		for _, m := range members {
			info := memberChannelInfo{
				UserID: m.UserID,
				Role:   m.Role,
				Email:  m.Email,
			}

			// Get display name from platform users table
			if u, err := userStore.GetByID(ctx, m.UserID); err == nil && u != nil {
				info.DisplayName = u.DisplayName
				if info.Email == "" {
					info.Email = u.Email
				}
			}

			// Get linked channels (only enabled+verified)
			if links, err := channelStore.ListByUser(ctx, m.UserID); err == nil {
				for _, link := range links {
					if link.Enabled && link.Verified {
						info.LinkedChannels = append(info.LinkedChannels, link.ChannelType)
					}
				}
			}

			result = append(result, info)
		}

		respondJSON(w, http.StatusOK, map[string]any{
			"members": result,
			"count":   len(result),
		})
	}
}
