package fleet

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// SchedulerAccess abstracts scheduler operations for the PlanActivator.
// This avoids import cycles between fleet and scheduler/tools packages.
// The daemon bridges the concrete implementation.
type SchedulerAccess interface {
	AddJob(job *SchedulerJob) error
	RemoveJob(id string) error
	GetJob(id string) *SchedulerJob
	ValidateCron(expr string) error
}

// SchedulerJob is a minimal job representation for fleet plan activation.
type SchedulerJob struct {
	ID      string
	Name    string
	Mode    string // "fleet_poll"
	Cron    string
	Flow    string // holds the plan key for fleet_poll mode
	Enabled bool
}

// FleetStartFunc is a function that starts a headless fleet session.
// Returns the session ID of the created session.
// It is injected by the daemon/API layer to avoid import cycles.
type FleetStartFunc func(ctx context.Context, cfg HeadlessFleetConfig) (string, error)

// FleetRecoverFunc is a function that recovers an interrupted fleet session
// after a daemon restart by reading the JSONL transcript and resuming.
type FleetRecoverFunc func(ctx context.Context, cfg RecoverFleetConfig) error

// HeadlessFleetConfig holds parameters for starting a headless fleet session.
type HeadlessFleetConfig struct {
	Plan           *FleetPlan
	InitialMsg     string // the formatted issue context or trigger message
	IssueNumber    int    // GitHub issue number (0 if not issue-triggered)
	Repo           string // GitHub repo "owner/repo"
	CompletionFunc func() // called when the session finishes (for MarkCompleted)
}

// RecoverFleetConfig holds parameters for recovering an interrupted fleet session.
type RecoverFleetConfig struct {
	Plan           *FleetPlan
	SessionID      string // original session ID (to resume the same transcript)
	IssueNumber    int
	Repo           string
	CompletionFunc func() // called when the recovered session finishes
}

// PlanActivator manages the lifecycle of fleet plan activations.
// It creates scheduler jobs for activated plans and removes them on deactivation.
type PlanActivator struct {
	planRegistry *PlanRegistry
	scheduler    SchedulerAccess
	fleetStart   FleetStartFunc
	fleetRecover FleetRecoverFunc

	// monitors tracks active GitHub monitors by plan key
	monitors   map[string]*GitHubMonitor
	monitorsMu sync.RWMutex
}

// NewPlanActivator creates a new activator.
func NewPlanActivator(planReg *PlanRegistry, sched SchedulerAccess, fleetStart FleetStartFunc) *PlanActivator {
	return &PlanActivator{
		planRegistry: planReg,
		scheduler:    sched,
		fleetStart:   fleetStart,
		monitors:     make(map[string]*GitHubMonitor),
	}
}

// SetRecoverFunc sets the function used to recover interrupted fleet sessions.
// Called by the daemon after the plan activator is created, since the recover
// function depends on API-layer components that initialize later.
func (a *PlanActivator) SetRecoverFunc(fn FleetRecoverFunc) {
	a.fleetRecover = fn
}

