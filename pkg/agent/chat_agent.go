package agent

import (
	"context"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
	"gopkg.in/yaml.v3"
)

// ChatAgent implements a dynamic chat agent without flow definitions.
// It wraps ADK's llmagent in a persistent chat session where the LLM
// decides which tools to call and how to proceed.
//
// Two-phase execution:
//   - Phase A: Dynamic execution with tool-use loops, recording an execution trace
//   - Phase B: After complex tasks (2+ tool calls), offer to distill into a reusable YAML flow
type ChatAgent struct {
	LLM            model.LLM
	Tools          []tool.Tool
	Toolsets       []tool.Toolset
	SessionService session.Service
	SystemPrompt   *SystemPromptBuilder
	DebugMode      bool
	AutoApprove    bool
	MaxToolCalls   int // Max consecutive tool calls per turn (default: 25)

	// Flow distillation
	FlowSaveThreshold int            // Min tool calls to offer flow save (default: 1)
	FlowSaveDir       string         // Directory for saved flows (default: agents dir)
	FlowRegistry      *FlowRegistry  // Registry for saved flows
	FlowDistiller     *FlowDistiller // Distiller for trace-to-YAML conversion

	// Internal: reuse AstonishAgent for approval formatting
	approvalHelper *AstonishAgent

	// Internal: pending trace for flow distillation (stored here because
	// session state copies don't persist complex objects like *ExecutionTrace)
	pendingTrace *ExecutionTrace
}

// NewChatAgent creates a ChatAgent with all configured tools and toolsets.
func NewChatAgent(llm model.LLM, internalTools []tool.Tool, toolsets []tool.Toolset,
	sessionService session.Service, promptBuilder *SystemPromptBuilder,
	debugMode bool, autoApprove bool) *ChatAgent {

	maxToolCalls := 25
	flowSaveThreshold := 1

	return &ChatAgent{
		LLM:               llm,
		Tools:             internalTools,
		Toolsets:          toolsets,
		SessionService:    sessionService,
		SystemPrompt:      promptBuilder,
		DebugMode:         debugMode,
		AutoApprove:       autoApprove,
		MaxToolCalls:      maxToolCalls,
		FlowSaveThreshold: flowSaveThreshold,
		approvalHelper:    &AstonishAgent{LLM: llm, AutoApprove: autoApprove},
	}
}

