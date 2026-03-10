package fleet

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
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
	Repo           string                             // GitHub repo "owner/repo"
	GHToken        string                             // resolved GitHub token (injected as GH_TOKEN)
	CompletionFunc func(err error)                    // called when the session finishes (nil=success, non-nil=failed)
	BallChangeFunc func(ball string, commentID int64) // called when ball moves between agents/human
}

// RecoverFleetConfig holds parameters for recovering an interrupted fleet session.
type RecoverFleetConfig struct {
	Plan           *FleetPlan
	SessionID      string // original session ID (to resume the same transcript)
	IssueNumber    int
	Repo           string
	GHToken        string                             // resolved GitHub token (injected as GH_TOKEN)
	CompletionFunc func(err error)                    // called when the recovered session finishes
	BallChangeFunc func(ball string, commentID int64) // called when ball moves between agents/human
}

// GHTokenResolverFunc resolves the GitHub token for a fleet plan by looking
// up the plan's credentials map in the encrypted credential store.
// Returns empty string if no GitHub credential is configured or available.
type GHTokenResolverFunc func(plan *FleetPlan) string

// PlanActivator manages the lifecycle of fleet plan activations.
// It creates scheduler jobs for activated plans and removes them on deactivation.
type PlanActivator struct {
	planRegistry    *PlanRegistry
	scheduler       SchedulerAccess
	fleetStart      FleetStartFunc
	fleetRecover    FleetRecoverFunc
	ghTokenResolver GHTokenResolverFunc

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

// SetGHTokenResolver sets the function used to resolve GitHub tokens for fleet plans.
// Called by the daemon after the plan activator is created.
func (a *PlanActivator) SetGHTokenResolver(fn GHTokenResolverFunc) {
	a.ghTokenResolver = fn
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
		monitor.GHToken = a.ResolveGHTokenForPlan(plan)
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
			monitor.GHToken = a.ResolveGHTokenForPlan(plan)
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
				// Recover sessions where agents had the ball (full recovery).
				agentBall := mon.GetAgentBallIssues()
				for _, ab := range agentBall {
					repo := GetConfigString(plan.Channel.Config, "repo")
					issueNum := ab.IssueNumber

					log.Printf("[plan-activator] Recovering interrupted session %s for issue #%d (plan %q, ball=agents)",
						ab.SessionID, issueNum, summary.Key)

					recoverCfg := RecoverFleetConfig{
						Plan:        plan,
						SessionID:   ab.SessionID,
						IssueNumber: issueNum,
						Repo:        repo,
						GHToken:     a.ResolveGHTokenForPlan(plan),
						CompletionFunc: func(sessionErr error) {
							if sessionErr != nil {
								mon.MarkFailed(issueNum, sessionErr.Error())
							} else {
								mon.MarkCustomer(issueNum, 0)
							}
						},
						BallChangeFunc: func(ball string, commentID int64) {
							switch ball {
							case "customer":
								mon.MarkCustomer(issueNum, commentID)
							case "agents":
								mon.UpdateLastCommentID(issueNum, commentID)
							}
						},
					}
					if err := a.fleetRecover(context.Background(), recoverCfg); err != nil {
						log.Printf("[plan-activator] Failed to recover session %s for issue #%d: %v",
							ab.SessionID, issueNum, err)
						mon.MarkFailed(issueNum, fmt.Sprintf("recovery failed: %v", err))
					}
				}

				// For sessions where the customer has the ball, start a
				// lightweight comment watcher instead of full recovery.
				// When a new customer comment arrives, it triggers recovery.
				customerBall := mon.GetCustomerBallIssues()
				if len(customerBall) > 0 {
					log.Printf("[plan-activator] Starting comment watcher for %d customer-ball issues (plan %q)",
						len(customerBall), summary.Key)

					// Capture plan-level variables for the callback closure.
					planCopy := plan
					monRef := mon
					planKey := summary.Key

					mon.WatchForCustomerReplies(context.Background(), func(issueNumber int, sessionID string, _ string) {
						repo := GetConfigString(planCopy.Channel.Config, "repo")
						issNum := issueNumber
						sessID := sessionID

						log.Printf("[plan-activator] Customer replied on issue #%d (plan %q), triggering recovery for session %s",
							issNum, planKey, sessID)

						recoverCfg := RecoverFleetConfig{
							Plan:        planCopy,
							SessionID:   sessID,
							IssueNumber: issNum,
							Repo:        repo,
							GHToken:     a.ResolveGHTokenForPlan(planCopy),
							CompletionFunc: func(sessionErr error) {
								if sessionErr != nil {
									monRef.MarkFailed(issNum, sessionErr.Error())
								} else {
									monRef.MarkCustomer(issNum, 0)
								}
							},
							BallChangeFunc: func(ball string, commentID int64) {
								switch ball {
								case "customer":
									monRef.MarkCustomer(issNum, commentID)
								case "agents":
									monRef.UpdateLastCommentID(issNum, commentID)
								}
							},
						}
						if err := a.fleetRecover(context.Background(), recoverCfg); err != nil {
							log.Printf("[plan-activator] Failed to recover session %s after customer reply on issue #%d: %v",
								sessID, issNum, err)
							monRef.MarkFailed(issNum, fmt.Sprintf("recovery after human reply failed: %v", err))
						}
					})
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
	repo := GetConfigString(plan.Channel.Config, "repo")
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
				GHToken:     a.ResolveGHTokenForPlan(plan),
				CompletionFunc: func(sessionErr error) {
					if sessionErr != nil {
						monitor.MarkFailed(issueNum, sessionErr.Error())
					} else {
						// Session exited cleanly. Ball moves to customer (waiting
						// for potential new comments or the issue is done).
						monitor.MarkCustomer(issueNum, 0)
					}
				},
				BallChangeFunc: func(ball string, commentID int64) {
					switch ball {
					case "customer":
						monitor.MarkCustomer(issueNum, commentID)
					case "agents":
						// Update the comment cursor even when ball stays with agents
						monitor.UpdateLastCommentID(issueNum, commentID)
					}
				},
			}
			sessionID, startErr := a.fleetStart(ctx, cfg)
			if startErr != nil {
				log.Printf("[plan-activator] Failed to start fleet session for issue #%d: %v", issue.Number, startErr)
				continue
			}
			monitor.MarkAgents(issue.Number, sessionID)
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

// GetFailedIssues returns failed issues for a plan. Returns nil if the plan
// has no monitor (not a github_issues plan or not activated).
func (a *PlanActivator) GetFailedIssues(planKey string) []FailedIssue {
	a.monitorsMu.RLock()
	mon := a.monitors[planKey]
	a.monitorsMu.RUnlock()

	if mon == nil {
		return nil
	}
	return mon.GetFailedIssues()
}

// RetryFailedIssue resets a failed issue to "agents" so recovery can resume
// the session. Returns the monitor for the caller to proceed with recovery.
func (a *PlanActivator) RetryFailedIssue(planKey string, issueNumber int) (*GitHubMonitor, error) {
	a.monitorsMu.RLock()
	mon := a.monitors[planKey]
	a.monitorsMu.RUnlock()

	if mon == nil {
		return nil, fmt.Errorf("no monitor found for plan %q", planKey)
	}

	if err := mon.ResetToAgents(issueNumber); err != nil {
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
		log.Printf("[plan-activator] Warning: failed to remove scheduler job %q during cleanup: %v", jobName, err)
	}

	// 2. Remove in-memory monitor and clear its state file
	a.monitorsMu.Lock()
	monitor := a.monitors[planKey]
	delete(a.monitors, planKey)
	a.monitorsMu.Unlock()

	if monitor != nil {
		if err := monitor.ClearState(); err != nil {
			log.Printf("[plan-activator] Warning: failed to clear monitor state for %q: %v", planKey, err)
		}
	} else {
		// No in-memory monitor, but the state file may still exist on disk.
		// Remove it directly using the same path convention as GitHubMonitor.
		statePath := filepath.Join(a.planRegistry.Dir(), ".state", planKey+".json")
		if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
			log.Printf("[plan-activator] Warning: failed to remove state file %q: %v", statePath, err)
		}
	}

	log.Printf("[plan-activator] Cleaned up resources for plan %q", planKey)
}
