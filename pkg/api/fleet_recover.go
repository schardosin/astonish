package api

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/tools"
	"google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

// RecoverFleetSession recovers an interrupted fleet session after a daemon
// restart. It reads the JSONL transcript from the previous session,
// reconstructs the channel with prior messages, and resumes the fleet Run()
// loop from where it left off.
//
// The recovered session uses the SAME session ID so that:
//   - The transcript file is appended to (not overwritten)
//   - The session index entry remains valid
//   - The Studio UI shows a single continuous session
func RecoverFleetSession(ctx context.Context, cfg fleet.RecoverFleetConfig) error {
	plan := cfg.Plan
	if plan == nil {
		return fmt.Errorf("plan is required for recovery")
	}

	fleetCfg := &plan.FleetConfig

	// Resolve and create workspace directory (may not exist after restart)
	workspaceDir := plan.ResolveWorkspaceDir()
	if workspaceDir != "" {
		if mkErr := os.MkdirAll(workspaceDir, 0755); mkErr != nil {
			log.Printf("[fleet-recover] Warning: could not create workspace %s: %v", workspaceDir, mkErr)
		}
	}

	// Get required dependencies (same as StartHeadlessFleetSession)
	subAgentMgr := tools.GetSubAgentManager()
	if subAgentMgr == nil {
		return fmt.Errorf("sub-agent system not initialized")
	}

	// Read the JSONL transcript to recover the message history
	fileStore := getFleetFileStore()
	if fileStore == nil {
		return fmt.Errorf("file store not available for session recovery")
	}

	transcriptPath := filepath.Join(fileStore.BaseDir(), studioChatAppName, studioChatUserID, cfg.SessionID+".jsonl")
	transcript := session.NewTranscript(transcriptPath)

	if !transcript.Exists() {
		return fmt.Errorf("transcript not found for session %s", cfg.SessionID)
	}

	events, err := transcript.ReadEvents()
	if err != nil {
		return fmt.Errorf("reading transcript: %w", err)
	}

	if len(events) == 0 {
		return fmt.Errorf("transcript for session %s has no events, nothing to recover", cfg.SessionID)
	}

	// Convert transcript events back to fleet messages
	recoveredMessages := eventsToFleetMessages(events)
	if len(recoveredMessages) == 0 {
		return fmt.Errorf("no recoverable messages in transcript for session %s", cfg.SessionID)
	}

	log.Printf("[fleet-recover] Session %s: recovered %d messages from transcript", cfg.SessionID, len(recoveredMessages))

	// If recovery was triggered by a customer reply, append it to the
	// recovered messages so the session sees it in the thread context,
	// recovery summary, and milestone tracker. The customer's comment
	// exists on GitHub but was not in the JSONL transcript (the session
	// was stopped when the customer replied).
	if cfg.CustomerMessage != "" {
		customerMsg := fleet.Message{
			ID:        uuid.New().String(),
			Sender:    "customer",
			Text:      cfg.CustomerMessage,
			Timestamp: time.Now(),
			Mentions:  fleet.ParseMentions(cfg.CustomerMessage),
		}
		recoveredMessages = append(recoveredMessages, customerMsg)
		log.Printf("[fleet-recover] Session %s: injected customer reply that triggered recovery (%d chars)",
			cfg.SessionID, len(cfg.CustomerMessage))
	}

	// Create a GitHubIssueChannel pre-loaded with recovered messages.
	// LoadMessages advances the read cursor past all recovered messages
	// so Run() does not re-process them.
	ghChannel := fleet.NewGitHubIssueChannel(cfg.Repo, cfg.IssueNumber, cfg.GHToken)
	ghChannel.LoadMessages(recoveredMessages)

	// Seed the comment cursor so the poller skips all existing comments
	// (both our fleet comments and human replies we already recovered).
	ghChannel.SeedLastCommentID()

	// Start polling for NEW human replies
	ghChannel.StartPoller(context.Background())

	// Create a new FleetSession but with the SAME session ID
	fleetSession := fleet.NewFleetSession(plan.Key, fleetCfg, ghChannel, subAgentMgr)
	fleetSession.ID = cfg.SessionID // override with original session ID
	fleetSession.Plan = plan
	fleetSession.Headless = true

	// Derive task slug from the issue context (same as initial start).
	if cfg.IssueNumber > 0 && cfg.IssueTitle != "" {
		fleetSession.TaskSlug = fleet.TaskSlugFromIssue(cfg.IssueNumber, cfg.IssueTitle)
	}

	// Load existing project context file from the workspace (no regeneration
	// on recovery; the file should already exist from the original session).
	if workspaceDir != "" && fleetCfg.ProjectContext != nil {
		fleetSession.ProjectContext = fleet.LoadProjectContextFile(workspaceDir, fleetCfg.ProjectContext)
	}

	// Reconstruct the progress tracker from recovered messages so agents
	// know about prior approvals, completions, and handoffs.
	for _, msg := range recoveredMessages {
		if msg.Sender == "customer" {
			for _, m := range fleet.AnalyzeCustomerMessageForMilestones(msg) {
				fleetSession.Progress.AddMilestone(m)
			}
		} else {
			for _, m := range fleet.AnalyzeMessageForMilestones(msg) {
				fleetSession.Progress.AddMilestone(m)
			}
		}
	}

	milestoneCount := len(fleetSession.Progress.GetMilestones())

	// When recovering after a customer reply (CustomerMessage is non-empty),
	// skip the "daemon restart" announcement and LLM summary. The customer's
	// reply is already in recoveredMessages and the agent has full thread
	// context — adding a system comment would just be noise on the GitHub issue.
	//
	// For actual daemon restarts (CustomerMessage is empty), generate an LLM
	// summary so the resume agent has accurate context about what happened.
	isCustomerReply := cfg.CustomerMessage != ""

	if !isCustomerReply {
		fleetSession.Progress.AddMilestone(fleet.Milestone{
			Type:    fleet.MilestoneResume,
			Agent:   "system",
			Summary: "Session resumed after daemon restart",
		})
		milestoneCount++

		resumeText := "Fleet session resumed after daemon restart. Continuing from where we left off."
		if subAgentMgr.LLM != nil {
			if summary := generateRecoverySummary(recoveredMessages, subAgentMgr.LLM); summary != "" {
				resumeText = summary
			}
		}

		restartMsg := fleet.Message{
			ID:        uuid.New().String(),
			Sender:    "system",
			Text:      resumeText,
			Timestamp: time.Now(),
		}
		if postErr := ghChannel.PostMessage(context.Background(), restartMsg); postErr != nil {
			log.Printf("[fleet-recover] Warning: could not post restart message: %v", postErr)
		}
		ghChannel.PostExternal(restartMsg)
	}

	log.Printf("[fleet-recover] Session %s: reconstructed %d milestones from transcript (customer_reply=%v)",
		cfg.SessionID, milestoneCount, isCustomerReply)

	// Determine who should act next based on the last message.
	lastMsg := recoveredMessages[len(recoveredMessages)-1]
	resumeTarget := determineResumeTarget(lastMsg, fleetCfg, subAgentMgr)
	fleetSession.ResumeTarget = resumeTarget

	log.Printf("[fleet-recover] Session %s: resume target is %q (last sender: %s)",
		cfg.SessionID, resumeTarget, lastMsg.Sender)

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

	// Wire transcript in APPEND mode (do NOT write a new header).
	// The existing transcript file already has the header and prior events.
	wireFleetTranscriptAppend(fleetSession, transcriptPath, len(events))

	// Register in the in-memory session registry
	registry := getFleetSessionRegistry()
	registry.Register(fleetSession)

	// Start the fleet message loop in a background goroutine
	go func() {
		defer func() {
			registry.Unregister(fleetSession.ID)
			log.Printf("[fleet-recover] Session %s removed from registry", fleetSession.ID)
		}()

		if runErr := fleetSession.Run(context.Background()); runErr != nil {
			log.Printf("[fleet-recover] Session %s error: %v", fleetSession.ID, runErr)
		}
	}()

	log.Printf("[fleet-recover] Session %s recovered and running (issue #%d, target: %s)",
		cfg.SessionID, cfg.IssueNumber, resumeTarget)
	return nil
}

