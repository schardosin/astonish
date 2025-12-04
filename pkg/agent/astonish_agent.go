package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/planner"
	"github.com/schardosin/astonish/pkg/ui"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// ErrWaitingForApproval is returned when a tool needs user approval
var ErrWaitingForApproval = fmt.Errorf("interrupt: waiting for user approval")

// minimalReadonlyContext implements agent.ReadonlyContext for fetching tools from toolsets
type minimalReadonlyContext struct {
	context.Context
	actions *session.EventActions
	state   session.State
}

func (m *minimalReadonlyContext) AgentName() string                    { return "astonish-agent" }
func (m *minimalReadonlyContext) AppName() string                      { return "astonish" }
func (m *minimalReadonlyContext) UserContent() *genai.Content          { return nil }
func (m *minimalReadonlyContext) InvocationID() string                 { return "" }
func (m *minimalReadonlyContext) ReadonlyState() session.ReadonlyState { return nil }
func (m *minimalReadonlyContext) UserID() string                       { return "" }
func (m *minimalReadonlyContext) SessionID() string                    { return "" }
func (m *minimalReadonlyContext) Branch() string                       { return "" }
func (m *minimalReadonlyContext) Actions() *session.EventActions {
	if m.actions == nil {
		return &session.EventActions{}
	}
	return m.actions
}
func (m *minimalReadonlyContext) SearchMemory(ctx context.Context, query string) (*memory.SearchResponse, error) {
	return nil, nil
}
func (m *minimalReadonlyContext) FunctionCallID() string { return "" }
func (m *minimalReadonlyContext) Artifacts() agent.Artifacts { return nil }
func (m *minimalReadonlyContext) State() session.State       { return m.state }

// RunnableTool defines an interface for tools that can be executed.
// This matches the signature of Run method in adk-go's internal tool implementations.
type RunnableTool interface {
	Run(ctx tool.Context, args any) (map[string]any, error)
}

// ToolWithDeclaration allows inspecting the tool's schema
type ToolWithDeclaration interface {
	Declaration() *genai.FunctionDeclaration
}

// ProtectedToolset wraps a toolset and returns tools wrapped with ProtectedTool
type ProtectedToolset struct {
	underlying tool.Toolset
	state      session.State
	agent      *AstonishAgent
	yieldFunc  func(*session.Event, error) bool
}

// Name returns the name of the underlying toolset
func (p *ProtectedToolset) Name() string {
	return p.underlying.Name()
}

// Tools returns the underlying toolset's tools, wrapped with ProtectedTool
func (p *ProtectedToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	underlyingTools, err := p.underlying.Tools(ctx)
	if err != nil {
		return nil, err
	}
	
	wrappedTools := make([]tool.Tool, len(underlyingTools))
	for i, t := range underlyingTools {
		wrappedTools[i] = &ProtectedTool{
			Tool:      t,
			State:     p.state,
			Agent:     p.agent,
			YieldFunc: p.yieldFunc,
		}
	}
	
	return wrappedTools, nil
}

// FilteredToolset wraps a toolset and filters tools based on allowed list
type FilteredToolset struct {
	underlying   tool.Toolset
	allowedTools []string
}

// Name returns the name of the underlying toolset
func (f *FilteredToolset) Name() string {
	return f.underlying.Name()
}

// Tools returns only the tools that are in the allowed list
func (f *FilteredToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	underlyingTools, err := f.underlying.Tools(ctx)
	if err != nil {
		return nil, err
	}
	
	// Create a map for fast lookup
	allowedMap := make(map[string]bool)
	for _, name := range f.allowedTools {
		allowedMap[name] = true
	}
	
	// Filter tools
	var filteredTools []tool.Tool
	for _, t := range underlyingTools {
		if allowedMap[t.Name()] {
			filteredTools = append(filteredTools, t)
		}
	}
	
	return filteredTools, nil
}



// ProtectedTool wraps a standard tool and adds an approval gate.
type ProtectedTool struct {
	tool.Tool                // Embed the underlying tool
	State     session.State  // Access to session state
	Agent     *AstonishAgent // Access to helper methods
	YieldFunc func(*session.Event, error) bool // For emitting events
}

// Declaration forwards the call to the underlying tool if it supports it
func (p *ProtectedTool) Declaration() *genai.FunctionDeclaration {
	if declTool, ok := p.Tool.(ToolWithDeclaration); ok {
		return declTool.Declaration()
	}
	return nil
}

// ProcessRequest always delegates to underlying tool to register it with the LLM
// Approval is checked later during Run(), not during ProcessRequest()
func (p *ProtectedTool) ProcessRequest(ctx tool.Context, req *model.LLMRequest) error {
	// Always delegate to underlying tool so it gets registered with the LLM
	// Approval will be checked when the tool is actually executed (Run method)
	if processor, ok := p.Tool.(interface {
		ProcessRequest(tool.Context, *model.LLMRequest) error
	}); ok {
		return processor.ProcessRequest(ctx, req)
	}
	return nil // Tool doesn't implement ProcessRequest, that's okay
}


// Run intercepts the execution to check for approval
func (p *ProtectedTool) Run(ctx tool.Context, args any) (map[string]any, error) {
	toolName := p.Tool.Name()
	approvalKey := fmt.Sprintf("approval:%s", toolName)

	// 1. Check if we already have approval
	if approved, _ := p.State.Get(approvalKey); approved == true {
		// Consume approval
		p.State.Set(approvalKey, false)
		
		// We use a broader interface check here to be safe
		if rt, ok := p.Tool.(interface {
			Run(tool.Context, any) (map[string]any, error)
		}); ok {
			return rt.Run(ctx, args)
		}
		return nil, fmt.Errorf("underlying tool does not implement Run")
	}

	// 2. Format arguments for display
	var argsMap map[string]any
	if m, ok := args.(map[string]any); ok {
		argsMap = m
	} else {
		// If args is a struct (common in MCP), wrap it for display
		argsMap = map[string]any{"arguments": args}
	}

	// 3. Set the approval state
	p.State.Set("awaiting_approval", true)
	p.State.Set("approval_tool", toolName)
	p.State.Set("approval_args", argsMap)

	// 4. Emit the UI Event
	prompt := p.Agent.formatToolApprovalRequest(toolName, argsMap)
	
	p.YieldFunc(&session.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Parts: []*genai.Part{{Text: prompt}},
				Role:  "model",
			},
		},
		Actions: session.EventActions{
			StateDelta: map[string]any{
				"awaiting_approval": true,
				"approval_options":  []string{"Yes", "No"},
			},
		},
	}, nil)

	// 5. Return error to stop execution and wait for approval
	// This prevents the LLM from seeing a tool result before approval
	return nil, ErrWaitingForApproval
}


