package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// ReActPlanner implements a manual ReAct (Reasoning + Acting) loop
// for models that do not support native tool calling.
// ApprovalCallback is called when a tool needs approval
type ApprovalCallback func(toolName string, args map[string]any) (bool, error)

type ReActPlanner struct {
	LLM              model.LLM
	Tools            []tool.Tool
	ApprovalCallback ApprovalCallback
	State            session.State
	DebugMode        bool
}

// NewReActPlanner creates a new ReActPlanner.
func NewReActPlanner(llm model.LLM, tools []tool.Tool) *ReActPlanner {
	return &ReActPlanner{
		LLM:   llm,
		Tools: tools,
	}
}

func NewReActPlannerWithApproval(llm model.LLM, tools []tool.Tool, approvalCallback ApprovalCallback, state session.State, debugMode bool) *ReActPlanner {
	return &ReActPlanner{
		LLM:              llm,
		Tools:            tools,
		ApprovalCallback: approvalCallback,
		State:            state,
		DebugMode:        debugMode,
	}
}

// Run executes the ReAct loop to answer the input question.
// systemInstruction is optional additional context from the agent configuration.
func (p *ReActPlanner) Run(ctx context.Context, input string, systemInstruction string) (string, error) {
	// Check if we're resuming from a paused state
	var history string
	var startStep int
	
	if p.State != nil {
		if savedHistory, err := p.State.Get("_react_history"); err == nil {
			if h, ok := savedHistory.(string); ok {
				history = h
				if p.DebugMode {
					fmt.Println("[ReAct DEBUG] Resuming from saved state")
				}
			}
		}
		if savedStep, err := p.State.Get("_react_step"); err == nil {
			if s, ok := savedStep.(int); ok {
				startStep = s
			}
		}
	}
	
	// If not resuming, construct initial system prompt
	if history == "" {
		// 1. Construct System Prompt
		toolDescriptions := p.getToolDescriptions()
		toolNames := p.getToolNames()
		
		// Incorporate the agent's system instruction if provided
		var systemContext string
		if systemInstruction != "" {
			systemContext = fmt.Sprintf("%s\n\n", systemInstruction)
		}
		
		systemPrompt := fmt.Sprintf(`%sAnswer the following questions as best you can. You have access to the following tools:

%s

IMPORTANT: You must ONLY use the tools listed above. Do not invent new tools or actions.

Use the following format:

Question: the input question you must answer
Thought: you should always think about what to do
Action: the action to take, should be one of [%s]
Action Input: the input to the action

STOP HERE. Do NOT write "Observation:" - the system will execute the tool and provide the observation.

After receiving the observation, continue with:
Thought: [your analysis of the observation]
... (this Thought/Action/Action Input/Observation cycle can repeat N times)

When you have enough information:
Thought: I now know the final answer
Final Answer: the final answer to the original input question

Begin!

Question: %s
Thought:`, systemContext, toolDescriptions, toolNames, input)

		history = systemPrompt
	}
	
	maxSteps := 10

	// Helper for float32 pointer
	temp := float32(0.0)
	
	for i := startStep; i < maxSteps; i++ {
		var action, actionInput string
		var skipLLM bool

		// CHECK: Do we have a pending action from a resume?
		if p.State != nil {
			if pendingAction, _ := p.State.Get("_react_pending_action"); pendingAction != nil {
				if act, ok := pendingAction.(string); ok {
					action = act
					if pendingInput, _ := p.State.Get("_react_pending_input"); pendingInput != nil {
						if inp, ok := pendingInput.(string); ok {
							actionInput = inp
						}
					}
					
					// Clear the pending state so we don't loop forever
					p.State.Set("_react_pending_action", nil)
					p.State.Set("_react_pending_input", nil)
					
					skipLLM = true
					if p.DebugMode {
						fmt.Printf("[ReAct] Resuming execution of pending tool: %s\n", action)
					}
				}
			}
		}

		if !skipLLM {
			// 2. Call LLM
		req := &model.LLMRequest{
			Contents: []*genai.Content{
				{
					Role: "user",
					Parts: []*genai.Part{
						{Text: history},
					},
				},
			},
			Config: &genai.GenerateContentConfig{
				Temperature:     &temp, // Deterministic for planning
				StopSequences:   []string{"Observation:"}, // Stop at observation to let us execute tool
			},
		}

		var responseText string
		// We use non-streaming for simplicity in the loop, or we could stream and buffer.
		// Let's consume the stream to get the full text.
		for resp, err := range p.LLM.GenerateContent(ctx, req, false) {
			if err != nil {
				return "", fmt.Errorf("LLM generation failed: %w", err)
			}
			if resp.Content != nil {
				for _, part := range resp.Content.Parts {
					responseText += part.Text
				}
			}
		}
		
		// CRITICAL: Truncate everything after "STOP HERE" to prevent the model from hallucinating
		// the observation and final answer. The model should ONLY generate up to the Action Input,
		// then we execute the tool and provide the real observation.
		if stopIdx := strings.Index(responseText, "STOP HERE"); stopIdx != -1 {
			// Keep everything up to and including "STOP HERE"
			responseText = responseText[:stopIdx+9] // 9 = len("STOP HERE")
			if p.DebugMode {
				fmt.Println("[ReAct DEBUG] Truncated output after STOP HERE")
			}
		}
		
		if p.DebugMode {
			fmt.Printf("[ReAct] Step %d LLM Output: %s\n", i+1, responseText)
		}
		
		// Update history with the LLM's response
		history += responseText

		// 3. Check for Final Answer
		if strings.Contains(responseText, "Final Answer:") {
			// Extract final answer
			parts := strings.Split(responseText, "Final Answer:")
			if len(parts) >= 2 {
				answer := strings.TrimSpace(parts[1])
				// Clear saved state
				if p.State != nil {
					p.State.Set("_react_history", nil)
					p.State.Set("_react_step", nil)
				}
				return answer, nil
			}
		}
		
		// Parse Action
		// Allow hyphens, dots, or other safe chars in tool names (non-whitespace)
		actionRegex := regexp.MustCompile(`Action:\s*([^\s]+)`)
		actionMatch := actionRegex.FindStringSubmatch(responseText)
		if len(actionMatch) < 2 {
			// Heuristic: If it wrote "Action Input" but missed "Action", or if the text is very long, it failed.
			if strings.Contains(responseText, "Action Input:") {
				history += "\n\nObservation: Error: Invalid Format. You provided an Input but no Action. Please use the 'Action: <ToolName>' format.\n\nThought: "
				continue
			}
			// No action found - might be just thinking, continue
			// The responseText is already added to history, so just continue.
			continue
		}
		action = actionMatch[1]
		
		// Parse Action Input - it might be multiline or contain code blocks
		// Look for "Action Input:" and capture everything until "STOP HERE", "Observation:", or end
		actionInputRegex := regexp.MustCompile(`(?s)Action Input:\s*(.*?)(?:\n\nSTOP HERE|\n\nObservation:|$)`)
		inputMatch := actionInputRegex.FindStringSubmatch(responseText)
		if len(inputMatch) >= 2 {
			actionInput = strings.TrimSpace(inputMatch[1])
			
			// Strip markdown code blocks if present
			// Handle ```python\ncode\n``` or ```\ncode\n```
			if strings.HasPrefix(actionInput, "```") {
				// Find the first newline after ```
				lines := strings.Split(actionInput, "\n")
				if len(lines) > 1 {
					// Skip first line (```python or ```)
					codeLines := lines[1:]
					// Remove last line if it's ```
					if len(codeLines) > 0 && strings.TrimSpace(codeLines[len(codeLines)-1]) == "```" {
						codeLines = codeLines[:len(codeLines)-1]
					}
					actionInput = strings.Join(codeLines, "\n")
				}
			}
			actionInput = strings.TrimSpace(actionInput)
		}
		} // End of !skipLLM block

		// Execute tool
		if p.DebugMode {
			fmt.Printf("[ReAct] Executing Tool: %s with Input: %s\n", action, actionInput)
		}
		observation, err := p.executeTool(ctx, action, actionInput)
		if err != nil {
			// Check if this is an approval required error
			if err.Error() == "tool approval required" {
				// Save current state before pausing
				if p.State != nil {
					p.State.Set("_react_history", history)
					p.State.Set("_react_step", i)
					
					// NEW: Save the pending action details
					p.State.Set("_react_pending_action", action)
					p.State.Set("_react_pending_input", actionInput)

					if p.DebugMode {
						fmt.Printf("[ReAct DEBUG] Saving state at step %d\n", i)
					}
				}
				// Return a special result indicating approval is needed
				// The caller will handle pausing and requesting approval
				return "", fmt.Errorf("APPROVAL_REQUIRED:%s:%s", action, actionInput)
			}
			// Other errors - treat as observation
			observation = fmt.Sprintf("Error: %v", err)
		}
		
		if p.DebugMode {
			fmt.Printf("[ReAct] Observation: %s\n", observation)
		}
		
		// Append observation to history
		history += fmt.Sprintf("\n\nObservation: %s\n\nThought: ", observation)
	}

	return "", fmt.Errorf("max ReAct steps (%d) reached without final answer", maxSteps)
}

