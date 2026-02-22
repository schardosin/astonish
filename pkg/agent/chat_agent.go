package agent

import (
	"context"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/memory"
	persistentsession "github.com/schardosin/astonish/pkg/session"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
	"gopkg.in/yaml.v3"
)

// KnowledgeSearchResult holds a single result from the knowledge vector search.
type KnowledgeSearchResult struct {
	Path    string
	Score   float64
	Snippet string
}

// KnowledgeSearchFunc performs a vector search and returns matching results.
// Used to auto-retrieve relevant knowledge before LLM execution.
type KnowledgeSearchFunc func(ctx context.Context, query string, maxResults int, minScore float64) ([]KnowledgeSearchResult, error)

// ChatAgent implements a dynamic chat agent without flow definitions.
// It wraps ADK's llmagent in a persistent chat session where the LLM
// decides which tools to call and how to proceed.
//
// Execution records a trace. After reusable tasks, auto-distillation
// generates a flow YAML + knowledge doc. /distill remains as manual fallback.
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
	FlowSaveDir   string         // Directory for saved flows (default: agents dir)
	FlowRegistry  *FlowRegistry  // Registry for saved flows
	FlowDistiller *FlowDistiller // Distiller for trace-to-YAML conversion

	// Memory and flow reuse
	MemoryManager      *memory.Manager     // Persistent memory manager
	FlowContextBuilder *FlowContextBuilder // Converts flow YAML to execution plan
	FlowSearcher       FlowMemorySearcher  // Vector-based flow matching (nil = LLM-only)
	KnowledgeSearch    KnowledgeSearchFunc // Auto-retrieve relevant knowledge per turn (nil = disabled)

	// Self-management callbacks
	SelfMDRefresher  func() // Called after config changes to regenerate SELF.md
	FlowKnowledgeDir string // Path to memory/flows/ for knowledge docs

	// Context compaction
	Compactor *persistentsession.Compactor // Manages context window compaction (nil = disabled)

	// Internal: reuse AstonishAgent for approval formatting
	approvalHelper *AstonishAgent

	// Internal: execution traces for on-demand /distill
	lastTrace    *ExecutionTrace   // most recent turn's trace
	traceHistory []*ExecutionTrace // all traces in this session

	// Internal: cached result from PreviewDistill for ConfirmAndDistill
	pendingDistill *distillPreview
}

// distillPreview holds the result of PreviewDistill for use by ConfirmAndDistill.
type distillPreview struct {
	Description string            // LLM-generated task description
	Traces      []*ExecutionTrace // selected traces to distill
}

// NewChatAgent creates a ChatAgent with all configured tools and toolsets.
func NewChatAgent(llm model.LLM, internalTools []tool.Tool, toolsets []tool.Toolset,
	sessionService session.Service, promptBuilder *SystemPromptBuilder,
	debugMode bool, autoApprove bool) *ChatAgent {

	maxToolCalls := 25

	return &ChatAgent{
		LLM:            llm,
		Tools:          internalTools,
		Toolsets:       toolsets,
		SessionService: sessionService,
		SystemPrompt:   promptBuilder,
		DebugMode:      debugMode,
		AutoApprove:    autoApprove,
		MaxToolCalls:   maxToolCalls,
		approvalHelper: &AstonishAgent{LLM: llm, AutoApprove: autoApprove},
	}
}

