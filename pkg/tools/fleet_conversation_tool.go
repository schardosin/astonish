package tools

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/persona"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// --- run_fleet_phase tool (unified: handles both single-agent and conversation phases) ---

// RunFleetPhaseArgs is the input schema for the run_fleet_phase tool.
type RunFleetPhaseArgs struct {
	Fleet        string            `json:"fleet" jsonschema:"The fleet key (e.g., 'software-dev')."`
	Phase        string            `json:"phase" jsonschema:"Phase name for tracking (e.g., 'requirements', 'design', 'implementation')."`
	Primary      string            `json:"primary" jsonschema:"Agent key that executes the phase and produces deliverables (e.g., 'po', 'architect', 'dev')."`
	Reviewers    []string          `json:"reviewers,omitempty" jsonschema:"Optional: agent keys that review and discuss with the primary before deliverables are produced (e.g., ['po']). When provided, enables multi-agent conversation mode."`
	Goal         string            `json:"goal" jsonschema:"The task/goal for this phase. Include ALL context: original user request, prior phase outputs, file paths, constraints, decisions made."`
	Artifacts    map[string]string `json:"artifacts,omitempty" jsonschema:"Optional: input artifacts from prior phases. Map of name to file path (e.g., {'requirements': '~/project/docs/requirements.md'})."`
	Deliverables []string          `json:"deliverables,omitempty" jsonschema:"Optional: expected output file paths. When provided, the phase ends when all files exist on disk."`
	MaxTurns     int               `json:"max_turns,omitempty" jsonschema:"Optional: max conversation turns (only used when reviewers are present). Default: 10."`
	Timeout      int               `json:"timeout,omitempty" jsonschema:"Optional: timeout in seconds for the phase. Default: 300 (5 minutes). Use 600+ for phases that delegate to OpenCode."`
}

// RunFleetPhaseResult is the output of the run_fleet_phase tool.
type RunFleetPhaseResult struct {
	Status       string   `json:"status"`                 // "success", "completed", "max_turns_reached", "error", "timeout"
	Phase        string   `json:"phase"`                  // Phase name
	Agent        string   `json:"agent"`                  // Primary agent key
	Persona      string   `json:"persona"`                // Primary agent persona name
	Result       string   `json:"result,omitempty"`       // Text output (single-agent mode) or conversation summary
	TurnsUsed    int      `json:"turns_used"`             // Number of turns taken
	ToolCalls    int      `json:"tool_calls"`             // Number of tool calls (single-agent mode)
	Deliverables []string `json:"deliverables,omitempty"` // Deliverable files created
	Missing      []string `json:"missing,omitempty"`      // Deliverable files still missing
	Duration     string   `json:"duration"`               // Wall clock time
	Error        string   `json:"error,omitempty"`        // Error message
}

// conversationTurn tracks a single turn in a multi-agent conversation.
type conversationTurn struct {
	Agent   string // Agent key that took this turn
	Role    string // "primary" or "reviewer"
	Message string // The agent's output (response text)
}

func runFleetPhase(ctx tool.Context, args RunFleetPhaseArgs) (RunFleetPhaseResult, error) {
	if subAgentManagerVar == nil {
		return RunFleetPhaseResult{
			Status: "error",
			Error:  "Sub-agent system is not available. Ensure sub_agents is enabled in config.",
		}, nil
	}
	if fleetRegistryVar == nil || personaRegistryVar == nil {
		return RunFleetPhaseResult{
			Status: "error",
			Error:  "Fleet system is not initialized.",
		}, nil
	}

	// Resolve fleet
	fleetCfg, ok := fleetRegistryVar.GetFleet(args.Fleet)
	if !ok {
		available := fleetRegistryVar.ListFleets()
		keys := make([]string, len(available))
		for i, f := range available {
			keys[i] = f.Key
		}
		return RunFleetPhaseResult{
			Status: "error",
			Error:  fmt.Sprintf("Fleet %q not found. Available fleets: %s", args.Fleet, strings.Join(keys, ", ")),
		}, nil
	}

	// Resolve primary agent
	primaryAgentCfg, ok := fleetCfg.Agents[args.Primary]
	if !ok {
		agentKeys := make([]string, 0, len(fleetCfg.Agents))
		for k := range fleetCfg.Agents {
			agentKeys = append(agentKeys, k)
		}
		return RunFleetPhaseResult{
			Status: "error",
			Error:  fmt.Sprintf("Agent %q not found in fleet %q. Available agents: %s", args.Primary, args.Fleet, strings.Join(agentKeys, ", ")),
		}, nil
	}
	primaryPersona, ok := personaRegistryVar.GetPersona(primaryAgentCfg.Persona)
	if !ok {
		return RunFleetPhaseResult{
			Status: "error",
			Error:  fmt.Sprintf("Persona %q not found for agent %q.", primaryAgentCfg.Persona, args.Primary),
		}, nil
	}

	// Determine timeout override
	var timeoutOverride time.Duration
	if args.Timeout > 0 {
		if args.Timeout > 3600 {
			args.Timeout = 3600
		}
		timeoutOverride = time.Duration(args.Timeout) * time.Second
	}

	// Dispatch: conversation mode (with reviewers) or single-agent mode
	if len(args.Reviewers) > 0 {
		return runConversationPhase(ctx, args, fleetCfg, primaryAgentCfg, primaryPersona, timeoutOverride)
	}
	return runSingleAgentPhase(ctx, args, primaryAgentCfg, primaryPersona, timeoutOverride)
}

