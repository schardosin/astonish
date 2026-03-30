package fleet

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SchedulerAccess abstracts scheduler operations for the PlanActivator.
// This avoids import cycles between fleet and scheduler/tools packages.
// The daemon bridges the concrete implementation.
type SchedulerAccess interface {
	AddJob(job *SchedulerJob) error
	RemoveJob(id string) error
	RemoveJobByName(name string) error
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
	InitialMsg     string                             // the formatted issue context or trigger message
	IssueNumber    int                                // GitHub issue number (0 if not issue-triggered)
	IssueTitle     string                             // GitHub issue title (for task slug derivation)
	Repo           string                             // GitHub repo "owner/repo"
	GHToken        string                             // resolved GitHub token (injected as GH_TOKEN)
	CompletionFunc func(err error)                    // called when the session finishes (nil=success, non-nil=failed)
	BallChangeFunc func(ball string, commentID int64) // called when ball moves between agents/human
}

// RecoverFleetConfig holds parameters for recovering an interrupted fleet session.
type RecoverFleetConfig struct {
	Plan            *FleetPlan
	SessionID       string // original session ID (to resume the same transcript)
	IssueNumber     int
	IssueTitle      string // GitHub issue title (for task slug derivation)
	Repo            string
	GHToken         string                             // resolved GitHub token (injected as GH_TOKEN)
	CustomerMessage string                             // customer comment that triggered recovery (empty for restart recovery)
	CompletionFunc  func(err error)                    // called when the recovered session finishes
	BallChangeFunc  func(ball string, commentID int64) // called when ball moves between agents/human
}

// GHTokenResolverFunc resolves the GitHub token for a fleet plan by looking
// up the plan's credentials map in the encrypted credential store.
// Returns empty string if no GitHub credential is configured or available.
type GHTokenResolverFunc func(plan *FleetPlan) string

// PlanActivator manages the lifecycle of fleet plan activations.
// It creates scheduler jobs for activated plans and removes them on deactivation.
type PlanActivator struct {
	planRegistryFn  func() *PlanRegistry
	scheduler       SchedulerAccess
	fleetStart      FleetStartFunc
	fleetRecover    FleetRecoverFunc
	ghTokenResolver GHTokenResolverFunc

	// sessionRegistry provides active session lookup for CheckForWork.
	// Set by the daemon after initialization via SetSessionRegistry.
	sessionRegistry *SessionRegistry

	// monitors tracks active GitHub monitors by plan key
	monitors   map[string]*GitHubMonitor
	monitorsMu sync.RWMutex
}

// NewPlanActivator creates a new activator.
// The registryFn is called each time the activator needs the PlanRegistry,
// ensuring it always sees the current instance even if the package-level
// variable is replaced (e.g., when the Studio lazy chat init re-creates it).
func NewPlanActivator(registryFn func() *PlanRegistry, sched SchedulerAccess, fleetStart FleetStartFunc) *PlanActivator {
	return &PlanActivator{
		planRegistryFn: registryFn,
		scheduler:      sched,
		fleetStart:     fleetStart,
		monitors:       make(map[string]*GitHubMonitor),
	}
}

// registry returns the current PlanRegistry instance.
func (a *PlanActivator) registry() *PlanRegistry {
	return a.planRegistryFn()
}

// SetRecoverFunc sets the function used to recover interrupted fleet sessions.
// Called by the daemon after the plan activator is created, since the recover
// function depends on API-layer components that initialize later.
func (a *PlanActivator) SetRecoverFunc(fn FleetRecoverFunc) {
	a.fleetRecover = fn
}

// SetGHTokenResolver sets the function used to resolve GitHub tokens for fleet plans.
// Called by the daemon after the plan activator is created.
func (a *PlanActivator) SetGHTokenResolver(fn GHTokenResolverFunc) {
	a.ghTokenResolver = fn
}

