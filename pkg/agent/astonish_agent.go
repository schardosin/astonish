package agent

import (
	"fmt"
	"iter"
	"regexp"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
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
	
	// Convert args to map for display
	argsMap, ok := args.(map[string]any)
	if !ok {
		argsMap = map[string]any{"args": args}
	}
	
	// Emit the approval request
	approvalText := p.Agent.formatToolApprovalRequest(toolName, argsMap)
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
				"approval_args":     argsMap,
			},
		},
	}
	p.YieldFunc(approvalEvent, nil)
	
	// 3. Set the state to "Awaiting" so the main loop knows what to do
	p.State.Set("awaiting_approval", true)
	p.State.Set("approval_tool", toolName)
	p.State.Set("approval_args", argsMap)
	
	// 3. Set the state to "Awaiting" so the main loop knows what to do
	p.State.Set("awaiting_approval", true)
	p.State.Set("approval_tool", toolName)
	p.State.Set("approval_args", argsMap)
	
	// 4. RETURN A RESULT TO THE LLM (The "Soft Refusal")
	// This completes the tool execution successfully from ADK's perspective
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
			
			// Emit node transition
			if !a.emitNodeTransition(currentNodeName, state, yield) {
				return
			}
			
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
				
				// Emit node transition for the new node
				if !a.emitNodeTransition(currentNodeName, state, yield) {
					return
				}
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

			if node.Type == "input" {
				// Render prompt
				prompt := a.renderString(node.Prompt, state)
				
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
							"current_node": currentNodeName,
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
				
				// Emit node transition
				if !a.emitNodeTransition(currentNodeName, state, yield) {
					return
				}

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
	
	// Filter tools based on node configuration
	var nodeTools []tool.Tool
	if node.Tools {
		for _, t := range a.Tools {
			// Filter based on tools_selection if present
			if len(node.ToolsSelection) > 0 {
				selected := false
				for _, sel := range node.ToolsSelection {
					if sel == t.Name() {
						selected = true
						break
					}
				}
				if !selected {
					continue
				}
			}
			nodeTools = append(nodeTools, t)
		}
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
		
		// Forward events to caller
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
			break
		}
	}
	
	return true
}

// formatToolApprovalRequest formats a tool approval request
func (a *AstonishAgent) formatToolApprovalRequest(toolName string, args map[string]interface{}) string {
	var sb strings.Builder
	sb.WriteString("╭────────────── Tool Execution ──────────────╮\n")
	sb.WriteString(fmt.Sprintf("│ Tool: %-37s│\n", toolName))
	sb.WriteString("│                                            │\n")
	sb.WriteString("│ ** Arguments **                            │\n")
	
	for key, value := range args {
		line := fmt.Sprintf("%s: %v", key, value)
		if len(line) > 37 {
			line = line[:34] + "..."
		}
		sb.WriteString(fmt.Sprintf("│ %-42s │\n", line))
	}
	
	sb.WriteString("╰────────────────────────────────────────────╯\n")
	sb.WriteString("\nDo you approve this execution?\n")
	sb.WriteString("> Yes\n")
	sb.WriteString("  No")
	
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