// AstonishAgent implements the logic for running Astonish agents.
type AstonishAgent struct {
	Config   *config.AgentConfig
	LLM      model.LLM
	Tools    []tool.Tool
	Toolsets []tool.Toolset
	DebugMode     bool
	SessionService session.Service
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
					fmt.Printf(" - [Error listing tools for toolset %s: %v]\n", ts.Name(), err)
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
		if !awaiting {
			events := ctx.Session().Events()
			// Look backwards for the last model event with state delta
			for i := events.Len() - 1; i >= 0; i-- {
				ev := events.At(i)
				if a.DebugMode {
					fmt.Printf("[DEBUG] Scan Event %d: Author=%s, StateDelta=%v\n", i, ev.Author, ev.Actions.StateDelta)
				}
				if ev.Actions.StateDelta != nil {
					if val, ok := ev.Actions.StateDelta["awaiting_approval"]; ok {
						if b, ok := val.(bool); ok && b {
							awaiting = true
							if toolVal, ok := ev.Actions.StateDelta["approval_tool"]; ok {
								state.Set("approval_tool", toolVal)
							}
							break
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
			fmt.Printf("[DEBUG] Run: hasUserInput=true, awaiting=%v\n", awaiting)
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
			input := strings.TrimSpace(inputBuilder.String())
			
			if a.DebugMode {
				fmt.Printf("[DEBUG] Run: Awaiting=true, Tool='%s', Input='%s'\n", toolNameStr, input)
			}
			
			if strings.EqualFold(input, "Yes") {
				// Approved!
				if toolNameStr != "" {
					approvalKey := fmt.Sprintf("approval:%s", toolNameStr)
					state.Set(approvalKey, true)
					if a.DebugMode {
						fmt.Printf("[DEBUG] Run: Set approval for %s. Key=%s\n", toolNameStr, approvalKey)
					}
				}
			}
			
			// Clear waiting state
			state.Set("awaiting_approval", false)
			state.Set("approval_tool", "")
			state.Set("approval_args", nil)
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

		// Wrap yield to inject pendingStateDelta
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
							fmt.Printf("Warning: Failed to initialize key '%s': %v\n", key, err)
						}
						pendingStateDelta[key] = val
					}
				}
				// Initialize raw_tool_output keys
				for key := range node.RawToolOutput {
					if _, err := state.Get(key); err != nil {
						val := ""
						if err := state.Set(key, val); err != nil {
							fmt.Printf("Warning: Failed to initialize key '%s': %v\n", key, err)
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
							// If it's a string, split by newline
							if strVal, ok := val.(string); ok {
								lines := strings.Split(strings.TrimSpace(strVal), "\n")
								for _, line := range lines {
									trimmed := strings.TrimSpace(line)
									if trimmed == "" {
										continue
									}
									// Filter out lines that look like LLM preamble/commentary
									// Accept lines that start with a number followed by colon (PR format)
									// or lines that don't look like natural language sentences
									if strings.Contains(trimmed, ":") {
										// Check if it starts with a number (likely a PR)
										parts := strings.SplitN(trimmed, ":", 2)
										if len(parts) == 2 {
											// Check if the first part is numeric (or starts with #)
											firstPart := strings.TrimSpace(parts[0])
											if len(firstPart) > 0 {
												// Remove leading # if present
												if firstPart[0] == '#' {
													firstPart = firstPart[1:]
												}
												// Check if it's a number
												if _, err := fmt.Sscanf(firstPart, "%d", new(int)); err == nil {
													inputOptions = append(inputOptions, trimmed)
													continue
												}
											}
										}
									}
									// If it doesn't match the PR format, skip it (likely LLM commentary)
								}
								if len(inputOptions) > 0 {
									continue
								}
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
							"current_node":  currentNodeName,
							"input_options": inputOptions,
						},
					},
				}
				
				if !yield(promptEvent, nil) {
					return
				}
				return
			}
			
			// Update state
			state.Set("current_node", currentNodeName)
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
				input := strings.TrimSpace(inputBuilder.String())

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

			// DEBUG LOGGING TO ABSOLUTE PATH
			f, _ := os.OpenFile("/Users/I851355/Projects/astonish/agent_debug_absolute.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if f != nil {
				fmt.Fprintf(f, "[DEBUG] Processing Node: %s, Type: %s\n", node.Name, node.Type)
				f.Close()
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
							// If it's a string, split by newline
							if strVal, ok := val.(string); ok {
								lines := strings.Split(strings.TrimSpace(strVal), "\n")
								for _, line := range lines {
									trimmed := strings.TrimSpace(line)
									if trimmed == "" {
										continue
									}
									// Filter out lines that look like LLM preamble/commentary
									// Accept lines that start with a number followed by colon (PR format)
									// or lines that don't look like natural language sentences
									if strings.Contains(trimmed, ":") {
										// Check if it starts with a number (likely a PR)
										parts := strings.SplitN(trimmed, ":", 2)
										if len(parts) == 2 {
											// Check if the first part is numeric (or starts with #)
											firstPart := strings.TrimSpace(parts[0])
											if len(firstPart) > 0 {
												// Remove leading # if present
												if firstPart[0] == '#' {
													firstPart = firstPart[1:]
												}
												// Check if it's a number
												if _, err := fmt.Sscanf(firstPart, "%d", new(int)); err == nil {
													inputOptions = append(inputOptions, trimmed)
													continue
												}
											}
										}
									}
									// If it doesn't match the PR format, skip it (likely LLM commentary)
								}
								if len(inputOptions) > 0 {
									continue
								}
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
							"current_node":  currentNodeName,
							"input_options": inputOptions,
						},
					},
				}
				
				if !yield(promptEvent, nil) {
					return
				}
				
				return
			} else if node.Type == "llm" {
				if !a.executeLLMNode(ctx, node, currentNodeName, state, yield) {
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

			} else if node.Type == "tool" {
				if !a.handleToolNode(ctx, node, state, yield) {
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



// emitNodeTransition emits a node transition event
func (a *AstonishAgent) emitNodeTransition(nodeName string, state session.State, yield func(*session.Event, error) bool) bool {
	if nodeName == "END" {
		event := &session.Event{
			Actions: session.EventActions{
				StateDelta: map[string]any{
					"current_node": "END",
					"node_type":    "END",
				},
			},
		}
		return yield(event, nil)
	}
	
	// Get node info
	node, found := a.getNode(nodeName)
	if !found {
		return true
	}
	
	// Add to node history
	historyVal, _ := state.Get("temp:node_history")
	history, ok := historyVal.([]string)
	if !ok {
		history = []string{}
	}
	history = append(history, nodeName)
	
	event := &session.Event{
		// LLMResponse removed to prevent static "--- Node ---" log
		Actions: session.EventActions{
			StateDelta: map[string]any{
				"current_node":       nodeName,
				"temp:node_history":  history,
				"temp:node_type":     node.Type,
				"node_type":          node.Type,
			},
		},
	}
	
	return yield(event, nil)
}

// handleUpdateStateNode handles update_state nodes
func (a *AstonishAgent) handleUpdateStateNode(ctx agent.InvocationContext, node *config.Node, state session.State, yield func(*session.Event, error) bool) bool {
	// Fallback to simple Updates map if Action is not set
	if node.Action == "" && len(node.Updates) > 0 {
		stateDelta := make(map[string]any)
		for key, valueTemplate := range node.Updates {
			value := a.renderString(valueTemplate, state)
			if err := state.Set(key, value); err != nil {
				yield(nil, fmt.Errorf("failed to set state key %s: %w", key, err))
				return false
			}
			stateDelta[key] = value
		}
		// Emit event
		event := &session.Event{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: fmt.Sprintf("Updated state: %v", stateDelta)}},
					Role:  "model",
				},
			},
			Actions: session.EventActions{StateDelta: stateDelta},
		}
		return yield(event, nil)
	}

	// Python-compatible logic
	if len(node.OutputModel) != 1 {
		yield(nil, fmt.Errorf("update_state node must have exactly one key in output_model defining the target variable"))
		return false
	}

	var targetVar string
	for k := range node.OutputModel {
		targetVar = k
		break
	}

	var valueToUse any
	if node.SourceVariable != "" {
		val, err := state.Get(node.SourceVariable)
		if err != nil {
			// If source variable missing, is it an error? Python raises KeyError.
			yield(nil, fmt.Errorf("failed to get source variable %s: %w", node.SourceVariable, err))
			return false
		}
		valueToUse = val
	} else if node.Value != nil {
		valueToUse = node.Value
	} else {
		yield(nil, fmt.Errorf("update_state node must have either source_variable or value"))
		return false
	}

	// Render string values
	if strVal, ok := valueToUse.(string); ok {
		valueToUse = a.renderString(strVal, state)
	}

	stateDelta := make(map[string]any)

	switch node.Action {
	case "overwrite":
		if err := state.Set(targetVar, valueToUse); err != nil {
			yield(nil, fmt.Errorf("failed to set state variable %s: %w", targetVar, err))
			return false
		}
		stateDelta[targetVar] = valueToUse

	case "append":
		// Get existing list
		existing, err := state.Get(targetVar)
		var list []any
		if err == nil && existing != nil {
			if l, ok := existing.([]any); ok {
				list = l
			} else if l, ok := existing.([]string); ok {
				list = make([]any, len(l))
				for i, v := range l {
					list[i] = v
				}
			} else {
				// Initialize new list if type mismatch
				list = []any{}
			}
		} else {
			list = []any{}
		}

		// Append
		if valList, ok := valueToUse.([]any); ok {
			list = append(list, valList...)
		} else if valList, ok := valueToUse.([]string); ok {
			for _, v := range valList {
				list = append(list, v)
			}
		} else {
			list = append(list, valueToUse)
		}

		if err := state.Set(targetVar, list); err != nil {
			yield(nil, fmt.Errorf("failed to set state variable %s: %w", targetVar, err))
			return false
		}
		stateDelta[targetVar] = list

	default:
		yield(nil, fmt.Errorf("unsupported action: %s", node.Action))
		return false
	}

	// Emit event with state delta
	event := &session.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Parts: []*genai.Part{{
					Text: fmt.Sprintf("Updated state: %s", ui.FormatAsYamlLike(stateDelta, 0)),
				}},
				Role: "model",
			},
		},
		Actions: session.EventActions{
			StateDelta: stateDelta,
		},
	}

	return yield(event, nil)
}

// handleToolNode handles tool execution nodesval flow when resuming after pause
func (a *AstonishAgent) handleToolApproval(ctx agent.InvocationContext, state session.State, yield func(*session.Event, error) bool) bool {
	// Check if user provided approval
	if ctx.UserContent() == nil || len(ctx.UserContent().Parts) == 0 {
		return true // No user input, continue
	}
	
	// Get the tool name that's awaiting approval
	toolNameVal, _ := state.Get("approval_tool")
	toolName, _ := toolNameVal.(string)
	
	// Extract approval response
	var responseText string
	for _, part := range ctx.UserContent().Parts {
		if part.Text != "" {
			responseText += part.Text
		}
	}
	responseText = strings.ToLower(strings.TrimSpace(responseText))
	
	approved := responseText == "yes" || responseText == "y" || responseText == "approve"
	
	if approved {
		// Grant approval using the tool-specific key
		approvalKey := fmt.Sprintf("approval:%s", toolName)
		state.Set(approvalKey, true)
		state.Set("awaiting_approval", false)
		
		// [MCP-SPECIFIC FIX] Inject a very specific instruction for MCP tools
		// MCP tools are sensitive to exact arguments - must retry with same args
		retryPrompt := fmt.Sprintf(
			"User approved execution. IMMEDIATELY call the function '%s' again with the exact same arguments as before.",
			toolName,
		)
		
		// Override the user input in the context so the LLM sees the instruction
		ctx.UserContent().Parts = []*genai.Part{{
			Text: retryPrompt,
		}}
		
		// Emit approval confirmation
		event := &session.Event{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{{
						Text: "[ℹ️ Info] Tool execution approved. Resuming...\n",
					}},
					Role: "model",
				},
			},
			Actions: session.EventActions{
				StateDelta: map[string]any{
					approvalKey:         true,
					"awaiting_approval": false,
				},
			},
		}
		return yield(event, nil)
	} else {
		// User denied - move to next node
		state.Set("awaiting_approval", false)
		
		event := &session.Event{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{{
						Text: "[ℹ️ Info] Tool execution denied by user. Skipping to next node.\n",
					}},
					Role: "model",
				},
			},
			Actions: session.EventActions{
				StateDelta: map[string]any{
					"awaiting_approval": false,
				},
			},
		}
		yield(event, nil)
		
		// Get current node and move to next
		currentNodeVal, _ := state.Get("current_node")
		currentNode, _ := currentNodeVal.(string)
		nextNode, err := a.getNextNode(currentNode, state)
		if err != nil {
			yield(nil, err)
			return false
		}
		state.Set("current_node", nextNode)
		
		return true // Continue to next node
	}
}