// FormatOutput takes the ReAct result and formats it according to the output schema.
// This is called after the ReAct loop completes to ensure the output matches the expected structure.
func (p *ReActPlanner) FormatOutput(ctx context.Context, reactResult string, outputSchema map[string]string, systemInstruction string) (string, error) {
	if len(outputSchema) == 0 {
		// No schema, return as-is
		return reactResult, nil
	}
	
	// Build schema description
	var schemaDesc strings.Builder
	schemaDesc.WriteString("{\n")
	for key, typeName := range outputSchema {
		schemaDesc.WriteString(fmt.Sprintf("  \"%s\": <%s>,\n", key, typeName))
	}
	schemaDesc.WriteString("}")
	
	// Create formatting prompt
	var systemContext string
	if systemInstruction != "" {
		systemContext = fmt.Sprintf("Context: %s\n\n", systemInstruction)
	}
	
	formatPrompt := fmt.Sprintf(`%sYou are a data formatter. Take the following result and format it as a JSON object matching this schema:

%s

Result to format:
%s

Return ONLY the JSON object, no other text or markdown.`, systemContext, schemaDesc.String(), reactResult)

	// Call LLM to format
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Role: "user",
				Parts: []*genai.Part{
					{Text: formatPrompt},
				},
			},
		},
		Config: &genai.GenerateContentConfig{
			Temperature:      func() *float32 { t := float32(0.0); return &t }(),
			ResponseMIMEType: "application/json",
		},
	}

	var responseText string
	for resp, err := range p.LLM.GenerateContent(ctx, req, false) {
		if err != nil {
			return "", fmt.Errorf("formatting LLM call failed: %w", err)
		}
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				responseText += part.Text
			}
		}
	}
	
	// Clean up markdown if present
	responseText = strings.TrimSpace(responseText)
	responseText = strings.TrimPrefix(responseText, "```json")
	responseText = strings.TrimPrefix(responseText, "```")
	responseText = strings.TrimSuffix(responseText, "```")
	responseText = strings.TrimSpace(responseText)
	
	return responseText, nil
}

