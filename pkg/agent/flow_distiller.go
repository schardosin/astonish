package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// FlowValidationResult mirrors api.FlowValidationResult to avoid import cycles.
type FlowValidationResult struct {
	Valid  bool
	Errors []string
}

// DistillerToolInfo mirrors api.ToolInfo to avoid import cycles.
type DistillerToolInfo struct {
	Name        string
	Description string
	Source      string
}

// FlowDistiller converts execution traces into YAML flow definitions.
type FlowDistiller struct {
	LLM          func(ctx context.Context, prompt string) (string, error)
	Tools        []tool.Tool    // For building available tools list
	Toolsets     []tool.Toolset // For building available tools list
	MaxRetries   int            // Default: 3
	GetSchema    func() string  // Returns the flow schema string
	ValidateYAML func(yamlStr string, tools []DistillerToolInfo) FlowValidationResult
}

// internalOnlyTools lists tools that exist only for the chat agent's
// internal use and must never appear in distilled flows.
var internalOnlyTools = map[string]bool{
	"memory_save": true,
}

// NewFlowDistiller creates a FlowDistiller that uses the provided ADK model.LLM.
// The getSchema and validateYAML functions break the import cycle with pkg/api.
func NewFlowDistiller(
	llm model.LLM,
	tools []tool.Tool,
	toolsets []tool.Toolset,
	getSchema func() string,
	validateYAML func(yamlStr string, tools []DistillerToolInfo) FlowValidationResult,
) *FlowDistiller {
	return &FlowDistiller{
		LLM: func(ctx context.Context, prompt string) (string, error) {
			req := &model.LLMRequest{
				Contents: []*genai.Content{
					{
						Parts: []*genai.Part{{Text: prompt}},
						Role:  "user",
					},
				},
			}
			var text string
			for resp, err := range llm.GenerateContent(ctx, req, false) {
				if err != nil {
					return text, err
				}
				if resp.Content != nil {
					for _, p := range resp.Content.Parts {
						if p.Text != "" {
							text += p.Text
						}
					}
				}
			}
			if text == "" {
				return "", fmt.Errorf("empty response from LLM")
			}
			return text, nil
		},
		Tools:        tools,
		Toolsets:     toolsets,
		MaxRetries:   3,
		GetSchema:    getSchema,
		ValidateYAML: validateYAML,
	}
}

// DistillRequest holds the inputs for flow distillation.
type DistillRequest struct {
	UserRequest string
	Trace       *ExecutionTrace
}

// DistillResult holds the output of flow distillation.
type DistillResult struct {
	YAML        string   // The generated YAML flow
	FlowName    string   // Suggested filename (snake_case, no extension)
	Description string   // Human-readable description for registry
	Tags        []string // For registry indexing
}

// Distill converts an execution trace into a YAML flow definition.
func (d *FlowDistiller) Distill(ctx context.Context, req DistillRequest) (*DistillResult, error) {
	maxRetries := d.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	// Build available tools list for validation
	availableTools := d.buildToolInfoList()

	var lastYAML string
	var lastErrors []string

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Build prompt (include validation errors on retry)
		prompt := d.buildDistillationPrompt(req, lastYAML, lastErrors)

		// Call LLM
		response, err := d.LLM(ctx, prompt)
		if err != nil {
			return nil, fmt.Errorf("LLM call failed on attempt %d: %w", attempt+1, err)
		}

		// Extract YAML from response
		yamlStr := extractYAMLBlock(response)
		if yamlStr == "" {
			lastErrors = []string{"No YAML code block found in response. Please wrap the flow in ```yaml ... ``` markers."}
			lastYAML = ""
			continue
		}

		// Validate if validator is available
		if d.ValidateYAML != nil {
			validation := d.ValidateYAML(yamlStr, availableTools)
			if !validation.Valid {
				lastYAML = yamlStr
				lastErrors = validation.Errors
				continue
			}
		}

		// Extract metadata from response
		result := &DistillResult{YAML: yamlStr}
		d.extractMetadata(response, result)

		return result, nil
	}

	// All retries exhausted
	if lastYAML != "" {
		// Return the last attempt with a warning
		result := &DistillResult{YAML: lastYAML}
		d.extractMetadata("", result)
		if result.Description == "" {
			result.Description = "Generated flow (may have validation warnings)"
		}
		return result, fmt.Errorf("flow generated but has validation errors after %d attempts: %s",
			maxRetries, strings.Join(lastErrors, "; "))
	}

	return nil, fmt.Errorf("failed to generate valid YAML flow after %d attempts", maxRetries)
}

