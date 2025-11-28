package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"log"
	"regexp"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
	pkgtools "github.com/schardosin/astonish/pkg/tools"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
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
}

func (m *minimalReadonlyContext) AgentName() string                    { return "astonish-agent" }
func (m *minimalReadonlyContext) AppName() string                      { return "astonish" }
func (m *minimalReadonlyContext) UserContent() *genai.Content          { return nil }
func (m *minimalReadonlyContext) InvocationID() string                 { return "" }
func (m *minimalReadonlyContext) ReadonlyState() session.ReadonlyState { return nil }
func (m *minimalReadonlyContext) UserID() string                       { return "" }
func (m *minimalReadonlyContext) SessionID() string                    { return "" }
func (m *minimalReadonlyContext) Branch() string                       { return "" }

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

// RunnableTool interface for executing tools.
type RunnableTool interface {
	tool.Tool
	Run(ctx tool.Context, args any) (map[string]any, error)
}

// ProtectedTool wraps a standard tool and adds an approval gate.
type ProtectedTool struct {
	tool.Tool                // Embed the underlying tool
	State     session.State  // Access to session state
	Agent     *AstonishAgent // Access to helper methods
	YieldFunc func(*session.Event, error) bool // For emitting events
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
				
				promptEvent := &session.Event{
					LLMResponse: model.LLMResponse{
						Content: &genai.Content{
							Parts: []*genai.Part{{Text: prompt}},
							Role:  "model",
						},
					},
					Actions: session.EventActions{
						StateDelta: map[string]any{
							"current_node": currentNodeName,
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
				if err := state.Set("current_node", "END"); err != nil {
					yield(nil, err)
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
		return true
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
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Parts: []*genai.Part{{
					Text: fmt.Sprintf("\n--- Node %s ---\n", nodeName),
				}},
				Role: "model",
			},
		},
		Actions: session.EventActions{
			StateDelta: map[string]any{
				"current_node":       nodeName,
				"temp:node_history":  history,
				"temp:node_type":     node.Type,
			},
		},
	}
	
	return yield(event, nil)
}

// handleToolApproval handles tool approval flow when resuming after pause
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
	// Emit info message
	infoEvent := &session.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Parts: []*genai.Part{{
					Text: fmt.Sprintf("[ℹ️ Info] Starting LLM node processing for %s\n", nodeName),
				}},
				Role: "model",
			},
		},
	}
	if !yield(infoEvent, nil) {
		return false
	}
	
	// Render prompt and system instruction
	instruction := a.renderString(node.Prompt, state)
	systemInstruction := a.renderString(node.System, state)
	
	// Combine system and instruction if both present
	if systemInstruction != "" {
		instruction = systemInstruction + "\n\n" + instruction
	}
	
	// 2. Initialize LLM Agent
	// We need to pass tools if the node uses them
	var nodeTools []tool.Tool
	if node.Tools {

		
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

	// Build list of known tool names for detection (from internal tools)
	var knownToolNames []string
	for _, t := range nodeTools {
		knownToolNames = append(knownToolNames, t.Name())
	}
	
	// Create ADK llmagent for this node
	// NEW STRATEGY: Keep MCP toolsets as toolsets, don't extract individual tools
	// - Internal tools go via Tools field
	// - MCP toolsets go via Toolsets field (wrapped with ProtectedToolset)
	// This ensures ProcessRequest is called and tools are sent to the LLM
	var llmAgent agent.Agent
	var err error
	
	if node.Tools {
		// Prepare internal tools (no wrapping needed - callback handles approval)
		var internalTools []tool.Tool
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
		if !node.ToolsAutoApproval {
			beforeToolCallbacks = []llmagent.BeforeToolCallback{
				func(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
					toolName := t.Name()

					// Debug logging
					if a.DebugMode {
						argsJSON, err := json.MarshalIndent(args, "", "  ")
						if err != nil {
							log.Printf("[DEBUG] Failed to marshal tool arguments for logging: %v", err)
						} else {
							// Use ANSI colors if possible, but we don't have constants here.
							// Just plain text for now or hardcode colors.
							// Cyan: \033[36m, Reset: \033[0m
							fmt.Printf("\n\033[36m[DEBUG] Tool Call (Intercepted): %s\033[0m\nArguments:\n%s\n", toolName, string(argsJSON))
						}
					}

					approvalKey := fmt.Sprintf("approval:%s", toolName)
					
					// Check if we already have approval for this tool
					approvedVal, _ := state.Get(approvalKey)
					if a.DebugMode {
						fmt.Printf("[DEBUG] Callback: Checking approval for %s. Key=%s, Value=%v\n", toolName, approvalKey, approvedVal)
					}

					approved := false
					if b, ok := approvedVal.(bool); ok && b {
						approved = true
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
						// Allow tool to run
						return nil, nil
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
					return map[string]any{
						"status": "pending_approval",
						"info":   "Execution paused for user approval. When you see this message in history, it means the user has APPROVED the execution. You MUST retry the tool call immediately.",
					}, nil
				},
			}
		}
		

		llmAgent, err = llmagent.New(llmagent.Config{
			Name:                nodeName,
			Model:               a.LLM,
			Instruction:         instruction,
			Tools:               internalTools,
			Toolsets:            mcpToolsets,
			BeforeToolCallbacks: beforeToolCallbacks,
		})
	} else {
		// No tools enabled

		llmAgent, err = llmagent.New(llmagent.Config{
			Name:        nodeName,
			Model:       a.LLM,
			Instruction: instruction,
		})
	}
	
	if err != nil {
		yield(nil, fmt.Errorf("failed to create llmagent: %w", err))
		return false
	}
	
	// Reset the pause flag before starting
	state.Set("force_pause", false)
	
	// Run the agent - ADK handles everything (native function calling)
	var fullResponse strings.Builder
	
	for event, err := range llmAgent.Run(ctx) {
		if err != nil {
			// Genuine error
			yield(nil, err)
			return false
		}
		
		// 1. FORWARD the event first (so the UI sees the tool output/logs)
		if !yield(event, nil) {
			return false
		}
		
		// 2. CHECK FOR INTERRUPT SIGNAL
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
	


	// Save to output_model if defined
	if len(node.OutputModel) > 0 {
		output := fullResponse.String()
		for key := range node.OutputModel {
			if err := state.Set(key, output); err != nil {
				yield(nil, err)
				return false
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
	if selectedTool == nil {
		yield(nil, fmt.Errorf("tool '%s' not found", toolName))
		return false
	}

	// Execute using internal helper
	toolResult, err := pkgtools.ExecuteTool(ctx, toolName, resolvedArgs)
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
	json.Unmarshal(resultBytes, &resultMap)
	
	stateDelta := make(map[string]any)
	
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
		// For now, we assume the tool result HAS a field matching the key?
		// Or does the node config specify mapping?
		// In `github_pr_description_generator`:
		// output_model: { prs: list }
		// args: { command: "gh pr list" }
		// The tool `shell_command` returns `stdout`.
		// There is no explicit mapping in `output_model` keys to tool result fields.
		// It seems we assume the tool result *is* the content?
		// Or we map from `stdout`?
		
		// In the python code, `tool` node has `output_model` and `raw_tool_output`.
		// `raw_tool_output` maps tool output fields to state fields.
		// `output_model` seems to imply parsing?
		
		// Example:
		// name: get_prs
		// output_model: { prs: list }
		// No `raw_tool_output`.
		// So it must take `stdout` and parse it into `prs`.
		
		if val, ok := resultMap["stdout"]; ok {
			valStr := fmt.Sprintf("%v", val)
			if typeName == "list" {
				// Split by newline
				lines := strings.Split(strings.TrimSpace(valStr), "\n")
				var cleanLines []string
				for _, line := range lines {
					if strings.TrimSpace(line) != "" {
						cleanLines = append(cleanLines, line)
					}
				}
				stateDelta[key] = cleanLines
				state.Set(key, cleanLines)
			} else {
				stateDelta[key] = valStr
				state.Set(key, valStr)
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
	// Calculate required width
	minContentWidth := 44
	contentWidth := minContentWidth
	
	// Check tool name length
	toolLineLen := len(fmt.Sprintf("Tool: %s", toolName))
	if toolLineLen > contentWidth {
		contentWidth = toolLineLen
	}
	
	// Check args length
	type argLine struct {
		key   string
		value string
		multiline bool
	}
	var processedArgs []argLine
	
	for key, value := range args {
		valStr := fmt.Sprintf("%v", value)
		lineLen := len(fmt.Sprintf("%s: %s", key, valStr))
		
		if lineLen > 40 { // Lower threshold to trigger multiline more often
			processedArgs = append(processedArgs, argLine{key: key, value: valStr, multiline: true})
			if len(valStr) > contentWidth {
				contentWidth = len(valStr)
			}
			if len(key) + 1 > contentWidth { // +1 for colon
				contentWidth = len(key) + 1
			}
		} else {
			processedArgs = append(processedArgs, argLine{key: key, value: valStr, multiline: false})
			if lineLen > contentWidth {
				contentWidth = lineLen
			}
		}
	}
	
	// Cap width to avoid breaking terminal (e.g. 120 chars)
	if contentWidth > 120 {
		contentWidth = 120
	}
	
	// Construct Box
	var sb strings.Builder
	
	// Header
	headerTitle := " Tool Execution "
	dashCount := (contentWidth + 2 - len(headerTitle)) / 2
	header := "\n╭" + strings.Repeat("─", dashCount) + headerTitle + strings.Repeat("─", contentWidth + 2 - dashCount - len(headerTitle)) + "╮\n"
	sb.WriteString(header)
	
	// Tool Name
	sb.WriteString(fmt.Sprintf("│ Tool: %-*s │\n", contentWidth - 6, toolName))
	
	// Spacer
	sb.WriteString("│ " + strings.Repeat(" ", contentWidth) + " │\n")
	
	// Arguments Header
	sb.WriteString(fmt.Sprintf("│ %-*s │\n", contentWidth, "** Arguments **"))
	
	// Arguments
	for _, arg := range processedArgs {
		if arg.multiline {
			// Key line
			sb.WriteString(fmt.Sprintf("│ %-*s │\n", contentWidth, arg.key + ":"))
			// Value line (potentially truncated if > 120)
			val := arg.value
			if len(val) > contentWidth {
				val = val[:contentWidth-3] + "..."
			}
			sb.WriteString(fmt.Sprintf("│ %-*s │\n", contentWidth, val))
		} else {
			line := fmt.Sprintf("%s: %s", arg.key, arg.value)
			if len(line) > contentWidth {
				line = line[:contentWidth-3] + "..."
			}
			sb.WriteString(fmt.Sprintf("│ %-*s │\n", contentWidth, line))
		}
	}
	
	// Footer
	sb.WriteString("╰" + strings.Repeat("─", contentWidth + 2) + "╯\n")
	// Removed explicit "Do you approve..." prompt as it's handled by interactive UI
	
	return sb.String()
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
	if condition == "true" {
		return true
	}
	if strings.Contains(condition, "==") {
		parts := strings.Split(condition, "==")
		if len(parts) == 2 {
			re := regexp.MustCompile(`\['([^']+)'\]`)
			match := re.FindStringSubmatch(parts[0])
			if len(match) == 2 {
				key := match[1]
				val, _ := state.Get(key)
				expected := strings.Trim(strings.TrimSpace(parts[1]), "'\"")
				return fmt.Sprintf("%v", val) == expected
			}
		}
	}
	return false
}

func (a *AstonishAgent) renderString(tmpl string, state session.State) string {
	re := regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)
	return re.ReplaceAllStringFunc(tmpl, func(match string) string {
		key := match[1 : len(match)-1]
		val, err := state.Get(key)
		if err != nil || val == nil {
			return match
		}
		return fmt.Sprintf("%v", val)
	})
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