// Run implements the agent.Run interface for ADK.
// It is called by the ADK runner for each user message.
func (c *ChatAgent) Run(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
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

		// --- Phase A: Dynamic Execution ---
		trace := NewExecutionTrace(userText)

		// Load memory and inject into system prompt
		c.SystemPrompt.MemoryContent = ""
		c.SystemPrompt.ExecutionPlan = ""
		c.SystemPrompt.RelevantKnowledge = ""

		var matchedFlowName string

		if c.MemoryManager != nil {
			memContent, memErr := c.MemoryManager.Load()
			if memErr != nil {
				if c.DebugMode {
					fmt.Printf("[Chat DEBUG] Failed to load memory: %v\n", memErr)
				}
			} else if memContent != "" {
				c.SystemPrompt.MemoryContent = memContent
				if c.DebugMode {
					fmt.Printf("[Chat DEBUG] Loaded memory (%d bytes)\n", len(memContent))
				}
			}

			// Check for matching saved flow
			if c.FlowRegistry != nil && c.FlowContextBuilder != nil {
				flowFile, plan := c.matchAndBuildPlan(userText, memContent)
				if plan != "" {
					matchedFlowName = flowFile
					c.SystemPrompt.ExecutionPlan = plan
					// Emit conversational info about the flow match
					flowDisplayName := strings.TrimSuffix(flowFile, ".yaml")
					flowDisplayName = strings.ReplaceAll(flowDisplayName, "_", " ")
					yield(&session.Event{
						LLMResponse: model.LLMResponse{
							Content: &genai.Content{
								Parts: []*genai.Part{{Text: fmt.Sprintf("I found a saved workflow for this (%s), let me use it.\n", flowDisplayName)}},
								Role:  "model",
							},
						},
					}, nil)
				}
			}
		}

		// Auto-retrieve relevant knowledge from vector store
		if c.KnowledgeSearch != nil && userText != "" {
			searchQuery := buildKnowledgeQuery(userText, matchedFlowName)
			if len(searchQuery) < 5 {
				if c.DebugMode {
					fmt.Printf("[Chat DEBUG] Auto knowledge search: skipped (processed query too short: %q)\n", searchQuery)
				}
			} else {
				results, searchErr := c.KnowledgeSearch(context.Background(), searchQuery, 5, 0.3)
				if searchErr != nil {
					if c.DebugMode {
						fmt.Printf("[Chat DEBUG] Auto knowledge search failed: %v\n", searchErr)
					}
				} else if len(results) > 0 {
					var kb strings.Builder
					for _, r := range results {
						kb.WriteString(fmt.Sprintf("**%s** (relevance: %.0f%%)\n", r.Path, r.Score*100))
						kb.WriteString(r.Snippet)
						kb.WriteString("\n\n")
					}
					c.SystemPrompt.RelevantKnowledge = escapeCurlyPlaceholders(kb.String())
					if c.DebugMode {
						fmt.Printf("[Chat DEBUG] Auto knowledge search: %d results injected for query: %s\n", len(results), truncateQuery(searchQuery, 60))
					}
				} else if c.DebugMode {
					fmt.Printf("[Chat DEBUG] Auto knowledge search: no results for query: %s\n", truncateQuery(searchQuery, 60))
				}
			}
		} else if c.KnowledgeSearch == nil && c.DebugMode {
			fmt.Println("[Chat DEBUG] Auto knowledge search: disabled (KnowledgeSearch not wired)")
		}

		// Build system prompt (includes memory and execution plan if set)
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

		// Build BeforeModelCallbacks
		var beforeModelCallbacks []llmagent.BeforeModelCallback
		if c.Compactor != nil {
			beforeModelCallbacks = append(beforeModelCallbacks, c.Compactor.BeforeModelCallback())
		}

		// Create llmagent with all tools
		llmAgent, err := llmagent.New(llmagent.Config{
			Name:                 "chat",
			Model:                c.LLM,
			Instruction:          instruction,
			Tools:                c.Tools,
			Toolsets:             c.Toolsets,
			BeforeModelCallbacks: beforeModelCallbacks,
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
				// If we were using an execution plan and it failed, inform the user
				// conversationally. The orphan cleanup in the provider layer ensures
				// the next turn's history is valid.
				if c.SystemPrompt.ExecutionPlan != "" {
					c.SystemPrompt.ExecutionPlan = ""
					if c.DebugMode {
						fmt.Printf("[Chat DEBUG] Flow execution failed: %v, cleared execution plan\n", err)
					}
					yield(&session.Event{
						LLMResponse: model.LLMResponse{
							Content: &genai.Content{
								Parts: []*genai.Part{{Text: "The saved workflow ran into an issue, so I'll try a different approach. Could you repeat your request?"}},
								Role:  "model",
							},
						},
					}, nil)
					return
				}
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
			fmt.Printf("[Chat DEBUG] postLoop reached. Trace tool call count: %d\n",
				trace.ToolCallCount())
			for i, step := range trace.Steps {
				fmt.Printf("[Chat DEBUG]   Step %d: %s (success: %v)\n", i+1, step.ToolName, step.Success)
			}
		}

		// Store the trace for on-demand /distill
		c.lastTrace = trace
		c.traceHistory = append(c.traceHistory, trace)
	}
}

