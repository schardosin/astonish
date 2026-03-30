package agent

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// FlowContextBuilder converts a flow YAML into an execution plan
// that can be injected into the chat system prompt. This allows
// the chat LLM to replicate a saved flow's behavior using its own
// dynamic tool-use loop rather than switching to the flow engine.
type FlowContextBuilder struct {
	DebugMode bool
}

// curlyPlaceholder matches {variable} patterns that ADK's InjectSessionState
// would try to resolve as session state keys. We escape them to <variable>.
var curlyPlaceholder = regexp.MustCompile(`\{([^{}]+)\}`)

// flowForContext is a minimal struct for parsing flow YAML for context building.
type flowForContext struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Nodes       []flowContextNode `yaml:"nodes"`
	Flow        []flowContextEdge `yaml:"flow"`
}

type flowContextNode struct {
	Name           string            `yaml:"name"`
	Type           string            `yaml:"type"`
	Prompt         string            `yaml:"prompt,omitempty"`
	System         string            `yaml:"system,omitempty"`
	OutputModel    map[string]string `yaml:"output_model,omitempty"`
	Tools          bool              `yaml:"tools,omitempty"`
	ToolsSelection []string          `yaml:"tools_selection,omitempty"`
	UserMessage    []string          `yaml:"user_message,omitempty"`
}

type flowContextEdge struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// BuildExecutionPlan converts a flow YAML string into a human-readable
// execution plan. The memoryContent is used to resolve parameter values
// that may be known from memory.
func (b *FlowContextBuilder) BuildExecutionPlan(flowYAML string, flowName string, memoryContent string) string {
	if flowYAML == "" {
		return ""
	}

	var flow flowForContext
	if err := yaml.Unmarshal([]byte(flowYAML), &flow); err != nil {
		if b.DebugMode {
			slog.Debug("failed to parse flow yaml", "component", "flow-context", "error", err)
		}
		return ""
	}

	if len(flow.Nodes) == 0 {
		return ""
	}

	// Build node lookup
	nodeMap := make(map[string]*flowContextNode, len(flow.Nodes))
	for i := range flow.Nodes {
		nodeMap[flow.Nodes[i].Name] = &flow.Nodes[i]
	}

	// Walk the flow edges to get ordered node names
	orderedNodes := b.walkFlow(flow.Flow, nodeMap)

	// Build the plan
	var sb strings.Builder

	desc := flow.Description
	if desc == "" {
		desc = flowName
	}
	sb.WriteString(fmt.Sprintf("A saved flow matches this request: **%s**\n", desc))
	sb.WriteString("Follow these steps using your tools. ")
	sb.WriteString("If memory provides the needed parameters, proceed without asking the user. ")
	sb.WriteString("If a parameter is missing or the user explicitly provided a different value, use that instead.\n\n")

	stepNum := 0
	for _, nodeName := range orderedNodes {
		node, ok := nodeMap[nodeName]
		if !ok {
			continue
		}

		stepNum++
		switch node.Type {
		case "input":
			sb.WriteString(fmt.Sprintf("**Step %d** (gather input): %s\n", stepNum, node.Name))
			if node.Prompt != "" {
				sb.WriteString(fmt.Sprintf("  Prompt: %s\n", strings.TrimSpace(node.Prompt)))
			}
			if len(node.OutputModel) > 0 {
				sb.WriteString("  Parameters needed:\n")
				for field := range node.OutputModel {
					memVal := resolveFromMemory(field, memoryContent)
					if memVal != "" {
						sb.WriteString(fmt.Sprintf("    - %s (from memory: %s)\n", field, memVal))
					} else {
						sb.WriteString(fmt.Sprintf("    - %s\n", field))
					}
				}
			}

		case "llm":
			if node.Tools {
				sb.WriteString(fmt.Sprintf("**Step %d** (tool execution): %s\n", stepNum, node.Name))
				if len(node.ToolsSelection) > 0 {
					sb.WriteString(fmt.Sprintf("  Tools: %s\n", strings.Join(node.ToolsSelection, ", ")))
				}
				// Extract instructions from the system prompt (full strategy, not just first line)
				if node.System != "" {
					instruction := extractKeyInstruction(node.System)
					if instruction != "" {
						if strings.Contains(instruction, "\n") {
							// Multi-line: render as indented block
							sb.WriteString("  Instructions:\n")
							for _, line := range strings.Split(instruction, "\n") {
								if line == "" {
									sb.WriteString("\n")
								} else {
									sb.WriteString(fmt.Sprintf("    %s\n", line))
								}
							}
						} else {
							sb.WriteString(fmt.Sprintf("  Instruction: %s\n", instruction))
						}
					}
				}
				if node.Prompt != "" {
					sb.WriteString(fmt.Sprintf("  Task: %s\n", summarizePrompt(node.Prompt)))
				}
			} else {
				sb.WriteString(fmt.Sprintf("**Step %d** (processing): %s\n", stepNum, node.Name))
				// This is typically a formatting/processing node
				if node.System != "" {
					sb.WriteString("  Output format instructions:\n")
					// Include the full system prompt for formatting nodes
					// since format fidelity matters
					for _, line := range strings.Split(strings.TrimSpace(node.System), "\n") {
						trimmed := strings.TrimSpace(line)
						if trimmed != "" {
							sb.WriteString(fmt.Sprintf("    %s\n", trimmed))
						}
					}
				}
			}

		default:
			// output or other node types
			if node.Type == "output" {
				stepNum-- // Don't count output as a visible step
			}
		}
		sb.WriteString("\n")
	}

	// Escape {variable} placeholders to prevent ADK's InjectSessionState
	// from trying to resolve them as session state keys.
	plan := sb.String()
	plan = curlyPlaceholder.ReplaceAllString(plan, "<$1>")
	return plan
}