// SetSessionRegistry sets the session registry for active session lookups.
// Called by the daemon after the plan activator is created.
func (a *PlanActivator) SetSessionRegistry(reg *SessionRegistry) {
	a.sessionRegistry = reg
}

// ResolveGHTokenForPlan resolves the GitHub token for a fleet plan.
func (a *PlanActivator) ResolveGHTokenForPlan(plan *FleetPlan) string {
	if a.ghTokenResolver != nil {
		return a.ghTokenResolver(plan)
	}
	return ""
}

// Activate activates a fleet plan by creating a scheduler job that polls
// the plan's configured channel on the configured schedule.
func (a *PlanActivator) Activate(ctx context.Context, planKey string) error {
	plan, ok := a.registry().GetPlan(planKey)
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
	schedule := plan.Channel.GetSchedule()
	if err := a.scheduler.ValidateCron(schedule); err != nil {
		return fmt.Errorf("invalid cron schedule %q: %w", schedule, err)
	}

	// For GitHub Issues: initialize the monitor and mark existing issues as seen
	// so we only trigger on NEW issues created after activation.
	if plan.Channel.Type == "github_issues" {
		monitor := NewGitHubMonitor(planKey, plan.Channel.Config, a.registry().Dir())
		monitor.GHToken = a.ResolveGHTokenForPlan(plan)
		if err := monitor.LoadState(); err != nil {
			slog.Warn("failed to load monitor state", "component", "plan-activator", "plan", planKey, "error", err)
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

	if err := a.registry().Save(plan); err != nil {
		// Rollback: remove the scheduler job
		_ = a.scheduler.RemoveJob(job.ID)
		return fmt.Errorf("failed to save activation state: %w", err)
	}

	slog.Info("plan activated", "component", "plan-activator", "plan", planKey, "job_id", job.ID, "cron", schedule)
	return nil
}

// Deactivate deactivates a fleet plan by removing its scheduler job.
func (a *PlanActivator) Deactivate(_ context.Context, planKey string) error {
	plan, ok := a.registry().GetPlan(planKey)
	if !ok {
		return fmt.Errorf("fleet plan %q not found", planKey)
	}

	if !plan.IsActivated() {
		return fmt.Errorf("plan %q is not activated", planKey)
	}

	// Remove scheduler job
	if plan.Activation.SchedulerJobID != "" {
		if err := a.scheduler.RemoveJob(plan.Activation.SchedulerJobID); err != nil {
			slog.Warn("failed to remove scheduler job", "component", "plan-activator", "job_id", plan.Activation.SchedulerJobID, "error", err)
		}
	}

	// Remove monitor
	a.monitorsMu.Lock()
	delete(a.monitors, planKey)
	a.monitorsMu.Unlock()

	// Update plan
	plan.Activation = PlanActivationState{} // clear all activation state
	plan.UpdatedAt = time.Now()

	if err := a.registry().Save(plan); err != nil {
		return fmt.Errorf("failed to save deactivation state: %w", err)
	}

	slog.Info("plan deactivated", "component", "plan-activator", "plan", planKey)
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
	plan, ok := a.registry().GetPlan(planKey)
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
//
// Recovery of interrupted sessions is handled by the first poll cycle via
// CheckForWork — no separate recovery path needed.
func (a *PlanActivator) RestoreActivated() error {
	plans := a.registry().ListPlans()
	restored := 0

	for _, summary := range plans {
		plan, ok := a.registry().GetPlan(summary.Key)
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
				slog.Warn("scheduler job missing, re-creating", "component", "plan-activator", "job_id", plan.Activation.SchedulerJobID, "plan", summary.Key)

				schedule := plan.Channel.GetSchedule()

				job := &SchedulerJob{
					Name:    fmt.Sprintf("fleet-plan:%s", summary.Key),
					Mode:    "fleet_poll",
					Cron:    schedule,
					Flow:    summary.Key,
					Enabled: true,
				}

				if err := a.scheduler.AddJob(job); err != nil {
					slog.Error("failed to re-create scheduler job", "component", "plan-activator", "plan", summary.Key, "error", err)
					// Clear activation state since we can't restore the job
					plan.Activation = PlanActivationState{}
					plan.UpdatedAt = time.Now()
					if err := a.registry().Save(plan); err != nil {
						slog.Error("failed to save fleet plan state", "plan", plan.Name, "error", err)
					}
					continue
				}

				// Update the job ID in the plan
				plan.Activation.SchedulerJobID = job.ID
				plan.UpdatedAt = time.Now()
				if err := a.registry().Save(plan); err != nil {
					slog.Error("failed to save fleet plan state", "plan", plan.Name, "error", err)
				}

				slog.Info("re-created scheduler job", "component", "plan-activator", "job_id", job.ID, "plan", summary.Key, "cron", schedule)
			}
		}

		// Re-create the monitor for this plan
		if plan.Channel.Type == "github_issues" {
			monitor := NewGitHubMonitor(summary.Key, plan.Channel.Config, a.registry().Dir())
			monitor.GHToken = a.ResolveGHTokenForPlan(plan)
			if err := monitor.LoadState(); err != nil {
				slog.Warn("failed to load monitor state", "component", "plan-activator", "plan", summary.Key, "error", err)
			}

			a.monitorsMu.Lock()
			a.monitors[summary.Key] = monitor
			a.monitorsMu.Unlock()
		}

		restored++
		slog.Info("restored activated plan", "component", "plan-activator", "plan", summary.Key, "job_id", plan.Activation.SchedulerJobID)

		// NOTE: No explicit recovery of interrupted sessions here.
		// The first poll cycle (triggered by the scheduler within seconds)
		// runs CheckForWork which handles both new issues AND recovery of
		// interrupted sessions (issues with a sessionID but no active session).
	}

	if restored > 0 {
		slog.Info("restored activated plans", "component", "plan-activator", "count", restored)
	}
	return nil
}

// Poll is called by the scheduler executor when a fleet_poll job fires.
// It checks for new items on the plan's channel and starts fleet sessions.
func (a *PlanActivator) Poll(ctx context.Context, planKey string) (string, error) {
	plan, ok := a.registry().GetPlan(planKey)
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

// pollGitHubIssues uses CheckForWork to handle both new issues and customer
// replies in a single, unified pass.
func (a *PlanActivator) pollGitHubIssues(ctx context.Context, planKey string, plan *FleetPlan) (string, error) {
	a.monitorsMu.RLock()
	monitor, ok := a.monitors[planKey]
	a.monitorsMu.RUnlock()

	if !ok {
		return "", fmt.Errorf("no monitor found for plan %q (was it activated properly?)", planKey)
	}

	repo := GetConfigString(plan.Channel.Config, "repo")

	var (
		newStarts  int
		recoveries int
	)

	// isSessionActive checks if a session is currently running in the registry.
	isSessionActive := func(sessionID string) bool {
		if a.sessionRegistry == nil {
			return false
		}
		return a.sessionRegistry.IsFleetSession(sessionID)
	}

	err := monitor.CheckForWork(isSessionActive, func(item WorkItem) {
		if item.IsNewIssue {
			a.startNewSession(ctx, monitor, plan, repo, item)
			newStarts++
		} else if item.SessionID != "" {
			// Customer replied on a known issue with an existing session — recover it.
			a.recoverSession(ctx, monitor, plan, repo, item)
			recoveries++
		} else {
			// Customer replied on a known issue with no prior session
			// (e.g., pre-existing issue marked seen at activation time).
			// Start a fresh session.
			a.startNewSession(ctx, monitor, plan, repo, item)
			newStarts++
		}
	})

	// Update plan's poll state
	now := time.Now()
	plan.Activation.LastPollAt = now
	if err != nil {
		plan.Activation.LastPollStatus = "failed"
		plan.Activation.LastPollError = err.Error()
		if saveErr := a.registry().Save(plan); saveErr != nil {
			slog.Error("failed to save fleet plan state", "plan", plan.Name, "error", saveErr)
		}
		return "", fmt.Errorf("polling GitHub issues: %w", err)
	}

	var results []string
	if newStarts > 0 {
		plan.Activation.SessionsStarted += newStarts
		results = append(results, fmt.Sprintf("started %d new session(s)", newStarts))
	}
	if recoveries > 0 {
		results = append(results, fmt.Sprintf("recovered %d session(s)", recoveries))
	}
	if len(results) == 0 {
		results = append(results, "no new work")
	}

	plan.Activation.LastPollStatus = "success"
	plan.Activation.LastPollError = ""
	if err := a.registry().Save(plan); err != nil {
		slog.Error("failed to save fleet plan state", "plan", plan.Name, "error", err)
	}

	return strings.Join(results, "; "), nil
}

// startNewSession starts a fresh headless fleet session for a new GitHub issue.
func (a *PlanActivator) startNewSession(ctx context.Context, monitor *GitHubMonitor, plan *FleetPlan, repo string, item WorkItem) {
	if a.fleetStart == nil {
		return
	}

	// Fetch the full issue to get the body for FormatIssueContext.
	issues, err := monitor.fetchOpenLabeledIssues()
	if err != nil {
		slog.Error("failed to fetch issue details", "component", "plan-activator", "issue", item.IssueNumber, "error", err)
		return
	}

	var issue *GitHubIssue
	for i, iss := range issues {
		if iss.Number == item.IssueNumber {
			issue = &issues[i]
			break
		}
	}
	if issue == nil {
		slog.Warn("issue not found in fetched issues", "component", "plan-activator", "issue", item.IssueNumber)
		return
	}

	initialMsg := FormatIssueContext(*issue, repo)
	issueNum := item.IssueNumber

	cfg := HeadlessFleetConfig{
		Plan:        plan,
		InitialMsg:  initialMsg,
		IssueNumber: issueNum,
		IssueTitle:  issue.Title,
		Repo:        repo,
		GHToken:     a.ResolveGHTokenForPlan(plan),
		CompletionFunc: func(sessionErr error) {
			if sessionErr != nil {
				monitor.IncrementRetryCount(issueNum, sessionErr.Error())
			} else {
				monitor.ClearRetryOnSuccess(issueNum)
			}
		},
		BallChangeFunc: func(_ string, commentID int64) {
			monitor.UpdateCursor(issueNum, commentID)
		},
	}

	sessionID, startErr := a.fleetStart(ctx, cfg)
	if startErr != nil {
		slog.Error("failed to start fleet session", "component", "plan-activator", "issue", issueNum, "error", startErr)
		monitor.IncrementRetryCount(issueNum, fmt.Sprintf("start failed: %v", startErr))
		return
	}

	monitor.MarkSeen(issueNum, sessionID, issue.Title)
	slog.Info("started fleet session", "component", "plan-activator", "session_id", sessionID, "issue", issueNum, "title", issue.Title)
}

// recoverSession recovers an interrupted fleet session (daemon restart or customer reply).
func (a *PlanActivator) recoverSession(ctx context.Context, monitor *GitHubMonitor, plan *FleetPlan, repo string, item WorkItem) {
	if a.fleetRecover == nil {
		return
	}

	issueNum := item.IssueNumber
	sessionID := item.SessionID

	if sessionID == "" {
		slog.Error("cannot recover issue without session id", "component", "plan-activator", "issue", issueNum)
		return
	}

	logAction := "daemon restart"
	if item.CustomerReply != "" {
		logAction = "customer reply"
	}
	slog.Info("recovering session", "component", "plan-activator", "session_id", sessionID, "issue", issueNum, "trigger", logAction)

	recoverCfg := RecoverFleetConfig{
		Plan:            plan,
		SessionID:       sessionID,
		IssueNumber:     issueNum,
		IssueTitle:      item.IssueTitle,
		Repo:            repo,
		GHToken:         a.ResolveGHTokenForPlan(plan),
		CustomerMessage: item.CustomerReply,
		CompletionFunc: func(sessionErr error) {
			if sessionErr != nil {
				monitor.IncrementRetryCount(issueNum, sessionErr.Error())
			} else {
				monitor.ClearRetryOnSuccess(issueNum)
			}
		},
		BallChangeFunc: func(_ string, commentID int64) {
			monitor.UpdateCursor(issueNum, commentID)
		},
	}

	if err := a.fleetRecover(ctx, recoverCfg); err != nil {
		slog.Error("failed to recover session", "component", "plan-activator", "session_id", sessionID, "issue", issueNum, "error", err)
		monitor.IncrementRetryCount(issueNum, fmt.Sprintf("recovery failed: %v", err))
	}
}

// GetMonitor returns the GitHub monitor for a plan (for testing/inspection).
func (a *PlanActivator) GetMonitor(planKey string) *GitHubMonitor {
	a.monitorsMu.RLock()
	defer a.monitorsMu.RUnlock()
	return a.monitors[planKey]
}

// GetIssuesNeedingAttention returns issues that exceeded max retries for a plan.
// Returns nil if the plan has no monitor.
func (a *PlanActivator) GetIssuesNeedingAttention(planKey string) []IssueNeedingAttention {
	a.monitorsMu.RLock()
	mon := a.monitors[planKey]
	a.monitorsMu.RUnlock()

	if mon == nil {
		return nil
	}
	return mon.GetIssuesNeedingAttention()
}

// RetryFailedIssue resets the retry count for an issue so it will be picked up
// on the next poll cycle. Returns the monitor for the caller to proceed with
// immediate recovery if desired.
func (a *PlanActivator) RetryFailedIssue(planKey string, issueNumber int) (*GitHubMonitor, error) {
	a.monitorsMu.RLock()
	mon := a.monitors[planKey]
	a.monitorsMu.RUnlock()

	if mon == nil {
		return nil, fmt.Errorf("no monitor found for plan %q", planKey)
	}

	if err := mon.ResetRetryCount(issueNumber); err != nil {
		return nil, err
	}

	return mon, nil
}

// ForceCleanup removes all resources associated with a fleet plan regardless
// of the plan's current state. It is designed to be called before deleting a
// plan from the registry, and is safe to call even if the plan is not activated
// or has already been partially cleaned up.
//
// It removes:
//   - The scheduler job (looked up by the predictable name "fleet-plan:<key>")
//   - The in-memory GitHub monitor
//   - The persisted monitor state file (.state/<key>.json)
func (a *PlanActivator) ForceCleanup(planKey string) {
	jobName := fmt.Sprintf("fleet-plan:%s", planKey)

	// 1. Remove scheduler job by name (works even if we don't have the job UUID)
	if err := a.scheduler.RemoveJobByName(jobName); err != nil {
		slog.Warn("failed to remove scheduler job during cleanup", "component", "plan-activator", "job_name", jobName, "error", err)
	}

	// 2. Remove in-memory monitor and clear its state file
	a.monitorsMu.Lock()
	monitor := a.monitors[planKey]
	delete(a.monitors, planKey)
	a.monitorsMu.Unlock()

	if monitor != nil {
		if err := monitor.ClearState(); err != nil {
			slog.Warn("failed to clear monitor state", "component", "plan-activator", "plan", planKey, "error", err)
		}
	} else {
		// No in-memory monitor, but the state file may still exist on disk.
		// Remove it directly using the same path convention as GitHubMonitor.
		statePath := filepath.Join(a.registry().Dir(), ".state", planKey+".json")
		if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to remove state file", "component", "plan-activator", "path", statePath, "error", err)
		}
	}

	slog.Info("cleaned up resources for plan", "component", "plan-activator", "plan", planKey)
}
