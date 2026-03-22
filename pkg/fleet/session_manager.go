package fleet

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/schardosin/astonish/pkg/agent"
	"google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/tool"
)

// SessionState represents the current state of a fleet session.
type SessionState string

const (
	// StateIdle means the fleet is waiting for a message (from human or agent chain).
	StateIdle SessionState = "idle"
	// StateProcessing means an agent is currently being activated.
	StateProcessing SessionState = "processing"
	// StateWaitingForCustomer means an agent has requested customer input and the fleet is paused.
	StateWaitingForCustomer SessionState = "waiting_for_customer"
	// StateStopped means the fleet session has been stopped.
	StateStopped SessionState = "stopped"
)

// idleWatchdogTimeout is how long a headless session waits for a new message
// before the watchdog activates the entry point agent. This prevents sessions
// from hanging forever when routing fails or returns an empty target.
//
// Only applies to headless sessions (no human in the UI). Sessions with
// waitingAgent set (legitimately waiting for a human reply on GitHub) use a
// longer timeout to give humans time to respond.
const idleWatchdogTimeout = 5 * time.Minute

// FleetSession manages the message loop for a single fleet session.
// It monitors a channel for messages, routes them to the appropriate agent,
// activates agents, and posts their responses back to the channel.
type FleetSession struct {
	ID              string
	FleetKey        string
	FleetConfig     *FleetConfig
	Plan            *FleetPlan // Non-nil when started from a fleet plan (for prompt injection)
	Channel         Channel
	SubAgentManager *agent.SubAgentManager
	LLM             model.LLM // LLM used for routing decisions

	// ctx and cancel are set when Run() starts. ctx is used by external callers
	// (e.g., SessionRegistry.PostHumanMessage) to pass context to channel operations.
	ctx    context.Context
	cancel context.CancelFunc

	// State
	state            SessionState
	activeAgent      string // which agent is currently processing (or last processed)
	waitingAgent     string // which agent is waiting for human input
	ballWithCustomer bool   // true when ball was moved to customer (used for state tracking)
	mu               sync.RWMutex

	// OnStateChange is called whenever the session state changes.
	// Used by the API layer to stream state updates to the UI.
	OnStateChange func(state SessionState, activeAgent string)

	// OnAgentMessage is called when an agent posts a message.
	// Used for SSE streaming of agent responses.
	OnAgentMessage func(msg Message)

	// OnMessagePosted is called after every message is posted to the channel
	// (human, agent, or system). Used for transcript persistence.
	OnMessagePosted func(msg Message)

	// OnSessionDone is called when Run() exits (clean stop or error).
	// The error argument is nil for clean stops and non-nil when the session
	// stopped due to consecutive agent failures. Used by the plan activator
	// to mark issues as failed.
	OnSessionDone func(sessionID string, sessionErr error)

	// OnBallChange is called when the "ball" transitions between agents and
	// human. Used by the plan activator to update the monitor state file so
	// daemon restarts know whether to recover the session (ball=agents) or
	// just watch for new comments (ball=human).
	//
	// Values: "agents" or "customer". "failed" is handled by OnSessionDone.
	OnBallChange func(ball string)

	// lastError stores the error from Run() so SSE viewers can include it
	// in the fleet_done event. Protected by mu.
	lastError error

	// ResumeTarget, when set before Run(), is used as the initial pending
	// target agent instead of waiting for a new message. This is used during
	// session recovery to continue from where the session left off, and
	// for auto-starting chat sessions (entry point agent activates immediately).
	ResumeTarget string

	// Headless is true for sessions started by the scheduler (no human in
	// the UI). Used by the idle watchdog to decide whether to auto-activate
	// the entry point agent when the session sits idle too long.
	Headless bool

	// Progress tracks key milestones (approvals, completions, handoffs)
	// across the session lifetime. Injected into agent prompts so agents
	// always know the current project state, even when the conversation
	// thread is truncated. Survives recovery via JSONL persistence.
	Progress *ProgressTracker

	// ProjectContext holds the content of the project context file (e.g.,
	// AGENTS.md) generated at session startup. Injected into every agent's
	// system prompt so agents understand the codebase structure, conventions,
	// and build commands. Empty when the fleet template does not define a
	// project_context section.
	ProjectContext string

	// TaskSlug is a short, URL/branch-safe identifier for the task this
	// session is working on. For github_issues channels it is derived from
	// the issue number and title (e.g., "issue-6-improve-payoff-chart").
	// Used to resolve artifact branch_pattern placeholders like "fleet/{task}".
	TaskSlug string

	// WorkspaceDir is the per-session isolated workspace directory.
	// Each session gets its own copy of the project (via git clone or cp -a)
	// so parallel sessions don't collide. Set at session creation before Run().
	// When sandbox is active, this is set to "/root" (container workspace).
	WorkspaceDir string

	// SandboxTools holds sandbox-wrapped tool copies for this fleet session.
	// When set, activateAgent() uses these instead of the global SubAgentManager
	// tools. This allows fleet sessions to route tool calls through their own
	// per-session container without mutating the shared SubAgentManager singleton.
	SandboxTools []tool.Tool

	// OnCleanup is called to destroy the sandbox container when the session is
	// deleted. IMPORTANT: this is NOT called when Run() exits — headless sessions
	// exit Run() on "ball to customer" (poll/recover cycle) and the container
	// must survive for recovery. OnCleanup is invoked from session deletion paths.
	OnCleanup func()
}