// eventsToFleetMessages converts ADK session events (from a JSONL transcript)
// back into fleet.Message objects for channel recovery.
func eventsToFleetMessages(events []*adksession.Event) []fleet.Message {
	var messages []fleet.Message
	for _, event := range events {
		if event.LLMResponse.Content == nil {
			continue
		}

		// Extract text from all parts
		var text string
		for _, part := range event.LLMResponse.Content.Parts {
			if part.Text != "" {
				text += part.Text
			}
		}
		if text == "" {
			continue
		}

		// Determine sender from Author field (set by fleetMessageToEvent)
		sender := event.Author
		if sender == "" {
			if event.LLMResponse.Content.Role == genai.RoleUser {
				sender = "customer"
			} else {
				sender = "agent"
			}
		}
		if sender == "user" {
			sender = "customer"
		}

		msg := fleet.Message{
			ID:        event.ID,
			Sender:    sender,
			Text:      text,
			Mentions:  fleet.ParseMentions(text),
			Timestamp: event.Timestamp,
		}

		messages = append(messages, msg)
	}
	return messages
}

// determineResumeTarget figures out which agent should act next after recovery.
// It looks at the last message in the recovered thread:
//   - If from a human: route to the entry point
//   - If from an agent: use LLM routing (or fallback to entry point)
//   - If from system: route to entry point
func determineResumeTarget(lastMsg fleet.Message, fleetCfg *fleet.FleetConfig, subAgentMgr *agent.SubAgentManager) string {
	if lastMsg.Sender == "customer" {
		return fleet.RouteCustomerMessage(fleetCfg, "")
	}

	if lastMsg.Sender == "system" {
		return fleetCfg.GetEntryPoint()
	}

	// Last message was from an agent. Try LLM routing with a short timeout.
	// The SubAgentManager has the LLM reference we need for routing.
	if subAgentMgr.LLM != nil {
		routeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		routing := fleet.RouteWithLLM(routeCtx, lastMsg, fleetCfg, subAgentMgr.LLM)
		switch routing.Target {
		case "customer":
			// The agent was waiting for customer input. Rather than returning ""
			// (which blocks forever in headless sessions with no customer), activate
			// the entry point to reassess. If human input is truly needed the
			// agent will say so, the session will enter WaitingForHuman state,
			// and the GitHub comment poller will pick up any new replies.
			return fleetCfg.GetEntryPoint()
		case "none":
			// Same rationale: "none" after recovery usually means the LLM could
			// not determine next steps from the truncated context. The entry
			// point agent can re-evaluate with the full progress tracker.
			return fleetCfg.GetEntryPoint()
		case "self":
			return lastMsg.Sender
		default:
			return routing.Target
		}
	}

	// Fallback: re-activate the entry point agent to reassess the situation
	return fleetCfg.GetEntryPoint()
}

