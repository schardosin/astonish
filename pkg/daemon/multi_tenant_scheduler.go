package daemon

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/schardosin/astonish/pkg/scheduler"
	"github.com/schardosin/astonish/pkg/store"
)

// MultiTenantScheduler manages scheduled job execution across all organizations
// and teams in platform mode. It replaces the single-instance scheduler.Scheduler
// for platform deployments, iterating all orgs → all teams on every tick.
//
// In personal mode, the legacy single-instance scheduler.Scheduler is used instead.
type MultiTenantScheduler struct {
	backend  store.PlatformBackend
	executor *scheduler.Executor
	deliver  scheduler.DeliverFunc
	logger   *log.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// running tracks in-flight job IDs globally to prevent double dispatch.
	// Key format: "orgSlug/teamSlug/jobID" to ensure uniqueness across tenants.
	running map[string]struct{}
	runMu   sync.Mutex
}

// NewMultiTenantScheduler creates a new multi-tenant scheduler for platform mode.
func NewMultiTenantScheduler(
	backend store.PlatformBackend,
	executor *scheduler.Executor,
	deliver scheduler.DeliverFunc,
	logger *log.Logger,
) *MultiTenantScheduler {
	if logger == nil {
		logger = log.Default()
	}
	return &MultiTenantScheduler{
		backend:  backend,
		executor: executor,
		deliver:  deliver,
		logger:   logger,
		running:  make(map[string]struct{}),
	}
}

// Start begins the multi-tenant scheduler tick loop.
func (mts *MultiTenantScheduler) Start(ctx context.Context) {
	mts.ctx, mts.cancel = context.WithCancel(ctx)

	// Refresh NextRun for all jobs across all tenants on startup
	mts.refreshAllNextRuns()

	mts.wg.Add(1)
	go mts.loop()

	mts.logger.Printf("[scheduler] Multi-tenant scheduler started")
}

// Stop gracefully shuts down the scheduler and waits for in-flight jobs.
func (mts *MultiTenantScheduler) Stop() {
	if mts.cancel != nil {
		mts.cancel()
	}
	mts.wg.Wait()
	mts.logger.Printf("[scheduler] Multi-tenant scheduler stopped")
}

// loop is the main tick loop. It checks for due jobs every 30 seconds.
func (mts *MultiTenantScheduler) loop() {
	defer mts.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-mts.ctx.Done():
			return
		case <-ticker.C:
			mts.tick()
		}
	}
}

// tick iterates all orgs → all teams → all due jobs, dispatching execution.
func (mts *MultiTenantScheduler) tick() {
	ctx := mts.ctx

	// List all organizations
	orgs, err := mts.backend.Organizations().List(ctx)
	if err != nil {
		mts.logger.Printf("[scheduler] Failed to list organizations: %v", err)
		return
	}

	for _, org := range orgs {
		if org.Status != "active" {
			continue
		}
		mts.tickOrg(ctx, org.Slug)
	}
}

// tickOrg processes all teams in a single organization.
func (mts *MultiTenantScheduler) tickOrg(ctx context.Context, orgSlug string) {
	orgStore, err := mts.backend.ForOrg(orgSlug)
	if err != nil {
		mts.logger.Printf("[scheduler] Failed to resolve org %q: %v", orgSlug, err)
		return
	}

	teams, err := orgStore.Teams().ListTeams(ctx)
	if err != nil {
		mts.logger.Printf("[scheduler] Failed to list teams for org %q: %v", orgSlug, err)
		return
	}

	for _, team := range teams {
		mts.tickTeam(ctx, orgSlug, orgStore, team.Slug)
	}
}

// tickTeam processes all due jobs for a single team.
func (mts *MultiTenantScheduler) tickTeam(ctx context.Context, orgSlug string, orgStore store.OrgDataStore, teamSlug string) {
	teamStore := orgStore.ForTeam(teamSlug)
	schedulerStore := teamStore.ScheduledJobs()
	jobs := schedulerStore.List(ctx)
	now := time.Now()

	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		if job.NextRun == nil {
			continue
		}
		if now.Before(*job.NextRun) {
			continue
		}

		// Check backoff
		if job.ConsecutiveFailures > 0 && job.LastRun != nil {
			backoff := backoffDuration(job.ConsecutiveFailures)
			if now.Before(job.LastRun.Add(backoff)) {
				continue // Still in backoff
			}
		}

		// Build running key: orgSlug/teamSlug/jobID
		runKey := orgSlug + "/" + teamSlug + "/" + job.ID

		// Skip if already running
		mts.runMu.Lock()
		if _, alreadyRunning := mts.running[runKey]; alreadyRunning {
			mts.runMu.Unlock()
			continue
		}
		mts.running[runKey] = struct{}{}
		mts.runMu.Unlock()

		// Dispatch execution in a goroutine
		mts.wg.Add(1)
		go func(sj *store.ScheduledJob, key, org, team string, os store.OrgDataStore, ts store.TeamDataStore, ss store.SchedulerStore) {
			defer mts.wg.Done()
			defer func() {
				mts.runMu.Lock()
				delete(mts.running, key)
				mts.runMu.Unlock()
			}()

			mts.executeJob(ctx, sj, org, team, os, ts, ss)
		}(job, runKey, orgSlug, teamSlug, orgStore, teamStore, schedulerStore)
	}
}

