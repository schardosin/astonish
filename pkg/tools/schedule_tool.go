package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// SchedulerAccess provides access to scheduler operations without importing
// the scheduler package directly (breaking the import cycle).
type SchedulerAccess interface {
	AddJob(job *SchedulerJob) error
	ListJobs() []*SchedulerJob
	RemoveJob(id string) error
	UpdateJob(job *SchedulerJob) error
	RunNow(ctx context.Context, jobID string) (string, error)
	GetJobByName(name string) *SchedulerJob
	ValidateCron(expr string) error
}

// SchedulerJob mirrors scheduler.Job but lives in the tools package to avoid
// import cycles. The daemon bridges between these types.
type SchedulerJob struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Mode      string            `json:"mode"` // "routine", "adaptive", or "fleet_poll"
	Cron      string            `json:"cron"`
	Timezone  string            `json:"timezone,omitempty"`
	Flow      string            `json:"flow,omitempty"`
	Params    map[string]string `json:"params,omitempty"`
	Instr     string            `json:"instructions,omitempty"`
	Channel   string            `json:"channel,omitempty"`
	Target    string            `json:"target,omitempty"`
	Enabled   bool              `json:"enabled"`
	TestFirst bool              `json:"test_first,omitempty"`

	// Runtime state (populated on list)
	LastRun    *time.Time `json:"last_run,omitempty"`
	LastStatus string     `json:"last_status,omitempty"`
	NextRun    *time.Time `json:"next_run,omitempty"`
	Failures   int        `json:"consecutive_failures,omitempty"`
	CreatedAt  time.Time  `json:"created_at,omitempty"`
}

// schedulerAccessVar holds the SchedulerAccess implementation.
// Set by the daemon via SetSchedulerAccess.
var schedulerAccessVar SchedulerAccess

// SetSchedulerAccess registers the scheduler access implementation.
// Called by the daemon after scheduler initialization.
func SetSchedulerAccess(sa SchedulerAccess) {
	schedulerAccessVar = sa
}

// --- schedule_job tool ---

type ScheduleJobArgs struct {
	Name         string            `json:"name" jsonschema:"A short descriptive name for this scheduled job"`
	Mode         string            `json:"mode" jsonschema:"Execution mode: 'routine' (run a flow with fixed params) or 'adaptive' (LLM-driven agentic execution)"`
	Schedule     string            `json:"schedule" jsonschema:"Cron expression (5-field: minute hour day month weekday). Example: '0 9 * * *' for 9 AM daily, '0 */2 * * *' for every 2 hours, '0 9 * * 1-5' for weekdays at 9 AM"`
	Timezone     string            `json:"timezone,omitempty" jsonschema:"IANA timezone (e.g. 'America/Sao_Paulo'). Defaults to system local timezone"`
	Flow         string            `json:"flow,omitempty" jsonschema:"Flow name to execute (required for routine mode)"`
	Params       map[string]string `json:"params,omitempty" jsonschema:"Flow parameters as key-value pairs (for routine mode)"`
	Instructions string            `json:"instructions,omitempty" jsonschema:"Task instructions for the AI agent (required for adaptive mode). Be specific and detailed. MUST include the exact output format the user last saw, with a concrete example copied from the conversation."`
	Channel      string            `json:"channel,omitempty" jsonschema:"Optional label for delivery channel (e.g. 'telegram'). Results are broadcast to all active channels automatically."`
	Target       string            `json:"target,omitempty" jsonschema:"Optional target ID. Results are broadcast to all active channels automatically — you do NOT need to provide this."`
	TestFirst    bool              `json:"test_first,omitempty" jsonschema:"If true, execute immediately to test, then ask user to confirm before enabling. Only set this AFTER the user has approved the test plan."`
}

type ScheduleJobResult struct {
	Status  string `json:"status"`
	JobID   string `json:"job_id,omitempty"`
	Message string `json:"message"`
	Result  string `json:"result,omitempty"` // Only populated when test_first=true
}