// Run implements the agent.Run interface for ADK.
// It is called by the ADK runner for each user message.
func (c *ChatAgent) Run(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		state := ctx.Session().State()

		// Extract user text
		userText := ""
		if ctx.UserContent() != nil {
			for _, p := range ctx.UserContent().Parts {
				if p.Text != "" {
					userText += p.Text
				}
			}
		}

		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] User message: %s\n", userText)
		}

		// Check if we're handling a flow save response
		pendingVal, _ := state.Get("chat:pending_flow_save")
		if pending, ok := pendingVal.(bool); ok && pending {
			if strings.EqualFold(strings.TrimSpace(userText), "yes") {
				c.handleFlowSave(ctx, state, yield)
			}
			// Clear pending state via StateDelta and clear the trace
			c.pendingTrace = nil
			yield(&session.Event{
				Actions: session.EventActions{
					StateDelta: map[string]any{
						"chat:pending_flow_save": false,
					},
				},
			}, nil)
			if strings.EqualFold(strings.TrimSpace(userText), "yes") {
				return
			}
			// If they said something other than "yes", fall through to process as new message
		}

		// --- Phase A: Dynamic Execution ---
		trace := NewExecutionTrace(userText)

		// Build system prompt
		instruction := c.SystemPrompt.Build()

		// Create the AfterToolCallback for trace recording
		afterToolCallback := func(ctx tool.Context, t tool.Tool, input, output map[string]any, err error) (map[string]any, error) {
			trace.RecordStep(t.Name(), input, output, err)
			if c.DebugMode {
				status := "OK"
				if err != nil {
					status = fmt.Sprintf("ERROR: %v", err)
				}
				fmt.Printf("[Chat DEBUG] Tool call recorded: %s -> %s\n", t.Name(), status)
			}
			return output, err
		}

		// Create llmagent with all tools
		llmAgent, err := llmagent.New(llmagent.Config{
			Name:        "chat",
			Model:       c.LLM,
			Instruction: instruction,
			Tools:       c.Tools,
			Toolsets:    c.Toolsets,
			AfterToolCallbacks: []llmagent.AfterToolCallback{
				afterToolCallback,
			},
		})
		if err != nil {
			yield(nil, fmt.Errorf("failed to create chat llmagent: %w", err))
			return
		}

		// Run the llmagent
		toolCallCount := 0
		maxToolCalls := c.MaxToolCalls
		lastToolCallSeen := false

		for event, err := range llmAgent.Run(ctx) {
			if err != nil {
				yield(nil, err)
				return
			}

			// Count tool calls and capture text output
			if event.LLMResponse.Content != nil {
				for _, p := range event.LLMResponse.Content.Parts {
					if p.FunctionCall != nil {
						toolCallCount++
						lastToolCallSeen = true
						if toolCallCount >= maxToolCalls {
							yield(&session.Event{
								LLMResponse: model.LLMResponse{
									Content: &genai.Content{
										Parts: []*genai.Part{{Text: fmt.Sprintf("\n[Max tool calls reached (%d). Stopping.]", maxToolCalls)}},
										Role:  "model",
									},
								},
							}, nil)
							goto postLoop
						}
					}
					// Capture text that comes after tool calls (the final formatted output)
					if p.Text != "" && lastToolCallSeen {
						trace.AppendOutput(p.Text)
					}
				}
			}

			// Check for approval pause
			if event.Actions.StateDelta != nil {
				if awaitingVal, ok := event.Actions.StateDelta["awaiting_approval"]; ok {
					if awaiting, ok := awaitingVal.(bool); ok && awaiting {
						// Yield the approval event and return -- the runner will
						// call us again with the user's response
						yield(event, nil)
						return
					}
				}
			}

			// Yield event to the caller (console/web)
			if !yield(event, nil) {
				return
			}
		}

	postLoop:
		// Finalize the trace
		trace.Finalize()

		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] postLoop reached. Trace tool call count: %d, threshold: %d\n",
				trace.ToolCallCount(), c.FlowSaveThreshold)
			for i, step := range trace.Steps {
				fmt.Printf("[Chat DEBUG]   Step %d: %s (success: %v)\n", i+1, step.ToolName, step.Success)
			}
		}

		// --- Phase B: Flow Save Offer ---
		if trace.ToolCallCount() >= c.FlowSaveThreshold {
			// Store trace on the ChatAgent itself (state.Set doesn't persist across invocations)
			c.pendingTrace = trace

			// Emit the offer -- use StateDelta to persist the pending flag in session state
			yield(&session.Event{
				LLMResponse: model.LLMResponse{
					Content: &genai.Content{
						Parts: []*genai.Part{{Text: fmt.Sprintf(
							"\n\n---\nThis task used %d tool calls. Would you like to save it as a reusable flow? (yes/no)",
							trace.ToolCallCount())}},
						Role: "model",
					},
				},
				Actions: session.EventActions{
					StateDelta: map[string]any{
						"chat:pending_flow_save": true,
					},
				},
			}, nil)
		}
	}
}