// runSingleAgentPhase executes a phase with a single agent (no reviewers).
func runSingleAgentPhase(
	ctx tool.Context,
	args RunFleetPhaseArgs,
	agentCfg fleet.FleetAgentConfig,
	personaCfg *persona.PersonaConfig,
	timeoutOverride time.Duration,
) (RunFleetPhaseResult, error) {
	// Build sub-agent prompt
	subAgentPrompt := fleet.BuildSubAgentPrompt(personaCfg, agentCfg)

	// Build tool filter
	toolFilter := buildAgentToolFilter(agentCfg)

	// Look up progress channel
	var workerOnEvent func(event *adksession.Event)
	if parentSessionID, err := ctx.State().Get(FleetParentSessionKey); err == nil {
		if mainID, ok := parentSessionID.(string); ok {
			if ch := getFleetProgressChWrite(mainID); ch != nil {
				workerOnEvent = buildWorkerProgressCallback(ch, args.Phase, args.Primary)

				// Send phase start event
				ch <- FleetProgressEvent{
					Type:    "phase_start",
					Phase:   args.Phase,
					Agent:   args.Primary,
					Message: fmt.Sprintf("Starting phase: %s (agent: %s)", args.Phase, args.Primary),
				}
			}
		}
	}

	// Build sub-agent task description with artifacts and deliverables context
	taskDescription := buildSingleAgentTaskDescription(args)

	// Forward FleetParentSessionKey, phase, and agent so tools inside the worker
	// (e.g., opencode) can look up the progress channel and tag events correctly.
	var workerSessionState map[string]any
	if parentSessionID, err := ctx.State().Get(FleetParentSessionKey); err == nil {
		if mainID, ok := parentSessionID.(string); ok && mainID != "" {
			workerSessionState = map[string]any{
				FleetParentSessionKey: mainID,
				FleetPhaseKey:         args.Phase,
				FleetAgentKey:         args.Primary,
			}
		}
	}

	task := agent.SubAgentTask{
		Name:            fmt.Sprintf("fleet-%s-%s", args.Fleet, args.Phase),
		Instructions:    subAgentPrompt,
		Description:     taskDescription,
		ToolFilter:      toolFilter,
		ParentID:        ctx.SessionID(),
		CustomPrompt:    true,
		ParentDepth:     1,
		OnEvent:         workerOnEvent,
		TimeoutOverride: timeoutOverride,
		SessionState:    workerSessionState,
	}

	result := subAgentManagerVar.RunTask(ctx, task)

	// Check deliverables if specified
	var created, missing []string
	if len(args.Deliverables) > 0 {
		created, missing = checkDeliverables(args.Deliverables)
	}

	return RunFleetPhaseResult{
		Status:       result.Status,
		Phase:        args.Phase,
		Agent:        args.Primary,
		Persona:      personaCfg.Name,
		Result:       result.Result,
		TurnsUsed:    1,
		ToolCalls:    result.ToolCalls,
		Deliverables: created,
		Missing:      missing,
		Duration:     result.Duration.Round(100 * 1e6).String(),
		Error:        result.Error,
	}, nil
}