// PreviewDistill analyzes the conversation trace history and identifies the
// primary task to distill. Returns a description for user confirmation.
// The result is cached internally for use by ConfirmAndDistill.
func (c *ChatAgent) PreviewDistill(ctx context.Context) (string, error) {
	// Filter traces that have tool calls (conversational turns are not distillable)
	var substantive []*ExecutionTrace
	for _, t := range c.traceHistory {
		if t.ToolCallCount() > 0 {
			substantive = append(substantive, t)
		}
	}

	if len(substantive) == 0 {
		return "", fmt.Errorf("no tasks with tool calls found in this session — nothing to distill")
	}

	// If only one substantive trace, skip the LLM assessment
	if len(substantive) == 1 {
		desc := fmt.Sprintf("%s (%d tool calls)", substantive[0].UserRequest, substantive[0].ToolCallCount())
		c.pendingDistill = &distillPreview{
			Description: desc,
			Traces:      substantive,
		}
		return desc, nil
	}

	// Multiple traces — ask the LLM to identify the primary task
	if c.FlowDistiller == nil {
		// No LLM available for assessment, fall back to most recent substantive trace
		last := substantive[len(substantive)-1]
		desc := fmt.Sprintf("%s (%d tool calls)", last.UserRequest, last.ToolCallCount())
		c.pendingDistill = &distillPreview{
			Description: desc,
			Traces:      []*ExecutionTrace{last},
		}
		return desc, nil
	}

	// Build assessment prompt
	var sb strings.Builder
	sb.WriteString("Analyze these conversation traces and identify the primary TASK worth saving as a reusable workflow.\n\n")

	for i, t := range c.traceHistory {
		sb.WriteString(fmt.Sprintf("Trace %d: %s\n", i+1, t.Summary()))
	}

	sb.WriteString("\nRules:\n")
	sb.WriteString("- Select only traces that form a single coherent task with tool calls\n")
	sb.WriteString("- Multiple traces may form ONE task (e.g., first attempt fails, user provides credentials, second attempt succeeds)\n")
	sb.WriteString("- Ignore conversational turns, troubleshooting tangents, and Q&A about previous results\n")
	sb.WriteString("- If multiple distinct tasks exist, pick the most substantial one (most tool calls)\n\n")

	sb.WriteString("Respond with EXACTLY two lines:\n")
	sb.WriteString("traces: <comma-separated trace numbers>\n")
	sb.WriteString("description: <one-line description of the task>\n")

	response, err := c.FlowDistiller.LLM(ctx, sb.String())
	if err != nil {
		// Fall back to most recent substantive trace
		last := substantive[len(substantive)-1]
		desc := fmt.Sprintf("%s (%d tool calls)", last.UserRequest, last.ToolCallCount())
		c.pendingDistill = &distillPreview{
			Description: desc,
			Traces:      []*ExecutionTrace{last},
		}
		return desc, nil
	}

	// Parse response
	var selectedIndices []int
	var description string
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "traces:") {
			parts := strings.Split(strings.TrimSpace(strings.TrimPrefix(line, "traces:")), ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				var idx int
				if _, err := fmt.Sscanf(p, "%d", &idx); err == nil && idx >= 1 && idx <= len(c.traceHistory) {
					selectedIndices = append(selectedIndices, idx-1) // convert to 0-based
				}
			}
		} else if strings.HasPrefix(line, "description:") {
			description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}

	// Build the selected traces list
	var selected []*ExecutionTrace
	if len(selectedIndices) > 0 {
		for _, idx := range selectedIndices {
			selected = append(selected, c.traceHistory[idx])
		}
	} else {
		// LLM didn't return valid indices, fall back to all substantive traces
		selected = substantive
	}

	if description == "" {
		// Build description from selected traces
		var reqs []string
		for _, t := range selected {
			reqs = append(reqs, t.UserRequest)
		}
		description = strings.Join(reqs, " → ")
	}

	c.pendingDistill = &distillPreview{
		Description: description,
		Traces:      selected,
	}
	return description, nil
}