// NewFleetSession creates a new fleet session.
func NewFleetSession(
	fleetKey string,
	fleetCfg *FleetConfig,
	channel Channel,
	subAgentMgr *agent.SubAgentManager,
) *FleetSession {
	return &FleetSession{
		ID:              uuid.New().String(),
		FleetKey:        fleetKey,
		FleetConfig:     fleetCfg,
		Channel:         channel,
		SubAgentManager: subAgentMgr,
		LLM:             subAgentMgr.LLM,
		state:           StateIdle,
		Progress:        NewProgressTracker(),
	}
}

// Cleanup calls the OnCleanup callback if set. Used by session deletion
// paths to destroy sandbox containers. Safe to call multiple times.
func (fs *FleetSession) Cleanup() {
	if fs.OnCleanup != nil {
		fs.OnCleanup()
	}
}

// filterSandboxTools filters sandbox-wrapped tools by an allow list.
// If allowList is empty, all tools are returned (no filtering).
func filterSandboxTools(tools []tool.Tool, allowList []string) []tool.Tool {
	if len(allowList) == 0 {
		return tools
	}
	allowSet := make(map[string]bool, len(allowList))
	for _, name := range allowList {
		allowSet[name] = true
	}
	var filtered []tool.Tool
	for _, t := range tools {
		if allowSet[t.Name()] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// GetState returns the current session state and active agent.
func (fs *FleetSession) GetState() (SessionState, string) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.state, fs.activeAgent
}

// LastError returns the error from the last Run() invocation, or nil if
// the session ended cleanly or is still running.
func (fs *FleetSession) LastError() error {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.lastError
}

// setState updates the session state and notifies listeners.
func (fs *FleetSession) setState(state SessionState, activeAgent string) {
	fs.mu.Lock()
	fs.state = state
	fs.activeAgent = activeAgent
	fs.mu.Unlock()

	if fs.OnStateChange != nil {
		fs.OnStateChange(state, activeAgent)
	}
}

// Run starts the fleet session message loop.
// It blocks until the context is cancelled or the session is stopped.
// InitContext pre-initializes the session context and cancel function.
// This allows the session to appear as "running" to external callers
// (e.g., PostHumanMessage) before Run() is called. Run() will reuse
// this context if it was pre-initialized.
func (fs *FleetSession) InitContext(ctx context.Context, cancel context.CancelFunc) {
	fs.ctx = ctx
	fs.cancel = cancel
}

func (fs *FleetSession) Run(ctx context.Context) (runErr error) {
	// If context was pre-initialized via InitContext, reuse it.
	// Otherwise create a new cancellable context from the provided one.
	if fs.ctx != nil {
		ctx = fs.ctx
	} else {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		fs.ctx = ctx
		fs.cancel = cancel
	}
	cancel := fs.cancel
	log.Printf("[fleet] Session %s started for fleet %q", fs.ID, fs.FleetKey)
	defer func() {
		cancel()
		fs.mu.Lock()
		fs.lastError = runErr
		fs.mu.Unlock()
		log.Printf("[fleet] Session %s stopped", fs.ID)
		if fs.OnSessionDone != nil {
			fs.OnSessionDone(fs.ID, runErr)
		}
	}()

	// pendingTarget holds the next agent to activate when LLM routing
	// determined who should go next. This skips WaitForMessage on the
	// next iteration to avoid the deadlock where the message is already
	// in the channel.
	var pendingTarget string

	// If resuming after a restart (or auto-starting), use the pre-computed target.
	if fs.ResumeTarget != "" {
		pendingTarget = fs.ResumeTarget
		fs.ResumeTarget = "" // consume it
		log.Printf("[fleet] Resuming session with target agent %s", pendingTarget)
	}

	// Track consecutive agent failures to prevent infinite error loops.
	// In headless sessions (no human to intervene), repeated failures mean
	// the session should stop rather than hang forever.
	const maxConsecutiveErrors = 3
	consecutiveErrors := 0

	for {
		// Check context
		if ctx.Err() != nil {
			fs.setState(StateStopped, "")
			return ctx.Err()
		}

		var targetAgent string

		if pendingTarget != "" {
			// We already know who should act next from a previous routing decision.
			targetAgent = pendingTarget
			pendingTarget = ""
			log.Printf("[fleet] Auto-chaining to agent %s", targetAgent)
		} else {
			// Wait for the next message.
			// For headless sessions, arm the idle watchdog so the session
			// does not hang forever if an agent gets stuck.
			// In headless mode, sessions exit cleanly when the ball is with
			// the customer — the plan scheduler will detect the customer's
			// reply and recover the session. This avoids keeping a session
			// (and its 15s GitHub API poller) alive while a human thinks.
			fs.setState(StateIdle, "")

			var waitCtx context.Context
			var waitCancel context.CancelFunc

			if fs.Headless {
				// Ball is always with agents here (customer-ball exits above).
				// Arm the watchdog so we detect stuck sessions.
				waitCtx, waitCancel = context.WithTimeout(ctx, idleWatchdogTimeout)
			} else {
				waitCtx, waitCancel = context.WithCancel(ctx)
			}

			msg, err := fs.Channel.WaitForMessage(waitCtx)
			waitCancel()

			if err != nil {
				if ctx.Err() != nil {
					fs.setState(StateStopped, "")
					return nil // clean shutdown (parent context cancelled)
				}
				// Idle watchdog fired: the child context timed out but the
				// parent is still alive. Activate the entry point to reassess.
				if fs.Headless && waitCtx.Err() == context.DeadlineExceeded {
					entryPoint := fs.FleetConfig.GetEntryPoint()
					log.Printf("[fleet] Idle watchdog fired for session %s (idle >%v), activating entry point @%s",
						fs.ID, idleWatchdogTimeout, entryPoint)

					watchdogMsg := Message{
						ID:        uuid.New().String(),
						Sender:    "system",
						Text:      fmt.Sprintf("Idle watchdog: no activity for %v. Re-activating @%s to reassess.", idleWatchdogTimeout, entryPoint),
						Timestamp: time.Now(),
					}
					_ = fs.Channel.PostMessage(ctx, watchdogMsg)
					fs.postExternal(watchdogMsg)
					fs.notifyMessagePosted(watchdogMsg)

					pendingTarget = entryPoint
					continue
				}
				return fmt.Errorf("waiting for message: %w", err)
			}

			// Customer messages use fast-path routing (no LLM needed)
			if msg.IsFromCustomer() {
				targetAgent = RouteCustomerMessage(fs.FleetConfig, fs.waitingAgent)

				// Customer message goes into the target agent's memory only.
				// The customer doesn't have their own "memory" in the fleet model;
				// their messages land in whichever agent receives them.
				msg.MemoryKeys = []string{targetAgent}

				// Persist customer message to transcript so it survives
				// daemon restarts. Without this, recovery reads the JSONL
				// and reconstructs the thread without any customer messages,
				// causing agents to lose all customer feedback/approvals.
				fs.notifyMessagePosted(msg)

				// Clear waiting state since customer responded
				if fs.waitingAgent != "" {
					fs.mu.Lock()
					fs.waitingAgent = ""
					fs.mu.Unlock()
				}

				// Ball moves to agents since a customer replied
				fs.notifyBallChange("agents")

				// Customer intervention resets the error counter
				consecutiveErrors = 0

				// Track milestones from customer messages (approvals, etc.)
				if fs.Progress != nil {
					for _, m := range AnalyzeCustomerMessageForMilestones(msg) {
						fs.Progress.AddMilestone(m)
					}
				}
			} else {
				// This is an agent or system message that arrived from outside
				// the main loop (e.g., a message posted by an external caller).
				// Skip it; we don't route these.
				continue
			}
		}

		// Activate the target agent
		fs.setState(StateProcessing, targetAgent)

		response, err := fs.activateAgent(ctx, targetAgent)
		if err != nil {
			consecutiveErrors++
			log.Printf("[fleet] Error activating agent %s (%d/%d): %v",
				targetAgent, consecutiveErrors, maxConsecutiveErrors, err)

			errMsg := Message{
				ID:         uuid.New().String(),
				Sender:     "system",
				Text:       fmt.Sprintf("Error from %s: %v", targetAgent, err),
				MemoryKeys: []string{targetAgent}, // error goes into the failing agent's memory
				Timestamp:  time.Now(),
			}
			if postErr := fs.Channel.PostMessage(ctx, errMsg); postErr != nil {
				log.Printf("[fleet] Error posting error message: %v", postErr)
			}
			fs.postExternal(errMsg)
			fs.notifyMessagePosted(errMsg)

			// Stop the session after too many consecutive failures.
			// In headless mode there is no human to fix the problem, so
			// continuing would just loop forever.
			if consecutiveErrors >= maxConsecutiveErrors {
				log.Printf("[fleet] Session %s stopping after %d consecutive errors", fs.ID, consecutiveErrors)
				stopMsg := Message{
					ID:     uuid.New().String(),
					Sender: "system",
					Text: fmt.Sprintf("Fleet session stopped: %d consecutive agent errors. "+
						"Last error from %s: %v", consecutiveErrors, targetAgent, err),
					Timestamp: time.Now(),
				}
				_ = fs.Channel.PostMessage(ctx, stopMsg)
				fs.postExternal(stopMsg)
				fs.notifyMessagePosted(stopMsg)
				fs.setState(StateStopped, "")
				return fmt.Errorf("stopped after %d consecutive errors", consecutiveErrors)
			}

			// For retriable errors (timeouts, network failures), retry the
			// same agent instead of falling into WaitForMessage where the
			// session would hang forever in headless mode with no human to
			// send a new message.
			if isRetriableError(err) {
				pendingTarget = targetAgent
				log.Printf("[fleet] Will retry agent %s (retriable error)", targetAgent)
			}

			continue
		}

		// Successful activation resets the error counter
		consecutiveErrors = 0

		// Use LLM to determine who should act next. The routing decision
		// determines who "owns" this message in memory.
		routing := RouteWithLLM(ctx, response, fs.FleetConfig, fs.LLM)
		log.Printf("[fleet] Routing decision for @%s's message: target=%s reason=%s",
			response.Sender, routing.Target, routing.Reason)

		// Stamp MemoryKeys based on routing decision:
		//   → customer: agent's own memory only (customer sees it externally)
		//   → self:     agent's own memory only (private continuation)
		//   → none:     agent's own memory only (final message)
		//   → other:    both sender + target memory (handoff/shared conversation)
		switch routing.Target {
		case "customer", "self", "none":
			response.MemoryKeys = []string{response.Sender}
		default:
			response.MemoryKeys = []string{response.Sender, routing.Target}
		}

		// Post agent's response to the channel (internal thread + SSE only;
		// GitHub posting is deferred until after routing to avoid flooding
		// the issue with intermediate messages during self-routing chains).
		if postErr := fs.Channel.PostMessage(ctx, response); postErr != nil {
			log.Printf("[fleet] Error posting agent response: %v", postErr)
			continue
		}
		fs.notifyMessagePosted(response)

		// Notify listeners
		if fs.OnAgentMessage != nil {
			fs.OnAgentMessage(response)
		}

		// Track milestones from the agent's response (approvals, completions, handoffs)
		if fs.Progress != nil {
			for _, m := range AnalyzeMessageForMilestones(response) {
				fs.Progress.AddMilestone(m)
			}
		}

		switch routing.Target {
		case "customer":
			// Post to GitHub — this is a deliverable directed at the customer.
			fs.postExternal(response)

			fs.mu.Lock()
			fs.waitingAgent = response.Sender
			fs.mu.Unlock()
			fs.setState(StateWaitingForCustomer, response.Sender)
			fs.notifyBallChange("customer")

			// In headless mode, exit cleanly. The plan scheduler will
			// detect the customer's reply and recover the session.
			if fs.Headless {
				log.Printf("[fleet] Session %s: ball moved to customer, exiting (headless)", fs.ID)
				return nil
			}

		case "self":
			// Sender still has the action; re-activate them.
			// Do NOT post to GitHub — this is an intermediate step in a
			// multi-activation chain. The message is already in the agent's
			// memory and SSE (for UI).
			pendingTarget = response.Sender

		case "none":
			// Post to GitHub — this is a final message with no follow-up.
			fs.postExternal(response)

			// No one needs to act; go idle and wait for next message.
			// Ball moves to customer since no agent has pending work.
			fs.notifyBallChange("customer")

			// In headless mode, exit cleanly. The plan scheduler will
			// detect the customer's reply and recover the session.
			if fs.Headless {
				log.Printf("[fleet] Session %s: no agent has pending work, exiting (headless)", fs.ID)
				return nil
			}
			continue

		default:
			// Post to GitHub — this message is being handed off to another agent,
			// so it represents a meaningful communication step.
			fs.postExternal(response)

			// Route to the specified agent.
			if fs.FleetConfig.CanTalkTo(response.Sender, routing.Target) {
				pendingTarget = routing.Target
			} else {
				log.Printf("[fleet] Warning: LLM routed to %s but %s cannot talk to them, ignoring",
					routing.Target, response.Sender)
			}
		}
	}
}

// notifyMessagePosted calls the OnMessagePosted callback if set.
func (fs *FleetSession) notifyMessagePosted(msg Message) {
	if fs.OnMessagePosted != nil {
		fs.OnMessagePosted(msg)
	}
}

// postExternal posts a message to the external system (e.g., GitHub) if the
// channel supports it. This is separate from PostMessage so the Run loop can
// control when external posting happens (e.g., deferring until after routing).
func (fs *FleetSession) postExternal(msg Message) {
	if poster, ok := fs.Channel.(ExternalPoster); ok {
		poster.PostExternal(msg)
	}
}

// notifyBallChange calls the OnBallChange callback if set and updates
// the internal ballWithCustomer flag.
func (fs *FleetSession) notifyBallChange(ball string) {
	fs.mu.Lock()
	fs.ballWithCustomer = (ball == "customer")
	fs.mu.Unlock()
	if fs.OnBallChange != nil {
		fs.OnBallChange(ball)
	}
}

// activateAgent builds the context and runs the agent as a sub-agent.
// The agent's context is built from their personal memory — all messages
// addressed to them (MemoryKeys contains agentKey) plus system messages.
//
// It wires an OnEvent callback to post intermediate progress messages to the
// channel as the agent works, so the team sees real-time status updates instead
// of one large message at the end.
func (fs *FleetSession) activateAgent(ctx context.Context, agentKey string) (Message, error) {
	agentCfg, ok := fs.FleetConfig.Agents[agentKey]
	if !ok {
		return Message{}, fmt.Errorf("agent %q not found in fleet", agentKey)
	}

	// Build system prompt with communication graph awareness
	systemPrompt := BuildAgentPrompt(agentCfg, fs.FleetConfig, agentKey, fs.Progress, fs.ProjectContext, fs.TaskSlug, fs.WorkspaceDir, fs.Plan)

	// Build thread context from the agent's personal memory
	threadContext, err := BuildThreadContext(ctx, fs.Channel, agentKey)
	if err != nil {
		return Message{}, fmt.Errorf("building agent memory context: %w", err)
	}

	// Build tool filter
	toolFilter := buildAgentToolFilter(agentCfg)

	// Build task description.
	// When the thread is empty or very short (no actionable content yet),
	// instruct the agent to greet and wait rather than proactively exploring.
	var taskDescription string
	if len(strings.TrimSpace(threadContext)) == 0 {
		taskDescription = fmt.Sprintf(
			"You are @%s in a team conversation. The conversation just started and there are no messages yet.\n"+
				"Introduce yourself briefly to the customer and ask what they would like to work on.\n"+
				"Do NOT use any tools. Do NOT explore the project. Just greet and ask.",
			agentKey,
		)
	} else {
		taskDescription = fmt.Sprintf(
			"You are @%s in a team conversation. Read the conversation thread below and respond.\n\n%s",
			agentKey, threadContext,
		)
	}

	// Determine timeout. Fleet agents do multi-step work (multiple LLM calls,
	// tool executions, file reads/writes) within a single activation. The
	// timeout covers the entire activation, not individual LLM calls.
	// OpenCode tasks can take 30-45 minutes for complex multi-step work,
	// so the fleet agent timeout must exceed that to allow completion.
	timeoutOverride := 60 * time.Minute

	// Track intermediate text for real-time progress messaging.
	// When the LLM produces text followed by a tool call in the same turn,
	// that text is a progress update (e.g., "I'll start by reading the docs").
	// We post it immediately so the team sees real-time status updates.
	var intermediateTextBuf strings.Builder
	var intermediatesMu sync.Mutex

	postIntermediateMessage := func(text string) {
		text = strings.TrimSpace(text)
		text = stripInlineToolCalls(text)
		if text == "" {
			return
		}
		msg := Message{
			ID:         uuid.New().String(),
			Sender:     agentKey,
			Text:       text,
			MemoryKeys: []string{agentKey}, // intermediate work is private to the agent
			Mentions:   ParseMentions(text),
			Timestamp:  time.Now(),
			Metadata: map[string]any{
				"intermediate": true,
			},
		}
		if postErr := fs.Channel.PostMessage(ctx, msg); postErr != nil {
			log.Printf("[fleet] Error posting intermediate message from %s: %v", agentKey, postErr)
			return
		}
		fs.notifyMessagePosted(msg)
		if fs.OnAgentMessage != nil {
			fs.OnAgentMessage(msg)
		}
	}

	onEvent := func(event *adksession.Event) {
		if event == nil || event.LLMResponse.Content == nil {
			return
		}

		intermediatesMu.Lock()
		defer intermediatesMu.Unlock()

		// Examine the parts in this event. An LLM turn can contain both
		// text and function calls. If there are function calls, any text
		// in this turn is an intermediate progress update.
		// Skip thought/reasoning parts (part.Thought) — these are internal
		// chain-of-thought and should never appear in the channel.
		var turnText string
		hasFunctionCall := false

		for _, part := range event.LLMResponse.Content.Parts {
			if part.Text != "" && !part.Thought {
				turnText += part.Text
			}
			if part.FunctionCall != nil {
				hasFunctionCall = true
			}
		}

		if turnText != "" {
			if hasFunctionCall {
				// Text + tool call in the same turn: this is an intermediate
				// progress update. Flush any buffered text along with this text.
				intermediateTextBuf.WriteString(turnText)
				postIntermediateMessage(intermediateTextBuf.String())
				intermediateTextBuf.Reset()
			} else {
				// Text only, no tool call. This could be the final message
				// or a continuation. Buffer it; it will either be flushed by
				// a subsequent tool call turn or become the final output.
				intermediateTextBuf.WriteString(turnText)
			}
		} else if hasFunctionCall && intermediateTextBuf.Len() > 0 {
			// FunctionCall arrived in a separate event from the text (some
			// providers split text and tool calls across events instead of
			// combining them). The previously buffered text is a progress
			// update — flush it as intermediate.
			postIntermediateMessage(intermediateTextBuf.String())
			intermediateTextBuf.Reset()
		}
	}

	// Execute as sub-agent
	task := agent.SubAgentTask{
		Name:            fmt.Sprintf("fleet-%s-%s", fs.FleetKey, agentKey),
		Instructions:    systemPrompt,
		Description:     taskDescription,
		ToolFilter:      toolFilter,
		ParentID:        fs.ID,
		CustomPrompt:    true,
		ParentDepth:     0,
		TimeoutOverride: timeoutOverride,
		OnEvent:         onEvent,
	}

	// When sandbox tools are set, filter them by the agent's tool filter
	// and use as OverrideTools. This routes tool calls through the fleet
	// session's own container without mutating the global SubAgentManager.
	if fs.SandboxTools != nil {
		task.OverrideTools = filterSandboxTools(fs.SandboxTools, toolFilter)
	}

	result := fs.SubAgentManager.RunTask(ctx, task)

	if result.Status == "error" || result.Status == "timeout" {
		return Message{}, fmt.Errorf("agent %s %s: %s", agentKey, result.Status, result.Error)
	}

	// The final output from RunTask is the concatenation of ALL text parts
	// (including the ones we already posted as intermediate messages).
	// We need to extract just the final portion that was NOT yet posted.
	// The buffered text in intermediateTextBuf is the unpublished final text.
	intermediatesMu.Lock()
	finalText := strings.TrimSpace(intermediateTextBuf.String())
	intermediatesMu.Unlock()

	// If the agent produced no unpublished text (e.g., the last turn also had
	// a tool call and was posted as intermediate), fall back to the full result.
	// This is a safety net; normally the last turn is text-only.
	if finalText == "" {
		finalText = strings.TrimSpace(result.Result)
	}

	// Strip inline tool calls that some models emit as plain text (e.g.,
	// write_file{"file_path":"...","content":"..."}) instead of structured
	// FunctionCall parts. These pollute GitHub comments with raw file contents.
	finalText = stripInlineToolCalls(finalText)

	// Parse mentions from the final response
	mentions := ParseMentions(finalText)

	return Message{
		ID:        uuid.New().String(),
		Sender:    agentKey,
		Text:      finalText,
		Mentions:  mentions,
		Timestamp: time.Now(),
		Metadata: map[string]any{
			"tool_calls": result.ToolCalls,
			"duration":   result.Duration.String(),
		},
	}, nil
}

// buildAgentToolFilter builds the tool filter for a fleet agent.
func buildAgentToolFilter(agentCfg FleetAgentConfig) []string {
	var toolFilter []string
	if !agentCfg.Tools.All && len(agentCfg.Tools.Names) > 0 {
		toolFilter = agentCfg.Tools.Names
	}
	if agentCfg.Delegate != nil {
		toolFilter = ensureTool(toolFilter, agentCfg.Delegate.Tool)
		toolFilter = ensureTool(toolFilter, "read_file")
		toolFilter = ensureTool(toolFilter, "write_file")
		toolFilter = ensureTool(toolFilter, "grep_search")
	}
	return toolFilter
}

// ensureTool adds a tool to the filter if not already present.
func ensureTool(tools []string, name string) []string {
	for _, t := range tools {
		if t == name {
			return tools
		}
	}
	return append(tools, name)
}

// Stop gracefully stops the fleet session.
func (fs *FleetSession) Stop() {
	fs.setState(StateStopped, "")
	if fs.cancel != nil {
		fs.cancel()
	}
	if err := fs.Channel.Close(); err != nil {
		log.Printf("[fleet] Error closing channel: %v", err)
	}
}

// isRetriableError returns true for transient errors that are worth retrying
// (timeouts, network failures, server errors). Returns false for errors that
// indicate a configuration problem (missing persona, missing agent, etc.)
// which would fail again immediately.
func isRetriableError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()

	retriablePatterns := []string{
		"context deadline exceeded",
		"timeout",
		"connection refused",
		"connection reset",
		"TLS handshake",
		"i/o timeout",
		"no such host",
		"server misbehaving",
		"500",
		"502",
		"503",
		"504",
		"429", // rate limited
	}

	lower := strings.ToLower(msg)
	for _, pattern := range retriablePatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// inlineToolCallPattern matches tool call text that models sometimes emit as
// plain text instead of structured FunctionCall parts. The pattern matches
// snake_case identifiers (2+ chars) immediately followed by a '{' and captures
// the start position so we can find the matching closing brace.
var inlineToolCallPattern = regexp.MustCompile(`[a-z][a-z0-9_]+\{`)

// stripInlineToolCalls removes tool call text that some models emit as plain
// text (e.g., `write_file{"file_path":"...","content":"..."}`) instead of
// structured FunctionCall parts. These pollute GitHub issue comments with
// raw file contents and tool arguments.
//
// The function finds snake_case_name{ patterns and removes everything from
// the tool name through the matching closing brace (handling nested braces).
func stripInlineToolCalls(text string) string {
	for {
		loc := inlineToolCallPattern.FindStringIndex(text)
		if loc == nil {
			break
		}

		// Find the matching closing brace starting from the '{' position
		braceStart := loc[1] - 1 // position of '{'
		depth := 0
		end := -1
		inString := false
		escaped := false

		for i := braceStart; i < len(text); i++ {
			ch := text[i]
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' && inString {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = !inString
				continue
			}
			if inString {
				continue
			}
			if ch == '{' {
				depth++
			} else if ch == '}' {
				depth--
				if depth == 0 {
					end = i + 1
					break
				}
			}
		}

		if end == -1 {
			// No matching brace found — strip from tool name to end of text
			text = strings.TrimSpace(text[:loc[0]])
			break
		}

		// Remove the tool call and any surrounding whitespace
		before := strings.TrimRight(text[:loc[0]], " \t")
		after := strings.TrimLeft(text[end:], " \t\n")
		text = before
		if after != "" {
			if text != "" {
				text += "\n"
			}
			text += after
		}
	}

	return strings.TrimSpace(text)
}
