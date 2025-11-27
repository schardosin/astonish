package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
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

// RunnableTool interface for executing tools.
type RunnableTool interface {
	tool.Tool
	Run(ctx tool.Context, args any) (map[string]any, error)
	Declaration() *genai.FunctionDeclaration
}

// ProtectedTool wraps a standard tool and adds an approval gate.
type ProtectedTool struct {
	tool.Tool                // Embed the underlying tool
	State     session.State  // Access to session state
	Agent     *AstonishAgent // Access to helper methods
	YieldFunc func(*session.Event, error) bool // For emitting events
}

// ProcessRequest secures the RAG phase - blocks execution without approval
func (p *ProtectedTool) ProcessRequest(ctx tool.Context, req *model.LLMRequest) error {
	toolName := p.Tool.Name()
	approvalKey := fmt.Sprintf("approval:%s", toolName)
	
	// Check if we already have approval
	if approved, _ := p.State.Get(approvalKey); approved == true {
		// We have approval - delegate to underlying tool
		if processor, ok := p.Tool.(interface {
			ProcessRequest(tool.Context, *model.LLMRequest) error
		}); ok {
			return processor.ProcessRequest(ctx, req)
		}
		return nil // Tool doesn't implement ProcessRequest, that's okay
	}
	
	// NO approval - block the RAG phase
	// We cannot ask the user here because the Agent loop hasn't started yet
	// So we just skip adding the tool's context data
	// NO approval - block the RAG phase
	// We cannot ask the user here because the Agent loop hasn't started yet
	// So we just skip adding the tool's context data
	return nil // Return nil so the agent continues, but WITHOUT the tool's data
}

// Run intercepts the execution to check for approval
func (p *ProtectedTool) Run(ctx tool.Context, args any) (map[string]any, error) {
	toolName := p.Tool.Name()
	
	// Debug log
	// Debug log
	
	// 1. Check if we already have approval in the state
	approvalKey := fmt.Sprintf("approval:%s", toolName)
	if approved, _ := p.State.Get(approvalKey); approved == true {
		// CONSUME the approval (one-time use)
		p.State.Set(approvalKey, false)
		
		// CONSUME the approval (one-time use)
		p.State.Set(approvalKey, false)
		
		// EXECUTE the real tool
		if rt, ok := p.Tool.(RunnableTool); ok {
			return rt.Run(ctx, args)
		}
		return nil, fmt.Errorf("underlying tool is not runnable")
	}
	
	// 2. We do NOT have approval. Trigger the flow.
	
	// 2. Format arguments for display
	var argsMap map[string]any
	if m, ok := args.(map[string]any); ok {
		argsMap = m
	} else {
		// If args is not a map (e.g. struct or primitive), wrap it
		argsMap = map[string]any{"args": args}
	}
	
	// Prompt for approval
	prompt := p.Agent.formatToolApprovalRequest(toolName, argsMap)
	
	// Yield event with approval prompt and state update
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
				"pending_tool_name": toolName,
				"pending_tool_args": args,
				"approval_options":  []string{"Yes", "No"}, // Trigger interactive selection
			},
		},
	}, nil)
	
	// Wait for user input (handled by runner loop)
	// But since we are in the agent logic, we need to know the result.
	// The runner loop will pause, get input, and resume.
	// When resuming, the input will be in ctx.UserContent()
	
	// Check if we have user content (meaning we resumed)
	if ctx.UserContent() != nil && len(ctx.UserContent().Parts) > 0 {
		// We have input! Check if it's approval
		input := ""
		for _, part := range ctx.UserContent().Parts {
			input += part.Text
		}
		input = strings.TrimSpace(strings.ToLower(input))
		
		approved := input == "yes" || input == "y"
		
		// Clear approval state
		p.State.Set("awaiting_approval", false)
		p.State.Set("pending_tool_name", nil)
		p.State.Set("pending_tool_args", nil)
		
		if approved {
			// If approved, set the approval key for the tool and re-run
			p.State.Set(approvalKey, true)
			// The agent loop will re-evaluate and call Run again, which will then execute the tool.
			// For now, we return a "soft refusal" to the LLM to indicate we're waiting.
			return map[string]any{
				"status": "APPROVAL_GRANTED_RESUMING",
				"info":   "Approval granted. The tool will be executed on the next turn.",
			}, nil
		} else {
			// If not approved, return a "soft refusal" indicating denial
			return map[string]any{
				"status": "APPROVAL_DENIED",
				"info":   "Tool execution denied by user.",
			}, nil
		}
	}
	
	// If we reached here, it means we just yielded the approval prompt and are waiting for user input.
	// We return a "soft refusal" to the LLM to indicate that execution is paused.
	return map[string]any{
		"status": "APPROVAL_REQUIRED",
		"info":   "Execution blocked. The user has been asked for approval. Please wait for their response.",
	}, nil
}

