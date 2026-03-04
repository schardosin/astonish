package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/schardosin/astonish/pkg/agent"
	"github.com/schardosin/astonish/pkg/fleet"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// --- Fleet progress streaming infrastructure ---

// FleetParentSessionKey is the session state key used to pass the main (parent)
// session ID to the orchestrator's child session. This allows run_fleet_phase
// to find the progress channel and forward worker sub-agent events.
const FleetParentSessionKey = "_fleet_parent_session"

// FleetPhaseKey is the session state key used to pass the current phase name
// to worker sub-agents. Tools like opencode read this to tag progress events
// with the correct phase for UI grouping.
const FleetPhaseKey = "_fleet_phase"

// FleetAgentKey is the session state key used to pass the current agent key
// to worker sub-agents. Tools like opencode read this to tag progress events
// with the correct agent for UI grouping.
const FleetAgentKey = "_fleet_agent"

// FleetProgressEvent represents a real-time progress update from the fleet
// orchestrator. These events are written to a channel and consumed by the
// SSE/Console transport layers for real-time user visibility.
//
// Rich fields (Args, Result, Text) provide full sub-thread detail so the
// frontend can render fleet phases identically to the main chat thread.
type FleetProgressEvent struct {
	Type    string `json:"type"`    // "phase_start", "phase_complete", "phase_failed", "text", "tool_call", "tool_result", "worker_tool_call", "worker_tool_result", "worker_text", "opencode_text", "opencode_tool_call", "opencode_tool_result", "opencode_step_start", "opencode_step_finish"
	Phase   string `json:"phase"`   // Phase name (if applicable)
	Agent   string `json:"agent"`   // Agent key (if applicable)
	Message string `json:"message"` // Human-readable progress message (truncated summary)
	Detail  string `json:"detail"`  // Additional detail (e.g., tool name for worker events)

	// Rich data fields for full sub-thread rendering in the UI
	Args   map[string]any `json:"args,omitempty"`   // Tool call arguments (for tool_call / worker_tool_call)
	Result any            `json:"result,omitempty"` // Summarized tool result (for tool_result / worker_tool_result)
	Text   string         `json:"text,omitempty"`   // Full text content (for text / worker_text, not truncated)
}

var (
	progressMu       sync.RWMutex
	progressChannels = make(map[string]chan FleetProgressEvent)
)

// GetFleetProgressCh returns the read-only progress channel for the given session.
// Returns nil if no fleet execution is active for the session.
func GetFleetProgressCh(sessionID string) <-chan FleetProgressEvent {
	progressMu.RLock()
	defer progressMu.RUnlock()
	ch, ok := progressChannels[sessionID]
	if !ok {
		return nil
	}
	return ch
}

// getFleetProgressChWrite returns the writable progress channel for the given session.
// Used internally by run_fleet_phase to forward worker events to the progress stream.
// Returns nil if no fleet execution is active for the session.
func getFleetProgressChWrite(sessionID string) chan FleetProgressEvent {
	progressMu.RLock()
	defer progressMu.RUnlock()
	return progressChannels[sessionID]
}

// createProgressCh creates a progress channel for the given session.
func createProgressCh(sessionID string) chan FleetProgressEvent {
	progressMu.Lock()
	defer progressMu.Unlock()
	ch := make(chan FleetProgressEvent, 50) // buffered to avoid blocking the orchestrator
	progressChannels[sessionID] = ch
	return ch
}

// cleanupProgressCh closes and removes the progress channel for the given session.
func cleanupProgressCh(sessionID string) {
	progressMu.Lock()
	defer progressMu.Unlock()
	if ch, ok := progressChannels[sessionID]; ok {
		close(ch)
		delete(progressChannels, sessionID)
	}
}

// --- fleet_execute tool ---

// FleetExecuteArgs is the input schema for the fleet_execute tool.
type FleetExecuteArgs struct {
	FleetKey string `json:"fleet_key" jsonschema:"The fleet key (e.g., 'software-dev'). Must match a loaded fleet definition."`
	Plan     string `json:"plan" jsonschema:"The approved execution plan as a JSON object. Must include: base_fleet, name, phases (array with name, primary, instructions, deliverables, depends_on), and optionally reviewers, reviews, preferences, and settings."`
	Request  string `json:"request" jsonschema:"The original user request in full. This is passed to the orchestrator and each sub-agent as context."`
	PlanName string `json:"plan_name,omitempty" jsonschema:"Optional name for saving the custom plan. If provided and the plan is new or modified, it will be saved for reuse."`
}