// runConversationPhase executes a multi-agent conversation phase.
func runConversationPhase(
	ctx tool.Context,
	args RunFleetPhaseArgs,
	fleetCfg *fleet.FleetConfig,
	primaryAgentCfg fleet.FleetAgentConfig,
	primaryPersona *persona.PersonaConfig,
	timeoutOverride time.Duration,
) (RunFleetPhaseResult, error) {
	// Resolve reviewer agents
	type resolvedAgent struct {
		key     string
		config  fleet.FleetAgentConfig
		persona string // persona display name
	}
	var reviewers []resolvedAgent
	for _, rKey := range args.Reviewers {
		rCfg, ok := fleetCfg.Agents[rKey]
		if !ok {
			return RunFleetPhaseResult{
				Status: "error",
				Error:  fmt.Sprintf("Reviewer agent %q not found in fleet %q.", rKey, args.Fleet),
			}, nil
		}
		rPersona, ok := personaRegistryVar.GetPersona(rCfg.Persona)
		if !ok {
			return RunFleetPhaseResult{
				Status: "error",
				Error:  fmt.Sprintf("Persona %q not found for reviewer agent %q.", rCfg.Persona, rKey),
			}, nil
		}
		reviewers = append(reviewers, resolvedAgent{
			key:     rKey,
			config:  rCfg,
			persona: rPersona.Name,
		})
	}

	maxTurns := args.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 10
	}

	// Look up progress channel and parent session ID for forwarding to workers
	var progressCh chan FleetProgressEvent
	var fleetMainID string
	if parentSessionID, err := ctx.State().Get(FleetParentSessionKey); err == nil {
		if mainID, ok := parentSessionID.(string); ok && mainID != "" {
			fleetMainID = mainID
			progressCh = getFleetProgressChWrite(mainID)
		}
	}

	sendProgress := func(evt FleetProgressEvent) {
		if progressCh != nil {
			progressCh <- evt
		}
	}

	sendProgress(FleetProgressEvent{
		Type:    "phase_start",
		Phase:   args.Phase,
		Agent:   args.Primary,
		Message: fmt.Sprintf("Starting phase: %s (primary: %s, reviewers: %s)", args.Phase, args.Primary, strings.Join(args.Reviewers, ", ")),
	})

	// Build artifact summary for prompts
	artifactSummary := buildArtifactSummary(args.Artifacts)

	// Conversation loop
	var turns []conversationTurn
	var lastMessage string
	totalToolCalls := 0

	for turn := 0; turn < maxTurns; turn++ {
		var currentAgent string
		var currentRole string
		var taskDescription string
		var agentCfg fleet.FleetAgentConfig

		if turn == 0 {
			currentAgent = args.Primary
			currentRole = "primary"
			agentCfg = primaryAgentCfg
			taskDescription = buildPrimaryFirstTurnPrompt(args, artifactSummary)
		} else if turns[len(turns)-1].Role == "primary" {
			reviewer := reviewers[turn%len(reviewers)]
			currentAgent = reviewer.key
			currentRole = "reviewer"
			agentCfg = reviewer.config
			taskDescription = buildReviewerTurnPrompt(args, artifactSummary, args.Primary, primaryPersona.Name, lastMessage)
		} else {
			currentAgent = args.Primary
			currentRole = "primary"
			agentCfg = primaryAgentCfg
			reviewerKey := turns[len(turns)-1].Agent
			reviewerName := reviewerKey
			for _, r := range reviewers {
				if r.key == reviewerKey {
					reviewerName = r.persona
					break
				}
			}
			taskDescription = buildPrimaryFollowUpPrompt(args, artifactSummary, reviewerKey, reviewerName, lastMessage)
		}

		// Build sub-agent prompt
		persona, _ := personaRegistryVar.GetPersona(agentCfg.Persona)
		conversationPrompt := fleet.BuildConversationAgentPrompt(persona, agentCfg, currentRole)

		// Build tool filter
		toolFilter := buildAgentToolFilter(agentCfg)

		sendProgress(FleetProgressEvent{
			Type:    "conversation_turn",
			Phase:   args.Phase,
			Agent:   currentAgent,
			Message: fmt.Sprintf("[%s] Turn %d: %s (%s)", args.Phase, turn+1, currentAgent, currentRole),
			Detail:  currentRole,
		})

		// Build OnEvent callback for this turn
		var workerOnEvent func(event *adksession.Event)
		if progressCh != nil {
			workerOnEvent = buildWorkerProgressCallback(progressCh, args.Phase, currentAgent)
		}

		// Build per-turn worker session state with phase/agent context
		var turnSessionState map[string]any
		if fleetMainID != "" {
			turnSessionState = map[string]any{
				FleetParentSessionKey: fleetMainID,
				FleetPhaseKey:         args.Phase,
				FleetAgentKey:         currentAgent,
			}
		}

		// Execute the turn as a sub-agent task
		task := agent.SubAgentTask{
			Name:            fmt.Sprintf("fleet-%s-%s-%s-t%d", args.Fleet, args.Phase, currentAgent, turn+1),
			Instructions:    conversationPrompt,
			Description:     taskDescription,
			ToolFilter:      toolFilter,
			ParentID:        ctx.SessionID(),
			CustomPrompt:    true,
			ParentDepth:     1,
			OnEvent:         workerOnEvent,
			TimeoutOverride: timeoutOverride,
			SessionState:    turnSessionState,
		}

		result := subAgentManagerVar.RunTask(ctx, task)
		totalToolCalls += result.ToolCalls

		if result.Status == "error" || result.Status == "timeout" {
			sendProgress(FleetProgressEvent{
				Type:    "conversation_turn_failed",
				Phase:   args.Phase,
				Agent:   currentAgent,
				Message: fmt.Sprintf("[%s] Turn %d failed: %s", args.Phase, turn+1, result.Error),
			})

			turns = append(turns, conversationTurn{
				Agent:   currentAgent,
				Role:    currentRole,
				Message: fmt.Sprintf("ERROR: %s", result.Error),
			})

			// Check if deliverables were created despite the error (e.g., timeout after OpenCode finished writing)
			if len(args.Deliverables) > 0 && currentRole == "primary" {
				created, missing := checkDeliverables(args.Deliverables)
				if len(missing) == 0 {
					sendProgress(FleetProgressEvent{
						Type:    "phase_complete",
						Phase:   args.Phase,
						Agent:   args.Primary,
						Message: fmt.Sprintf("[%s] Phase complete: deliverables found on disk despite error after %d turns", args.Phase, len(turns)),
					})
					return RunFleetPhaseResult{
						Status:       "completed",
						Phase:        args.Phase,
						Agent:        args.Primary,
						Persona:      primaryPersona.Name,
						Result:       buildConversationSummary(turns),
						TurnsUsed:    len(turns),
						ToolCalls:    totalToolCalls,
						Deliverables: created,
					}, nil
				}
			}

			if currentRole == "primary" {
				return RunFleetPhaseResult{
					Status:    "error",
					Phase:     args.Phase,
					Agent:     args.Primary,
					Persona:   primaryPersona.Name,
					Result:    buildConversationSummary(turns),
					TurnsUsed: len(turns),
					ToolCalls: totalToolCalls,
					Error:     fmt.Sprintf("Primary agent %q failed on turn %d: %s", currentAgent, turn+1, result.Error),
				}, nil
			}

			lastMessage = fmt.Sprintf("(Reviewer %s was unable to respond: %s)", currentAgent, result.Error)
			continue
		}

		lastMessage = result.Result
		turns = append(turns, conversationTurn{
			Agent:   currentAgent,
			Role:    currentRole,
			Message: lastMessage,
		})

		sendProgress(FleetProgressEvent{
			Type:    "conversation_turn_complete",
			Phase:   args.Phase,
			Agent:   currentAgent,
			Message: fmt.Sprintf("[%s] Turn %d complete: %s (%s)", args.Phase, turn+1, currentAgent, currentRole),
			Text:    truncateForProgress(lastMessage),
		})

		// After a primary turn, check if deliverables exist on disk
		if currentRole == "primary" && len(args.Deliverables) > 0 {
			created, missing := checkDeliverables(args.Deliverables)
			if len(missing) == 0 {
				sendProgress(FleetProgressEvent{
					Type:    "phase_complete",
					Phase:   args.Phase,
					Agent:   args.Primary,
					Message: fmt.Sprintf("[%s] Phase complete: all deliverables produced after %d turns", args.Phase, len(turns)),
				})

				return RunFleetPhaseResult{
					Status:       "completed",
					Phase:        args.Phase,
					Agent:        args.Primary,
					Persona:      primaryPersona.Name,
					Result:       buildConversationSummary(turns),
					TurnsUsed:    len(turns),
					ToolCalls:    totalToolCalls,
					Deliverables: created,
				}, nil
			}
		}
	}

	// Max turns reached
	var created, missing []string
	if len(args.Deliverables) > 0 {
		created, missing = checkDeliverables(args.Deliverables)
	}

	sendProgress(FleetProgressEvent{
		Type:    "phase_complete",
		Phase:   args.Phase,
		Agent:   args.Primary,
		Message: fmt.Sprintf("[%s] Max turns (%d) reached. Created: %d, Missing: %d", args.Phase, maxTurns, len(created), len(missing)),
	})

	status := "max_turns_reached"
	if len(missing) == 0 {
		status = "completed"
	}

	return RunFleetPhaseResult{
		Status:       status,
		Phase:        args.Phase,
		Agent:        args.Primary,
		Persona:      primaryPersona.Name,
		Result:       buildConversationSummary(turns),
		TurnsUsed:    len(turns),
		ToolCalls:    totalToolCalls,
		Deliverables: created,
		Missing:      missing,
	}, nil
}