// AstonishAgent implements the logic for running Astonish agents.
type AstonishAgent struct {
	Config *config.AgentConfig
	LLM    model.LLM
	Tools  []tool.Tool
}

// NewAstonishAgent creates a new AstonishAgent.
func NewAstonishAgent(cfg *config.AgentConfig, llm model.LLM, tools []tool.Tool) *AstonishAgent {
	return &AstonishAgent{
		Config: cfg,
		LLM:    llm,
		Tools:  tools,
	}
}

// Run executes the agent flow with stateful workflow management.
func (a *AstonishAgent) Run(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
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
		
		// [THE FIX] Inject a "System Event" to nudge the LLM
		// Replace the user's simple "Yes" with a specific instruction for the model
		retryPrompt := fmt.Sprintf(
			"User approved execution of tool '%s'. Proceed immediately with the tool execution.",
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
		}
	} else {

	}
	
	// Inject tool use instruction if tools are enabled
	if len(nodeTools) > 0 {
		instruction += "\n\nYou have access to tools. You MUST use the provided tools to fulfill the request. Do not just describe what you are going to do."
	}
	
	// Wrap tools dynamically if approval is required
	var safeTools []tool.Tool
	for _, t := range nodeTools {
		if !node.ToolsAutoApproval {
			// Log that we are wrapping
			// Wrap it with ProtectedTool
			safeTools = append(safeTools, &ProtectedTool{
				Tool:      t,
				State:     state,
				Agent:     a,
				YieldFunc: yield,
			})
		} else {
			safeTools = append(safeTools, t)
		}
	}
	
	// Debug: Log tool configuration
	// Debug: Log tool configuration



	// [NEW] Build list of known tool names for detection
	var knownToolNames []string
	for _, t := range nodeTools {
		knownToolNames = append(knownToolNames, t.Name())
	}
	
	// Create ADK llmagent for this node with wrapped tools
	llmAgent, err := llmagent.New(llmagent.Config{
		Name:        nodeName,
		Model:       a.LLM,
		Instruction: instruction,
		Tools:       safeTools, // Use wrapped tools
		// No callbacks needed!
	})
	if err != nil {
		yield(nil, fmt.Errorf("failed to create llmagent: %w", err))
		return false
	}
	
	// Run the agent - ADK handles everything (native or prompt-based function calling)
	var fullResponse strings.Builder
	for event, err := range llmAgent.Run(ctx) {
		if err != nil {
			// Real error - forward it
			yield(nil, err)
			return false
		}
		
		// 1. Accumulate the response
		if event.Content != nil {
			for _, part := range event.Content.Parts {
				if part.Text != "" {
					fullResponse.WriteString(part.Text)
				}
			}
		}
		
		// [FIX] Get the TOTAL text accumulated so far
		currentTotalText := fullResponse.String()
		
		// [FIX] Check currentTotalText, NOT eventText (fixes token fragmentation)
		hasXMLTag := strings.Contains(currentTotalText, "<tool_use>") || 
		             strings.Contains(currentTotalText, "<tool_name>") ||
		             strings.Contains(currentTotalText, "<function_calls>") ||
		             strings.Contains(currentTotalText, "<invoke>")
		
		// Check for known tool names as tags
		if !hasXMLTag {
			for _, tName := range knownToolNames {
				if strings.Contains(currentTotalText, "<"+tName+">") {
					hasXMLTag = true
					break
				}
			}
		}

		if hasXMLTag {
			
			// [FIX] Extract from currentTotalText
			toolName := extractToolNameFromXML(currentTotalText, knownToolNames)
			
			// Safety check: verify we haven't ALREADY paused for this exact tool execution
			isAlreadyWaiting, _ := state.Get("awaiting_approval")
			
			if toolName != "" && !node.ToolsAutoApproval && isAlreadyWaiting != true {
				
				// [FIX] Wait for the tool call to be complete (closing tag) before interrupting
				// This ensures we capture parameters
				isComplete := false
				
				// Check for standard closing tags
				if strings.Contains(currentTotalText, "</tool_use>") || 
				   strings.Contains(currentTotalText, "</function_calls>") ||
				   strings.Contains(currentTotalText, "</invoke>") ||
				   strings.Contains(currentTotalText, "/>") {
					isComplete = true
				}
				
				// Check for specific tool closing tag
				if strings.Contains(currentTotalText, "</"+toolName+">") {
					isComplete = true
				}
				
				// If not complete, continue accumulating
				if !isComplete {
					// Yield debug info but continue
					// debugEvent := &session.Event{
					// 	LLMResponse: model.LLMResponse{
					// 		Content: &genai.Content{
					// 			Parts: []*genai.Part{{
					// 				Text: fmt.Sprintf("[DEBUG] Tool '%s' detected but incomplete. Waiting for closing tag...", toolName),
					// 			}},
					// 			Role: "model",
					// 		},
					// 	},
					// }
					// yield(debugEvent, nil)
					
					// Forward the current event and continue
					if !yield(event, nil) {
						return false
					}
					continue
				}

				
				// [FIX] Yield the current event (containing the closing tag) BEFORE pausing
				// This ensures the console sees the </tool_use> and resets its filter state
				if !yield(event, nil) {
					return false
				}
				
				// Extract parameters
				params := extractParametersFromXML(currentTotalText)
				
				// Set state FIRST
				if err := state.Set("awaiting_approval", true); err != nil {
					yield(nil, fmt.Errorf("failed to set awaiting_approval: %w", err))
					return false
				}
				state.Set("approval_tool", toolName)
				state.Set("approval_args", params)
				
				// Emit approval request WITH StateDelta
				approvalText := a.formatToolApprovalRequest(toolName, params)
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
							"approval_tool":     toolName,
							"approval_args":     params,
						},
					},
				}
				yield(approvalEvent, nil)
				
				// STOP THE LOOP IMMEDIATELY
				return false
			}
		}
		
		// Check for ToolResponse in LLMResponse and handle raw_tool_output
		if event.LLMResponse.Content != nil && len(node.RawToolOutput) == 1 {
			for _, part := range event.LLMResponse.Content.Parts {
				if part.FunctionResponse != nil {
					// This is a tool response
					// Get the state key to update (we only support 1 key mapping for now, like Python)
					var stateKey string
					for k := range node.RawToolOutput {
						stateKey = k
						break
					}

					// Update state with the response map
					// We store the whole response map, similar to Python storing the dict
					response := part.FunctionResponse.Response
					
					// If the response has "stdout" and the map has only that, maybe we should store just the string?
					// But Python stores the whole dict. Let's store the whole dict (map[string]any).
					// However, state values are usually expected to be strings or lists for simple usage.
					// If we store a map, can it be used in prompts?
					// Yes, format_prompt handles it.
					
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

		// Forward event
		if !yield(event, nil) {
			return false
		}
		
		// Check for ProtectedTool Wrapper flag (Standard ADK calls)
		if awaiting, _ := state.Get("awaiting_approval"); awaiting == true {
			// The tool wrapper has paused execution for user approval
			// Return FALSE to signal the main Run loop to PAUSE and wait for user input
			return false
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
