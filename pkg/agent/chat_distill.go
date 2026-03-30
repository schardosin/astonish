package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/adk/session"
	"gopkg.in/yaml.v3"
)

// reconstructTraces rebuilds execution traces from persisted session events.
// This allows /distill to work across daemon restarts — the session transcript
// on disk contains all the tool call/response events we need.
//
// Strategy: walk events chronologically. Each user message starts a new trace.
// FunctionCall events provide tool name + args. FunctionResponse events provide
// results. Final text after tools becomes the trace output.
func (c *ChatAgent) reconstructTraces(ctx context.Context, ds DistillSession) []*ExecutionTrace {
	resp, err := c.SessionService.Get(ctx, &session.GetRequest{
		AppName:   ds.AppName,
		UserID:    ds.UserID,
		SessionID: ds.SessionID,
	})
	if err != nil || resp.Session == nil {
		if c.DebugMode {
			slog.Debug("reconstructTraces: failed to load session", "component", "chat", "sessionID", ds.SessionID, "error", err)
		}
		return nil
	}

	events := resp.Session.Events()
	if events.Len() == 0 {
		return nil
	}

	var traces []*ExecutionTrace
	var current *ExecutionTrace
	// Map pending function calls by name so we can match them with responses.
	// Using name rather than ID because some providers don't set FunctionCall.ID.
	pendingCalls := make(map[string]map[string]any) // tool name -> args

	for i := range events.Len() {
		event := events.At(i)
		if event.LLMResponse.Content == nil {
			continue
		}

		// User message starts a new trace
		if event.Author == "user" {
			// Finalize previous trace if it exists
			if current != nil {
				current.Finalize()
				traces = append(traces, current)
			}
			// Extract user text
			var userText string
			for _, p := range event.LLMResponse.Content.Parts {
				if p.Text != "" {
					userText += p.Text
				}
			}
			current = &ExecutionTrace{
				UserRequest: userText,
				StartedAt:   event.Timestamp,
			}
			pendingCalls = make(map[string]map[string]any)
			continue
		}

		// Agent events — only process if we have an active trace
		if current == nil {
			continue
		}

		for _, part := range event.LLMResponse.Content.Parts {
			if part.FunctionCall != nil {
				// Record the call args for later matching with the response
				pendingCalls[part.FunctionCall.Name] = part.FunctionCall.Args
			}

			if part.FunctionResponse != nil {
				toolName := part.FunctionResponse.Name
				toolArgs := pendingCalls[toolName]
				delete(pendingCalls, toolName)

				// Determine success from the response map
				success := true
				var errMsg string
				if part.FunctionResponse.Response != nil {
					if e, ok := part.FunctionResponse.Response["error"]; ok {
						if es, ok := e.(string); ok && es != "" {
							success = false
							errMsg = es
						}
					}
				}

				step := TraceStep{
					ToolName:  toolName,
					ToolArgs:  toolArgs,
					Success:   success,
					Timestamp: event.Timestamp,
				}
				if part.FunctionResponse.Response != nil {
					step.ToolResult = part.FunctionResponse.Response
				}
				if errMsg != "" {
					step.Error = errMsg
				}
				current.Steps = append(current.Steps, step)
			}

			// Text after tool calls is the final output
			if part.Text != "" && !part.Thought && part.FunctionCall == nil && part.FunctionResponse == nil {
				if len(current.Steps) > 0 {
					cleaned := thinkTagPattern.ReplaceAllString(part.Text, "")
					current.FinalOutput += cleaned
				}
			}
		}
	}

	// Finalize the last trace
	if current != nil {
		current.Finalize()
		traces = append(traces, current)
	}

	if c.DebugMode {
		slog.Debug("reconstructTraces: rebuilt traces", "component", "chat", "traces", len(traces), "sessionID", ds.SessionID)
		for i, t := range traces {
			slog.Debug("reconstructed trace", "component", "chat", "trace", i+1, "request", t.UserRequest, "toolCalls", t.ToolCallCount())
		}
	}

	return traces
}