// executeLLMNode executes an LLM node using ADK's llmagent
func (a *AstonishAgent) executeLLMNode(ctx agent.InvocationContext, node *config.Node, nodeName string, state session.State, yield func(*session.Event, error) bool) bool {
	// Render prompt and system instruction
	userPrompt := a.renderString(node.Prompt, state)
	systemInstruction := a.renderString(node.System, state)
	
	// Use system instruction as the main instruction for the agent
	// This ensures it goes to the System Prompt in the LLM request
	instruction := systemInstruction
	
	// If no system instruction, use the user prompt as instruction (fallback behavior)
	// But for Bedrock/Claude, we prefer separation.
	if instruction == "" {
		instruction = "You are a helpful AI assistant."
	}
	
	if a.DebugMode {
		fmt.Printf("[DEBUG] FINAL USER PROMPT:\n%s\n", userPrompt)
		fmt.Printf("[DEBUG] FINAL SYSTEM INSTRUCTION:\n%s\n", instruction)
	}
	
	// Manually append the User Message to the session history
	// This ensures that the LLM sees a User Message even if llmagent doesn't pick it up from context
	// or if history is empty.
	userEvent := &session.Event{
		InvocationID: ctx.InvocationID(),
		Branch:       ctx.Branch(),
		Author:       "user",
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Parts: []*genai.Part{{Text: userPrompt}},
				Role:  "user",
			},
		},
	}
	
	sess := ctx.Session()
	
	if a.SessionService != nil {
		
		// Unwrap ScopedSession if present, as SessionService might expect the underlying session type
		if scopedSess, ok := sess.(*ScopedSession); ok {
			sess = scopedSess.Session
		}
		
		// Try to append with (potentially unwrapped) session object
		if err := a.SessionService.AppendEvent(ctx, sess, userEvent); err != nil {
			// Retry with session fetched via Get (last resort)
			if sess.ID() != "" {
				appName := sess.AppName()
				if appName == "" {
					appName = "astonish"
				}
				userID := sess.UserID()
				if userID == "" {
					userID = "console_user"
				}
				
				getResp, getErr := a.SessionService.Get(ctx, &session.GetRequest{
					SessionID: sess.ID(),
					AppName:   appName,
					UserID:    userID,
				})
				if getErr == nil && getResp != nil && getResp.Session != nil {
					_ = a.SessionService.AppendEvent(ctx, getResp.Session, userEvent)
				}
			}
		}
	}

	// 2. Initialize LLM Agent
	// We need to pass tools if the node uses them
	var nodeTools []tool.Tool
	if node.Tools {
		// Validate that all selected tools exist
		if len(node.ToolsSelection) > 0 {
			foundTools := make(map[string]bool)
			
			// Check internal tools
			for _, t := range a.Tools {
				foundTools[t.Name()] = true
			}
			
			// Check MCP toolsets
			if len(a.Toolsets) > 0 {
				minimalCtx := &minimalReadonlyContext{Context: ctx}
				for _, ts := range a.Toolsets {
					tools, err := ts.Tools(minimalCtx)
					if err == nil {
						for _, t := range tools {
							foundTools[t.Name()] = true
						}
					}
				}
			}
			
			var missingTools []string
			for _, selected := range node.ToolsSelection {
				if !foundTools[selected] {
					missingTools = append(missingTools, selected)
				}
			}
			
			if len(missingTools) > 0 {
				yield(nil, fmt.Errorf("configured tools not found: %s", strings.Join(missingTools, ", ")))
				return false
			}
		}

		
		// Filter based on ToolsSelection
		if len(node.ToolsSelection) > 0 {

			for _, t := range a.Tools {
				for _, selected := range node.ToolsSelection {
					// Check against the underlying tool name if wrapped?
					// t.Name() should return the name.
					if t.Name() == selected {
						nodeTools = append(nodeTools, t)

					}
				}
			}

		} else {
			// If no selection, add all? Or none?
			// Python adds all if selection is empty?
			// For now, assume selection is required

			nodeTools = a.Tools
		}
	} else {

	}
	
	// Inject tool use instruction if tools are enabled
	if node.Tools {
		instruction += "\n\nIMPORTANT: You have access to tools that you MUST use to complete this task. Do not describe what you would do or say you are waiting for results. Instead, immediately call the appropriate tool with the required parameters. The tools are available and ready to use right now."
	}

	// Build OutputSchema from output_model if defined
	// This leverages ADK's native structured output support
	var outputSchema *genai.Schema
	var outputKey string
	if len(node.OutputModel) > 0 {
		// Add explicit instruction about the required output format
		instruction += "\n\nIMPORTANT: Your response MUST be a valid JSON object with the following structure:\n"
		instruction += "{\n"
		for key, typeName := range node.OutputModel {
			instruction += fmt.Sprintf("  \"%s\": <%s>,\n", key, typeName)
		}
		instruction += "}\n"
		instruction += "Do not include any other text, explanations, or markdown formatting. Return ONLY the JSON object."
		
		properties := make(map[string]*genai.Schema)
		required := []string{}
		
		for key, typeName := range node.OutputModel {
			var propType genai.Type
			var items *genai.Schema
			
			switch typeName {
			case "str", "string":
				propType = genai.TypeString
			case "int", "integer":
				propType = genai.TypeInteger
			case "float", "number":
				propType = genai.TypeNumber
			case "bool", "boolean":
				propType = genai.TypeBoolean
			case "list", "array":
				propType = genai.TypeArray
				// Default to string items, can be enhanced later
				items = &genai.Schema{Type: genai.TypeString}
			case "dict", "object", "any":
				propType = genai.TypeObject
			default:
				propType = genai.TypeString
			}
			
			schema := &genai.Schema{
				Type: propType,
			}
			if items != nil {
				schema.Items = items
			}
			
			properties[key] = schema
			required = append(required, key)
		}
		
		outputSchema = &genai.Schema{
			Type:       genai.TypeObject,
			Properties: properties,
			Required:   required,
		}
		
		// If there is only one output key, we might want to map it directly
		// But for now, we stick to the map/object structure
	}

	// Create ADK llmagent for this node
	// Strategy:
	// - Internal tools go via Tools field
	// - MCP toolsets go via Toolsets field
	// - OutputSchema is used for structured output (replaces manual JSON parsing)
	// Declare l (agent) early so it can be captured by the callback
	var l agent.Agent

	var llmAgent agent.Agent
	var err error

	var internalTools []tool.Tool
	if node.Tools {
		// Prepare internal tools (no wrapping needed - callback handles approval)
		if len(nodeTools) > 0 {
			internalTools = append(internalTools, nodeTools...)
		}

		// Prepare MCP toolsets (no wrapping needed - callback handles approval)
		var mcpToolsets []tool.Toolset
		if len(a.Toolsets) > 0 {
			for _, ts := range a.Toolsets {
				// Skip toolsets that don't contain any of the requested tools (if filtering is enabled)
				if len(node.ToolsSelection) > 0 {
					// Check if this toolset has any of the requested tools
					minimalCtx := &minimalReadonlyContext{Context: ctx}
					tsTools, err := ts.Tools(minimalCtx)
					if err != nil {

						continue
					}

					// Check if any tool in this toolset matches our selection
					hasMatchingTool := false
					for _, t := range tsTools {
						for _, allowed := range node.ToolsSelection {
							if t.Name() == allowed {
								hasMatchingTool = true
								break
							}
						}
						if hasMatchingTool {
							break
						}
					}

					if !hasMatchingTool {

						continue
					}
				}

				mcpToolsets = append(mcpToolsets, ts)

			}
		}


		// Apply tools_selection filter if specified
		if len(node.ToolsSelection) > 0 {


			// Filter internal tools
			var filteredInternalTools []tool.Tool
			for _, t := range internalTools {
				toolName := t.Name()
				for _, allowed := range node.ToolsSelection {
					if toolName == allowed {
						filteredInternalTools = append(filteredInternalTools, t)
						break
					}
				}
			}
			internalTools = filteredInternalTools

			// Wrap MCP toolsets with FilteredToolset to filter tools
			var filteredMCPToolsets []tool.Toolset
			for _, ts := range mcpToolsets {
				filteredMCPToolsets = append(filteredMCPToolsets, &FilteredToolset{
					underlying:   ts,
					allowedTools: node.ToolsSelection,
				})
			}
			mcpToolsets = filteredMCPToolsets


		}


		// Create BeforeToolCallback for approval if needed
		var beforeToolCallbacks []llmagent.BeforeToolCallback
		var afterToolCallbacks []llmagent.AfterToolCallback

		if !node.ToolsAutoApproval {
			beforeToolCallbacks = []llmagent.BeforeToolCallback{
				func(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
					toolName := t.Name()

					// DEBUG: Log tool execution attempt
					if a.DebugMode {
						argsJSON, _ := json.MarshalIndent(args, "", "  ")
						fmt.Printf("\n[DEBUG] ========== TOOL EXECUTION ATTEMPT ==========\n")
						fmt.Printf("[DEBUG] Tool Name: %s\n", toolName)
						fmt.Printf("[DEBUG] Arguments:\n%s\n", string(argsJSON))
						fmt.Printf("[DEBUG] ===============================================\n\n")
					}

					approvalKey := fmt.Sprintf("approval:%s", toolName)

					// Check if we already have approval for this tool
					approvedVal, _ := state.Get(approvalKey)

					approved := false
					if b, ok := approvedVal.(bool); ok && b {
						approved = true
					}

					if a.DebugMode {
						fmt.Printf("[DEBUG] Approval check: approved=%v, approvalKey=%s\n", approved, approvalKey)
					}

					// Fallback: Check history for explicit user approval if state is missing
					if !approved && a.SessionService != nil {
						// Retrieve session from service to get events
						sessResp, err := a.SessionService.Get(ctx, &session.GetRequest{
							AppName:   ctx.AppName(),
							UserID:    ctx.UserID(),
							SessionID: ctx.SessionID(),
						})

						if err == nil && sessResp != nil && sessResp.Session != nil {
							events := sessResp.Session.Events()
						if events.Len() >= 2 {
							lastEvent := events.At(events.Len() - 1)
							prevEvent := events.At(events.Len() - 2)

							// Check if last event is user "Yes"
							isUserYes := false
							if lastEvent.Author == "user" && lastEvent.LLMResponse.Content != nil && len(lastEvent.LLMResponse.Content.Parts) > 0 {
								text := strings.TrimSpace(lastEvent.LLMResponse.Content.Parts[0].Text)
								if strings.EqualFold(text, "Yes") {
									isUserYes = true
								}
							}

							if isUserYes {
								// Check if prev event was an approval request
								if prevEvent.Actions.StateDelta != nil {
									if val, ok := prevEvent.Actions.StateDelta["awaiting_approval"]; ok {
										if b, ok := val.(bool); ok && b {
											// Check if it was for THIS tool
											if toolVal, ok := prevEvent.Actions.StateDelta["approval_tool"]; ok {
												if tName, ok := toolVal.(string); ok && tName == toolName {
													approved = true
													if a.DebugMode {
														fmt.Println("[DEBUG] Callback: Approved via history check!")
													}
												}
											}
										}
									}
								}
							}
						}
						}
					}

					if approved {
						// Consume approval
						state.Set(approvalKey, false)
						if a.DebugMode {
							fmt.Printf("[DEBUG] Tool approved! Allowing execution to proceed.\n")
						}
						// Allow tool to run - returning nil means "proceed with actual execution"
						// The actual tool result will be captured by the ADK framework
						return nil, nil
					}

					if a.DebugMode {
						fmt.Printf("[DEBUG] Tool NOT approved. Requesting user approval...\n")
					}

					// No approval - request it
					// We set 'force_pause' to tell the outer loop to stop immediately
					state.Set("force_pause", true)
					state.Set("awaiting_approval", true)
					state.Set("approval_tool", toolName)
					state.Set("approval_args", args)

					// Emit approval request event
					prompt := a.formatToolApprovalRequest(toolName, args)
					yield(&session.Event{
						LLMResponse: model.LLMResponse{
							Content: &genai.Content{
								Parts: []*genai.Part{{Text: prompt}},
								Role:  "model",
							},
						},
						Actions: session.EventActions{
							StateDelta: map[string]any{
								"awaiting_approval": true,
								"approval_tool":     toolName,
								"approval_options":  []string{"Yes", "No"},
							},
						},
					}, nil)

					// Return a placeholder result
					// This string enters the LLM context. When we resume, we will overwrite this
					// context or instruct the LLM to ignore it and retry.
					placeholderResult := map[string]any{
						"status": "pending_approval",
						"info":   "Execution paused for user approval. When you see this message in history, it means the user has APPROVED the execution. You MUST retry the tool call immediately.",
					}

					if a.DebugMode {
						placeholderJSON, _ := json.MarshalIndent(placeholderResult, "", "  ")
						fmt.Printf("[DEBUG] Returning placeholder result:\n%s\n", string(placeholderJSON))
					}

					return placeholderResult, nil
				},
			}
		}

		// Add AfterToolCallback for debugging (removed force_stop logic that broke parallel processing)
		afterToolCallbacks = []llmagent.AfterToolCallback{
			func(ctx tool.Context, t tool.Tool, args map[string]any, result map[string]any, err error) (map[string]any, error) {
				if err != nil {
					// Tool failed, let the LLM see the error
					return result, err
				}

				toolName := t.Name()

				// DEBUG: Log successful tool execution
				if a.DebugMode {
					resultJSON, _ := json.MarshalIndent(result, "", "  ")
					fmt.Printf("\n[DEBUG] ========== AFTER TOOL CALLBACK ==========\n")
					fmt.Printf("[DEBUG] Tool Name: %s\n", toolName)
					fmt.Printf("[DEBUG] Result:\n%s\n", string(resultJSON))
					fmt.Printf("[DEBUG] ===========================================\n\n")
				}

				return result, nil
			},
		}

		llmAgent, err = llmagent.New(llmagent.Config{
			Name:                nodeName,
			Model:               a.LLM,
			Instruction:         instruction,
			Tools:               internalTools,
			Toolsets:            mcpToolsets,
			OutputSchema:        outputSchema,
			OutputKey:           outputKey,
			BeforeToolCallbacks: beforeToolCallbacks,
			AfterToolCallbacks:  afterToolCallbacks,
		})
	} else {
		// No tools enabled
		llmAgent, err = llmagent.New(llmagent.Config{
			Name:        nodeName,
			Model:       a.LLM,
			Instruction: instruction,
			Tools:       nodeTools,
			OutputSchema: outputSchema,
			OutputKey:    outputKey,
		})
	}
	l = llmAgent // Assign to 'l' after creation

	// Wrap session in LiveSession to ensure fresh history
	liveSess := &LiveSession{
		service: a.SessionService,
		ctx:     ctx,
		base:    sess,
	}

	// Create a ScopedContext that uses the LiveSession and the correct Agent (l)
	// This ensures:
	// 1. llmagent sees fresh history (via LiveSession.Events)
	// 2. llmagent sees itself as the agent (via ScopedContext.Agent override), fixing ContentsRequestProcessor
	scopedCtx := &ScopedContext{
		InvocationContext: ctx,
		session:           liveSess,
		state:             state,
		agent:             l,
	}

	// Use scopedCtx for Run
	ctx = scopedCtx

	if err != nil {
		yield(nil, fmt.Errorf("failed to create llmagent: %w", err))
		return false
	}

	// Reset the pause flag before starting
	state.Set("force_pause", false)
	
	// Determine if we should display LLM text output to the user
	// Logic:
	// - If output_model is defined: Suppress streaming (data extraction mode)
	// - If user_message is defined: Suppress streaming (controlled output mode)
	// - If neither is defined: Show streaming text (conversational mode)
	shouldDisplayText := len(node.OutputModel) == 0 && len(node.UserMessage) == 0
	
	// Run the agent - ADK handles everything (native function calling)
	var fullResponse strings.Builder
	var debugTextBuffer strings.Builder // Buffer for debug output
	toolCallCount := 0
	const maxToolCalls = 20 // Maximum tool calls to prevent infinite loops

	// Check if we should use ReAct fallback
	useReActFallback := false
	if fallbackVal, err := state.Get("_use_react_fallback"); err == nil {
		if b, ok := fallbackVal.(bool); ok && b {
			useReActFallback = true
		}
	}
	
	if useReActFallback {
		// Emit a message indicating we're using the fallback via spinner update
		yield(&session.Event{
			Actions: session.EventActions{
				StateDelta: map[string]any{
					"_spinner_text": fmt.Sprintf("Processing %s (Fallback: internal tool reasoning)...", node.Name),
				},
			},
		}, nil)
		
		// Collect all tools (internal + MCP) for ReAct planner
		allTools := make([]tool.Tool, 0, len(internalTools))
		allTools = append(allTools, internalTools...)
		
		// Add MCP tools
		if len(a.Toolsets) > 0 {
			minimalCtx := &minimalReadonlyContext{Context: ctx}
			for _, ts := range a.Toolsets {
				tsTools, err := ts.Tools(minimalCtx)
				if err != nil {
					continue
				}
				// Filter by tools_selection if specified
				if len(node.ToolsSelection) > 0 {
					for _, t := range tsTools {
						for _, selected := range node.ToolsSelection {
							if t.Name() == selected {
								allTools = append(allTools, t)
								break
							}
						}
					}
				} else {
					allTools = append(allTools, tsTools...)
				}
			}
		}
		
		// Create approval callback if tools_auto_approval is false
		var approvalCallback planner.ApprovalCallback
		if !node.ToolsAutoApproval {
			approvalCallback = func(toolName string, args map[string]any) (bool, error) {
				approvalKey := fmt.Sprintf("approval:%s", toolName)
				
				// Check if we already have approval for this tool
				approvedVal, _ := state.Get(approvalKey)
				approved := false
				if b, ok := approvedVal.(bool); ok && b {
					approved = true
				}
				
				if approved {
					// Consume approval
					state.Set(approvalKey, false)
					if a.DebugMode {
						fmt.Printf("[ReAct DEBUG] Tool %s approved! Allowing execution.\n", toolName)
					}
					return true, nil
				}
				
				if a.DebugMode {
					fmt.Printf("[ReAct DEBUG] Tool %s NOT approved. Requesting approval...\n", toolName)
				}
				
				// No approval - request it
				state.Set("force_pause", true)
				state.Set("awaiting_approval", true)
				state.Set("approval_tool", toolName)
				state.Set("approval_args", args)
				
				// Emit approval request event
				prompt := a.formatToolApprovalRequest(toolName, args)
				yield(&session.Event{
					LLMResponse: model.LLMResponse{
						Content: &genai.Content{
							Parts: []*genai.Part{{Text: prompt}},
							Role:  "model",
						},
					},
					Actions: session.EventActions{
						StateDelta: map[string]any{
							"awaiting_approval": true,
							"approval_tool":     toolName,
							"approval_options":  []string{"Yes", "No"},
						},
					},
				}, nil)
				
				return false, nil
			}
		}
		
		// Use manual ReAct planner with all tools and approval callback
		var reactPlanner *planner.ReActPlanner
		if approvalCallback != nil {
			reactPlanner = planner.NewReActPlannerWithApproval(a.LLM, allTools, approvalCallback, state, a.DebugMode)
		} else {
			reactPlanner = planner.NewReActPlanner(a.LLM, allTools)
		}
		
		// Strip output_model instructions from system instruction for ReAct
		// The ReAct loop should focus on tool usage, not output formatting
		cleanInstruction := instruction
		if len(node.OutputModel) > 0 {
			// Remove lines mentioning output_model fields
			lines := strings.Split(instruction, "\n")
			var cleanLines []string
			for _, line := range lines {
				// Skip lines that mention the output model fields or JSON formatting
				skipLine := false
				for key := range node.OutputModel {
					if strings.Contains(line, key) || strings.Contains(line, "JSON") || strings.Contains(line, "json") {
						skipLine = true
						break
					}
				}
				if !skipLine {
					cleanLines = append(cleanLines, line)
				}
			}
			cleanInstruction = strings.Join(cleanLines, "\n")
		}
		
		result, err := reactPlanner.Run(ctx, userPrompt, cleanInstruction) // Pass cleaned instruction
		if err != nil {
			// Check if this is an approval required error
			if strings.HasPrefix(err.Error(), "APPROVAL_REQUIRED:") {
				// Approval is needed - the callback has already emitted the approval request
				// Just return false to pause execution
				if a.DebugMode {
					fmt.Println("[ReAct DEBUG] Pausing for tool approval")
				}
				return false
			}
			yield(nil, fmt.Errorf("ReAct planner failed: %w", err))
			return false
		}
		
		// Format output according to output_model if specified
		if len(node.OutputModel) > 0 {
			formattedResult, formatErr := reactPlanner.FormatOutput(ctx, result, node.OutputModel, instruction)
			if formatErr != nil {
				yield(nil, fmt.Errorf("failed to format ReAct output: %w", formatErr))
				return false
			}
			result = formattedResult
			
			// Parse the formatted result and store in state
			var resultMap map[string]any
			if err := json.Unmarshal([]byte(result), &resultMap); err == nil {
				for key, value := range resultMap {
					state.Set(key, value)
				}
			}
		}
		
		// Handle user_message if defined
		if len(node.UserMessage) > 0 {
			var textParts []string
			for _, msgPart := range node.UserMessage {
				if val, err := state.Get(msgPart); err == nil {
					textParts = append(textParts, fmt.Sprintf("%v", val))
					if a.DebugMode {
						fmt.Printf("[ReAct DEBUG] Resolved '%s' to value: %v\n", msgPart, val)
					}
				}
			}
			
			if len(textParts) > 0 {
				// Emit user_message event
				userMessageEvent := &session.Event{
					LLMResponse: model.LLMResponse{
						Content: &genai.Content{
							Parts: []*genai.Part{{Text: strings.Join(textParts, " ")}},
							Role:  "model",
						},
					},
					Actions: session.EventActions{
						StateDelta: map[string]any{
							"_user_message_display": true,
						},
					},
				}
				yield(userMessageEvent, nil)
			}
		} else {
			// No user_message - yield the full result
			yield(&session.Event{
				LLMResponse: model.LLMResponse{
					Content: &genai.Content{
						Parts: []*genai.Part{{Text: result}},
						Role:  "model",
					},
				},
			}, nil)
		}
		
		return true
	}

	// We'll wrap the iteration in a function to allow retry
	runAgent := func() iter.Seq2[*session.Event, error] {
		return l.Run(ctx)
	}

	// Execute with fallback retry
	for event, err := range runAgent() {
		if err != nil {
			// Check for "Tool calling is not supported" error or OpenRouter 404
			if strings.Contains(err.Error(), "Tool calling is not supported") || 
			   strings.Contains(err.Error(), "No endpoints found that support tool use") {
				if a.DebugMode {
					fmt.Printf("[DEBUG] Caught tool calling error: %v. Switching to ReAct fallback.\n", err)
				}
				
				// Enable fallback for future runs
				state.Set("_use_react_fallback", true)
				
				// Emit a message indicating we're using the fallback via spinner update
				yield(&session.Event{
					Actions: session.EventActions{
						StateDelta: map[string]any{
							"_spinner_text": fmt.Sprintf("Processing %s (Fallback: internal tool reasoning)...", node.Name),
						},
					},
				}, nil)
				
				// Collect all tools (internal + MCP) for ReAct planner
				allTools := make([]tool.Tool, 0, len(internalTools))
				allTools = append(allTools, internalTools...)
				
				// Add MCP tools
				if len(a.Toolsets) > 0 {
					minimalCtx := &minimalReadonlyContext{Context: ctx}
					for _, ts := range a.Toolsets {
						tsTools, err := ts.Tools(minimalCtx)
						if err != nil {
							continue
						}
						// Filter by tools_selection if specified
						if len(node.ToolsSelection) > 0 {
							for _, t := range tsTools {
								for _, selected := range node.ToolsSelection {
									if t.Name() == selected {
										allTools = append(allTools, t)
										break
									}
								}
							}
						} else {
							allTools = append(allTools, tsTools...)
						}
					}
				}
				
				// Create approval callback if tools_auto_approval is false
				var approvalCallback planner.ApprovalCallback
				if !node.ToolsAutoApproval {
					approvalCallback = func(toolName string, args map[string]any) (bool, error) {
						approvalKey := fmt.Sprintf("approval:%s", toolName)
						approvedVal, _ := state.Get(approvalKey)
						approved := false
						if b, ok := approvedVal.(bool); ok && b {
							approved = true
						}
						if approved {
							state.Set(approvalKey, false)
							if a.DebugMode {
								fmt.Printf("[ReAct DEBUG] Tool %s approved!\n", toolName)
							}
							return true, nil
						}
						if a.DebugMode {
							fmt.Printf("[ReAct DEBUG] Tool %s NOT approved.\n", toolName)
						}
						state.Set("force_pause", true)
						state.Set("awaiting_approval", true)
						state.Set("approval_tool", toolName)
						state.Set("approval_args", args)
						prompt := a.formatToolApprovalRequest(toolName, args)
						yield(&session.Event{
							LLMResponse: model.LLMResponse{
								Content: &genai.Content{
									Parts: []*genai.Part{{Text: prompt}},
									Role:  "model",
								},
							},
							Actions: session.EventActions{
								StateDelta: map[string]any{
									"awaiting_approval": true,
									"approval_tool":     toolName,
									"approval_options":  []string{"Yes", "No"},
								},
							},
						}, nil)
						return false, nil
					}
				}
				var reactPlanner *planner.ReActPlanner
				if approvalCallback != nil {
					reactPlanner = planner.NewReActPlannerWithApproval(a.LLM, allTools, approvalCallback, state, a.DebugMode)
				} else {
					reactPlanner = planner.NewReActPlanner(a.LLM, allTools)
				}
				
				// Strip output_model instructions from system instruction for ReAct
				cleanInstruction := instruction
				if len(node.OutputModel) > 0 {
					lines := strings.Split(instruction, "\n")
					var cleanLines []string
					for _, line := range lines {
						skipLine := false
						for key := range node.OutputModel {
							if strings.Contains(line, key) || strings.Contains(line, "JSON") || strings.Contains(line, "json") {
								skipLine = true
								break
							}
						}
						if !skipLine {
							cleanLines = append(cleanLines, line)
						}
					}
					cleanInstruction = strings.Join(cleanLines, "\n")
				}
				
				// Execute planner
				result, planErr := reactPlanner.Run(ctx, userPrompt, cleanInstruction)
				if planErr != nil {
					if strings.HasPrefix(planErr.Error(), "APPROVAL_REQUIRED:") {
						if a.DebugMode {
							fmt.Println("[ReAct DEBUG] Pausing for tool approval")
						}
						return false
					}
					yield(nil, fmt.Errorf("ReAct planner fallback failed: %w", planErr))
					return false
				}
				
				// Format output according to output_model if specified
				if len(node.OutputModel) > 0 {
					formattedResult, formatErr := reactPlanner.FormatOutput(ctx, result, node.OutputModel, instruction)
					if formatErr != nil {
						yield(nil, fmt.Errorf("failed to format ReAct output: %w", formatErr))
						return false
					}
					result = formattedResult
					
					// Parse the formatted result and store in state
					var resultMap map[string]any
					if err := json.Unmarshal([]byte(result), &resultMap); err == nil {
						for key, value := range resultMap {
							state.Set(key, value)
						}
					}
				}
				
				// Handle user_message if defined
				if len(node.UserMessage) > 0 {
					var textParts []string
					for _, msgPart := range node.UserMessage {
						if val, err := state.Get(msgPart); err == nil {
							textParts = append(textParts, fmt.Sprintf("%v", val))
						}
					}
					
					if len(textParts) > 0 {
						userMessageEvent := &session.Event{
							LLMResponse: model.LLMResponse{
								Content: &genai.Content{
									Parts: []*genai.Part{{Text: strings.Join(textParts, " ")}},
									Role:  "model",
								},
							},
							Actions: session.EventActions{
								StateDelta: map[string]any{
									"_user_message_display": true,
								},
							},
						}
						yield(userMessageEvent, nil)
					}
				} else {
					// No user_message - yield the full result
					yield(&session.Event{
						LLMResponse: model.LLMResponse{
							Content: &genai.Content{
								Parts: []*genai.Part{{Text: result}},
								Role:  "model",
							},
						},
					}, nil)
				}
				
				return true
			}
			
			// Genuine error
			yield(nil, err)
			return false
		}
		
		// ... process event ...
		goto ProcessEvent
		
	ProcessEvent:


		// [ERROR HANDLING] Track tool errors but let them flow to the LLM
		// The LLM needs to see the error response to understand the tool failed
		// We'll stop after the LLM processes the error
		hasToolError := false
		var toolErrorMsg string
		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.FunctionResponse != nil {
					resp := part.FunctionResponse.Response
					
					// Check for the "error" key
					if errVal, hasError := resp["error"]; hasError && errVal != nil {
						toolName := part.FunctionResponse.Name
						
						// Convert error to string if it's an error type
						var errorStr string
						if errObj, ok := errVal.(error); ok {
							errorStr = errObj.Error()
						} else {
							errBytes, _ := json.Marshal(errVal)
							errorStr = string(errBytes)
						}
						
						if a.DebugMode {
							fmt.Printf("[DEBUG] Tool '%s' failed with error: %s\n", toolName, errorStr)
						}
						
						hasToolError = true
						toolErrorMsg = fmt.Sprintf("tool '%s' failed: %s", toolName, errorStr)
						// Don't return yet - let the error response flow to the LLM first
					}
				}
			}
		}
		
		// Debug logging for event flow
		if a.DebugMode {
			// fmt.Printf("[DEBUG] Event received: Type=%s, Node=%s\n", event.Type, event.NodeName) // Fields not available directly on event?
            // Let's just log the content parts
			if event.LLMResponse.Content != nil {
				for _, part := range event.LLMResponse.Content.Parts {
					if part.FunctionCall != nil {
						fmt.Printf("[DEBUG] -> Function Call: %s\n", part.FunctionCall.Name)
						toolCallCount++
					}
					if part.FunctionResponse != nil {
						respJSON, _ := json.MarshalIndent(part.FunctionResponse.Response, "", "  ")
						fmt.Printf("\n[DEBUG] ========== TOOL EXECUTION RESULT ==========\n")
						fmt.Printf("[DEBUG] Tool Name: %s\n", part.FunctionResponse.Name)
						fmt.Printf("[DEBUG] Response:\n%s\n", string(respJSON))
						fmt.Printf("[DEBUG] ============================================\n\n")
					}
					if part.Text != "" {
						// Buffer text instead of printing immediately
						debugTextBuffer.WriteString(part.Text)
					}
				}
			} else {
				// Also count function calls if they appear in other structures or if debug mode is off
				// But wait, we need to count them regardless of debug mode.
				// Let's do counting separately.
			}
		}

		// Count tool calls to prevent infinite loops
		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.FunctionCall != nil {
					if !a.DebugMode {
						toolCallCount++
					}
				}
			}
		}

		// Hard limit to prevent infinite loops
		if toolCallCount > maxToolCalls {
			if a.DebugMode {
				fmt.Printf("[DEBUG] Tool call limit exceeded (%d) for node '%s'\n", maxToolCalls, nodeName)
			}
			yield(nil, fmt.Errorf("tool call limit exceeded (%d) for node '%s'", maxToolCalls, nodeName))
			return false
		}
		
		// 1. Determine if this event should be displayed to the user
		shouldYieldEvent := true
		
		if event.LLMResponse.Content != nil && len(event.LLMResponse.Content.Parts) > 0 {
			// Check if this is a text-only event (no tool calls/responses)
			isTextOnly := true
			for _, part := range event.LLMResponse.Content.Parts {
				if part.FunctionCall != nil || part.FunctionResponse != nil {
					isTextOnly = false
					break
				}
			}
			
			// Suppress text-only events if user_message is not defined
			// This prevents unwanted LLM conversational output from being displayed
			if isTextOnly && !shouldDisplayText {
				shouldYieldEvent = false
			}
		}
		
		// 2. FORWARD the event if it should be displayed
		if shouldYieldEvent {
			if !yield(event, nil) {
				return false
			}
		}
		
		// 2a. If we detected a tool error, stop after yielding the error response
		// This allows the LLM to see the error before we stop
		if hasToolError {
			yield(nil, fmt.Errorf("%s", toolErrorMsg))
			return false
		}
		
		// 2b. CHECK FOR INTERRUPT SIGNAL
		// If the tool set this flag, we must stop immediately
		if shouldPause, _ := state.Get("force_pause"); shouldPause == true {
			// Clear the flag so it doesn't block the next run
			state.Set("force_pause", false)
			return false // Stops the loop, effectively pausing the agent
		}
		
		// 3. Accumulate text response for output_model (if needed)
		if event.Content != nil {

			for _, part := range event.Content.Parts {
				if part.Text != "" {
					fullResponse.WriteString(part.Text)
				}
			}
		}

		// 4. Handle raw_tool_output state updates
		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.FunctionResponse != nil {
					// This is a tool response - print it for debugging
					if a.DebugMode {
						respJSON, _ := json.MarshalIndent(part.FunctionResponse.Response, "", "  ")
						fmt.Printf("[DEBUG] Tool Response for %s: %s\n", part.FunctionResponse.Name, string(respJSON))
					}

					
					// Handle raw_tool_output if configured
					if len(node.RawToolOutput) == 1 {
						// Get the state key to update
						var stateKey string
						for k := range node.RawToolOutput {
							stateKey = k
							break
						}

						// Update state with the response map
						response := part.FunctionResponse.Response
						
						if err := state.Set(stateKey, response); err != nil {
							yield(nil, fmt.Errorf("failed to set state key %s: %w", stateKey, err))
							return false
						}

						// Add to StateDelta so UI updates
						if event.Actions.StateDelta == nil {
							event.Actions.StateDelta = make(map[string]any)
						}
						event.Actions.StateDelta[stateKey] = response
					}
				}
			}
		}
	}
	
	// Print accumulated debug text
	if a.DebugMode && debugTextBuffer.Len() > 0 {
		fmt.Printf("[DEBUG] Full LLM Response: %s\n", debugTextBuffer.String())
	}
	
	if a.DebugMode {
		fmt.Printf("[DEBUG] astonish_agent.go: After llmagent.Run loop. node.OutputModel length: %d, node.UserMessage length: %d\n", len(node.OutputModel), len(node.UserMessage))
	}

	// Distribute output_model values by parsing the LLM's text response
	// ADK's OutputSchema doesn't work reliably with tool-enabled nodes
	if len(node.OutputModel) > 0 {
		// Get the accumulated text response
		responseText := strings.TrimSpace(fullResponse.String())
		
		if a.DebugMode {
			fmt.Printf("[DEBUG] executeLLMNode: Attempting to extract output_model. Response length: %d\n", len(responseText))
		}
		
		if responseText != "" {
			// Try to parse as JSON
			cleaned := a.cleanAndFixJson(responseText)
			
			if a.DebugMode {
				fmt.Printf("[DEBUG] executeLLMNode: Cleaned JSON: %s\n", cleaned)
			}
			
			var parsedOutput map[string]any
			if err := json.Unmarshal([]byte(cleaned), &parsedOutput); err == nil {
				if a.DebugMode {
					fmt.Printf("[DEBUG] executeLLMNode: Successfully parsed JSON. Keys: %v\n", getKeys(parsedOutput))
				}
				
				// Distribute values to individual output_model keys
				delta := make(map[string]any)
				for key := range node.OutputModel {
					if val, ok := parsedOutput[key]; ok {
						if a.DebugMode {
							fmt.Printf("[DEBUG] executeLLMNode: Setting state key '%s' with value type: %T\n", key, val)
						}
						state.Set(key, val)
						delta[key] = val
					} else {
						if a.DebugMode {
							fmt.Printf("[DEBUG] executeLLMNode: Key '%s' not found in parsed output\n", key)
						}
					}
				}
				
				// Emit state delta if we updated anything
				if len(delta) > 0 {
					if a.DebugMode {
						fmt.Printf("[DEBUG] executeLLMNode: Emitting state delta with keys: %v\n", getKeys(delta))
					}
					yield(&session.Event{
						Actions: session.EventActions{
							StateDelta: delta,
						},
					}, nil)
				}
			} else {
				if a.DebugMode {
					fmt.Printf("[DEBUG] executeLLMNode: Failed to parse JSON: %v\n", err)
				}
			}
		} else {
			if a.DebugMode {
				fmt.Printf("[DEBUG] executeLLMNode: Response text is empty, skipping output_model extraction\n")
			}
		}
	}

	// Handle user_message if defined
	// IMPORTANT: We need to emit this with BOTH text content AND StateDelta
	// The text content will be displayed, and we'll add a special marker in StateDelta
	// to tell console.go to print the "Agent:" prefix
	if len(node.UserMessage) > 0 {
		var textParts []string
		
		for _, msgPart := range node.UserMessage {
			// Try to resolve as state variable first
			if val, err := state.Get(msgPart); err == nil {
				textParts = append(textParts, fmt.Sprintf("%v", val))
				
				if a.DebugMode {
					fmt.Printf("[DEBUG] astonish_agent.go: Resolved '%s' to value: %v\n", msgPart, val)
				}
			}
		}
		
		if len(textParts) > 0 {
			if a.DebugMode {
				fmt.Printf("[DEBUG] astonish_agent.go: Emitting user_message event with text: %s\n", strings.Join(textParts, " "))
			}
			
			// Emit event with text content AND a special marker in StateDelta
			// The marker tells console.go this is a user_message that needs the "Agent:" prefix
			userMessageEvent := &session.Event{
				LLMResponse: model.LLMResponse{
					Content: &genai.Content{
						Parts: []*genai.Part{{Text: strings.Join(textParts, " ")}},
						Role:  "model",
					},
				},
				Actions: session.EventActions{
					StateDelta: map[string]any{
						"_user_message_display": true, // Special marker for console.go
					},
				},
			}
			
			if !yield(userMessageEvent, nil) {
				return false
			}
			
			if a.DebugMode {
				fmt.Printf("[DEBUG] astonish_agent.go: user_message event yielded successfully\n")
			}
		}
	}
	
	return true
}