// buildDistillationPrompt constructs the prompt for the LLM.
func (d *FlowDistiller) buildDistillationPrompt(req DistillRequest, previousYAML string, validationErrors []string) string {
	var sb strings.Builder

	sb.WriteString("# Task\n\n")
	sb.WriteString("Convert this successful execution trace into a reusable Astonish YAML flow.\n\n")

	// Include the flow schema
	if d.GetSchema != nil {
		sb.WriteString("# Astonish Flow Schema\n\n")
		sb.WriteString(d.GetSchema())
	}

	// Available tools (excluding internal-only tools)
	toolInfos := d.buildToolInfoList()
	if len(toolInfos) > 0 {
		sb.WriteString("\n\n# Available Tools\n\nONLY use tools from this list:\n")
		for _, t := range toolInfos {
			if internalOnlyTools[t.Name] {
				continue
			}
			sb.WriteString(fmt.Sprintf("- %s: %s (source: %s)\n", t.Name, t.Description, t.Source))
		}
	}

	// Original user request
	sb.WriteString("\n\n# Original User Request\n\n")
	sb.WriteString(req.UserRequest)

	// Execution trace (successful steps only, excluding internal-only tools)
	steps := req.Trace.SuccessfulSteps()
	sb.WriteString("\n\n# Execution Trace (successful steps)\n\n")
	stepNum := 0
	for _, step := range steps {
		if internalOnlyTools[step.ToolName] {
			continue
		}
		stepNum++
		argsJSON, _ := json.MarshalIndent(step.ToolArgs, "", "  ")
		sb.WriteString(fmt.Sprintf("Step %d: Called **%s**\n", stepNum, step.ToolName))
		sb.WriteString(fmt.Sprintf("  Args: %s\n", string(argsJSON)))
		if step.DurationMs > 0 {
			sb.WriteString(fmt.Sprintf("  Duration: %dms\n", step.DurationMs))
		}
		sb.WriteString("\n")
	}

	// Instructions
	sb.WriteString("# Instructions\n\n")
	sb.WriteString("1. Create a valid YAML flow with input -> processing -> output nodes\n")
	sb.WriteString("2. Replace literal values (IPs, paths, specific queries) with {parameter} variables\n")
	sb.WriteString("3. Add an input node to collect parameters from the user\n")
	sb.WriteString("4. Each tool-calling step should be an LLM node with tools: true\n")
	sb.WriteString("5. Use tools_selection to restrict to only the tools that were actually used\n")
	sb.WriteString("6. After tool execution, add an LLM processing node that formats the output\n")
	sb.WriteString("7. Add an output node to display the formatted result\n")
	sb.WriteString("8. Include proper flow edges connecting START -> nodes -> END\n")
	sb.WriteString("9. NEVER include memory_save or other internal-only tools in the flow. Memory is a chat-mode concern, not a flow step.\n\n")

	// Include actual output if captured, so the distiller can replicate the format
	if req.Trace.FinalOutput != "" {
		sb.WriteString("# Actual Output Produced During Execution\n\n")
		sb.WriteString("The formatting LLM node MUST replicate this exact output format.\n")
		sb.WriteString("Include ALL columns, sections, emojis, and formatting shown below in the system prompt of the formatting node.\n\n")
		sb.WriteString("```markdown\n")
		sb.WriteString(req.Trace.FinalOutput)
		sb.WriteString("\n```\n\n")
	}

	// If retrying, include previous errors
	if previousYAML != "" && len(validationErrors) > 0 {
		sb.WriteString("# IMPORTANT: Previous Attempt Failed Validation\n\n")
		sb.WriteString("Your previous YAML had these errors:\n")
		for _, e := range validationErrors {
			sb.WriteString(fmt.Sprintf("- %s\n", e))
		}
		sb.WriteString("\nPrevious YAML:\n```yaml\n")
		sb.WriteString(previousYAML)
		sb.WriteString("\n```\n\n")
		sb.WriteString("Fix ALL the errors above and generate a corrected YAML flow.\n\n")
	}

	// Response format
	sb.WriteString("# Response Format\n\n")
	sb.WriteString("First, provide metadata on separate lines:\n")
	sb.WriteString("flow_name: suggested_filename_in_snake_case\n")
	sb.WriteString("description: one-line human description of what this flow does\n")
	sb.WriteString("tags: comma, separated, tags\n\n")
	sb.WriteString("Then provide the complete YAML flow in a ```yaml code block.\n")

	return sb.String()
}

// buildToolInfoList creates a DistillerToolInfo list from the available tools.
func (d *FlowDistiller) buildToolInfoList() []DistillerToolInfo {
	var infos []DistillerToolInfo

	for _, t := range d.Tools {
		infos = append(infos, DistillerToolInfo{
			Name:        t.Name(),
			Description: t.Description(),
			Source:      "internal",
		})
	}

	if len(d.Toolsets) > 0 {
		ctx := &minimalReadonlyContext{Context: context.Background()}
		for _, ts := range d.Toolsets {
			mcpTools, err := ts.Tools(ctx)
			if err != nil {
				continue
			}
			for _, t := range mcpTools {
				infos = append(infos, DistillerToolInfo{
					Name:        t.Name(),
					Description: t.Description(),
					Source:      ts.Name(),
				})
			}
		}
	}

	return infos
}

// extractMetadata parses flow_name, description, and tags from the LLM response text.
func (d *FlowDistiller) extractMetadata(response string, result *DistillResult) {
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "flow_name:") {
			result.FlowName = strings.TrimSpace(strings.TrimPrefix(line, "flow_name:"))
		} else if strings.HasPrefix(line, "description:") {
			result.Description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		} else if strings.HasPrefix(line, "tags:") {
			tagsStr := strings.TrimSpace(strings.TrimPrefix(line, "tags:"))
			for _, tag := range strings.Split(tagsStr, ",") {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					result.Tags = append(result.Tags, tag)
				}
			}
		}
	}

	// Defaults
	if result.FlowName == "" {
		result.FlowName = "distilled_flow"
	}
	if result.Description == "" {
		result.Description = "Auto-generated flow from chat execution"
	}
}

// extractYAMLBlock extracts YAML content from a ```yaml code block.
func extractYAMLBlock(text string) string {
	startMarker := "```yaml"
	endMarker := "```"

	startIdx := strings.Index(text, startMarker)
	if startIdx == -1 {
		return ""
	}

	startIdx += len(startMarker)
	remaining := text[startIdx:]

	endIdx := strings.Index(remaining, endMarker)
	if endIdx == -1 {
		return ""
	}

	return strings.TrimSpace(remaining[:endIdx])
}