// Activate activates a fleet plan by creating a scheduler job that polls
// the plan's configured channel on the configured schedule.
func (a *PlanActivator) Activate(ctx context.Context, planKey string) error {
	plan, ok := a.planRegistry.GetPlan(planKey)
	if !ok {
		return fmt.Errorf("fleet plan %q not found", planKey)
	}

	if plan.Channel.IsChat() {
		return fmt.Errorf("chat-based plans do not need activation (they are started manually)")
	}

	if plan.IsActivated() {
		return fmt.Errorf("plan %q is already activated (job ID: %s)", planKey, plan.Activation.SchedulerJobID)
	}

	// Validate cron schedule
	schedule := plan.Channel.Schedule
	if schedule == "" {
		schedule = "*/5 * * * *" // default: every 5 minutes
	}
	if err := a.scheduler.ValidateCron(schedule); err != nil {
		return fmt.Errorf("invalid cron schedule %q: %w", schedule, err)
	}

	// For GitHub Issues: initialize the monitor and mark existing issues as seen
	// so we only trigger on NEW issues created after activation.
	if plan.Channel.Type == "github_issues" {
		monitor := NewGitHubMonitor(planKey, plan.Channel.Config, a.planRegistry.Dir())
		if err := monitor.LoadState(); err != nil {
			log.Printf("[plan-activator] Warning: failed to load monitor state for %q: %v", planKey, err)
		}
		if err := monitor.MarkAllCurrentAsSeen(); err != nil {
			return fmt.Errorf("failed to snapshot current issues (needed to avoid processing backlog): %w", err)
		}

		a.monitorsMu.Lock()
		a.monitors[planKey] = monitor
		a.monitorsMu.Unlock()
	}

	// Create scheduler job
	job := &SchedulerJob{
		Name:    fmt.Sprintf("fleet-plan:%s", planKey),
		Mode:    "fleet_poll",
		Cron:    schedule,
		Flow:    planKey, // plan key stored in Flow field
		Enabled: true,
	}

	if err := a.scheduler.AddJob(job); err != nil {
		return fmt.Errorf("failed to create scheduler job: %w", err)
	}

	// Update plan activation state
	now := time.Now()
	plan.Activation = PlanActivationState{
		Activated:      true,
		SchedulerJobID: job.ID,
		ActivatedAt:    now,
	}
	plan.UpdatedAt = now

	if err := a.planRegistry.Save(plan); err != nil {
		// Rollback: remove the scheduler job
		_ = a.scheduler.RemoveJob(job.ID)
		return fmt.Errorf("failed to save activation state: %w", err)
	}

	log.Printf("[plan-activator] Plan %q activated with scheduler job %s (cron: %s)", planKey, job.ID, schedule)
	return nil
}

// Deactivate deactivates a fleet plan by removing its scheduler job.
func (a *PlanActivator) Deactivate(_ context.Context, planKey string) error {
	plan, ok := a.planRegistry.GetPlan(planKey)
	if !ok {
		return fmt.Errorf("fleet plan %q not found", planKey)
	}

	if !plan.IsActivated() {
		return fmt.Errorf("plan %q is not activated", planKey)
	}

	// Remove scheduler job
	if plan.Activation.SchedulerJobID != "" {
		if err := a.scheduler.RemoveJob(plan.Activation.SchedulerJobID); err != nil {
			log.Printf("[plan-activator] Warning: failed to remove scheduler job %s: %v", plan.Activation.SchedulerJobID, err)
		}
	}

	// Remove monitor
	a.monitorsMu.Lock()
	delete(a.monitors, planKey)
	a.monitorsMu.Unlock()

	// Update plan
	plan.Activation = PlanActivationState{} // clear all activation state
	plan.UpdatedAt = time.Now()

	if err := a.planRegistry.Save(plan); err != nil {
		return fmt.Errorf("failed to save deactivation state: %w", err)
	}

	log.Printf("[plan-activator] Plan %q deactivated", planKey)
	return nil
}

// PlanActivationStatus holds the current status of a plan's activation.
type PlanActivationStatus struct {
	Activated       bool      `json:"activated"`
	SchedulerJobID  string    `json:"scheduler_job_id,omitempty"`
	ActivatedAt     time.Time `json:"activated_at,omitempty"`
	LastPollAt      time.Time `json:"last_poll_at,omitempty"`
	LastPollStatus  string    `json:"last_poll_status,omitempty"`
	LastPollError   string    `json:"last_poll_error,omitempty"`
	SessionsStarted int       `json:"sessions_started,omitempty"`
}

// Status returns the activation status of a fleet plan.
func (a *PlanActivator) Status(planKey string) (*PlanActivationStatus, error) {
	plan, ok := a.planRegistry.GetPlan(planKey)
	if !ok {
		return nil, fmt.Errorf("fleet plan %q not found", planKey)
	}

	return &PlanActivationStatus{
		Activated:       plan.Activation.Activated,
		SchedulerJobID:  plan.Activation.SchedulerJobID,
		ActivatedAt:     plan.Activation.ActivatedAt,
		LastPollAt:      plan.Activation.LastPollAt,
		LastPollStatus:  plan.Activation.LastPollStatus,
		LastPollError:   plan.Activation.LastPollError,
		SessionsStarted: plan.Activation.SessionsStarted,
	}, nil
}