func (a *AstonishAgent) handleToolNode(ctx context.Context, node *config.Node, state session.State, yield func(*session.Event, error) bool) bool {
	// 1. Resolve arguments
	resolvedArgs := make(map[string]interface{})
	for key, val := range node.Args {
		if strVal, ok := val.(string); ok {
			resolvedArgs[key] = a.renderString(strVal, state)
		} else if mapVal, ok := val.(map[string]interface{}); ok && len(mapVal) == 1 {
			// Handle map arguments (e.g. owner: {owner: str}) -> resolve from state
			var stateKey string
			for k := range mapVal {
				stateKey = k
				break
			}
			
			if stateVal, err := state.Get(stateKey); err == nil {
				resolvedArgs[key] = stateVal
			} else {
				if a.DebugMode {
					fmt.Printf("[WARN] State key '%s' for arg '%s' not found in state.\n", stateKey, key)
				}
				resolvedArgs[key] = nil
			}
		} else {
			resolvedArgs[key] = val
		}
	}

	// 2. Identify Tool
	if len(node.ToolsSelection) == 0 {
		yield(nil, fmt.Errorf("tool node '%s' missing tools_selection", node.Name))
		return false
	}
	toolName := node.ToolsSelection[0]

	// 3. Approval Workflow
	approved := false
	if node.ToolsAutoApproval {
		approved = true
	} else {
		// Check if we already have approval for this specific tool execution
		approvalKey := fmt.Sprintf("approval:%s", toolName)
		val, _ := state.Get(approvalKey)
		if isApproved, ok := val.(bool); ok && isApproved {
			approved = true
			// Clear approval so we don't loop forever if we come back here? 
			// Actually, for a linear flow, it's fine. For a loop, we might want to clear it.
			// But clearing it might break if we crash and resume?
			// Let's clear it after execution.
		}
	}

	if !approved {
		// Set state for approval
		state.Set("awaiting_approval", true)
		state.Set("approval_tool", toolName)
		state.Set("approval_args", resolvedArgs)

		// Emit approval request
		approvalText := a.formatToolApprovalRequest(toolName, resolvedArgs)
		approvalEvent := &session.Event{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: approvalText}},
					Role:  "model",
				},
			},
			Actions: session.EventActions{
				StateDelta: map[string]any{
					"awaiting_approval": true,
					"current_node":      node.Name,
					"approval_tool":     toolName,
					"approval_args":     resolvedArgs,
					"approval_options":  []string{"Yes", "No"}, // Trigger interactive selection
				},
			},
		}
		// Yield and return false to pause execution
		yield(approvalEvent, nil)
		return false
	}

	// 4. Execute Tool
	// Find the tool in a.Tools
	var selectedTool tool.Tool
	for _, t := range a.Tools {
		if t.Name() == toolName {
			selectedTool = t
			break
		}
	}

	// If not found in internal tools, check Toolsets (MCP)
	if selectedTool == nil && a.Toolsets != nil {
		roCtx := &minimalReadonlyContext{Context: ctx}
		for _, ts := range a.Toolsets {
			tools, err := ts.Tools(roCtx)
			if err == nil {
				for _, t := range tools {
					if t.Name() == toolName {
						selectedTool = t
						break
					}
				}
			}
			if selectedTool != nil {
				break
			}
		}
	}
	if selectedTool == nil {
		yield(nil, fmt.Errorf("tool '%s' not found", toolName))
		return false
	}

	// 5. Type Conversion based on Schema
	if declTool, ok := selectedTool.(ToolWithDeclaration); ok {
		if a.DebugMode {
			fmt.Printf("[DEBUG] Tool '%s' implements ToolWithDeclaration\n", toolName)
		}
		decl := declTool.Declaration()
		if decl != nil && decl.ParametersJsonSchema != nil {
			if a.DebugMode {
				fmt.Printf("[DEBUG] ParametersJsonSchema type: %T\n", decl.ParametersJsonSchema)
			}
			if schema, ok := decl.ParametersJsonSchema.(*genai.Schema); ok {
				if a.DebugMode {
					fmt.Printf("[DEBUG] Schema Type: %s (Expected: %s)\n", schema.Type, genai.TypeObject)
				}
				if schema.Type == genai.TypeObject {
					for key, val := range resolvedArgs {
						if strVal, ok := val.(string); ok {
							if prop, ok := schema.Properties[key]; ok {
								if prop.Type == genai.TypeNumber || prop.Type == genai.TypeInteger {
									if num, err := strconv.ParseFloat(strVal, 64); err == nil {
										resolvedArgs[key] = num
										if a.DebugMode {
											fmt.Printf("[DEBUG] Converted arg '%s' from string to number: %v\n", key, num)
										}
									} else {
										// Fallback: Try to extract leading number (e.g. "709: Title" -> 709)
										// This handles cases where the selection includes the title
										re := regexp.MustCompile(`^(\d+)`)
										if match := re.FindStringSubmatch(strVal); len(match) > 1 {
											if num, err := strconv.ParseFloat(match[1], 64); err == nil {
												resolvedArgs[key] = num
												if a.DebugMode {
													fmt.Printf("[DEBUG] Extracted number from arg '%s': %v (from '%s')\n", key, num, strVal)
												}
											}
										}
									}
								} else if prop.Type == genai.TypeBoolean {
									// Try to convert to boolean
									if b, err := strconv.ParseBool(strVal); err == nil {
										resolvedArgs[key] = b
										if a.DebugMode {
											fmt.Printf("[DEBUG] Converted arg '%s' from string to boolean: %v\n", key, b)
										}
									}
								}
							}
						}
					}
				}
			} else if schemaMap, ok := decl.ParametersJsonSchema.(map[string]interface{}); ok {
				// Handle map[string]interface{} schema (common in MCP or other providers)
				
				if typeVal, ok := schemaMap["type"].(string); ok && typeVal == "object" {
					if props, ok := schemaMap["properties"].(map[string]interface{}); ok {
						for key, val := range resolvedArgs {
							if strVal, ok := val.(string); ok {
								if prop, ok := props[key].(map[string]interface{}); ok {
									propType, _ := prop["type"].(string)
									
									if propType == "number" || propType == "integer" {
										// Try to convert string to float64
										if num, err := strconv.ParseFloat(strVal, 64); err == nil {
											resolvedArgs[key] = num
										} else {
											// Fallback: Try to extract leading number (e.g. "709: Title" -> 709)
											re := regexp.MustCompile(`^(\d+)`)
											if match := re.FindStringSubmatch(strVal); len(match) > 1 {
												if num, err := strconv.ParseFloat(match[1], 64); err == nil {
													resolvedArgs[key] = num
												}
											}
										}
									} else if propType == "boolean" {
										// Try to convert string to boolean
										if b, err := strconv.ParseBool(strVal); err == nil {
											resolvedArgs[key] = b
										}
									}
								}
							}
							// Also handle int to float64 if needed
							if intVal, ok := val.(int); ok {
								// Check if schema expects number/integer
								if prop, ok := props[key].(map[string]interface{}); ok {
									propType, _ := prop["type"].(string)
									if propType == "number" {
										resolvedArgs[key] = float64(intVal)
									}
								}
							}
						}
					}
				}
			} else {
				if a.DebugMode {
					fmt.Printf("[DEBUG] ParametersJsonSchema is not *genai.Schema or map[string]interface{}\n")
				}
			}
		} else {
			if a.DebugMode {
				fmt.Printf("[DEBUG] Declaration or ParametersJsonSchema is nil\n")
			}
		}
	} else {
		if a.DebugMode {
			fmt.Printf("[DEBUG] Tool '%s' does NOT implement ToolWithDeclaration\n", toolName)
		}
	}

	// Execute using RunnableTool interface
	// Create tool context
	stateDelta := make(map[string]any)
	toolCtx := &minimalReadonlyContext{
		Context: ctx,
		actions: &session.EventActions{StateDelta: stateDelta},
		state:   state,
	}

	runnable, ok := selectedTool.(RunnableTool)
	if !ok {
		yield(nil, fmt.Errorf("tool '%s' does not implement Run method", toolName))
		return false
	}

	toolResult, err := runnable.Run(toolCtx, resolvedArgs)
	if err != nil {
		yield(nil, fmt.Errorf("tool execution failed: %w", err))
		return false
	}
	
	// 5. Process Output
	// toolResult is likely a struct (e.g. ShellCommandResult).
	// We need to extract fields based on `output_model` and `raw_tool_output`.
	
	// Convert result to map for easy access
	resultMap := make(map[string]interface{})
	// Marshal/Unmarshal hack to convert struct to map
	resultBytes, _ := json.Marshal(toolResult)
	err = json.Unmarshal(resultBytes, &resultMap)
	
	if err != nil {
		fmt.Printf("[DEBUG] JSON Unmarshal Error: %v\n", err)
	}
	// fmt.Printf("[DEBUG] Result JSON: %s\n", string(resultBytes))
	
	// Handle raw_tool_output
	for key, mapping := range node.RawToolOutput {
		// mapping is the field name in the tool result (e.g. "stdout")
		// key is the state key to set (e.g. "pr_diff")
		if val, ok := resultMap[mapping]; ok {
			stateDelta[key] = val
			state.Set(key, val)
		}
	}
	
	// Handle output_model
	for key, typeName := range node.OutputModel {
		// Try to find the content from various common keys
		var val interface{}
		found := false
		
		// Priority list of keys to check
		keysToCheck := []string{"stdout", "output", "content", "formatted_diff", "result"}
		
		// 1. Check explicit mapping first (if any) - though output_model doesn't define mapping usually
		
		// 2. Check common keys
		for _, k := range keysToCheck {
			if v, ok := resultMap[k]; ok {
				val = v
				found = true
				break
			}
		}
		
		// 3. If not found, check if the result itself is a string or simple type (and not a map/struct wrapper)
		if !found && len(resultMap) == 0 {
			// This might happen if toolResult was not a struct/map
			val = toolResult
			found = true
		}
		
		if found {
			if typeName == "list" {
				// Check if val is already a slice
				switch v := val.(type) {
				case []interface{}:
					stateDelta[key] = v
					state.Set(key, v)
					continue
				case []string:
					// Convert to []interface{}
					var list []interface{}
					for _, s := range v {
						list = append(list, s)
					}
					stateDelta[key] = list
					state.Set(key, list)
					continue
				}

				// Try to parse as JSON list first
				valStr := fmt.Sprintf("%v", val)
				var list []any
				if err := json.Unmarshal([]byte(valStr), &list); err == nil {
					stateDelta[key] = list
					state.Set(key, list)
				} else {
					// Fallback: Split by newline
					lines := strings.Split(strings.TrimSpace(valStr), "\n")
					var cleanLines []string
					for _, line := range lines {
						trimmedLine := strings.TrimSpace(line)
						if trimmedLine != "" {
							cleanLines = append(cleanLines, trimmedLine)
						}
					}
					stateDelta[key] = cleanLines
					state.Set(key, cleanLines)
				}
			} else if typeName == "any" {
				// Just set the value as is
				stateDelta[key] = val
				state.Set(key, val)
			} else {
				// Default to string
				valStr := fmt.Sprintf("%v", val)
				stateDelta[key] = valStr
				state.Set(key, valStr)
			}
		} else {
			if a.DebugMode {
				fmt.Printf("[WARN] Could not find output for key '%s' in tool result: %v\n", key, resultMap)
			}
		}
	}
	
	// Clear approval state
	state.Set("awaiting_approval", false)
	state.Set(fmt.Sprintf("approval:%s", toolName), false)
	
	// Yield result event
	yield(&session.Event{
		Actions: session.EventActions{
			StateDelta: stateDelta,
		},
	}, nil)
	
	return true
}