// executeJob runs a single job with full team context and updates state.
func (mts *MultiTenantScheduler) executeJob(
	ctx context.Context,
	storeJob *store.ScheduledJob,
	orgSlug, teamSlug string,
	orgStore store.OrgDataStore,
	teamStore store.TeamDataStore,
	schedulerStore store.SchedulerStore,
) {
	mts.logger.Printf("[scheduler] Executing job %q (mode: %s)", storeJob.Name, storeJob.Mode)

	// Build team-scoped execution context with all relevant stores
	execCtx := ctx
	execCtx = store.WithCredentialStore(execCtx, teamStore.Credentials())
	execCtx = store.WithFlowStore(execCtx, teamStore.Flows())
	execCtx = store.WithDrillReportStore(execCtx, teamStore.DrillReports())
	execCtx = store.WithSkillStores(execCtx, &store.SkillStores{
		Team: teamStore.Skills(),
	})
	execCtx = store.WithMCPServerStores(execCtx, &store.MCPServerStores{
		Platform: mts.backend.PlatformMCPServers(),
		Org:      orgStore.OrgMCPServers(),
		Team:     teamStore.MCPServers(),
	})
	execCtx = store.WithMemoryStore(execCtx, teamStore.Memories())
	execCtx = store.WithFleetTemplateStore(execCtx, teamStore.FleetTemplates())
	execCtx = store.WithFleetPlanStore(execCtx, teamStore.FleetPlans())

	// Inject cross-session memory merge function so that memory_save tool
	// performs dedup/merge instead of blind inserts during scheduled execution.
	if mts.executor.ChatAgent != nil && mts.executor.ChatAgent.PlatformReflector != nil {
		execCtx = store.WithMemorySaveOrMerge(execCtx, mts.executor.ChatAgent.PlatformReflector.MemorySaveOrMergeFunc())
	}

	// Inject per-team disabled tool list so the agent filters them out.
	if ts := teamStore.Settings(); ts != nil {
		if settings, err := ts.Get(ctx); err == nil && len(settings.DisabledTools) > 0 {
			execCtx = store.WithDisabledTools(execCtx, settings.DisabledTools)
		}
	}

	// Convert to scheduler.Job for the executor
	job := storeJobToSchedulerJob(storeJob)

	// Execute
	now := time.Now()
	result, execErr := mts.executor.Execute(execCtx, job)

	// Update runtime state in the team's store
	stored := schedulerStore.Get(ctx, storeJob.ID)
	if stored == nil {
		return
	}

	stored.LastRun = &now
	if execErr != nil {
		stored.LastStatus = "failed"
		stored.LastError = execErr.Error()
		stored.ConsecutiveFailures++
		mts.logger.Printf("[scheduler] Job %q failed (%d consecutive): %v",
			stored.Name, stored.ConsecutiveFailures, execErr)
	} else {
		stored.LastStatus = "success"
		stored.LastError = ""
		stored.ConsecutiveFailures = 0
		mts.logger.Printf("[scheduler] Job %q completed successfully (%d chars)",
			stored.Name, len(result))
	}

	// Compute next run
	stored.NextRun = scheduler.ComputeNextRun(stored.Schedule.Cron, stored.Schedule.Timezone)

	if err := schedulerStore.Update(ctx, stored); err != nil {
		mts.logger.Printf("[scheduler] Failed to update job state for %q: %v", stored.Name, err)
	}

	// Deliver results (skip fleet_poll — those handle their own delivery)
	if mts.deliver != nil && storeJob.Mode != "fleet_poll" {
		// Inject delivery context so the resolver knows which org/team this job belongs to
		deliverCtx := scheduler.WithDeliveryContext(ctx, &scheduler.DeliveryContext{
			OrgSlug:  orgSlug,
			TeamSlug: teamSlug,
		})
		if deliverErr := mts.deliver(deliverCtx, job, result, execErr); deliverErr != nil {
			mts.logger.Printf("[scheduler] Delivery failed for job %q: %v", storeJob.Name, deliverErr)
		}
	}
}

