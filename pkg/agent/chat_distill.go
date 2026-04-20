package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
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
				UserRequest: strings.TrimSpace(timestampPattern.ReplaceAllString(userText, "")),
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

// HasPendingDistill returns true if there is a pending distill preview
// for the given session, waiting for user confirmation.
func (c *ChatAgent) HasPendingDistill(sessionID string) bool {
	c.traceMu.Lock()
	defer c.traceMu.Unlock()
	return c.pendingDistill[sessionID] != nil
}

// CancelPendingDistill clears any pending distill preview for the given session.
func (c *ChatAgent) CancelPendingDistill(sessionID string) {
	c.traceMu.Lock()
	delete(c.pendingDistill, sessionID)
	c.traceMu.Unlock()
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

// extractInputParams parses the distilled YAML to find input nodes,
// then asks the LLM to fill in the actual values from the execution trace.
// Each input node produces one -p flag keyed by the node name (e.g.,
// -p get_openstack_params=https://identity-3.qa-de-1.cloud.sap/v3).
// The output_model field names are included in the LLM prompt as context
// but are not used as -p keys — the flow runner matches by node name.
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

	// secretInputPattern matches prompts or output_model fields that indicate
	// the node collects secrets. These nodes must not appear in -p flags.
	secretInputPattern := regexp.MustCompile(`(?i)(secret|password|token|api[_\s]?key)`)

	type inputNode struct {
		name        string
		prompt      string
		outputModel map[string]string
	}
	var inputs []inputNode
	for _, node := range flow.Nodes {
		if node.Type != "input" {
			continue
		}
		// Skip nodes that collect secrets — they should not appear in -p flags
		isSecret := secretInputPattern.MatchString(node.Prompt)
		if !isSecret {
			for field := range node.OutputModel {
				if secretInputPattern.MatchString(field) {
					isSecret = true
					break
				}
			}
		}
		if isSecret {
			continue
		}
		inputs = append(inputs, inputNode{name: node.Name, prompt: node.Prompt, outputModel: node.OutputModel})
	}
	if len(inputs) == 0 {
		return nil
	}

	// Build reverse map: output_model field name -> node name.
	// The LLM may respond with field names instead of node names; this lets
	// us accept both (e.g., "os_auth_url=..." maps to "get_parameters").
	fieldToNode := make(map[string]string)
	for _, inp := range inputs {
		for field := range inp.outputModel {
			fieldToNode[field] = inp.name
		}
	}

	// Build a prompt for the LLM to fill in the parameter values
	var sb strings.Builder
	sb.WriteString("Given this execution trace, determine the concrete value for each input parameter.\n\n")

	sb.WriteString("# Execution Trace\n")
	sb.WriteString("User request: " + trace.UserRequest + "\n\n")
	for i, step := range trace.SuccessfulSteps() {
		sb.WriteString(fmt.Sprintf("Step %d: tool=%s\n", i+1, step.ToolName))
		for k, v := range step.ToolArgs {
			sb.WriteString(fmt.Sprintf("  arg %s = %v\n", k, v))
		}
	}

	// Include the final agent output — it often mentions the concrete values
	// (hostnames, credentials, node names) that were used during execution.
	if trace.FinalOutput != "" {
		sb.WriteString("\n# Agent Output\n")
		sb.WriteString(trace.FinalOutput)
		sb.WriteString("\n")
	}

	sb.WriteString("\n# Input Parameters to Fill\n")
	sb.WriteString("Each line below is an input node. Use the NODE NAME as the key in your response.\n\n")
	for _, inp := range inputs {
		sb.WriteString(fmt.Sprintf("- %s (prompt: %q)", inp.name, inp.prompt))
		if len(inp.outputModel) > 0 {
			var fields []string
			for k := range inp.outputModel {
				fields = append(fields, k)
			}
			sb.WriteString(fmt.Sprintf(" [stores value as: %s]", strings.Join(fields, ", ")))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n# Instructions\n")
	sb.WriteString("For each input node, find the EXACT LITERAL value that was used during execution.\n")
	sb.WriteString("Look in the tool arguments (especially shell commands, API calls) and the agent output for concrete values.\n")
	sb.WriteString("Extract values like URLs, credential names, IDs, region names, hostnames, paths, etc. directly from the trace.\n\n")
	sb.WriteString("Respond with ONLY the values, one per line, using the NODE NAME as the key:\n")
	// Provide a concrete example using the actual input names
	if len(inputs) > 0 {
		sb.WriteString("Example format:\n")
		for _, inp := range inputs {
			sb.WriteString(fmt.Sprintf("  %s=<extracted value>\n", inp.name))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Do not add quotes, explanations, descriptions, or extra text. Just the key=value lines.\n")

	prompt := sb.String()

	if c.DebugMode {
		slog.Debug("llm param extraction prompt", "component", "chat", "prompt", prompt)
	}

	// Call LLM
	response, err := c.FlowDistiller.LLM(context.Background(), prompt)
	if err != nil {
		if c.DebugMode {
			slog.Debug("llm param extraction failed", "component", "chat", "error", err)
		}
		return nil
	}

	if c.DebugMode {
		slog.Debug("llm param extraction response", "component", "chat", "response", response)
	}

	// Parse response: expect "name=value" lines.
	// Accept both node names (get_parameters) and output_model field names
	// (os_auth_url) — map field names back to node names via fieldToNode.
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
		if val == "" || val == "<value>" || val == "<extracted value>" {
			continue
		}

		// Direct match: key is a node name
		if validNames[key] {
			resolved[key] = val
			continue
		}
		// Indirect match: key is an output_model field name
		if nodeName, ok := fieldToNode[key]; ok {
			if _, alreadySet := resolved[nodeName]; !alreadySet {
				resolved[nodeName] = val
			}
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

// --- Interactive Distill Review Methods ---

// DistillToReview runs flow distillation and stores the result as a review draft
// instead of saving immediately. Returns the DistillReview for the frontend to
// render a preview. The user can then request modifications or save.
func (c *ChatAgent) DistillToReview(ctx context.Context, ds DistillSession, print func(string)) (*DistillReview, error) {
	sessionID := ds.SessionID

	c.traceMu.Lock()
	preview := c.pendingDistill[sessionID]
	delete(c.pendingDistill, sessionID)
	c.traceMu.Unlock()

	if preview == nil || len(preview.Traces) == 0 {
		return nil, fmt.Errorf("no pending distill preview — call PreviewDistill first")
	}

	if c.FlowDistiller == nil {
		return nil, fmt.Errorf("flow distillation is not configured")
	}

	merged := c.mergeTraces(preview.Traces)
	flattenTraces(merged)

	print("Distilling execution into a reusable flow...\n")

	result, err := c.FlowDistiller.Distill(ctx, DistillRequest{
		UserRequest: merged.UserRequest,
		Trace:       merged,
	})
	if err != nil {
		if result == nil {
			return nil, fmt.Errorf("flow distillation failed: %w", err)
		}
		// Continue with best-effort result
		print(fmt.Sprintf("Note: flow generated with warnings: %v\n", err))
	}

	review := &DistillReview{
		YAML:        result.YAML,
		FlowName:    result.FlowName,
		Description: result.Description,
		Tags:        result.Tags,
		Explanation: result.Explanation,
		Traces:      preview.Traces,
	}

	c.traceMu.Lock()
	c.pendingDistillReview[sessionID] = review
	c.traceMu.Unlock()

	return review, nil
}

// HasPendingDistillReview returns true if there is a pending distill review
// for the given session, waiting for user modifications or save.
func (c *ChatAgent) HasPendingDistillReview(sessionID string) bool {
	c.traceMu.Lock()
	defer c.traceMu.Unlock()
	return c.pendingDistillReview[sessionID] != nil
}

// GetPendingDistillReview returns the current distill review for the session, or nil.
func (c *ChatAgent) GetPendingDistillReview(sessionID string) *DistillReview {
	c.traceMu.Lock()
	defer c.traceMu.Unlock()
	return c.pendingDistillReview[sessionID]
}

// CancelDistillReview clears any pending distill review for the given session.
func (c *ChatAgent) CancelDistillReview(sessionID string) {
	c.traceMu.Lock()
	delete(c.pendingDistillReview, sessionID)
	c.traceMu.Unlock()
}

// ModifyDistillReview modifies the pending distill review based on user feedback.
// Returns the updated DistillReview with the new YAML and explanation.
func (c *ChatAgent) ModifyDistillReview(ctx context.Context, sessionID string, changeRequest string) (*DistillReview, error) {
	c.traceMu.Lock()
	review := c.pendingDistillReview[sessionID]
	c.traceMu.Unlock()

	if review == nil {
		return nil, fmt.Errorf("no pending distill review for this session")
	}

	if c.FlowDistiller == nil {
		return nil, fmt.Errorf("flow distillation is not configured")
	}

	// Build modification request with history
	modReq := DistillModifyRequest{
		CurrentYAML:   review.YAML,
		ChangeRequest: changeRequest,
		History:       review.Modifications,
	}

	// Include the original trace if available
	if len(review.Traces) > 0 {
		modReq.OriginalTrace = c.mergeTraces(review.Traces)
	}

	result, err := c.FlowDistiller.ModifyFlow(ctx, modReq)
	if err != nil {
		if result == nil {
			return nil, fmt.Errorf("flow modification failed: %w", err)
		}
		// Continue with best-effort result
	}

	// Update the review
	c.traceMu.Lock()
	review.YAML = result.YAML
	review.FlowName = result.FlowName
	review.Description = result.Description
	review.Tags = result.Tags
	review.Explanation = result.Explanation
	review.Modifications = append(review.Modifications, changeRequest)
	c.traceMu.Unlock()

	return review, nil
}

// SaveDistillReview saves the current distill review to disk and registers it.
// Returns the file path and a suggested run command.
func (c *ChatAgent) SaveDistillReview(ctx context.Context, sessionID string) (filePath string, runCmd string, err error) {
	c.traceMu.Lock()
	review := c.pendingDistillReview[sessionID]
	delete(c.pendingDistillReview, sessionID)
	c.traceMu.Unlock()

	if review == nil {
		return "", "", fmt.Errorf("no pending distill review to save")
	}

	// Determine save directory
	saveDir := c.FlowSaveDir
	if saveDir == "" {
		configDir, cfgErr := os.UserConfigDir()
		if cfgErr != nil {
			return "", "", fmt.Errorf("failed to determine config directory: %w", cfgErr)
		}
		saveDir = filepath.Join(configDir, "astonish", "flows")
	}

	if mkErr := os.MkdirAll(saveDir, 0755); mkErr != nil {
		return "", "", fmt.Errorf("failed to create flow directory: %w", mkErr)
	}

	filename := review.FlowName + ".yaml"
	flowPath := filepath.Join(saveDir, filename)

	if _, statErr := os.Stat(flowPath); statErr == nil {
		filename = fmt.Sprintf("%s_%s.yaml", review.FlowName, time.Now().Format("20060102_150405"))
		flowPath = filepath.Join(saveDir, filename)
	}

	if writeErr := os.WriteFile(flowPath, []byte(review.YAML), 0644); writeErr != nil {
		return "", "", fmt.Errorf("failed to write flow file: %w", writeErr)
	}

	// Register in the flow registry
	if c.FlowRegistry != nil {
		entry := FlowRegistryEntry{
			FlowFile:    filename,
			Description: review.Description,
			Tags:        review.Tags,
			CreatedAt:   time.Now(),
		}
		if regErr := c.FlowRegistry.Register(entry); regErr != nil {
			if c.DebugMode {
				slog.Debug("failed to register flow", "component", "chat", "error", regErr)
			}
		}
	}

	// Build run command with parameter suggestions
	cmd := "astonish flows run " + review.FlowName
	if len(review.Traces) > 0 {
		merged := c.mergeTraces(review.Traces)
		paramFlags := c.extractInputParams(ctx, review.YAML, merged)
		for _, pf := range paramFlags {
			parts := strings.SplitN(pf, "=", 2)
			if len(parts) == 2 && strings.ContainsAny(parts[1], " \t") {
				cmd += fmt.Sprintf(` -p %s="%s"`, parts[0], parts[1])
			} else {
				cmd += " -p " + pf
			}
		}
	}
	cmd += " --auto-approve"

	return flowPath, cmd, nil
}

// DistillReviewIntent represents the classified intent of a user message
// during a distill review.
type DistillReviewIntent int

const (
	// DistillIntentSave means the user wants to save the flow as-is.
	DistillIntentSave DistillReviewIntent = iota
	// DistillIntentCancel means the user wants to cancel/abort the review.
	DistillIntentCancel
	// DistillIntentModify means the user wants to change the flow.
	DistillIntentModify
)

// ClassifyDistillReviewIntent determines the user's intent from their message
// during a distill review. It uses the LLM to understand natural language
// intent rather than brittle pattern matching.
func (c *ChatAgent) ClassifyDistillReviewIntent(ctx context.Context, msg string) DistillReviewIntent {
	trimmed := strings.TrimSpace(msg)

	// Internal marker from the Save/Cancel buttons — no LLM needed
	if trimmed == "__distill_save__" {
		return DistillIntentSave
	}
	if trimmed == "__distill_cancel__" {
		return DistillIntentCancel
	}

	// Use LLM to classify intent
	if c.FlowDistiller != nil && c.FlowDistiller.LLM != nil {
		intent := c.classifyViaLLM(ctx, trimmed)
		if intent >= 0 {
			return intent
		}
	}

	// Fallback if LLM is unavailable: treat as modification
	return DistillIntentModify
}

// classifyViaLLM asks the LLM to classify the user's intent.
// Returns -1 if classification fails.
func (c *ChatAgent) classifyViaLLM(ctx context.Context, msg string) DistillReviewIntent {
	prompt := fmt.Sprintf(`You are classifying a user's message during a flow review process.
The user has just been shown a generated flow (YAML workflow) and is being asked to review it.
They can do one of three things:

1. SAVE — They want to accept and save the flow as-is. This includes any affirmative response like "yes", "save it", "looks good", "ship it", "go ahead", "you can save it now", etc.
2. CANCEL — They want to discard the flow entirely. This includes "no", "cancel", "nevermind", "don't save", "scrap it", etc.
3. MODIFY — They want to make specific changes to the flow. This includes requests like "add an error handler", "rename the first node", "change the model to gpt-4", etc.

User message: %q

Respond with exactly one word: SAVE, CANCEL, or MODIFY`, msg)

	response, err := c.FlowDistiller.LLM(ctx, prompt)
	if err != nil {
		if c.DebugMode {
			slog.Debug("distill intent classification LLM failed", "component", "chat", "error", err)
		}
		return -1
	}

	answer := strings.ToUpper(strings.TrimSpace(response))
	// Extract the first word in case the LLM is verbose
	if idx := strings.IndexAny(answer, " \n\t.,"); idx > 0 {
		answer = answer[:idx]
	}

	switch answer {
	case "SAVE":
		return DistillIntentSave
	case "CANCEL":
		return DistillIntentCancel
	case "MODIFY":
		return DistillIntentModify
	default:
		if c.DebugMode {
			slog.Debug("distill intent classification: unrecognized LLM response", "component", "chat", "response", response)
		}
		return -1
	}
}