func scheduleJob(ctx tool.Context, args ScheduleJobArgs) (ScheduleJobResult, error) {
	if schedulerAccessVar == nil {
		return ScheduleJobResult{}, fmt.Errorf("scheduler is not available — the daemon must be running with scheduler enabled")
	}

	// Validate mode
	if args.Mode != "routine" && args.Mode != "adaptive" {
		return ScheduleJobResult{
			Status:  "error",
			Message: "Mode must be 'routine' or 'adaptive'. Note: 'fleet_poll' jobs are created automatically when a fleet plan is activated, not via this tool.",
		}, nil
	}

	// Validate required fields per mode
	if args.Mode == "routine" && args.Flow == "" {
		return ScheduleJobResult{
			Status:  "error",
			Message: "Routine mode requires a 'flow' name. If no flow exists for this task, call `distill_flow` first to create one from the conversation, then use the resulting flow name here.",
		}, nil
	}
	if args.Mode == "adaptive" && args.Instructions == "" {
		return ScheduleJobResult{
			Status:  "error",
			Message: "Adaptive mode requires 'instructions'",
		}, nil
	}

	// Validate cron expression
	if err := schedulerAccessVar.ValidateCron(args.Schedule); err != nil {
		return ScheduleJobResult{
			Status:  "error",
			Message: fmt.Sprintf("Invalid cron expression: %v. Use 5-field format: minute hour day-of-month month day-of-week", err),
		}, nil
	}

	// Validate timezone if provided
	if args.Timezone != "" {
		if _, err := time.LoadLocation(args.Timezone); err != nil {
			return ScheduleJobResult{
				Status:  "error",
				Message: fmt.Sprintf("Invalid timezone %q: %v", args.Timezone, err),
			}, nil
		}
	}

	// Create job
	// Note: channel and target are stored but delivery is broadcast-based —
	// results go to all active channel targets regardless of these values.
	job := &SchedulerJob{
		Name:      args.Name,
		Mode:      args.Mode,
		Cron:      args.Schedule,
		Timezone:  args.Timezone,
		Flow:      args.Flow,
		Params:    args.Params,
		Instr:     args.Instructions,
		Channel:   args.Channel,
		Target:    args.Target,
		Enabled:   !args.TestFirst, // Disabled if test_first, enabled otherwise
		TestFirst: args.TestFirst,
	}

	// Add job to store
	if err := schedulerAccessVar.AddJob(job); err != nil {
		return ScheduleJobResult{
			Status:  "error",
			Message: fmt.Sprintf("Failed to create job: %v", err),
		}, nil
	}

	// If test_first, execute immediately and return result
	if args.TestFirst {
		result, execErr := schedulerAccessVar.RunNow(ctx, job.ID)
		status := "test_complete"
		msg := fmt.Sprintf("Job %q created and tested. The job is currently DISABLED — ask the user if the test result looks good, and if so, call update_scheduled_job to enable it.", args.Name)
		if execErr != nil {
			status = "test_failed"
			msg = fmt.Sprintf("Job %q created but test execution failed: %v. The job is DISABLED. Ask the user if they want to fix the issue or remove the job.", args.Name, execErr)
		}

		return ScheduleJobResult{
			Status:  status,
			JobID:   job.ID,
			Message: msg,
			Result:  result,
		}, nil
	}

	return ScheduleJobResult{
		Status:  "created",
		JobID:   job.ID,
		Message: fmt.Sprintf("Job %q created and enabled. Schedule: %s (%s)", args.Name, args.Schedule, timezoneLabel(args.Timezone)),
	}, nil
}

// --- list_scheduled_jobs tool ---

type ListScheduledJobsArgs struct {
	Filter string `json:"filter,omitempty" jsonschema:"Optional filter to match jobs by name or mode (e.g., 'daily-report', 'flow'). If empty, lists all jobs."`
}

type ListScheduledJobsResult struct {
	Jobs    []JobSummary `json:"jobs"`
	Count   int          `json:"count"`
	Message string       `json:"message,omitempty"`
}

type JobSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Mode     string `json:"mode"`
	Schedule string `json:"schedule"`
	Timezone string `json:"timezone,omitempty"`
	PlanKey  string `json:"plan_key,omitempty"` // fleet plan key for fleet_poll jobs
	Enabled  bool   `json:"enabled"`
	Status   string `json:"last_status"`
	NextRun  string `json:"next_run,omitempty"`
	Failures int    `json:"consecutive_failures,omitempty"`
}

func listScheduledJobs(ctx tool.Context, args ListScheduledJobsArgs) (ListScheduledJobsResult, error) {
	if schedulerAccessVar == nil {
		return ListScheduledJobsResult{
			Message: "Scheduler is not available — the daemon must be running with scheduler enabled",
		}, nil
	}

	jobs := schedulerAccessVar.ListJobs()
	summaries := make([]JobSummary, 0, len(jobs))
	for _, j := range jobs {
		if args.Filter != "" && !strings.Contains(j.Name, args.Filter) && !strings.Contains(j.Mode, args.Filter) {
			continue
		}
		summary := JobSummary{
			ID:       j.ID,
			Name:     j.Name,
			Mode:     j.Mode,
			Schedule: j.Cron,
			Timezone: j.Timezone,
			Enabled:  j.Enabled,
			Status:   j.LastStatus,
			Failures: j.Failures,
		}
		// For fleet_poll jobs, the Flow field holds the plan key
		if j.Mode == "fleet_poll" {
			summary.PlanKey = j.Flow
		}
		if j.NextRun != nil {
			summary.NextRun = j.NextRun.Format(time.RFC3339)
		}
		summaries = append(summaries, summary)
	}

	msg := fmt.Sprintf("%d scheduled job(s)", len(summaries))
	if len(summaries) == 0 {
		if args.Filter != "" {
			msg = fmt.Sprintf("No scheduled jobs matching %q", args.Filter)
		} else {
			msg = "No scheduled jobs"
		}
	}

	return ListScheduledJobsResult{
		Jobs:    summaries,
		Count:   len(summaries),
		Message: msg,
	}, nil
}

// --- remove_scheduled_job tool ---

type RemoveScheduledJobArgs struct {
	JobID   string `json:"job_id,omitempty" jsonschema:"The job ID to remove"`
	JobName string `json:"job_name,omitempty" jsonschema:"The job name to remove (alternative to job_id)"`
}

type RemoveScheduledJobResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func removeScheduledJob(ctx tool.Context, args RemoveScheduledJobArgs) (RemoveScheduledJobResult, error) {
	if schedulerAccessVar == nil {
		return RemoveScheduledJobResult{
			Status:  "error",
			Message: "Scheduler is not available",
		}, nil
	}

	jobID := args.JobID
	if jobID == "" && args.JobName != "" {
		// Look up by name
		job := schedulerAccessVar.GetJobByName(args.JobName)
		if job == nil {
			return RemoveScheduledJobResult{
				Status:  "error",
				Message: fmt.Sprintf("No job found with name %q", args.JobName),
			}, nil
		}
		jobID = job.ID
	}

	if jobID == "" {
		return RemoveScheduledJobResult{
			Status:  "error",
			Message: "Either job_id or job_name is required",
		}, nil
	}

	if err := schedulerAccessVar.RemoveJob(jobID); err != nil {
		return RemoveScheduledJobResult{
			Status:  "error",
			Message: fmt.Sprintf("Failed to remove job: %v", err),
		}, nil
	}

	return RemoveScheduledJobResult{
		Status:  "removed",
		Message: "Job removed successfully",
	}, nil
}

// --- update_scheduled_job tool ---

type UpdateScheduledJobArgs struct {
	JobID    string `json:"job_id" jsonschema:"The job ID to update"`
	Enabled  *bool  `json:"enabled,omitempty" jsonschema:"Enable or disable the job"`
	Schedule string `json:"schedule,omitempty" jsonschema:"New cron expression"`
	Timezone string `json:"timezone,omitempty" jsonschema:"New timezone"`
	Name     string `json:"name,omitempty" jsonschema:"New job name"`
}

type UpdateScheduledJobResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func updateScheduledJob(ctx tool.Context, args UpdateScheduledJobArgs) (UpdateScheduledJobResult, error) {
	if schedulerAccessVar == nil {
		return UpdateScheduledJobResult{
			Status:  "error",
			Message: "Scheduler is not available",
		}, nil
	}

	jobs := schedulerAccessVar.ListJobs()
	var job *SchedulerJob
	for _, j := range jobs {
		if j.ID == args.JobID {
			job = j
			break
		}
	}
	if job == nil {
		return UpdateScheduledJobResult{
			Status:  "error",
			Message: fmt.Sprintf("Job %q not found", args.JobID),
		}, nil
	}

	// Apply updates
	changes := []string{}
	if args.Enabled != nil {
		job.Enabled = *args.Enabled
		if *args.Enabled {
			changes = append(changes, "enabled")
		} else {
			changes = append(changes, "disabled")
		}
	}
	if args.Schedule != "" {
		if err := schedulerAccessVar.ValidateCron(args.Schedule); err != nil {
			return UpdateScheduledJobResult{
				Status:  "error",
				Message: fmt.Sprintf("Invalid cron: %v", err),
			}, nil
		}
		job.Cron = args.Schedule
		changes = append(changes, "schedule updated")
	}
	if args.Timezone != "" {
		if _, err := time.LoadLocation(args.Timezone); err != nil {
			return UpdateScheduledJobResult{
				Status:  "error",
				Message: fmt.Sprintf("Invalid timezone: %v", err),
			}, nil
		}
		job.Timezone = args.Timezone
		changes = append(changes, "timezone updated")
	}
	if args.Name != "" {
		job.Name = args.Name
		changes = append(changes, "renamed")
	}

	if err := schedulerAccessVar.UpdateJob(job); err != nil {
		return UpdateScheduledJobResult{
			Status:  "error",
			Message: fmt.Sprintf("Failed to update: %v", err),
		}, nil
	}

	msg := fmt.Sprintf("Job %q updated: %s", job.Name, strings.Join(changes, ", "))
	// Add a note for fleet_poll jobs that schedule changes should ideally
	// be done through deactivate/reactivate to stay in sync with the plan.
	if job.Mode == "fleet_poll" && args.Schedule != "" {
		msg += ". Note: for fleet_poll jobs, consider deactivating and reactivating the fleet plan to keep the plan config in sync."
	}

	return UpdateScheduledJobResult{
		Status:  "updated",
		Message: msg,
	}, nil
}

// --- Tool constructors ---

// NewScheduleJobTool creates the schedule_job tool.
func NewScheduleJobTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "schedule_job",
		Description: `Create a scheduled job on a cron schedule. Modes: "routine" (run a saved flow with fixed params) or "adaptive" (LLM-driven agentic execution with instructions). Uses 5-field cron syntax. Set test_first=true to test before enabling. Results broadcast to all active channels automatically. Only call after user approval.`,
	}, scheduleJob)
}

// NewListScheduledJobsTool creates the list_scheduled_jobs tool.
func NewListScheduledJobsTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "list_scheduled_jobs",
		Description: "List all scheduled jobs with their status, next run time, and configuration.",
	}, listScheduledJobs)
}

// NewRemoveScheduledJobTool creates the remove_scheduled_job tool.
func NewRemoveScheduledJobTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "remove_scheduled_job",
		Description: "Remove a scheduled job by ID or name. The job will no longer execute.",
	}, removeScheduledJob)
}

// NewUpdateScheduledJobTool creates the update_scheduled_job tool.
func NewUpdateScheduledJobTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "update_scheduled_job",
		Description: "Update a scheduled job. Can enable/disable, change schedule, timezone, or name. Works for all job modes (routine, adaptive, fleet_poll). For fleet_poll jobs, schedule changes should ideally be done by deactivating and reactivating the fleet plan.",
	}, updateScheduledJob)
}