// refreshAllNextRuns computes and sets NextRun for all enabled jobs across all tenants.
func (mts *MultiTenantScheduler) refreshAllNextRuns() {
	ctx := context.Background()

	orgs, err := mts.backend.Organizations().List(ctx)
	if err != nil {
		mts.logger.Printf("[scheduler] Failed to list orgs for NextRun refresh: %v", err)
		return
	}

	var totalJobs int
	for _, org := range orgs {
		if org.Status != "active" {
			continue
		}
		orgStore, err := mts.backend.ForOrg(org.Slug)
		if err != nil {
			continue
		}
		teams, err := orgStore.Teams().ListTeams(ctx)
		if err != nil {
			continue
		}
		for _, team := range teams {
			teamStore := orgStore.ForTeam(team.Slug)
			ss := teamStore.ScheduledJobs()
			jobs := ss.List(ctx)
			for _, job := range jobs {
				if !job.Enabled {
					continue
				}
				nextRun := scheduler.ComputeNextRun(job.Schedule.Cron, job.Schedule.Timezone)
				if nextRun != nil && (job.NextRun == nil || !nextRun.Equal(*job.NextRun)) {
					job.NextRun = nextRun
					_ = ss.Update(ctx, job)
					totalJobs++
				}
			}
		}
	}

	if totalJobs > 0 {
		mts.logger.Printf("[scheduler] Refreshed NextRun for %d jobs across all tenants", totalJobs)
	}
}

// RunNow executes a job immediately with team-scoped context.
// Used by the LLM tool bridge and API handlers when they need the daemon's
// multi-tenant scheduler to perform execution.
func (mts *MultiTenantScheduler) RunNow(ctx context.Context, schedulerStore store.SchedulerStore, teamStore store.TeamDataStore, jobID string) (string, error) {
	storeJob := schedulerStore.Get(ctx, jobID)
	if storeJob == nil {
		return "", nil
	}

	// Build team-scoped execution context
	execCtx := ctx
	execCtx = store.WithCredentialStore(execCtx, teamStore.Credentials())
	execCtx = store.WithFlowStore(execCtx, teamStore.Flows())
	execCtx = store.WithDrillReportStore(execCtx, teamStore.DrillReports())
	execCtx = store.WithSkillStores(execCtx, &store.SkillStores{
		Team: teamStore.Skills(),
	})
	execCtx = store.WithMCPServerStores(execCtx, &store.MCPServerStores{
		Platform: mts.backend.PlatformMCPServers(),
		Team:     teamStore.MCPServers(),
	})
	execCtx = store.WithMemoryStore(execCtx, teamStore.Memories())
	execCtx = store.WithFleetTemplateStore(execCtx, teamStore.FleetTemplates())
	execCtx = store.WithFleetPlanStore(execCtx, teamStore.FleetPlans())

	// Inject cross-session memory merge function for fleet plan execution.
	if mts.executor.ChatAgent != nil && mts.executor.ChatAgent.PlatformReflector != nil {
		execCtx = store.WithMemorySaveOrMerge(execCtx, mts.executor.ChatAgent.PlatformReflector.MemorySaveOrMergeFunc())
	}

	// Convert and execute
	job := storeJobToSchedulerJob(storeJob)
	result, execErr := mts.executor.Execute(execCtx, job)

	// Update runtime state
	now := time.Now()
	storeJob.LastRun = &now
	if execErr != nil {
		storeJob.LastStatus = "failed"
		storeJob.LastError = execErr.Error()
		storeJob.ConsecutiveFailures++
	} else {
		storeJob.LastStatus = "success"
		storeJob.LastError = ""
		storeJob.ConsecutiveFailures = 0
	}
	storeJob.NextRun = scheduler.ComputeNextRun(storeJob.Schedule.Cron, storeJob.Schedule.Timezone)
	_ = schedulerStore.Update(ctx, storeJob)

	return result, execErr
}

// backoffSteps defines the error backoff delays for consecutive failures.
var backoffSteps = []time.Duration{
	30 * time.Second,
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	60 * time.Minute,
}

// backoffDuration returns the delay for a given failure count.
func backoffDuration(failures int) time.Duration {
	idx := failures - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(backoffSteps) {
		idx = len(backoffSteps) - 1
	}
	return backoffSteps[idx]
}
