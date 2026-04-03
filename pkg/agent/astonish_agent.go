package agent

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// ErrWaitingForApproval is returned when a tool needs user approval
var ErrWaitingForApproval = fmt.Errorf("interrupt: waiting for user approval")

// AstonishAgent implements the logic for running Astonish agents.
type AstonishAgent struct {
	Config          *config.AgentConfig
	LLM             model.LLM
	Tools           []tool.Tool
	Toolsets        []tool.Toolset
	DebugMode       bool
	IsWebMode       bool // If true, avoids ANSI codes in output
	AutoApprove     bool // If true, automatically approves all tool executions
	SessionService  session.Service
	Redactor        *credentials.Redactor // Redacts credential values from tool/LLM outputs (nil = disabled)
	CredentialStore *credentials.Store    // Credential store for placeholder substitution (nil = disabled)
}

// NewAstonishAgent creates a new AstonishAgent.
func NewAstonishAgent(cfg *config.AgentConfig, llm model.LLM, tools []tool.Tool) *AstonishAgent {
	return &AstonishAgent{
		Config:   cfg,
		LLM:      llm,
		Tools:    tools,
		Toolsets: nil,
	}
}

// NewAstonishAgentWithToolsets creates a new AstonishAgent with both tools and toolsets.
func NewAstonishAgentWithToolsets(cfg *config.AgentConfig, llm model.LLM, tools []tool.Tool, toolsets []tool.Toolset) *AstonishAgent {
	return &AstonishAgent{
		Config:   cfg,
		LLM:      llm,
		Tools:    tools,
		Toolsets: toolsets,
	}
}