// walkFlow traverses the flow edges starting from START and returns
// ordered node names (excluding START and END).
func (b *FlowContextBuilder) walkFlow(edges []flowContextEdge, nodeMap map[string]*flowContextNode) []string {
	// Build adjacency: from -> to
	adj := make(map[string]string, len(edges))
	for _, e := range edges {
		adj[e.From] = e.To
	}

	var ordered []string
	current := "START"
	visited := make(map[string]bool)

	for i := 0; i < 50; i++ { // safety limit
		next, ok := adj[current]
		if !ok || next == "END" || next == "" {
			break
		}
		if visited[next] {
			break // cycle detection
		}
		visited[next] = true
		ordered = append(ordered, next)
		current = next
	}

	return ordered
}

// resolveFromMemory tries to find a value for a parameter field name
// in the memory content. It looks for patterns like:
//   - "field_name: value" or "field_name = value"
//   - Lines containing both the field name keywords and an IP/hostname/value
//
// Returns the found value or empty string.
func resolveFromMemory(fieldName string, memoryContent string) string {
	if memoryContent == "" {
		return ""
	}

	// Normalize field name to keywords: "ssh_user" -> ["ssh", "user"]
	keywords := strings.FieldsFunc(fieldName, func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	})

	lines := strings.Split(memoryContent, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "- ")

		lower := strings.ToLower(trimmed)

		// Check if all keywords are present in the line
		allPresent := true
		for _, kw := range keywords {
			if !strings.Contains(lower, strings.ToLower(kw)) {
				allPresent = false
				break
			}
		}
		if !allPresent {
			continue
		}

		// Try to extract value from "key: value" or "key = value" patterns
		for _, sep := range []string{": ", "= ", "=", ":"} {
			idx := strings.Index(trimmed, sep)
			if idx > 0 {
				val := strings.TrimSpace(trimmed[idx+len(sep):])
				// Clean up common wrapping
				val = strings.Trim(val, "`\"'")
				if val != "" {
					return val
				}
			}
		}
	}

	return ""
}

// extractKeyInstruction gets the first meaningful instruction from a system prompt.
// Skips "You are a..." preamble lines and returns the first actionable line.
func extractKeyInstruction(system string) string {
	lines := strings.Split(strings.TrimSpace(system), "\n")
	var result []string
	pastPreamble := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if pastPreamble {
				result = append(result, "")
			}
			continue
		}
		// Skip generic preamble lines at the start
		if !pastPreamble {
			lower := strings.ToLower(trimmed)
			if strings.HasPrefix(lower, "you are") || strings.HasPrefix(lower, "your job") ||
				strings.HasPrefix(lower, "your role") || strings.HasPrefix(lower, "your task") {
				continue
			}
			pastPreamble = true
		}
		result = append(result, trimmed)
	}

	if len(result) == 0 {
		return ""
	}

	// Join and truncate to a reasonable limit
	full := strings.Join(result, "\n")
	if len(full) > 1000 {
		full = full[:1000] + "\n..."
	}
	return full
}

// summarizePrompt returns a concise version of a prompt, replacing
// {variable} placeholders with a note and truncating if needed.
func summarizePrompt(prompt string) string {
	p := strings.TrimSpace(prompt)
	// Collapse multi-line to single line
	p = strings.ReplaceAll(p, "\n", " ")
	// Collapse multiple spaces
	for strings.Contains(p, "  ") {
		p = strings.ReplaceAll(p, "  ", " ")
	}
	if len(p) > 200 {
		p = p[:200] + "..."
	}
	return p
}