// handleFlowSave performs flow distillation and saves the result.
func (c *ChatAgent) handleFlowSave(ctx agent.InvocationContext, state session.State, yield func(*session.Event, error) bool) {
	// Get the stored trace
	trace := c.pendingTrace
	if trace == nil {
		yield(&session.Event{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: "No execution trace found. Cannot save flow."}},
					Role:  "model",
				},
			},
		}, nil)
		return
	}

	// Check if distiller is available
	if c.FlowDistiller == nil {
		yield(&session.Event{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: "Flow distillation is not configured. The execution trace has been recorded."}},
					Role:  "model",
				},
			},
		}, nil)
		return
	}

	// Emit "distilling..." message
	yield(&session.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Parts: []*genai.Part{{Text: "Distilling execution into a reusable flow..."}},
				Role:  "model",
			},
		},
		Actions: session.EventActions{
			StateDelta: map[string]any{
				"_spinner_text": "Distilling flow...",
			},
		},
	}, nil)

	// Run distillation
	result, err := c.FlowDistiller.Distill(context.Background(), DistillRequest{
		UserRequest: trace.UserRequest,
		Trace:       trace,
	})
	if err != nil {
		yield(&session.Event{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: fmt.Sprintf("Flow distillation failed: %v", err)}},
					Role:  "model",
				},
			},
		}, nil)
		// If we got partial YAML despite errors, still try to save
		if result == nil {
			return
		}
	}

	// Determine save directory
	saveDir := c.FlowSaveDir
	if saveDir == "" {
		configDir, cfgErr := os.UserConfigDir()
		if cfgErr != nil {
			yield(&session.Event{
				LLMResponse: model.LLMResponse{
					Content: &genai.Content{
						Parts: []*genai.Part{{Text: fmt.Sprintf("Failed to determine config directory: %v", cfgErr)}},
						Role:  "model",
					},
				},
			}, nil)
			return
		}
		saveDir = filepath.Join(configDir, "astonish", "agents")
	}

	// Create the directory if it doesn't exist
	if mkErr := os.MkdirAll(saveDir, 0755); mkErr != nil {
		yield(&session.Event{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: fmt.Sprintf("Failed to create flow directory: %v", mkErr)}},
					Role:  "model",
				},
			},
		}, nil)
		return
	}

	// Save the YAML file
	filename := result.FlowName + ".yaml"
	flowPath := filepath.Join(saveDir, filename)

	// Avoid overwriting existing files
	if _, statErr := os.Stat(flowPath); statErr == nil {
		// File exists, add timestamp suffix
		filename = fmt.Sprintf("%s_%s.yaml", result.FlowName, time.Now().Format("20060102_150405"))
		flowPath = filepath.Join(saveDir, filename)
	}

	if writeErr := os.WriteFile(flowPath, []byte(result.YAML), 0644); writeErr != nil {
		yield(&session.Event{
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: fmt.Sprintf("Failed to write flow file: %v", writeErr)}},
					Role:  "model",
				},
			},
		}, nil)
		return
	}

	// Register in the flow registry
	if c.FlowRegistry != nil {
		entry := FlowRegistryEntry{
			FlowFile:    filename,
			Description: result.Description,
			Tags:        result.Tags,
			CreatedAt:   time.Now(),
		}
		if regErr := c.FlowRegistry.Register(entry); regErr != nil {
			if c.DebugMode {
				fmt.Printf("[Chat DEBUG] Failed to register flow: %v\n", regErr)
			}
		}
	}

	// Emit success message
	msg := fmt.Sprintf("Flow saved as `%s`\n", flowPath)
	msg += fmt.Sprintf("  Description: %s\n", result.Description)
	if len(result.Tags) > 0 {
		msg += fmt.Sprintf("  Tags: %s\n", strings.Join(result.Tags, ", "))
	}

	// Build run command with parameter suggestions from the execution trace
	runCmd := "astonish flows run " + result.FlowName
	paramFlags := c.extractInputParams(ctx, result.YAML, trace)
	for _, pf := range paramFlags {
		// Quote the value if it contains spaces
		parts := strings.SplitN(pf, "=", 2)
		if len(parts) == 2 && strings.ContainsAny(parts[1], " \t") {
			runCmd += fmt.Sprintf(` -p %s="%s"`, parts[0], parts[1])
		} else {
			runCmd += " -p " + pf
		}
	}
	runCmd += " --auto-approve"
	msg += "\nYou can run this flow with:\n  " + runCmd

	yield(&session.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Parts: []*genai.Part{{Text: msg}},
				Role:  "model",
			},
		},
	}, nil)
}

// flowYAML is a minimal struct for parsing the distilled YAML to extract input nodes.
type flowYAML struct {
	Nodes []flowNode `yaml:"nodes"`
}

