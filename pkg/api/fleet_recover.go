package api

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/session"
	"github.com/schardosin/astonish/pkg/tools"
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
	personaReg := GetPersonaRegistry()
	if personaReg == nil {
		return fmt.Errorf("persona system not initialized")
	}

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

	// Create a GitHubIssueChannel pre-loaded with recovered messages.
	// LoadMessages advances the read cursor past all recovered messages
	// so Run() does not re-process them.
	ghChannel := fleet.NewGitHubIssueChannel(cfg.Repo, cfg.IssueNumber)
	ghChannel.LoadMessages(recoveredMessages)

	// Seed the comment cursor so the poller skips all existing comments
	// (both our fleet comments and human replies we already recovered).
	ghChannel.SeedLastCommentID()

	// Start polling for NEW human replies
	ghChannel.StartPoller(context.Background())

	// Create a new FleetSession but with the SAME session ID
	fleetSession := fleet.NewFleetSession(plan.Key, fleetCfg, ghChannel, subAgentMgr, personaReg)
	fleetSession.ID = cfg.SessionID // override with original session ID
	fleetSession.Plan = plan

	// Post a system message about the restart
	restartMsg := fleet.Message{
		ID:        uuid.New().String(),
		Sender:    "system",
		Text:      "Fleet session resumed after daemon restart. Continuing from where we left off.",
		Timestamp: time.Now(),
	}
	if postErr := ghChannel.PostMessage(context.Background(), restartMsg); postErr != nil {
		log.Printf("[fleet-recover] Warning: could not post restart message: %v", postErr)
	}

	// Determine who should act next based on the last message.
	lastMsg := recoveredMessages[len(recoveredMessages)-1]
	resumeTarget := determineResumeTarget(lastMsg, fleetCfg, subAgentMgr)
	fleetSession.ResumeTarget = resumeTarget

	log.Printf("[fleet-recover] Session %s: resume target is %q (last sender: %s)",
		cfg.SessionID, resumeTarget, lastMsg.Sender)

	// Wire completion callback for issue lifecycle tracking
	if cfg.CompletionFunc != nil {
		fleetSession.OnSessionDone = func(_ string) {
			cfg.CompletionFunc()
		}
	}

	// Wire transcript in APPEND mode (do NOT write a new header).
	// The existing transcript file already has the header and prior events.
	wireFleetTranscriptAppend(fleetSession, transcriptPath, len(events))

	// Persist the restart message to the transcript
	if fleetSession.OnMessagePosted != nil {
		fleetSession.OnMessagePosted(restartMsg)
	}

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
				sender = "human"
			} else {
				sender = "agent"
			}
		}
		if sender == "user" {
			sender = "human"
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
	if lastMsg.Sender == "human" {
		return fleet.RouteHumanMessage(fleetCfg, "")
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
		case "human":
			// The agent was waiting for human input. Since we are recovering
			// after restart, return empty so Run() waits for a new message
			// (the comment poller will pick up any new human comments).
			return ""
		case "none":
			return ""
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