// RestoreActivated re-creates monitors for plans that were activated before
// a daemon restart. Called during startup. It verifies that the corresponding
// scheduler job still exists; if not, it re-creates the job to heal the state.
func (a *PlanActivator) RestoreActivated() error {
	plans := a.planRegistry.ListPlans()
	restored := 0

	for _, summary := range plans {
		plan, ok := a.planRegistry.GetPlan(summary.Key)
		if !ok {
			continue
		}
		if !plan.IsActivated() {
			continue
		}

		// Verify the scheduler job still exists. If it was lost (e.g., manual
		// deletion, corrupted jobs.json), re-create it so polling resumes.
		if plan.Activation.SchedulerJobID != "" {
			existing := a.scheduler.GetJob(plan.Activation.SchedulerJobID)
			if existing == nil {
				log.Printf("[plan-activator] Scheduler job %s for plan %q is missing, re-creating...",
					plan.Activation.SchedulerJobID, summary.Key)

				schedule := plan.Channel.Schedule
				if schedule == "" {
					schedule = "*/5 * * * *"
				}

				job := &SchedulerJob{
					Name:    fmt.Sprintf("fleet-plan:%s", summary.Key),
					Mode:    "fleet_poll",
					Cron:    schedule,
					Flow:    summary.Key,
					Enabled: true,
				}

				if err := a.scheduler.AddJob(job); err != nil {
					log.Printf("[plan-activator] Failed to re-create scheduler job for %q: %v", summary.Key, err)
					// Clear activation state since we can't restore the job
					plan.Activation = PlanActivationState{}
					plan.UpdatedAt = time.Now()
					_ = a.planRegistry.Save(plan)
					continue
				}

				// Update the job ID in the plan
				plan.Activation.SchedulerJobID = job.ID
				plan.UpdatedAt = time.Now()
				_ = a.planRegistry.Save(plan)

				log.Printf("[plan-activator] Re-created scheduler job %s for plan %q (cron: %s)",
					job.ID, summary.Key, schedule)
			}
		}

		// Re-create the monitor for this plan
		if plan.Channel.Type == "github_issues" {
			monitor := NewGitHubMonitor(summary.Key, plan.Channel.Config, a.planRegistry.Dir())
			if err := monitor.LoadState(); err != nil {
				log.Printf("[plan-activator] Warning: failed to load monitor state for %q: %v", summary.Key, err)
			}

			a.monitorsMu.Lock()
			a.monitors[summary.Key] = monitor
			a.monitorsMu.Unlock()
		}

		restored++
		log.Printf("[plan-activator] Restored activated plan %q (job: %s)", summary.Key, plan.Activation.SchedulerJobID)

		// Check for interrupted fleet sessions that need recovery.
		if plan.Channel.Type == "github_issues" && a.fleetRecover != nil {
			a.monitorsMu.RLock()
			mon := a.monitors[summary.Key]
			a.monitorsMu.RUnlock()

			if mon != nil {
				inProgress := mon.GetInProgressIssues()
				for _, ip := range inProgress {
					repo := getConfigString(plan.Channel.Config, "repo")
					issueNum := ip.IssueNumber

					log.Printf("[plan-activator] Recovering interrupted session %s for issue #%d (plan %q)",
						ip.SessionID, issueNum, summary.Key)

					recoverCfg := RecoverFleetConfig{
						Plan:        plan,
						SessionID:   ip.SessionID,
						IssueNumber: issueNum,
						Repo:        repo,
						CompletionFunc: func() {
							mon.MarkCompleted(issueNum)
						},
					}
					if err := a.fleetRecover(context.Background(), recoverCfg); err != nil {
						log.Printf("[plan-activator] Failed to recover session %s for issue #%d: %v",
							ip.SessionID, issueNum, err)
						// Mark as completed so we don't retry endlessly
						mon.MarkCompleted(issueNum)
					}
				}
			}
		}
	}

	if restored > 0 {
		log.Printf("[plan-activator] Restored %d activated plan(s)", restored)
	}
	return nil
}