// PreviewDistill analyzes the conversation trace history for the given session
// and identifies the primary task to distill. Returns a description for user
// confirmation. The result is cached internally for use by ConfirmAndDistill.
func (c *ChatAgent) PreviewDistill(ctx context.Context, ds DistillSession) (string, error) {
	sessionID := ds.SessionID

	c.traceMu.Lock()
	sessionTraces := c.traceHistory[sessionID]
	c.traceMu.Unlock()

	// If no in-memory traces, reconstruct from persisted session events.
	// This handles daemon restarts — traces are ephemeral but session
	// events survive on disk.
	if len(sessionTraces) == 0 && c.SessionService != nil && ds.AppName != "" && ds.UserID != "" {
		reconstructed := c.reconstructTraces(ctx, ds)
		if len(reconstructed) > 0 {
			c.traceMu.Lock()
			c.traceHistory[sessionID] = reconstructed
			sessionTraces = reconstructed
			c.traceMu.Unlock()
		}
	}

	// Filter traces that have tool calls (conversational turns are not distillable)
	var substantive []*ExecutionTrace
	for _, t := range sessionTraces {
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
		c.traceMu.Lock()
		c.pendingDistill[sessionID] = &distillPreview{
			Description: desc,
			Traces:      substantive,
		}
		c.traceMu.Unlock()
		return desc, nil
	}

	// Multiple traces — ask the LLM to identify the primary task
	if c.FlowDistiller == nil {
		// No LLM available for assessment, fall back to most recent substantive trace
		last := substantive[len(substantive)-1]
		desc := fmt.Sprintf("%s (%d tool calls)", last.UserRequest, last.ToolCallCount())
		c.traceMu.Lock()
		c.pendingDistill[sessionID] = &distillPreview{
			Description: desc,
			Traces:      []*ExecutionTrace{last},
		}
		c.traceMu.Unlock()
		return desc, nil
	}

	// Build assessment prompt
	var sb strings.Builder
	sb.WriteString("Analyze these conversation traces and identify the primary TASK worth saving as a reusable workflow.\n\n")

	for i, t := range sessionTraces {
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
		c.traceMu.Lock()
		c.pendingDistill[sessionID] = &distillPreview{
			Description: desc,
			Traces:      []*ExecutionTrace{last},
		}
		c.traceMu.Unlock()
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
				if _, err := fmt.Sscanf(p, "%d", &idx); err == nil && idx >= 1 && idx <= len(sessionTraces) {
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
			selected = append(selected, sessionTraces[idx])
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

	c.traceMu.Lock()
	c.pendingDistill[sessionID] = &distillPreview{
		Description: description,
		Traces:      selected,
	}
	c.traceMu.Unlock()
	return description, nil
}

// ConfirmAndDistill runs flow distillation using the traces identified by
// a prior call to PreviewDistill. The print function receives status/result text.
func (c *ChatAgent) ConfirmAndDistill(ctx context.Context, ds DistillSession, print func(string)) error {
	sessionID := ds.SessionID

	c.traceMu.Lock()
	preview := c.pendingDistill[sessionID]
	delete(c.pendingDistill, sessionID) // clear regardless of outcome
	c.traceMu.Unlock()

	if preview == nil || len(preview.Traces) == 0 {
		return fmt.Errorf("no pending distill preview — call PreviewDistill first")
	}

	if c.FlowDistiller == nil {
		return fmt.Errorf("flow distillation is not configured")
	}

	// Merge selected traces into one combined trace for the distiller
	merged := c.mergeTraces(preview.Traces)

	// Flatten sub-agent traces: replace delegate_tasks steps with children's
	// actual tool calls so the distilled flow has no sub-agent concepts
	flattenTraces(merged)

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
				slog.Debug("failed to register flow", "component", "chat", "error", regErr)
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
			slog.Debug("failed to parse yaml for param extraction", "component", "chat", "error", err)
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
			slog.Debug("llm param extraction failed", "component", "chat", "error", err)
		}
		return nil
	}

	if c.DebugMode {
		slog.Debug("llm param extraction response", "component", "chat", "response", response)
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