// wireFleetTranscriptAppend wires the OnMessagePosted callback to APPEND to
// an existing JSONL transcript file. Unlike wireFleetTranscript, this does NOT
// write a new header (the file already has one from the original session).
func wireFleetTranscriptAppend(fs *fleet.FleetSession, transcriptPath string, priorEventCount int) {
	transcript := session.NewTranscript(transcriptPath)

	// Start the invocation counter from where the prior transcript left off
	invocationCounter := priorEventCount

	fs.OnMessagePosted = func(msg fleet.Message) {
		invocationCounter++
		event := fleetMessageToEvent(msg, invocationCounter)
		if err := transcript.AppendEvent(event); err != nil {
			log.Printf("[fleet-recover] Warning: could not persist fleet message: %v", err)
		}
	}
}

// generateRecoverySummary uses the LLM to produce a structured summary of the
// conversation history for injection into the resumed session. The summary
// tells agents exactly what has happened, what was approved, and what remains.
//
// This solves the critical problem of agents re-doing work after recovery:
// the regular thread context truncates to ~40K chars, losing earlier approvals
// and completions. The summary preserves this information in a compact format
// that fits in the recent message window.
func generateRecoverySummary(messages []fleet.Message, llm model.LLM) string {
	if len(messages) == 0 {
		return ""
	}

	// Build a compact representation of ALL messages (no truncation).
	// Each message: "@sender: first 300 chars of text"
	// This is cheaper than the full text and fits in a single LLM call.
	var threadText strings.Builder
	for _, msg := range messages {
		text := msg.Text
		if len(text) > 300 {
			text = text[:300] + "..."
		}
		text = strings.ReplaceAll(text, "\n", " ")
		threadText.WriteString(fmt.Sprintf("@%s: %s\n", msg.Sender, text))
	}

	// Cap the total input to avoid exceeding context limits
	input := threadText.String()
	const maxInputChars = 80000
	if len(input) > maxInputChars {
		// Keep the first 20K and last 60K to preserve both beginning and end
		input = input[:20000] + "\n\n[... middle of conversation omitted ...]\n\n" + input[len(input)-60000:]
	}

	prompt := fmt.Sprintf(`You are summarizing a team conversation that was interrupted and is being resumed.
Produce a structured status report that tells agents EXACTLY where things stand.

The conversation has %d messages. Here is the full thread:

---
%s
---

Produce a summary with EXACTLY this structure (fill in the details):

## Session Recovery Summary

**Original Task:** [1-2 sentence description of what was requested]

**Decisions Made (DO NOT re-request these):**
- [List each approval/decision with who approved and what was approved]

**Work Completed (DO NOT redo this):**
- [List each completed deliverable, step, or milestone with specifics]

**Current State:**
- [What is the immediate next action needed]
- [Who should be doing it]
- [Any blockers or pending items]

**Key Artifacts:**
- [List important files, PRs, branches that exist]

Be specific with names, numbers, file paths, PR numbers, test counts, etc.
Do NOT be vague. The agents reading this have NO other context.`, len(messages), input)

	summaryCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	req := &genai.GenerateContentConfig{
		Temperature:     genai.Ptr(float32(0.0)),
		MaxOutputTokens: 1500,
	}

	llmReq := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText(prompt, genai.RoleUser),
		},
		Config: req,
	}

	var responseText strings.Builder
	for resp, err := range llm.GenerateContent(summaryCtx, llmReq, false) {
		if err != nil {
			log.Printf("[fleet-recover] LLM summary failed: %v", err)
			return ""
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Text != "" {
					responseText.WriteString(part.Text)
				}
			}
		}
	}

	summary := strings.TrimSpace(responseText.String())
	if summary == "" {
		return ""
	}

	log.Printf("[fleet-recover] Generated recovery summary (%d chars)", len(summary))
	return "Fleet session resumed after daemon restart.\n\n" + summary
}