// FleetExecuteResult is the output of the fleet_execute tool.
type FleetExecuteResult struct {
	Status  string `json:"status"` // "completed", "partial", "failed", "error"
	Summary string `json:"summary"`
	Error   string `json:"error,omitempty"`
}

func fleetExecute(ctx tool.Context, args FleetExecuteArgs) (FleetExecuteResult, error) {
	if subAgentManagerVar == nil {
		return FleetExecuteResult{
			Status: "error",
			Error:  "Sub-agent system is not available. Ensure sub_agents is enabled in config.",
		}, nil
	}
	if fleetRegistryVar == nil || personaRegistryVar == nil {
		return FleetExecuteResult{
			Status: "error",
			Error:  "Fleet system is not initialized.",
		}, nil
	}

	// Parse the plan from JSON
	var plan fleet.FleetPlan
	if err := json.Unmarshal([]byte(args.Plan), &plan); err != nil {
		return FleetExecuteResult{
			Status: "error",
			Error:  fmt.Sprintf("Failed to parse plan JSON: %v", err),
		}, nil
	}

	// Use fleet_key from args, falling back to plan.BaseFleet
	fleetKey := args.FleetKey
	if fleetKey == "" {
		fleetKey = plan.BaseFleet
	}
	plan.BaseFleet = fleetKey

	// Validate the plan
	if err := plan.Validate(); err != nil {
		return FleetExecuteResult{
			Status: "error",
			Error:  fmt.Sprintf("Invalid plan: %v", err),
		}, nil
	}

	// Load the base fleet config
	fleetCfg, ok := fleetRegistryVar.GetFleet(fleetKey)
	if !ok {
		return FleetExecuteResult{
			Status: "error",
			Error:  fmt.Sprintf("Fleet %q not found.", fleetKey),
		}, nil
	}

	// Validate agent references against the fleet
	if err := plan.ValidateAgentRefs(func(key string) bool {
		_, exists := fleetCfg.Agents[key]
		return exists
	}); err != nil {
		return FleetExecuteResult{
			Status: "error",
			Error:  err.Error(),
		}, nil
	}

	// Resolve the leader persona for the orchestrator
	var leaderPersonaKey string
	if fleetCfg.Leader != nil {
		leaderPersonaKey = fleetCfg.Leader.Persona
	} else {
		leaderPersonaKey = "project_lead" // fallback
	}
	leaderPersona, ok := personaRegistryVar.GetPersona(leaderPersonaKey)
	if !ok {
		return FleetExecuteResult{
			Status: "error",
			Error:  fmt.Sprintf("Leader persona %q not found.", leaderPersonaKey),
		}, nil
	}

	// Save the plan if requested
	if fleetPlansDirVar != "" && args.PlanName != "" {
		plan.Name = args.PlanName
		planKey := slugify(args.PlanName)
		if saveErr := fleet.SaveFleetPlan(fleetPlansDirVar, planKey, &plan); saveErr != nil {
			// Non-fatal: log but continue
			_ = saveErr
		}
	}

	// Build orchestrator system prompt
	orchestratorPrompt := fleet.BuildOrchestratorPrompt(leaderPersona, fleetCfg, &plan, args.Request)

	// Set up progress channel
	sessionID := ctx.SessionID()
	progressCh := createProgressCh(sessionID)
	defer cleanupProgressCh(sessionID)

	// Send initial progress event
	progressCh <- FleetProgressEvent{
		Type:    "fleet_start",
		Message: fmt.Sprintf("Starting fleet execution: %s (%d phases)", plan.Name, len(plan.Phases)),
	}

	// Build the OnEvent callback that classifies events and writes to the progress channel
	onEvent := buildProgressCallback(progressCh)

	// The orchestrator gets run_fleet_phase + tools for reviewing outputs and setup
	orchestratorTools := []string{"run_fleet_phase", "read_file", "grep_search", "file_tree", "shell_command"}

	// Build and execute the orchestrator sub-agent
	task := agent.SubAgentTask{
		Name:            fmt.Sprintf("fleet-orchestrator-%s", fleetKey),
		Instructions:    orchestratorPrompt,
		Description:     fmt.Sprintf("Execute the approved fleet plan for: %s", args.Request),
		ToolFilter:      orchestratorTools,
		ParentID:        sessionID,
		OnEvent:         onEvent,
		CustomPrompt:    true,             // Use BuildOrchestratorPrompt directly, no generic wrapper
		TimeoutOverride: 30 * time.Minute, // Orchestrator runs all phases sequentially
		ParentDepth:     0,                // Top-level fleet execution
		SessionState: map[string]any{
			FleetParentSessionKey: sessionID, // So run_fleet_phase can find the progress channel
		},
	}

	result := subAgentManagerVar.RunTask(ctx, task)

	// Send completion progress event
	progressCh <- FleetProgressEvent{
		Type:    "fleet_complete",
		Message: fmt.Sprintf("Fleet execution %s", result.Status),
	}

	return FleetExecuteResult{
		Status:  result.Status,
		Summary: result.Result,
		Error:   result.Error,
	}, nil
}