// Poll is called by the scheduler executor when a fleet_poll job fires.
// It checks for new items on the plan's channel and starts fleet sessions.
func (a *PlanActivator) Poll(ctx context.Context, planKey string) (string, error) {
	plan, ok := a.planRegistry.GetPlan(planKey)
	if !ok {
		return "", fmt.Errorf("fleet plan %q not found", planKey)
	}

	if !plan.IsActivated() {
		return "plan not activated, skipping", nil
	}

	switch plan.Channel.Type {
	case "github_issues":
		return a.pollGitHubIssues(ctx, planKey, plan)
	default:
		return "", fmt.Errorf("unsupported channel type for polling: %q", plan.Channel.Type)
	}
}

// pollGitHubIssues polls for new GitHub issues and starts fleet sessions.
func (a *PlanActivator) pollGitHubIssues(ctx context.Context, planKey string, plan *FleetPlan) (string, error) {
	a.monitorsMu.RLock()
	monitor, ok := a.monitors[planKey]
	a.monitorsMu.RUnlock()

	if !ok {
		return "", fmt.Errorf("no monitor found for plan %q (was it activated properly?)", planKey)
	}

	// Poll for new issues
	newIssues, err := monitor.Poll()

	// Update plan's poll state regardless of result
	now := time.Now()
	plan.Activation.LastPollAt = now
	if err != nil {
		plan.Activation.LastPollStatus = "failed"
		plan.Activation.LastPollError = err.Error()
		_ = a.planRegistry.Save(plan)
		return "", fmt.Errorf("polling GitHub issues: %w", err)
	}

	if len(newIssues) == 0 {
		plan.Activation.LastPollStatus = "no_new_items"
		plan.Activation.LastPollError = ""
		_ = a.planRegistry.Save(plan)
		return "no new issues", nil
	}

	plan.Activation.LastPollStatus = "success"
	plan.Activation.LastPollError = ""

	// Start a fleet session for each new issue
	repo := getConfigString(plan.Channel.Config, "repo")
	started := 0

	for _, issue := range newIssues {
		// Format the issue as the initial message
		initialMsg := FormatIssueContext(issue, repo)

		// Start a headless fleet session first to get the session ID.
		// We need the session ID before marking in-progress so the monitor
		// state has the correct reference for recovery after restart.
		if a.fleetStart != nil {
			issueNum := issue.Number
			cfg := HeadlessFleetConfig{
				Plan:        plan,
				InitialMsg:  initialMsg,
				IssueNumber: issueNum,
				Repo:        repo,
				CompletionFunc: func() {
					monitor.MarkCompleted(issueNum)
				},
			}
			sessionID, startErr := a.fleetStart(ctx, cfg)
			if startErr != nil {
				log.Printf("[plan-activator] Failed to start fleet session for issue #%d: %v", issue.Number, startErr)
				continue
			}
			monitor.MarkInProgress(issue.Number, sessionID)
			started++
			log.Printf("[plan-activator] Started fleet session %s for issue #%d (%s)", sessionID, issue.Number, issue.Title)
		}
	}

	plan.Activation.SessionsStarted += started
	_ = a.planRegistry.Save(plan)

	return fmt.Sprintf("found %d new issue(s), started %d fleet session(s)", len(newIssues), started), nil
}

// GetMonitor returns the GitHub monitor for a plan (for testing/inspection).
func (a *PlanActivator) GetMonitor(planKey string) *GitHubMonitor {
	a.monitorsMu.RLock()
	defer a.monitorsMu.RUnlock()
	return a.monitors[planKey]
}