// GetSchedulerTools returns all scheduler tools. These should be appended to the
// tool list in the chat factory when the scheduler is available.
func GetSchedulerTools() ([]tool.Tool, error) {
	var tools []tool.Tool

	schedTool, err := NewScheduleJobTool()
	if err != nil {
		return nil, fmt.Errorf("schedule_job: %w", err)
	}
	tools = append(tools, schedTool)

	listTool, err := NewListScheduledJobsTool()
	if err != nil {
		return nil, fmt.Errorf("list_scheduled_jobs: %w", err)
	}
	tools = append(tools, listTool)

	removeTool, err := NewRemoveScheduledJobTool()
	if err != nil {
		return nil, fmt.Errorf("remove_scheduled_job: %w", err)
	}
	tools = append(tools, removeTool)

	updateTool, err := NewUpdateScheduledJobTool()
	if err != nil {
		return nil, fmt.Errorf("update_scheduled_job: %w", err)
	}
	tools = append(tools, updateTool)

	return tools, nil
}

// timezoneLabel returns a human-friendly timezone label.
func timezoneLabel(tz string) string {
	if tz == "" {
		return "local timezone"
	}
	return tz
}

// SchedulerHTTPAccess implements SchedulerAccess by talking to the daemon's
// HTTP API. Used by the console/chat when running outside the daemon process.
type SchedulerHTTPAccess struct {
	BaseURL string // e.g., "http://localhost:9393"
}

func (s *SchedulerHTTPAccess) AddJob(job *SchedulerJob) error {
	data, _ := json.Marshal(job)
	resp, err := http.Post(s.BaseURL+"/api/scheduler/jobs", "application/json", strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("failed to contact daemon: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("daemon returned status %d", resp.StatusCode)
	}
	// Read back the created job to get the ID
	var created SchedulerJob
	if err := json.NewDecoder(resp.Body).Decode(&created); err == nil && created.ID != "" {
		job.ID = created.ID
	}
	return nil
}

func (s *SchedulerHTTPAccess) ListJobs() []*SchedulerJob {
	resp, err := http.Get(s.BaseURL + "/api/scheduler/jobs")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var result struct {
		Jobs []*SchedulerJob `json:"jobs"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Jobs
}

func (s *SchedulerHTTPAccess) RemoveJob(id string) error {
	req, err := http.NewRequest(http.MethodDelete, s.BaseURL+"/api/scheduler/jobs/"+id, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon returned status %d", resp.StatusCode)
	}
	return nil
}

func (s *SchedulerHTTPAccess) UpdateJob(job *SchedulerJob) error {
	data, _ := json.Marshal(job)
	req, err := http.NewRequest(http.MethodPut, s.BaseURL+"/api/scheduler/jobs/"+job.ID, strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon returned status %d", resp.StatusCode)
	}
	return nil
}

func (s *SchedulerHTTPAccess) RunNow(ctx context.Context, jobID string) (string, error) {
	resp, err := http.Post(s.BaseURL+"/api/scheduler/jobs/"+jobID+"/run", "application/json", nil)
	if err != nil {
		return "", fmt.Errorf("failed to contact daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("daemon returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Result string `json:"result"`
		Error  string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Error != "" {
		return result.Result, fmt.Errorf("%s", result.Error)
	}
	return result.Result, nil
}

func (s *SchedulerHTTPAccess) GetJobByName(name string) *SchedulerJob {
	jobs := s.ListJobs()
	for _, j := range jobs {
		if strings.EqualFold(j.Name, name) {
			return j
		}
	}
	return nil
}

func (s *SchedulerHTTPAccess) ValidateCron(expr string) error {
	// Validate locally — no need to call daemon for this
	return validateCronExpr(expr)
}

// validateCronExpr validates a 5-field cron expression locally.
func validateCronExpr(expr string) error {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return fmt.Errorf("expected 5 fields (minute hour day month weekday), got %d", len(fields))
	}
	// Basic validation — the full validation happens server-side via robfig/cron
	return nil
}