// buildProgressCallback creates an OnEvent callback that classifies ADK events
// and writes FleetProgressEvent entries to the progress channel.
func buildProgressCallback(ch chan FleetProgressEvent) func(event *adksession.Event) {
	return func(event *adksession.Event) {
		if event == nil || event.LLMResponse.Content == nil {
			return
		}

		for _, part := range event.LLMResponse.Content.Parts {
			// Tool calls from the orchestrator (run_fleet_phase calls)
			if part.FunctionCall != nil {
				name := part.FunctionCall.Name
				if name == "run_fleet_phase" {
					// Extract phase info from args
					phaseName := "unknown"
					agentKey := "unknown"
					if args := part.FunctionCall.Args; args != nil {
						if p, ok := args["phase"].(string); ok && p != "" {
							phaseName = p
						}
						if a, ok := args["primary"].(string); ok && a != "" {
							agentKey = a
						}
					}
					ch <- FleetProgressEvent{
						Type:    "phase_start",
						Phase:   phaseName,
						Agent:   agentKey,
						Message: fmt.Sprintf("Starting phase: %s (agent: %s)", phaseName, agentKey),
						Args:    part.FunctionCall.Args,
					}
				} else {
					ch <- FleetProgressEvent{
						Type:    "tool_call",
						Message: fmt.Sprintf("Orchestrator calling: %s", name),
						Detail:  name,
						Args:    part.FunctionCall.Args,
					}
				}
			}

			// Tool results
			if part.FunctionResponse != nil {
				name := part.FunctionResponse.Name
				if name == "run_fleet_phase" {
					// Extract result status
					status := "completed"
					if resp := part.FunctionResponse.Response; resp != nil {
						if s, ok := resp["status"].(string); ok {
							status = s
						}
					}
					phaseName := ""
					if resp := part.FunctionResponse.Response; resp != nil {
						if p, ok := resp["phase"].(string); ok {
							phaseName = p
						}
					}
					evtType := "phase_complete"
					if status != "success" {
						evtType = "phase_failed"
					}
					ch <- FleetProgressEvent{
						Type:    evtType,
						Phase:   phaseName,
						Message: fmt.Sprintf("Phase %s: %s", phaseName, status),
						Result:  summarizeFleetToolResult(part.FunctionResponse.Response),
					}
				} else {
					ch <- FleetProgressEvent{
						Type:    "tool_result",
						Message: fmt.Sprintf("Orchestrator %s returned", name),
						Detail:  name,
						Result:  summarizeFleetToolResult(part.FunctionResponse.Response),
					}
				}
			}

			// Text output from orchestrator (progress updates, summaries)
			if part.Text != "" && part.FunctionCall == nil && part.FunctionResponse == nil && !part.Thought {
				msg := part.Text
				if len(msg) > 200 {
					msg = msg[:200] + "..."
				}
				ch <- FleetProgressEvent{
					Type:    "text",
					Message: strings.TrimSpace(msg),
					Text:    part.Text, // Full text, not truncated
				}
			}
		}
	}
}