// formatToolApprovalRequest formats a tool approval request
func (a *AstonishAgent) formatToolApprovalRequest(toolName string, args map[string]interface{}) string {
	return ui.RenderToolBox(toolName, args)
}

func (a *AstonishAgent) getNode(name string) (*config.Node, bool) {
	for i := range a.Config.Nodes {
		if a.Config.Nodes[i].Name == name {
			return &a.Config.Nodes[i], true
		}
	}
	return nil, false
}

func (a *AstonishAgent) getNextNode(current string, state session.State) (string, error) {
	for _, item := range a.Config.Flow {
		if item.From == current {
			if item.To != "" {
				return item.To, nil
			}
			// Check edges
			for _, edge := range item.Edges {
				result := a.evaluateCondition(edge.Condition, state)
				if result {
					return edge.To, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no transition found from node: %s", current)
}

func (a *AstonishAgent) evaluateCondition(condition string, state session.State) bool {
	// Handle simple "true" condition
	if condition == "true" {
		return true
	}

	// Convert session.State to map[string]interface{}
	stateMap := a.stateToMap(state)

	// Use Starlark evaluator
	result, err := EvaluateCondition(condition, stateMap)
	if err != nil {
		if a.DebugMode {
			fmt.Printf("[DEBUG] Condition evaluation error for '%s': %v\n", condition, err)
		}
		return false
	}

	return result
}

// stateToMap converts session.State to map[string]interface{}
func (a *AstonishAgent) stateToMap(state session.State) map[string]interface{} {
	stateMap := make(map[string]interface{})
	
	// Use the All() iterator to get all key-value pairs
	for key, value := range state.All() {
		stateMap[key] = value
	}
	
	return stateMap
}

func (a *AstonishAgent) renderString(tmpl string, state session.State) string {
	// Use a regex that captures content inside {} but not nested {}
	// This allows for expressions like {comment["patch"]}
	re := regexp.MustCompile(`\{([^{}]+)\}`)
	
	// Convert state to map once for efficiency if needed, but renderString might be called often
	// For now, convert inside the loop or pass it?
	// stateToMap is relatively cheap if state is small.
	stateMap := a.stateToMap(state)

	return re.ReplaceAllStringFunc(tmpl, func(match string) string {
		expr := match[1 : len(match)-1]
		
		// Try to evaluate the expression using Starlark
		val, err := EvaluateExpression(expr, stateMap)
		if err != nil {
			// If evaluation fails, try simple lookup as fallback (or just return match)
			// The original logic just did state.Get(key)
			// If EvaluateExpression failed, it might be because it's not a valid expression or key missing
			return match
		}
		
		if val == nil {
			return match
		}
		
		formatted := ui.FormatAsYamlLike(val, 0)
		if a.DebugMode {
			fmt.Printf("[DEBUG] renderString: Replaced '{%s}' with:\n%s\n", expr, formatted)
		}
		return formatted
	})
}

func (a *AstonishAgent) cleanAndFixJson(input string) string {
	trimmed := strings.TrimSpace(input)
	
	// Strategy 1: Check if the input starts with JSON (most reliable for structured output)
	// This handles cases where the LLM returns pure JSON without markdown
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		// Find the matching closing bracket
		startChar := string(trimmed[0])
		endChar := "]"
		if startChar == "{" {
			endChar = "}"
		}
		
		depth := 0
		endIdx := -1
		inString := false
		escapeNext := false
		
		for i := 0; i < len(trimmed); i++ {
			ch := trimmed[i]
			
			// Handle string escaping to avoid counting brackets inside strings
			if escapeNext {
				escapeNext = false
				continue
			}
			if ch == '\\' {
				escapeNext = true
				continue
			}
			if ch == '"' {
				inString = !inString
				continue
			}
			
			// Only count brackets outside of strings
			if !inString {
				if string(ch) == startChar {
					depth++
				} else if string(ch) == endChar {
					depth--
					if depth == 0 {
						endIdx = i
						break
					}
				}
			}
		}
		
		if endIdx != -1 {
			return strings.TrimSpace(trimmed[:endIdx+1])
		}
	}
	
	// Strategy 2: Look for markdown JSON code blocks
	re := regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")
	match := re.FindStringSubmatch(trimmed)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	
	// Strategy 3: Try to find JSON object/array anywhere in the text
	// This is the fallback for cases where JSON is embedded in other text
	startIdx := -1
	startChar := ""
	for i, ch := range trimmed {
		if ch == '[' || ch == '{' {
			startIdx = i
			startChar = string(ch)
			break
		}
	}
	
	if startIdx == -1 {
		// No JSON found, return as-is
		return trimmed
	}
	
	// Find the matching closing bracket with proper string handling
	endChar := "]"
	if startChar == "{" {
		endChar = "}"
	}
	
	depth := 0
	endIdx := -1
	inString := false
	escapeNext := false
	
	for i := startIdx; i < len(trimmed); i++ {
		ch := trimmed[i]
		
		// Handle string escaping
		if escapeNext {
			escapeNext = false
			continue
		}
		if ch == '\\' {
			escapeNext = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		
		// Only count brackets outside of strings
		if !inString {
			if string(ch) == startChar {
				depth++
			} else if string(ch) == endChar {
				depth--
				if depth == 0 {
					endIdx = i
					break
				}
			}
		}
	}
	
	if endIdx != -1 {
		return strings.TrimSpace(trimmed[startIdx : endIdx+1])
	}
	
	return trimmed
}

// getKeys returns the keys of a map as a slice
func getKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// extractToolNameFromXML extracts the tool name from XML-formatted tool calls
func extractToolNameFromXML(text string, knownTools []string) string {
	// 1. [NEW] Handle <invoke name="tool_name"> format (Seen in logs)
	reInvoke := regexp.MustCompile(`<invoke name="([^"]+)">`)
	matchesInvoke := reInvoke.FindStringSubmatch(text)
	if len(matchesInvoke) > 1 {
		return strings.TrimSpace(matchesInvoke[1])
	}

	// 2. Try to extract from <tool_name>...</tool_name>
	re := regexp.MustCompile(`<tool_name>([^<]+)</tool_name>`)
	matches := re.FindStringSubmatch(text)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	
	// 3. Fallback for other formats
	re2 := regexp.MustCompile(`<tool_use>.*?</tool_use>`)
	if re2.MatchString(text) {
		commonTools := []string{"read_file", "write_file", "execute_command", "search_files", "list_files"}
		// Add known tools to common tools
		commonTools = append(commonTools, knownTools...)
		
		for _, toolName := range commonTools {
			if strings.Contains(text, toolName) {
				return toolName
			}
		}
	}

	// 4. [NEW] Check for known tools as tags directly
	for _, toolName := range knownTools {
		// Check for <toolName>
		if strings.Contains(text, "<"+toolName+">") {
			return toolName
		}
	}
	
	return ""
}

// extractParametersFromXML extracts parameters from XML-formatted tool calls
func extractParametersFromXML(text string) map[string]any {
	params := make(map[string]any)
	
	// 1. Handle <parameter name="key">value</parameter> format (Seen in logs)
	reParam := regexp.MustCompile(`<parameter name="([^"]+)">([^<]+)</parameter>`)
	matchesParam := reParam.FindAllStringSubmatch(text, -1)
	for _, match := range matchesParam {
		if len(match) >= 3 {
			params[match[1]] = strings.TrimSpace(match[2])
		}
	}
	
	// If we found params using the new method, return them
	if len(params) > 0 {
		return params
	}

	// 2. Handle <key>value</key> format (without backreferences - Go doesn't support them)
	// Extract all potential parameter tags
	re := regexp.MustCompile(`<([a-zA-Z_][a-zA-Z0-9_]*)>([^<]*)</([a-zA-Z_][a-zA-Z0-9_]*)>`)
	matches := re.FindAllStringSubmatch(text, -1)
	
	for _, match := range matches {
		if len(match) >= 4 {
			openTag := match[1]
			value := strings.TrimSpace(match[2])
			closeTag := match[3]
			
			// Only accept if opening and closing tags match
			if openTag != closeTag {
				continue
			}
			
			// Skip structural tags
			if openTag == "tool_use" || openTag == "tool_name" || 
			   openTag == "function_calls" || openTag == "invoke" ||
			   openTag == "parameters" {
				continue
			}
			
			params[openTag] = value
		}
	}
	
	// Generic fallback
	if len(params) == 0 {
		params["detected"] = "prompt-based tool call (check raw logs)"
	}
	
	return params
}

// ScopedState wraps a parent state and allows local overrides
type ScopedState struct {
	Parent session.State
	Local  map[string]any
}

func (s *ScopedState) Get(key string) (any, error) {
	if v, ok := s.Local[key]; ok {
		return v, nil
	}
	return s.Parent.Get(key)
}

func (s *ScopedState) Set(key string, value any) error {
	s.Local[key] = value
	return nil
}

func (s *ScopedState) Delete(key string) error {
	delete(s.Local, key)
	return nil
}

func (s *ScopedState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		// Yield local keys
		for k, v := range s.Local {
			if !yield(k, v) {
				return
			}
		}
		// Yield parent keys if not in local
		for k, v := range s.Parent.All() {
			if _, ok := s.Local[k]; !ok {
				if !yield(k, v) {
					return
				}
			}
		}
	}
}

// ScopedContext wraps an InvocationContext and overrides the state
type ScopedContext struct {
	agent.InvocationContext
	state   session.State
	session session.Session
	agent   agent.Agent
}

func (s *ScopedContext) SessionID() string {
	return s.session.ID()
}

func (s *ScopedContext) Session() session.Session {
	return &ScopedSession{
		Session: s.session,
		state:   s.state,
	}
}

func (s *ScopedContext) State() session.State {
	return s.state
}

func (s *ScopedContext) Agent() agent.Agent {
	if s.agent != nil {
		return s.agent
	}
	return s.InvocationContext.Agent()
}

// ScopedSession wraps a Session and overrides the state
type ScopedSession struct {
	session.Session
	state session.State
}

func (s *ScopedSession) State() session.State {
	return s.state
}

// LiveSession wraps a session and fetches fresh data from the service on access
type LiveSession struct {
	service session.Service
	ctx     context.Context
	base    session.Session
}

func (s *LiveSession) ID() string {
	return s.base.ID()
}

func (s *LiveSession) AppName() string {
	return s.base.AppName()
}

func (s *LiveSession) UserID() string {
	return s.base.UserID()
}

func (s *LiveSession) getAppAndUser() (string, string) {
	appName := s.base.AppName()
	if appName == "" {
		appName = "astonish"
	}
	userID := s.base.UserID()
	if userID == "" {
		userID = "console_user"
	}
	return appName, userID
}

func (s *LiveSession) LastUpdateTime() time.Time {
	appName, userID := s.getAppAndUser()
	resp, err := s.service.Get(s.ctx, &session.GetRequest{
		SessionID: s.base.ID(),
		AppName:   appName,
		UserID:    userID,
	})
	if err != nil || resp == nil || resp.Session == nil {
		return s.base.LastUpdateTime()
	}
	return resp.Session.LastUpdateTime()
}

func (s *LiveSession) State() session.State {
	appName, userID := s.getAppAndUser()
	resp, err := s.service.Get(s.ctx, &session.GetRequest{
		SessionID: s.base.ID(),
		AppName:   appName,
		UserID:    userID,
	})
	if err != nil || resp == nil || resp.Session == nil {
		return s.base.State()
	}
	return resp.Session.State()
}

func (s *LiveSession) Events() session.Events {
	appName, userID := s.getAppAndUser()
	resp, err := s.service.Get(s.ctx, &session.GetRequest{
		SessionID: s.base.ID(),
		AppName:   appName,
		UserID:    userID,
	})
	if err != nil || resp == nil || resp.Session == nil {
		return s.base.Events()
	}
	return resp.Session.Events()
}

// handleParallelNode handles nodes with parallel configuration
func (a *AstonishAgent) handleParallelNode(ctx agent.InvocationContext, node *config.Node, state session.State, yield func(*session.Event, error) bool) bool {
	pConfig := node.Parallel
	if pConfig == nil {
		return false
	}

	// 1. Get the list to iterate over
	listKey := strings.Trim(pConfig.ForEach, "{}") // Remove potential braces
	listVal, err := state.Get(listKey)
	if err != nil {
		yield(nil, fmt.Errorf("failed to get parallel list '%s': %w", listKey, err))
		return false
	}
	

	// Convert to slice
	var items []any
	if l, ok := listVal.([]any); ok {
		items = l
	} else if l, ok := listVal.([]string); ok {
		for _, v := range l {
			items = append(items, v)
		}
	} else if l, ok := listVal.([]int); ok {
		for _, v := range l {
			items = append(items, v)
		}
	} else {
		// Try reflection or assume empty/error
		// Also handle if it's a list of maps (common in JSON)
		if l, ok := listVal.([]map[string]any); ok {
			for _, v := range l {
				items = append(items, v)
			}
		} else if l, ok := listVal.([]interface{}); ok {
			items = l
		} else {
			yield(nil, fmt.Errorf("variable '%s' is not a list (type: %T)", listKey, listVal))
			return false
		}
	}

	if len(items) == 0 {
		return true
	}

	// 2. Prepare for aggregation
	if len(node.OutputModel) != 1 {
		yield(nil, fmt.Errorf("parallel node must have exactly one key in output_model"))
		return false
	}
	var outputKey string
	for k := range node.OutputModel {
		outputKey = k
		break
	}

	// 3. Execute in Parallel
	maxConcurrency := 1
	if pConfig.MaxConcurrency > 0 {
		maxConcurrency = pConfig.MaxConcurrency
	}
	
	// Semaphore to limit concurrency
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex // Protects results and yield
	
	// Pre-allocate results to preserve order
	results := make([]any, len(items))
	// Track success to know if we should include the result
	successes := make([]bool, len(items))

	// Initialize progress bar UI
	prog := ui.NewParallelProgram(len(items), node.Name)
	
	// Channel to signal UI completion
	uiDone := make(chan struct{})
	
	// Run UI in a goroutine
	go func() {
		defer close(uiDone)
		if _, err := prog.Run(); err != nil {
			fmt.Printf("Error running progress UI: %v\n", err)
		}
	}()

	// Wrap yield to be thread-safe and track cancellation
	yieldCancelled := false
	safeYield := func(event *session.Event, err error) bool {
		mu.Lock()
		defer mu.Unlock()
		if yieldCancelled {
			return false
		}
		
		// Log tool calls and errors to UI
		if event != nil && event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.FunctionCall != nil {
					msg := fmt.Sprintf("Calling tool: %s", part.FunctionCall.Name)
					prog.Send(ui.ItemLogMsg(msg))
				}
				if part.FunctionResponse != nil {
					msg := fmt.Sprintf("Tool finished: %s", part.FunctionResponse.Name)
					prog.Send(ui.ItemLogMsg(msg))
				}
			}
		}
		if err != nil {
			msg := fmt.Sprintf("Error: %v", err)
			prog.Send(ui.ItemLogMsg(msg))
		}
		
		// Suppress StateDelta and LLMResponse to prevent console printing during parallel execution
		// We only want to collect the results, not print them as they happen
		if event != nil {
			if event.Actions.StateDelta != nil {
				event.Actions.StateDelta = nil
			}
			// Also suppress content streaming
			if event.LLMResponse.Content != nil {
				event.LLMResponse.Content = nil
			}
		}

		if !yield(event, err) {
			yieldCancelled = true
			return false
		}
		return true
	}

	// Track active workers
	var activeWorkers int32

	for i, item := range items {
		wg.Add(1)
		go func(idx int, it any) {
			defer wg.Done()
			
			// Acquire semaphore
			sem <- struct{}{}
			
			// Update active count
			atomic.AddInt32(&activeWorkers, 1)
			prog.Send(ui.ActiveCountMsg(atomic.LoadInt32(&activeWorkers)))
			
			defer func() { 
				// Update active count
				atomic.AddInt32(&activeWorkers, -1)
				prog.Send(ui.ActiveCountMsg(atomic.LoadInt32(&activeWorkers)))
				<-sem 
			}()

			// Check for cancellation or stop flag
			select {
			case <-ctx.Done():
				return
			default:
				// Check for force_stop_parallel flag in parent state
				// Use a mutex if needed, but state.Get is thread-safe enough for this check
				if val, err := state.Get("force_stop_parallel"); err == nil {
					if b, ok := val.(bool); ok && b {
						return
					}
				}
			}

			// fmt.Printf("[⚡️ Parallel] Processing item %d/%d for node '%s'\n", idx+1, len(items), node.Name)

			scopedState := &ScopedState{
				Parent: state,
				Local:  make(map[string]any),
			}
			
			scopedState.Local[pConfig.As] = it
			if pConfig.IndexAs != "" {
				scopedState.Local[pConfig.IndexAs] = idx
			}
			
			// Workaround for "Severity" template error in ADK llmagent
			if _, err := scopedState.Get("Severity"); err != nil {
				scopedState.Local["Severity"] = "{{Severity}}"
			}

			// Create ephemeral session for isolation
			// This ensures each parallel branch has its own history
			// Include node name to avoid collisions between different parallel nodes in the same flow
			newSessionID := fmt.Sprintf("%s:%s:parallel-%d", ctx.Session().ID(), node.Name, idx)
			
			// Create a session using the service
			// We need to pass AppName and UserID if possible
			createReq := &session.CreateRequest{
				SessionID: newSessionID,
			}
			
			// If we can get real values:
			if s, ok := ctx.Session().(interface{ AppName() string }); ok {
				createReq.AppName = s.AppName()
			}
			if s, ok := ctx.Session().(interface{ UserID() string }); ok {
				createReq.UserID = s.UserID()
			}

			createResp, err := a.SessionService.Create(ctx, createReq)
			if err != nil {
				safeYield(nil, fmt.Errorf("failed to create ephemeral session: %w", err))
				return
			}
			
			scopedCtx := &ScopedContext{
				InvocationContext: ctx,
				state:             scopedState,
				session:           createResp.Session,
			}

			success := false
			if node.Type == "tool" {
				success = a.handleToolNode(scopedCtx, node, scopedState, safeYield)
			} else if node.Type == "llm" {
				success = a.executeLLMNode(scopedCtx, node, node.Name, scopedState, safeYield)
			} else {
				safeYield(nil, fmt.Errorf("unsupported type for parallel node: %s", node.Type))
				return
			}
			
			// Signal UI that item is finished
			prog.Send(ui.ItemFinishedMsg{})

			if !success {
				// If execution failed, don't try to get the result
				// Just return - the error has already been yielded
				return
			}

			val, err := scopedState.Get(outputKey)
			if err == nil {
				mu.Lock()
				results[idx] = val
				successes[idx] = true
				mu.Unlock()
			}
		}(i, item)
	}

	wg.Wait()
	
	// Ensure UI is done (though model handles auto-quit)
	// We can wait a tiny bit to ensure the final render happens if needed, 
	// but usually wg.Wait() + the model's logic is enough.
	<-uiDone

	// Check if cancelled during execution (includes errors)
	if yieldCancelled {
		return false
	}

	// 4. Update Parent State with Aggregated Results
	// Filter results based on success to maintain density if needed, 
	// OR keep them sparse?
	// The original sequential logic appended only on success.
	// So we should filter.
	var finalResults []any
	for i, s := range successes {
		if s {
			finalResults = append(finalResults, results[i])
		}
	}

	existingVal, _ := state.Get(outputKey)
	var final []any
	
	if existingVal != nil {
		if l, ok := existingVal.([]any); ok {
			final = l
		} else {
			final = []any{}
		}
	} else {
		final = []any{}
	}
	
	// Aggregate results based on output_action
	// If output_action is "append", flatten lists; otherwise keep as-is
	outputAction := "append" // Default behavior
	if node.OutputAction != "" {
		outputAction = node.OutputAction
	}
	
	for _, res := range finalResults {
		if outputAction == "append" {
			// Check if res is a JSON string that needs parsing
			if strRes, ok := res.(string); ok {
				// Try to parse as JSON
				cleaned := a.cleanAndFixJson(strRes)
				var parsed any
				if err := json.Unmarshal([]byte(cleaned), &parsed); err == nil {
					// Successfully parsed - check if it's a map with the output key
					if parsedMap, ok := parsed.(map[string]any); ok {
						// Check if the map contains the output key (e.g., "review_comment_validated")
						if val, ok := parsedMap[outputKey]; ok {
							// Extract the value from the nested structure
							if l, ok := val.([]any); ok {
								// It's a list - flatten it
								final = append(final, l...)
								continue
							} else {
								// It's a single value - append it
								final = append(final, val)
								continue
							}
						}
					}
					// If parsed but doesn't match expected structure, treat as regular value
					res = parsed
				}
				// If parsing failed, continue with string as-is
			}
			
			// Flatten lists when appending
			if l, ok := res.([]any); ok {
				// Flatten the list - append all items from the list
				final = append(final, l...)
			} else {
				// Not a list, append as-is
				final = append(final, res)
			}
		} else {
			// Keep results as-is (don't flatten)
			final = append(final, res)
		}
	}
	
	state.Set(outputKey, final)
	
	yield(&session.Event{
		Actions: session.EventActions{
			StateDelta: map[string]any{
				outputKey: final,
			},
		},
	}, nil)

	return true
}

// handleOutputNode handles output nodes
func (a *AstonishAgent) handleOutputNode(ctx agent.InvocationContext, node *config.Node, state session.State, yield func(*session.Event, error) bool) bool {
	var parts []string
	for _, msgPart := range node.UserMessage {
		// Check if part is a state variable
		if val, err := state.Get(msgPart); err == nil {
			parts = append(parts, ui.FormatAsYamlLike(val, 0))
		} else {
			// Not a state variable, use as literal
			parts = append(parts, msgPart)
		}
	}

	message := strings.Join(parts, " ")
	
	// Emit message event
	evt := &session.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Parts: []*genai.Part{{Text: message}},
				Role:  "model",
			},
		},
	}
	
	return yield(evt, nil)
}