type flowNode struct {
	Name        string            `yaml:"name"`
	Type        string            `yaml:"type"`
	Prompt      string            `yaml:"prompt,omitempty"`
	OutputModel map[string]string `yaml:"output_model,omitempty"`
}

// extractInputParams parses the distilled YAML to find input node names,
// then asks the LLM to fill in the actual values from the execution trace.
// Returns a slice of "nodeName=value" strings suitable for -p flags.
func (c *ChatAgent) extractInputParams(invCtx agent.InvocationContext, yamlStr string, trace *ExecutionTrace) []string {
	if trace == nil || yamlStr == "" || c.FlowDistiller == nil {
		return nil
	}

	// Parse YAML to find input node names and their prompts
	var flow flowYAML
	if err := yaml.Unmarshal([]byte(yamlStr), &flow); err != nil {
		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] Failed to parse YAML for param extraction: %v\n", err)
		}
		return nil
	}

	type inputNode struct {
		name        string
		prompt      string
		outputModel map[string]string
	}
	var inputs []inputNode
	for _, node := range flow.Nodes {
		if node.Type == "input" {
			inputs = append(inputs, inputNode{name: node.Name, prompt: node.Prompt, outputModel: node.OutputModel})
		}
	}
	if len(inputs) == 0 {
		return nil
	}

	// Build a prompt for the LLM to fill in the parameter values
	var sb strings.Builder
	sb.WriteString("Given this execution trace, determine what SHORT value the user would type for each input node.\n\n")

	sb.WriteString("# Execution Trace\n")
	sb.WriteString("User request: " + trace.UserRequest + "\n\n")
	for i, step := range trace.SuccessfulSteps() {
		sb.WriteString(fmt.Sprintf("Step %d: tool=%s\n", i+1, step.ToolName))
		for k, v := range step.ToolArgs {
			sb.WriteString(fmt.Sprintf("  arg %s = %v\n", k, v))
		}
	}

	sb.WriteString("\n# Input Parameters to Fill\n")
	for _, inp := range inputs {
		sb.WriteString(fmt.Sprintf("- %s (prompt: %q)", inp.name, inp.prompt))
		if len(inp.outputModel) > 0 {
			var fields []string
			for k := range inp.outputModel {
				fields = append(fields, k)
			}
			sb.WriteString(fmt.Sprintf(" [extracts fields: %s]", strings.Join(fields, ", ")))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n# Instructions\n")
	sb.WriteString("Each input node shows a prompt to the user and the user types a SHORT answer.\n")
	sb.WriteString("From the trace, determine the EXACT LITERAL value that was used.\n")
	sb.WriteString("The value must be what a user would type at the prompt - concise and minimal.\n\n")
	sb.WriteString("Examples of GOOD values: 192.168.1.200, root, /var/log/syslog, 8080, my-server\n")
	sb.WriteString("Examples of BAD values: the server IP is 192.168.1.200, ssh root user at ip 192.168.1.200\n\n")
	sb.WriteString("Respond with ONLY the parameter values, one per line, in this exact format:\n")
	sb.WriteString("parameter_name=value\n\n")
	sb.WriteString("Do not add quotes, explanations, descriptions, or extra text. Just the key=value lines.\n")

	// Call LLM
	response, err := c.FlowDistiller.LLM(context.Background(), sb.String())
	if err != nil {
		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] LLM param extraction failed: %v\n", err)
		}
		return nil
	}

	if c.DebugMode {
		fmt.Printf("[Chat DEBUG] LLM param extraction response:\n%s\n", response)
	}

	// Parse response: expect "name=value" lines
	validNames := make(map[string]bool, len(inputs))
	for _, inp := range inputs {
		validNames[inp.name] = true
	}

	resolved := make(map[string]string)
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if validNames[key] && val != "" {
			resolved[key] = val
		}
	}

	// Build result in input order
	var params []string
	for _, inp := range inputs {
		if val, ok := resolved[inp.name]; ok {
			params = append(params, inp.name+"="+val)
		} else {
			params = append(params, inp.name+"=<value>")
		}
	}

	return params
}