// buildWorkerProgressCallback creates an OnEvent callback for fleet worker
// sub-agents. It forwards worker tool calls, tool results, and text to the
// progress channel, tagged with phase/agent info for the UI to display.
//
// Rich data (args, results, full text) is included so the frontend can render
// worker activity identically to the main chat thread.
func buildWorkerProgressCallback(ch chan FleetProgressEvent, phaseName, agentKey string) func(event *adksession.Event) {
	return func(event *adksession.Event) {
		if event == nil || event.LLMResponse.Content == nil {
			return
		}

		for _, part := range event.LLMResponse.Content.Parts {
			// Worker tool calls
			if part.FunctionCall != nil {
				ch <- FleetProgressEvent{
					Type:    "worker_tool_call",
					Phase:   phaseName,
					Agent:   agentKey,
					Message: fmt.Sprintf("[%s/%s] Calling %s", phaseName, agentKey, part.FunctionCall.Name),
					Detail:  part.FunctionCall.Name,
					Args:    part.FunctionCall.Args,
				}
			}

			// Worker tool results
			if part.FunctionResponse != nil {
				status := "ok"
				if resp := part.FunctionResponse.Response; resp != nil {
					if errMsg, ok := resp["error"].(string); ok && errMsg != "" {
						status = "error"
					}
				}
				ch <- FleetProgressEvent{
					Type:    "worker_tool_result",
					Phase:   phaseName,
					Agent:   agentKey,
					Message: fmt.Sprintf("[%s/%s] %s returned (%s)", phaseName, agentKey, part.FunctionResponse.Name, status),
					Detail:  part.FunctionResponse.Name,
					Result:  summarizeFleetToolResult(part.FunctionResponse.Response),
				}
			}

			// Worker text output (full text, not truncated)
			if part.Text != "" && part.FunctionCall == nil && part.FunctionResponse == nil && !part.Thought {
				msg := part.Text
				if len(msg) > 200 {
					msg = msg[:200] + "..."
				}
				ch <- FleetProgressEvent{
					Type:    "worker_text",
					Phase:   phaseName,
					Agent:   agentKey,
					Message: strings.TrimSpace(msg),
					Text:    part.Text, // Full text, not truncated
				}
			}
		}
	}
}

// summarizeFleetToolResult extracts a useful summary from a tool response map.
// It mirrors the logic used for the main SSE handler's tool_result events.
func summarizeFleetToolResult(resp map[string]any) any {
	if resp == nil {
		return nil
	}
	if v, ok := resp["result"]; ok {
		if s, ok := v.(string); ok {
			return truncateFleetResult(s)
		}
	}
	if v, ok := resp["output"]; ok {
		if s, ok := v.(string); ok {
			return truncateFleetResult(s)
		}
	}
	if v, ok := resp["error"]; ok {
		return v
	}
	return resp
}

// truncateFleetResult limits tool output for fleet events to a reasonable length.
func truncateFleetResult(s string) string {
	const maxLen = 2000
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n\n... (truncated)"
}

// NewFleetExecuteTool creates the fleet_execute tool.
func NewFleetExecuteTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "fleet_execute",
		Description: `Execute an approved fleet plan using a dedicated orchestrator agent.

Call this ONLY after the user has reviewed and approved a plan from fleet_plan.

The tool spawns an orchestrator agent (Project Lead persona) that executes the plan by delegating each phase to specialized sub-agents via run_fleet_phase. Progress is streamed to the user in real time.

Arguments:
- fleet_key: The fleet to use (e.g., 'software-dev')
- plan: The approved plan as a JSON object (from the planning conversation)
- request: The original user request (full context for the agents)
- plan_name: Optional name for saving the plan for reuse

The tool blocks until the orchestrator completes all phases and returns a summary of results.`,
	}, fleetExecute)
}

// GetFleetPlanningTools returns the fleet planning and execution tools.
// These replace run_fleet_phase on the main ChatAgent.
func GetFleetPlanningTools() ([]tool.Tool, error) {
	planTool, err := NewFleetPlanTool()
	if err != nil {
		return nil, fmt.Errorf("creating fleet_plan tool: %w", err)
	}
	execTool, err := NewFleetExecuteTool()
	if err != nil {
		return nil, fmt.Errorf("creating fleet_execute tool: %w", err)
	}
	return []tool.Tool{planTool, execTool}, nil
}
