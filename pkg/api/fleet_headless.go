package api

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/tools"
)

// StartHeadlessFleetSession creates and runs a fleet session without an HTTP
// context. Used by the scheduler when a GitHub monitor detects a new issue.
// The session runs in a background goroutine and is registered in the session
// registry so it appears in the Studio UI if open.
//
// sessionStore is the PG-backed session store for platform mode (nil for personal mode).
// When non-nil, session metadata and transcript events are persisted to PostgreSQL
// instead of the file-based session store.
//
// For github_issues plans, the session uses a GitHubIssueChannel that posts
// every agent message as a comment on the issue and polls for human replies.
// For chat plans, it falls back to the in-memory ChatChannel.
func StartHeadlessFleetSession(ctx context.Context, cfg fleet.HeadlessFleetConfig, sessionStore store.SessionStore, fleetStores *FleetStores) (string, error) {
	plan := cfg.Plan
	if plan == nil {
		return "", fmt.Errorf("plan is required")
	}

	fleetCfg := &plan.FleetConfig

	// Resolve the base workspace (~/astonish_projects/<repo-name>/) where
	// the wizard cloned the repo and generated AGENTS.md.
	baseDir := plan.ResolveWorkspaceDir()

	// Resolve per-session workspace directory. Each session gets its own
	// isolated workspace (via git clone --local from the base) under the
	// sessions dir. For headless sessions with an issue number, the task
	// slug provides a human-readable container name.
	var taskSlug string
	if cfg.IssueNumber > 0 && cfg.IssueTitle != "" {
		taskSlug = fleet.TaskSlugFromIssue(cfg.IssueNumber, cfg.IssueTitle)
	}

	var workspaceDir string
	if wsDir, wsErr := config.GetWorkspacesDir(); wsErr == nil {
		workspaceDir = fleet.ResolveSessionWorkspaceDir(
			wsDir, "", taskSlug)
		if err := fleet.SetupSessionWorkspace(workspaceDir, plan.ResolveProjectSource(), baseDir); err != nil {
			slog.Warn("could not set up workspace", "component", "fleet-headless", "workspace", workspaceDir, "error", err)
			workspaceDir = "" // fall back to legacy behavior
		}
	}
	// Fall back to the legacy shared workspace if no file store or setup failed.
	if workspaceDir == "" {
		workspaceDir = baseDir
		if workspaceDir != "" {
			if err := os.MkdirAll(workspaceDir, 0755); err != nil {
				slog.Warn("could not create workspace", "component", "fleet-headless", "workspace", workspaceDir, "error", err)
			} else {
				slog.Info("workspace directory (legacy)", "component", "fleet-headless", "workspace", workspaceDir)
			}
		}
	}

	// Get required dependencies
	subAgentMgr := tools.GetSubAgentManager()
	if subAgentMgr == nil {
		return "", fmt.Errorf("sub-agent system not initialized")
	}

	// Create the appropriate channel based on the plan's channel type.
	var channel fleet.Channel
	var ghChannel *fleet.GitHubIssueChannel

	if cfg.IssueNumber > 0 && cfg.Repo != "" && plan.Channel.Type == "github_issues" {
		ghChannel = fleet.NewGitHubIssueChannel(cfg.Repo, cfg.IssueNumber, cfg.GHToken)
		channel = ghChannel
	} else {
		channel = fleet.NewChatChannel(plan.Key)
	}

	fleetSession := fleet.NewFleetSession(plan.Key, fleetCfg, channel, subAgentMgr)
	fleetSession.Plan = plan
	fleetSession.Headless = true
	fleetSession.TaskSlug = taskSlug
	fleetSession.WorkspaceDir = workspaceDir
	fleetSession.TeamSlug = cfg.TeamSlug

	// Wire sandbox container for this fleet session (fails if sandbox is enabled but unavailable)
	var credStore store.CredentialStore
	if fleetStores != nil {
		credStore = fleetStores.Credentials
	}
	if err := wireFleetSandbox(fleetSession, plan, credStore, nil, ""); err != nil {
		return "", fmt.Errorf("cannot start headless fleet session: %w", err)
	}

	// Load project context (AGENTS.md) from the base workspace.
	// The wizard generated it during plan creation; no regeneration needed.
	if baseDir != "" && fleetCfg.ProjectContext != nil {
		fleetSession.ProjectContext = fleet.LoadProjectContextFile(baseDir, fleetCfg.ProjectContext)
	}

	// Register in the in-memory registry
	registry := getFleetSessionRegistry()
	registry.Register(fleetSession)

	// Determine user ID. In platform mode (sessionStore != nil), use the
	// plan creator's identity so the session has access to their credentials.
	// Falls back to SystemUserID for legacy plans without a creator.
	// In personal mode, fall back to the default studio user string
	// (file store doesn't require UUID format).
	userID := cfg.UserID
	if userID == "" && sessionStore != nil {
		userID = store.SystemUserID
	} else if userID == "" && sessionStore == nil {
		userID = studioChatUserID
	}

	// Persist to the session index
	persistFleetSessionMeta(fleetSession, fleetCfg, userID, cfg.IssueNumber, cfg.Repo, sessionStore)

	// Create transcript (JSONL for personal mode, PG events for platform mode)
	wireFleetTranscript(fleetSession, userID, sessionStore)

	// Wire completion callback for issue lifecycle tracking
	if cfg.CompletionFunc != nil {
		fleetSession.OnSessionDone = func(_ string, sessionErr error) {
			cfg.CompletionFunc(sessionErr)
		}
	}

	// Wire ball-change callback so the monitor state tracks who has the ball.
	if cfg.BallChangeFunc != nil {
		fleetSession.OnBallChange = func(ball string) {
			var commentID int64
			if ghChannel != nil {
				commentID = ghChannel.LastCommentID()
			}
			cfg.BallChangeFunc(ball, commentID)
		}
	}

	// Start the fleet message loop in a background goroutine.
	// Enrich the context with the session store so child sub-agent sessions
	// are persisted to PostgreSQL (making tool executions visible in traces).
	runCtx := context.Background()
	if sessionStore != nil {
		runCtx = store.WithSessionService(runCtx, sessionStore)
		runCtx = store.WithUserID(runCtx, userID)
	}
	// Inject tenant-scoped stores (FlowStore, DrillReportStore, CredentialStore,
	// SkillStores, etc.) so fleet sub-agents can access team drills, credentials,
	// and other platform-mode resources during execution.
	runCtx = fleetStores.InjectIntoContextForPlan(runCtx, plan)

	go func() {
		defer func() {
			registry.Unregister(fleetSession.ID)
			slog.Info("session removed from registry", "component", "fleet-headless", "session_id", fleetSession.ID)
		}()

		if err := fleetSession.Run(runCtx); err != nil {
			slog.Error("session error", "component", "fleet-headless", "session_id", fleetSession.ID, "error", err)
		}
	}()

	// Post the initial message to kick off the fleet.
	// For GitHub channels, this stays in-memory only (the issue body IS the
	// initial message; we do not re-post it as a comment).
	if cfg.InitialMsg != "" {
		initialMsg := fleet.Message{
			Sender:    "customer",
			Text:      cfg.InitialMsg,
			Timestamp: time.Now(),
		}
		if err := channel.PostMessage(context.Background(), initialMsg); err != nil {
			slog.Error("failed to post initial message", "component", "fleet-headless", "error", err)
			return "", err
		}
		if fleetSession.OnMessagePosted != nil {
			fleetSession.OnMessagePosted(initialMsg)
		}
	}

	// For GitHub channels, seed the comment cursor (so the poller skips
	// existing comments) and start polling for human replies.
	if ghChannel != nil {
		ghChannel.SeedLastCommentID()
		ghChannel.StartPoller(context.Background())
	}

	slog.Info("session started", "component", "fleet-headless", "session_id", fleetSession.ID, "plan", plan.Key, "issue", cfg.IssueNumber)
	return fleetSession.ID, nil
}