// ConfirmAndDistill runs flow distillation using the traces identified by
// a prior call to PreviewDistill. The print function receives status/result text.
func (c *ChatAgent) ConfirmAndDistill(ctx context.Context, print func(string)) error {
	preview := c.pendingDistill
	c.pendingDistill = nil // clear regardless of outcome

	if preview == nil || len(preview.Traces) == 0 {
		return fmt.Errorf("no pending distill preview — call PreviewDistill first")
	}

	if c.FlowDistiller == nil {
		return fmt.Errorf("flow distillation is not configured")
	}

	// Merge selected traces into one combined trace for the distiller
	merged := c.mergeTraces(preview.Traces)

	print("Distilling execution into a reusable flow...\n")

	// Run distillation
	result, err := c.FlowDistiller.Distill(ctx, DistillRequest{
		UserRequest: merged.UserRequest,
		Trace:       merged,
	})
	if err != nil {
		print(fmt.Sprintf("Flow distillation failed: %v\n", err))
		if result == nil {
			return err
		}
	}

	// Determine save directory
	saveDir := c.FlowSaveDir
	if saveDir == "" {
		configDir, cfgErr := os.UserConfigDir()
		if cfgErr != nil {
			return fmt.Errorf("failed to determine config directory: %w", cfgErr)
		}
		saveDir = filepath.Join(configDir, "astonish", "flows")
	}

	// Create the directory if it doesn't exist
	if mkErr := os.MkdirAll(saveDir, 0755); mkErr != nil {
		return fmt.Errorf("failed to create flow directory: %w", mkErr)
	}

	// Save the YAML file
	filename := result.FlowName + ".yaml"
	flowPath := filepath.Join(saveDir, filename)

	// Avoid overwriting existing files
	if _, statErr := os.Stat(flowPath); statErr == nil {
		filename = fmt.Sprintf("%s_%s.yaml", result.FlowName, time.Now().Format("20060102_150405"))
		flowPath = filepath.Join(saveDir, filename)
	}

	if writeErr := os.WriteFile(flowPath, []byte(result.YAML), 0644); writeErr != nil {
		return fmt.Errorf("failed to write flow file: %w", writeErr)
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

	// Build success message
	msg := fmt.Sprintf("\nFlow saved as `%s`\n", flowPath)
	msg += fmt.Sprintf("  Description: %s\n", result.Description)
	if len(result.Tags) > 0 {
		msg += fmt.Sprintf("  Tags: %s\n", strings.Join(result.Tags, ", "))
	}

	// Build run command with parameter suggestions
	runCmd := "astonish flows run " + result.FlowName
	paramFlags := c.extractInputParams(ctx, result.YAML, merged)
	for _, pf := range paramFlags {
		parts := strings.SplitN(pf, "=", 2)
		if len(parts) == 2 && strings.ContainsAny(parts[1], " \t") {
			runCmd += fmt.Sprintf(` -p %s="%s"`, parts[0], parts[1])
		} else {
			runCmd += " -p " + pf
		}
	}
	runCmd += " --auto-approve"
	msg += "\nYou can run this flow with:\n  " + runCmd + "\n"

	print(msg)
	return nil
}

// AutoDistill performs background flow distillation after the LLM signals a reusable task.
// It uses the most recent trace(s), runs the distiller, saves the flow,
// generates a knowledge doc, and updates the registry.
// The description parameter comes from the [DISTILL: ...] marker in the LLM response.
func (c *ChatAgent) AutoDistill(ctx context.Context, description string) error {
	if c.FlowDistiller == nil {
		return fmt.Errorf("flow distillation is not configured")
	}

	// Find the most recent substantive trace
	var trace *ExecutionTrace
	for i := len(c.traceHistory) - 1; i >= 0; i-- {
		if c.traceHistory[i].ToolCallCount() > 0 {
			trace = c.traceHistory[i]
			break
		}
	}
	if trace == nil {
		return fmt.Errorf("no trace with tool calls found")
	}

	// Run distillation
	result, err := c.FlowDistiller.Distill(ctx, DistillRequest{
		UserRequest: trace.UserRequest,
		Trace:       trace,
	})
	if err != nil {
		if result == nil {
			return err
		}
	}

	// Determine save directory
	saveDir := c.FlowSaveDir
	if saveDir == "" {
		configDir, cfgErr := os.UserConfigDir()
		if cfgErr != nil {
			return fmt.Errorf("failed to determine config directory: %w", cfgErr)
		}
		saveDir = filepath.Join(configDir, "astonish", "flows")
	}

	if mkErr := os.MkdirAll(saveDir, 0755); mkErr != nil {
		return fmt.Errorf("failed to create flow directory: %w", mkErr)
	}

	filename := result.FlowName + ".yaml"
	flowPath := filepath.Join(saveDir, filename)

	if _, statErr := os.Stat(flowPath); statErr == nil {
		filename = fmt.Sprintf("%s_%s.yaml", result.FlowName, time.Now().Format("20060102_150405"))
		flowPath = filepath.Join(saveDir, filename)
	}

	if writeErr := os.WriteFile(flowPath, []byte(result.YAML), 0644); writeErr != nil {
		return fmt.Errorf("failed to write flow file: %w", writeErr)
	}

	// Register in flow registry
	entry := FlowRegistryEntry{
		FlowFile:    filename,
		Description: result.Description,
		Tags:        result.Tags,
		CreatedAt:   time.Now(),
	}
	if c.FlowRegistry != nil {
		if regErr := c.FlowRegistry.Register(entry); regErr != nil {
			if c.DebugMode {
				fmt.Printf("[AutoDistill] Failed to register flow: %v\n", regErr)
			}
		}
	}

	// Generate flow knowledge doc
	if c.FlowKnowledgeDir != "" {
		doc := GenerateFlowKnowledgeDoc(result.YAML, entry)
		docName := strings.TrimSuffix(filename, ".yaml") + ".md"
		docPath := filepath.Join(c.FlowKnowledgeDir, docName)
		if mkErr := os.MkdirAll(c.FlowKnowledgeDir, 0755); mkErr == nil {
			_ = os.WriteFile(docPath, []byte(doc), 0644)
			// The fsnotify watcher will pick up the new file and reindex it
		}
	}

	// Refresh SELF.md to reflect the new flow
	if c.SelfMDRefresher != nil {
		c.SelfMDRefresher()
	}

	if c.DebugMode {
		fmt.Printf("[AutoDistill] Flow saved: %s (%s)\n", flowPath, description)
	}

	return nil
}

// mergeTraces combines multiple execution traces into a single trace.
// The user request is joined, and all steps are concatenated in order.
func (c *ChatAgent) mergeTraces(traces []*ExecutionTrace) *ExecutionTrace {
	if len(traces) == 1 {
		return traces[0]
	}

	var requests []string
	var allSteps []TraceStep
	var finalOutput string

	for _, t := range traces {
		requests = append(requests, t.UserRequest)
		allSteps = append(allSteps, t.Steps...)
		if t.FinalOutput != "" {
			finalOutput = t.FinalOutput // use the last non-empty output
		}
	}

	return &ExecutionTrace{
		UserRequest: strings.Join(requests, " → "),
		Steps:       allSteps,
		FinalOutput: finalOutput,
		StartedAt:   traces[0].StartedAt,
		EndedAt:     traces[len(traces)-1].EndedAt,
	}
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
func (c *ChatAgent) extractInputParams(ctx context.Context, yamlStr string, trace *ExecutionTrace) []string {
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

// matchAndBuildPlan checks if the user's request matches a saved flow.
// Uses vector-based matching first (fast, no LLM call), falling back to
// LLM-based matching when ambiguous or when vector search is unavailable.
// Returns (flowFile, planText) -- both empty if no match.
func (c *ChatAgent) matchAndBuildPlan(userText string, memoryContent string) (string, string) {
	if c.FlowRegistry == nil || c.FlowContextBuilder == nil || c.FlowDistiller == nil {
		return "", ""
	}

	entries := c.FlowRegistry.Entries()
	if len(entries) == 0 {
		return "", ""
	}

	var matchedFile string

	// Strategy 1: Vector-based matching (fast, no LLM call)
	if c.FlowSearcher != nil {
		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] Checking flow registry via vector search...\n")
		}
		flowFile, score, err := c.FlowRegistry.FindMatchVector(context.Background(), c.FlowSearcher, userText)
		if err == nil && flowFile != "" {
			if score >= 0.8 {
				// High confidence — use directly
				matchedFile = flowFile
				if c.DebugMode {
					fmt.Printf("[Chat DEBUG] Vector match (high confidence %.2f): %s\n", score, flowFile)
				}
			} else if score >= 0.6 {
				// Ambiguous — fall through to LLM disambiguation
				if c.DebugMode {
					fmt.Printf("[Chat DEBUG] Vector match (ambiguous %.2f): %s — falling back to LLM\n", score, flowFile)
				}
			}
		}
	}

	// Strategy 2: LLM-based matching (fallback)
	if matchedFile == "" {
		matchPrompt := c.FlowRegistry.BuildMatchPrompt(userText)
		if matchPrompt == "" {
			return "", ""
		}

		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] Checking flow registry via LLM...\n")
		}

		response, err := c.FlowDistiller.LLM(context.Background(), matchPrompt)
		if err != nil {
			if c.DebugMode {
				fmt.Printf("[Chat DEBUG] Flow match LLM call failed: %v\n", err)
			}
			return "", ""
		}

		matchedFile = strings.TrimSpace(response)
		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] Flow match response: %q\n", matchedFile)
		}

		if strings.EqualFold(matchedFile, "NONE") || matchedFile == "" {
			return "", ""
		}

		if !strings.HasSuffix(matchedFile, ".yaml") {
			matchedFile += ".yaml"
		}
	}

	// Find the flow file on disk
	flowPath := c.resolveFlowPath(matchedFile)
	if flowPath == "" {
		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] Matched flow file not found on disk: %s\n", matchedFile)
		}
		return "", ""
	}

	// Read the flow YAML
	flowData, err := os.ReadFile(flowPath)
	if err != nil {
		if c.DebugMode {
			fmt.Printf("[Chat DEBUG] Failed to read flow file: %v\n", err)
		}
		return "", ""
	}

	// Build the execution plan
	plan := c.FlowContextBuilder.BuildExecutionPlan(string(flowData), matchedFile, memoryContent)
	if plan == "" {
		return "", ""
	}

	if c.DebugMode {
		fmt.Printf("[Chat DEBUG] Built execution plan from flow: %s (%d bytes)\n", matchedFile, len(plan))
	}

	return matchedFile, plan
}