func (p *ReActPlanner) getToolDescriptions() string {
	var sb strings.Builder
	for _, t := range p.Tools {
		sb.WriteString(fmt.Sprintf("%s: %s\n", t.Name(), t.Description()))
	}
	return sb.String()
}

func (p *ReActPlanner) getToolNames() string {
	var names []string
	for _, t := range p.Tools {
		names = append(names, t.Name())
	}
	return strings.Join(names, ", ")
}

func (p *ReActPlanner) executeTool(ctx context.Context, name string, inputJSON string) (string, error) {
	// Find the tool
	var selectedTool tool.Tool
	for _, t := range p.Tools {
		if t.Name() == name {
			selectedTool = t
			break
		}
	}
	
	if selectedTool == nil {
		return fmt.Sprintf("Error: Tool '%s' not found. Available tools: %s", name, p.getToolNames()), nil
	}
	
	// Parse input
	var args map[string]any
	// Try parsing as JSON first
	if err := json.Unmarshal([]byte(inputJSON), &args); err != nil {
		// If not JSON, wrap it appropriately based on the tool
		// Check the tool's schema to determine the correct field name
		// For now, use common patterns
		if name == "execute_python" {
			args = map[string]any{"code": inputJSON}
		} else if name == "run_python_code" {
			args = map[string]any{"python_code": inputJSON}
		} else if name == "shell_command" {
			args = map[string]any{"command": inputJSON}
		} else {
			args = map[string]any{"input": inputJSON}
		}
	}
	
	// Check for approval if callback is set
	if p.ApprovalCallback != nil {
		approved, err := p.ApprovalCallback(name, args)
		if err != nil {
			return fmt.Sprintf("Error checking approval: %v", err), nil
		}
		if !approved {
			// Tool execution was paused for approval
			// Return a special message that will be handled by the caller
			return "APPROVAL_REQUIRED", fmt.Errorf("tool approval required")
		}
	}
	
	// Execute the tool using ADK's Run method
	// tool.Tool is an interface, but concrete implementations have a Run method
	// We need to use type assertion or reflection to call it
	// Create a minimal tool context
	toolCtx := &minimalToolContext{
		Context: ctx,
		state:   p.State,
	}
	
	// Try to call Run using reflection or type assertion
	// Most ADK tools implement a Run(tool.Context, any) (map[string]any, error) method
	type runnableTool interface {
		Run(tool.Context, any) (map[string]any, error)
	}
	
	runnable, ok := selectedTool.(runnableTool)
	if !ok {
		return fmt.Sprintf("Error: Tool '%s' does not implement Run method", name), nil
	}
	
	result, err := runnable.Run(toolCtx, args)
	if err != nil {
		return fmt.Sprintf("Error executing tool: %v", err), nil
	}
	
	// Marshal result to string
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("%v", result), nil
	}
	return string(resultBytes), nil
}

// minimalToolContext implements tool.Context for ReAct tool execution
type minimalToolContext struct {
	context.Context // Embed to get Deadline, Done, Err, Value methods
	state           session.State
}

func (m *minimalToolContext) Actions() *session.EventActions {
	return &session.EventActions{}
}

func (m *minimalToolContext) Branch() string {
	return ""
}

func (m *minimalToolContext) AgentName() string {
	return "ReActPlanner"
}

func (m *minimalToolContext) AppName() string {
	return "astonish"
}

func (m *minimalToolContext) Artifacts() agent.Artifacts {
	return nil
}

func (m *minimalToolContext) FunctionCallID() string {
	return ""
}

func (m *minimalToolContext) InvocationID() string {
	return ""
}

func (m *minimalToolContext) SessionID() string {
	return ""
}

func (m *minimalToolContext) UserID() string {
	return ""
}

func (m *minimalToolContext) UserContent() *genai.Content {
	return nil
}

func (m *minimalToolContext) ReadonlyState() session.ReadonlyState {
	return nil
}

func (m *minimalToolContext) SearchMemory(ctx context.Context, query string) (*memory.SearchResponse, error) {
	return nil, nil
}

func (m *minimalToolContext) State() session.State {
	return m.state
}