// Run executes the agent flow with stateful workflow management.
func (a *AstonishAgent) Run(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
	if a.DebugMode {
		if a.Toolsets != nil {
			// Create a minimal context for listing tools
			roCtx := &minimalReadonlyContext{Context: context.Background()}
			for _, ts := range a.Toolsets {
				_, err := ts.Tools(roCtx)
				if err != nil {
					slog.Error("failed to list tools for toolset", "toolset", ts.Name(), "error", err)
				}
			}
		}
	}

	state := ctx.Session().State()
	hasUserInput := ctx.UserContent() != nil && len(ctx.UserContent().Parts) > 0

	// Check if we are resuming from an approval request
	if hasUserInput {
		awaiting := false
		val, err := state.Get("awaiting_approval")
		if err == nil {
			if b, ok := val.(bool); ok {
				awaiting = b
			}
		}

		// Fallback: Check event history if state is missing
		// BUT only check recent events - stop if we find awaiting_approval=false
		// (which means approval was resolved)
		if !awaiting {
			events := ctx.Session().Events()
			// Look backwards for the last model event with state delta
			for i := events.Len() - 1; i >= 0; i-- {
				ev := events.At(i)
				if a.DebugMode {
					slog.Debug("scan event", "index", i, "author", ev.Author, "state_delta", ev.Actions.StateDelta)
				}
				if ev.Actions.StateDelta != nil {
					if val, ok := ev.Actions.StateDelta["awaiting_approval"]; ok {
						if b, ok := val.(bool); ok {
							if b {
								// Found awaiting_approval=true
								awaiting = true
								if toolVal, ok := ev.Actions.StateDelta["approval_tool"]; ok {
									if err := state.Set("approval_tool", toolVal); err != nil {
										slog.Warn("failed to set approval_tool state", "error", err)
									}
								}
								break
							} else {
								// Found awaiting_approval=false - this means approval was resolved
								// Stop looking, we don't want to find older approval requests
								break
							}
						}
					}
				}
				// Stop if we go back too far (e.g. past previous user turn)
				if ev.Author == "user" && i < events.Len()-1 {
					break
				}
			}
		}

		if a.DebugMode {
			slog.Debug("run state", "has_user_input", true, "awaiting", awaiting)
		}

		if awaiting {
			// Get the tool that was waiting
			toolName, _ := state.Get("approval_tool")
			toolNameStr, _ := toolName.(string)

			// Get user response
			var inputBuilder strings.Builder
			for _, part := range ctx.UserContent().Parts {
				if part.Text != "" {
					inputBuilder.WriteString(part.Text)
				}
			}
			input := strings.TrimSpace(StripTimestamp(inputBuilder.String()))

			if a.DebugMode {
				slog.Debug("run awaiting approval", "tool", toolNameStr, "input", input)
			}

			if strings.EqualFold(input, "Yes") {
				// Approved!
				if toolNameStr != "" {
					// Get current node for node-scoped approval
					currentNode := ""
					if nodeVal, err := state.Get("current_node"); err == nil && nodeVal != nil {
						if nodeName, ok := nodeVal.(string); ok {
							currentNode = nodeName
						}
					}
					approvalKey := fmt.Sprintf("approval:%s:%s", currentNode, toolNameStr)
					if err := state.Set(approvalKey, true); err != nil {
						slog.Warn("failed to set approval state", "key", approvalKey, "error", err)
					}
					if a.DebugMode {
						slog.Debug("set approval", "tool", toolNameStr, "key", approvalKey)
					}
				}
			}

			// NOTE: Do NOT clear awaiting_approval, approval_tool, or approval_args here!
			// Let handleToolApproval handle the cleanup so it can inject the retry prompt.
			// The handleToolApproval function will process these and clear them.
		}
	}

	// Main loop
	return func(yield func(*session.Event, error) bool) {
		state := ctx.Session().State()

		// Get current_node from state, default to START
		currentNodeNameVal, _ := state.Get("current_node")
		currentNodeName, ok := currentNodeNameVal.(string)
		if !ok || currentNodeName == "" {
			currentNodeName = "START"
		}

		// Pending state delta to be attached to the next event
		pendingStateDelta := make(map[string]any)

		// Wrap yield to inject pendingStateDelta and redact credential values
		originalYield := yield
		yield = func(event *session.Event, err error) bool {
			if event != nil && len(pendingStateDelta) > 0 {
				if event.Actions.StateDelta == nil {
					event.Actions.StateDelta = make(map[string]any)
				}
				for k, v := range pendingStateDelta {
					// Only add if not already present (event takes precedence)
					if _, exists := event.Actions.StateDelta[k]; !exists {
						event.Actions.StateDelta[k] = v
					}
				}
				// Clear pendingStateDelta so we don't send it again
				pendingStateDelta = make(map[string]any)
			}
			// Redact credential values from LLM text responses before they
			// reach the user. The LLM may have received raw secrets via
			// resolve_credential and could accidentally echo them.
			redactEventText(a.Redactor, event)
			return originalYield(event, err)
		}

		// Initialize state keys from all nodes if not present
		// This mimics Python's behavior of pre-populating keys
		if currentNodeName == "START" {
			for _, node := range a.Config.Nodes {
				// Initialize output_model keys
				for key := range node.OutputModel {
					if _, err := state.Get(key); err != nil {
						val := ""
						if err := state.Set(key, val); err != nil {
							// Log error but continue
							slog.Warn("failed to initialize state key", "key", key, "error", err)
						}
						pendingStateDelta[key] = val
					}
				}
				// Initialize raw_tool_output keys
				for key := range node.RawToolOutput {
					if _, err := state.Get(key); err != nil {
						val := ""
						if err := state.Set(key, val); err != nil {
							slog.Warn("failed to initialize state key", "key", key, "error", err)
						}
						pendingStateDelta[key] = val
					}
				}
			}
		}

		// Check if we're awaiting tool approval
		if awaitingApproval, _ := state.Get("awaiting_approval"); awaitingApproval == true {
			if !a.handleToolApproval(ctx, state, yield) {
				return
			}
			// After handling approval, get the current node again
			currentNodeNameVal, _ = state.Get("current_node")
			currentNodeName, _ = currentNodeNameVal.(string)
		}

		// Check if we have user content (meaning this is a resume after user input)
		hasUserInput := ctx.UserContent() != nil && len(ctx.UserContent().Parts) > 0

		// If we're at START, move to first node
		if currentNodeName == "START" {
			nextNode, err := a.getNextNode("START", state)
			if err != nil {
				yield(nil, err)
				return
			}
			currentNodeName = nextNode
			// Main loop will emit the transition

			// Check if first node is an input node - if so, show prompt immediately
			node, found := a.getNode(currentNodeName)
			if found && node.Type == "input" && !hasUserInput {
				// Show the prompt and return, waiting for user input
				prompt := a.renderString(node.Prompt, state)

				// Resolve options if present
				var inputOptions []string
				if len(node.Options) > 0 {
					for _, opt := range node.Options {
						// Check if option is a state variable
						if val, err := state.Get(opt); err == nil {
							// If it's a list of strings, expand it
							if list, ok := val.([]string); ok {
								inputOptions = append(inputOptions, list...)
								continue
							}
							// If it's a generic list, try to convert elements to strings
							if list, ok := val.([]interface{}); ok {
								for _, item := range list {
									inputOptions = append(inputOptions, fmt.Sprintf("%v", item))
								}
								continue
							}
							// If it's a single string (LLM returned one item as string instead of array),
							// treat it as a single option
							if strVal, ok := val.(string); ok && strings.TrimSpace(strVal) != "" {
								inputOptions = append(inputOptions, strings.TrimSpace(strVal))
								continue
							}
						}
						// Otherwise treat as literal option
						inputOptions = append(inputOptions, opt)
					}
				}

				promptEvent := &session.Event{
					LLMResponse: model.LLMResponse{
						Content: &genai.Content{
							Parts: []*genai.Part{{Text: prompt}},
							Role:  "model",
						},
					},
					Actions: session.EventActions{
						StateDelta: map[string]any{
							"current_node":      currentNodeName,
							"input_options":     inputOptions,
							"waiting_for_input": true,
						},
					},
				}

				if !yield(promptEvent, nil) {
					return
				}
				return
			}

			// Update state
			if err := state.Set("current_node", currentNodeName); err != nil {
				slog.Warn("failed to set current_node state", "error", err)
			}
		}

		// Handle resume from input
		if currentNodeName != "START" && currentNodeName != "END" && hasUserInput {
			node, found := a.getNode(currentNodeName)
			if found && node.Type == "input" {
				// Extract user input
				var inputBuilder strings.Builder
				for _, part := range ctx.UserContent().Parts {
					if part.Text != "" {
						inputBuilder.WriteString(part.Text)
					}
				}
				input := strings.TrimSpace(StripTimestamp(inputBuilder.String()))

				// Build state delta with the input value
				stateDelta := make(map[string]any)
				for key := range node.OutputModel {
					stateDelta[key] = input
					state.Set(key, input)
					break
				}

				// Move to next node
				nextNode, err := a.getNextNode(currentNodeName, state)
				if err != nil {
					yield(nil, err)
					return
				}
				stateDelta["current_node"] = nextNode
				currentNodeName = nextNode

				// Yield event with state delta
				yield(&session.Event{
					Actions: session.EventActions{
						StateDelta: stateDelta,
					},
				}, nil)
				// Main loop will emit the transition for the next node
			}
		}

		// Main execution loop
		for {
			if currentNodeName == "END" {
				// Emit transition to END so UI knows we are done
				if !a.emitNodeTransition("END", state, yield) {
					return
				}

				if err := state.Set("current_node", "END"); err != nil {
					yield(nil, err)
					return
				}
				return
			}

			node, found := a.getNode(currentNodeName)
			if !found {
				yield(nil, fmt.Errorf("node not found: %s", currentNodeName))
				return
			}

			// Emit node transition before processing
			if !a.emitNodeTransition(currentNodeName, state, yield) {
				return
			}

			// Check for Parallel execution
			if node.Parallel != nil {
				if !a.handleParallelNode(ctx, node, state, yield) {
					return
				}

				// Move to next node
				nextNode, err := a.getNextNode(currentNodeName, state)
				if err != nil {
					yield(nil, err)
					return
				}
				currentNodeName = nextNode
				continue
			}

			if node.Type == "input" {
				// Render prompt
				prompt := a.renderString(node.Prompt, state)

				// Resolve options if present
				var inputOptions []string
				if len(node.Options) > 0 {
					for _, opt := range node.Options {
						// Check if option is a state variable
						if val, err := state.Get(opt); err == nil {
							// If it's a list of strings, expand it
							if list, ok := val.([]string); ok {
								inputOptions = append(inputOptions, list...)
								continue
							}
							// If it's a generic list, try to convert elements to strings
							if list, ok := val.([]interface{}); ok {
								for _, item := range list {
									inputOptions = append(inputOptions, fmt.Sprintf("%v", item))
								}
								continue
							}
							// If it's a single string (LLM returned one item as string instead of array),
							// treat it as a single option
							if strVal, ok := val.(string); ok && strings.TrimSpace(strVal) != "" {
								inputOptions = append(inputOptions, strings.TrimSpace(strVal))
								continue
							}
						}
						// Otherwise treat as literal option
						inputOptions = append(inputOptions, opt)
					}
				}

				// Yield prompt event and update state
				promptEvent := &session.Event{
					LLMResponse: model.LLMResponse{
						Content: &genai.Content{
							Parts: []*genai.Part{{Text: prompt}},
							Role:  "model",
						},
					},
					Actions: session.EventActions{
						StateDelta: map[string]any{
							"current_node":      currentNodeName,
							"input_options":     inputOptions,
							"waiting_for_input": true,
						},
					},
				}

				if !yield(promptEvent, nil) {
					return
				}

				return
			} else if node.Type == "llm" {
				success := a.executeLLMNode(ctx, node, currentNodeName, state, yield)

				// Check if node failed and set error flag
				if !success {
					// Check if this failure should stop execution
					hasError, _ := state.Get("_has_error")
					if hasErrorBool, ok := hasError.(bool); ok && hasErrorBool {
						// Error occurred and was handled by retry logic
						// Check if we should stop or continue
						// For now, transition to END to stop execution
						if a.DebugMode {
							slog.Debug("node failed with error, transitioning to END", "node", currentNodeName)
						}
						currentNodeName = "END"
						continue
					}
					// Node failed but no error flag - this shouldn't happen, but handle it
					return
				}

				// Node succeeded - move to next node
				nextNode, err := a.getNextNode(currentNodeName, state)
				if err != nil {
					yield(nil, err)
					return
				}
				currentNodeName = nextNode
				// Don't emit transition here - the main loop will do it

			} else if node.Type == "tool" {
				success := a.handleToolNode(ctx, node, state, yield)

				// Check if node failed and set error flag (same pattern as LLM nodes)
				if !success {
					// Check if this failure should stop execution
					hasError, _ := state.Get("_has_error")
					if hasErrorBool, ok := hasError.(bool); ok && hasErrorBool {
						// Error occurred and was handled - transition to END
						if a.DebugMode {
							slog.Debug("tool node failed with error, transitioning to END", "node", currentNodeName)
						}
						currentNodeName = "END"
						continue
					}
					// Node failed but no error flag - this is a pause (e.g., awaiting approval)
					return
				}

				// Move to next node
				nextNode, err := a.getNextNode(currentNodeName, state)
				if err != nil {
					yield(nil, err)
					return
				}
				currentNodeName = nextNode
				// Don't emit transition here - the main loop will do it

			} else if node.Type == "update_state" {
				if !a.handleUpdateStateNode(ctx, node, state, yield) {
					return
				}

				// Move to next node
				nextNode, err := a.getNextNode(currentNodeName, state)
				if err != nil {
					yield(nil, err)
					return
				}
				currentNodeName = nextNode
				// Don't emit transition here - the main loop will do it

			} else if node.Type == "output" {
				if !a.handleOutputNode(ctx, node, state, yield) {
					return
				}

				// Yield processor to allow events to propagate to the console runner
				// This mitigates a race condition where the next node's transition event
				// might be processed before the output content is fully flushed.
				time.Sleep(50 * time.Millisecond)

				// Move to next node
				nextNode, err := a.getNextNode(currentNodeName, state)
				if err != nil {
					yield(nil, err)
					return
				}
				currentNodeName = nextNode
				// Don't emit transition here - the main loop will do it

			} else {
				yield(nil, fmt.Errorf("unsupported node type: %s", node.Type))
				return
			}
		}
	}
}