// resolveFlowPath finds the full path for a flow file.
// Checks FlowSaveDir, then the default agents directory.
func (c *ChatAgent) resolveFlowPath(filename string) string {
	// Check FlowSaveDir first
	if c.FlowSaveDir != "" {
		p := filepath.Join(c.FlowSaveDir, filename)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Check default flows directory
	configDir, err := os.UserConfigDir()
	if err == nil {
		p := filepath.Join(configDir, "astonish", "flows", filename)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}

// truncateQuery shortens a string for debug logging, appending "..." if truncated.
func truncateQuery(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// urlPattern matches http/https URLs in text.
var urlPattern = regexp.MustCompile(`https?://\S+`)

// buildKnowledgeQuery pre-processes a user query for semantic search.
// It strips URLs (which dilute embedding semantics for models like MiniLM)
// and appends matched flow name tokens to boost relevance.
func buildKnowledgeQuery(userText, flowName string) string {
	// Strip URLs — they carry no semantic meaning for the embedding model
	q := urlPattern.ReplaceAllString(userText, "")
	// Collapse whitespace
	q = strings.Join(strings.Fields(q), " ")
	// Append flow name tokens if available (e.g. "download-youtube-video" -> "download youtube video")
	if flowName != "" {
		name := strings.TrimSuffix(flowName, ".yaml")
		name = strings.ReplaceAll(name, "-", " ")
		name = strings.ReplaceAll(name, "_", " ")
		q = strings.TrimSpace(q + " " + name)
	}
	return q
}