// --- Helper functions ---

// buildAgentToolFilter builds the tool filter for a fleet agent.
func buildAgentToolFilter(agentCfg fleet.FleetAgentConfig) []string {
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

// buildSingleAgentTaskDescription wraps the goal with artifact and deliverable context.
func buildSingleAgentTaskDescription(args RunFleetPhaseArgs) string {
	var sb strings.Builder

	sb.WriteString(args.Goal)

	if len(args.Artifacts) > 0 {
		sb.WriteString("\n\n## Input Artifacts\n\n")
		sb.WriteString(buildArtifactSummary(args.Artifacts))
	}

	if len(args.Deliverables) > 0 {
		sb.WriteString("\n\n## Expected Deliverables\n\n")
		sb.WriteString("Write these files to disk:\n")
		for _, d := range args.Deliverables {
			sb.WriteString(fmt.Sprintf("- `%s`\n", d))
		}
	}

	return sb.String()
}

// buildArtifactSummary creates a readable summary of input artifacts for prompts.
func buildArtifactSummary(artifacts map[string]string) string {
	if len(artifacts) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Input artifacts from prior phases:\n")
	for name, path := range artifacts {
		sb.WriteString(fmt.Sprintf("- **%s**: `%s` (read this file for context)\n", name, path))
	}
	return sb.String()
}

// buildPrimaryFirstTurnPrompt creates the task description for the primary agent's first turn.
func buildPrimaryFirstTurnPrompt(args RunFleetPhaseArgs, artifactSummary string) string {
	var sb strings.Builder

	sb.WriteString("## Your Goal\n\n")
	sb.WriteString(args.Goal)
	sb.WriteString("\n\n")

	if artifactSummary != "" {
		sb.WriteString("## Input Artifacts\n\n")
		sb.WriteString(artifactSummary)
		sb.WriteString("\n")
	}

	if len(args.Deliverables) > 0 {
		sb.WriteString("## Expected Deliverables\n\n")
		sb.WriteString("You must produce these files:\n")
		for _, d := range args.Deliverables {
			sb.WriteString(fmt.Sprintf("- `%s`\n", d))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Instructions\n\n")
	sb.WriteString("1. Read any input artifacts listed above to understand the context.\n")
	sb.WriteString("2. If anything is unclear or ambiguous, respond with specific questions.\n")
	sb.WriteString("   Your questions will be forwarded to a reviewer who can clarify.\n")
	sb.WriteString("3. If everything is clear, produce the deliverables by writing them to the specified file paths.\n")
	sb.WriteString("4. If you have questions, do NOT produce deliverables yet. Ask your questions first.\n")

	return sb.String()
}

// buildReviewerTurnPrompt creates the task description for a reviewer turn.
func buildReviewerTurnPrompt(args RunFleetPhaseArgs, artifactSummary, primaryKey, primaryName, lastMessage string) string {
	var sb strings.Builder

	sb.WriteString("## Context\n\n")
	sb.WriteString(fmt.Sprintf("You are participating in a conversation with the **%s** (%s).\n\n", primaryName, primaryKey))
	sb.WriteString("### Original Goal\n\n")
	sb.WriteString(args.Goal)
	sb.WriteString("\n\n")

	if artifactSummary != "" {
		sb.WriteString("### Input Artifacts\n\n")
		sb.WriteString(artifactSummary)
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("### Message from %s\n\n", primaryName))
	sb.WriteString(lastMessage)
	sb.WriteString("\n\n")

	sb.WriteString("## Instructions\n\n")
	sb.WriteString("1. Read the message above carefully.\n")
	sb.WriteString("2. If the primary agent asked questions, answer them thoroughly based on your expertise.\n")
	sb.WriteString("3. If you need to update any artifacts (e.g., clarify requirements by editing a file), do so.\n")
	sb.WriteString("4. Provide a clear response that addresses all points raised.\n")

	return sb.String()
}

// buildPrimaryFollowUpPrompt creates the task description for subsequent primary turns.
func buildPrimaryFollowUpPrompt(args RunFleetPhaseArgs, artifactSummary, reviewerKey, reviewerName, lastMessage string) string {
	var sb strings.Builder

	sb.WriteString("## Context\n\n")
	sb.WriteString(fmt.Sprintf("The **%s** (%s) has responded to your questions.\n\n", reviewerName, reviewerKey))

	sb.WriteString("### Original Goal\n\n")
	sb.WriteString(args.Goal)
	sb.WriteString("\n\n")

	if artifactSummary != "" {
		sb.WriteString("### Input Artifacts\n\n")
		sb.WriteString(artifactSummary)
		sb.WriteString("(These may have been updated by the reviewer. Re-read them for the latest content.)\n\n")
	}

	sb.WriteString(fmt.Sprintf("### Response from %s\n\n", reviewerName))
	sb.WriteString(lastMessage)
	sb.WriteString("\n\n")

	if len(args.Deliverables) > 0 {
		sb.WriteString("## Expected Deliverables\n\n")
		for _, d := range args.Deliverables {
			sb.WriteString(fmt.Sprintf("- `%s`\n", d))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Instructions\n\n")
	sb.WriteString("1. Read the reviewer's response and any updated artifacts.\n")
	sb.WriteString("2. If you are satisfied and have enough information, produce the deliverables.\n")
	sb.WriteString("3. If you still have questions, ask them. They will be forwarded to the reviewer.\n")
	sb.WriteString("4. Write deliverables to the file paths listed above.\n")

	return sb.String()
}

// checkDeliverables checks which deliverable files exist on disk.
func checkDeliverables(paths []string) (created, missing []string) {
	for _, p := range paths {
		expanded := expandPath(p)
		if _, err := os.Stat(expanded); err == nil {
			created = append(created, p)
		} else {
			missing = append(missing, p)
		}
	}
	return
}

// buildConversationSummary builds a readable summary of the conversation turns.
func buildConversationSummary(turns []conversationTurn) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Conversation had %d turns:\n", len(turns)))
	for i, turn := range turns {
		msg := turn.Message
		if len(msg) > 200 {
			msg = msg[:200] + "..."
		}
		sb.WriteString(fmt.Sprintf("  Turn %d [%s/%s]: %s\n", i+1, turn.Agent, turn.Role, msg))
	}
	return sb.String()
}

// truncateForProgress truncates a message for progress event display.
func truncateForProgress(s string) string {
	if len(s) > 500 {
		return s[:500] + "..."
	}
	return s
}

// NewRunFleetPhaseTool creates the unified run_fleet_phase tool.
func NewRunFleetPhaseTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "run_fleet_phase",
		Description: `Execute a fleet phase by delegating work to specialized agents with persona identities and team behaviors.

This tool handles both single-agent and multi-agent conversation phases:

**Single-agent mode** (no reviewers): The primary agent runs autonomously and returns its result.

**Conversation mode** (with reviewers): The primary agent can ask questions that are forwarded to reviewers. Reviewers answer, and the primary continues until deliverables are produced or max_turns is reached.

Arguments:
- fleet: The fleet key (e.g., 'software-dev')
- phase: Phase name for tracking (e.g., 'requirements', 'design')
- primary: Agent key that executes/produces deliverables (e.g., 'po', 'architect', 'dev')
- reviewers: Optional agent keys for conversation mode (e.g., ['po']). Omit for single-agent.
- goal: Detailed task description with ALL context the agent needs
- artifacts: Optional input files from prior phases (name -> file path)
- deliverables: Optional expected output file paths (phase completes when all exist)
- max_turns: Optional max conversation turns (default: 10, only for conversation mode)
- timeout: Optional timeout in seconds (default: 300). Set 600+ for OpenCode delegate phases.`,
	}, runFleetPhase)
}
