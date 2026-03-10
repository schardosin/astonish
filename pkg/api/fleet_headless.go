package api

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/tools"
)

// StartHeadlessFleetSession creates and runs a fleet session without an HTTP
// context. Used by the scheduler when a GitHub monitor detects a new issue.
// The session runs in a background goroutine and is registered in the session
// registry so it appears in the Studio UI if open.
//
// For github_issues plans, the session uses a GitHubIssueChannel that posts
// every agent message as a comment on the issue and polls for human replies.
// For chat plans, it falls back to the in-memory ChatChannel.
func StartHeadlessFleetSession(ctx context.Context, cfg fleet.HeadlessFleetConfig) (string, error) {
	plan := cfg.Plan
	if plan == nil {
		return "", fmt.Errorf("plan is required")
	}

	fleetCfg := &plan.FleetConfig

	// Resolve and create workspace directory before the session starts.
	// This ensures agents have a known project directory and don't need to
	// search the filesystem.
	workspaceDir := plan.ResolveWorkspaceDir()
	if workspaceDir != "" {
		if err := os.MkdirAll(workspaceDir, 0755); err != nil {
			log.Printf("[fleet-headless] Warning: could not create workspace %s: %v", workspaceDir, err)
		} else {
			log.Printf("[fleet-headless] Workspace directory: %s", workspaceDir)
		}
	}

	// Get required dependencies
	personaReg := GetPersonaRegistry()
	if personaReg == nil {
		return "", fmt.Errorf("persona system not initialized")
	}

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

	fleetSession := fleet.NewFleetSession(plan.Key, fleetCfg, channel, subAgentMgr, personaReg)
	fleetSession.Plan = plan
	fleetSession.Headless = true

	// Register in the in-memory registry
	registry := getFleetSessionRegistry()
	registry.Register(fleetSession)

	// Persist to the session index
	persistFleetSessionMeta(fleetSession, fleetCfg, cfg.IssueNumber, cfg.Repo)

	// Create JSONL transcript
	wireFleetTranscript(fleetSession)

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

	// Start the fleet message loop in a background goroutine
	go func() {
		defer func() {
			registry.Unregister(fleetSession.ID)
			log.Printf("[fleet-headless] Session %s removed from registry", fleetSession.ID)
		}()

		if err := fleetSession.Run(context.Background()); err != nil {
			log.Printf("[fleet-headless] Session %s error: %v", fleetSession.ID, err)
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
			log.Printf("[fleet-headless] Error posting initial message: %v", err)
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

	log.Printf("[fleet-headless] Session %s started for plan %q (issue #%d)", fleetSession.ID, plan.Key, cfg.IssueNumber)
	return fleetSession.ID, nil
}
