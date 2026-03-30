package agent

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/ui"
	"google.golang.org/adk/session"
)

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
	// If START has no outgoing connection, gracefully go to END
	if current == "START" {
		return "END", nil
	}
	return "", fmt.Errorf("no transition found from node: %s", current)
}

func (a *AstonishAgent) evaluateCondition(condition string, state session.State) bool {
	// Handle simple "true" condition
	if condition == "true" {
		return true
	}

	// Convert session.State to map[string]interface{}
	stateMap := a.stateToMap(state)

	// Use Starlark evaluator
	result, err := EvaluateCondition(condition, stateMap)
	if err != nil {
		if a.DebugMode {
			slog.Debug("condition evaluation error", "condition", condition, "error", err)
		}
		return false
	}

	return result
}

// stateToMap converts session.State to map[string]interface{}
func (a *AstonishAgent) stateToMap(state session.State) map[string]interface{} {
	stateMap := make(map[string]interface{})

	// Use the All() iterator to get all key-value pairs
	for key, value := range state.All() {
		stateMap[key] = value
	}

	return stateMap
}

func (a *AstonishAgent) renderString(tmpl string, state session.State) string {
	// Use a regex that captures content inside {} but not nested {}
	// This allows for expressions like {comment["patch"]}
	re := regexp.MustCompile(`\{([^{}]+)\}`)

	// Convert state to map once for efficiency if needed, but renderString might be called often
	// For now, convert inside the loop or pass it?
	// stateToMap is relatively cheap if state is small.
	stateMap := a.stateToMap(state)

	return re.ReplaceAllStringFunc(tmpl, func(match string) string {
		expr := match[1 : len(match)-1]

		// Try to evaluate the expression using Starlark
		val, err := EvaluateExpression(expr, stateMap)
		if err != nil {
			// If evaluation fails, the placeholder doesn't exist in state
			// Convert {var} to <var> to prevent ADK from trying to process it
			// This allows example text like "PR #{number}: {title}" to remain readable
			if a.DebugMode {
				slog.Debug("renderString: converting placeholder to angle brackets (not in state)", "expr", expr)
			}
			return "<" + expr + ">"
		}

		if val == nil {
			// Value is nil, convert to angle brackets
			if a.DebugMode {
				slog.Debug("renderString: converting placeholder to angle brackets (value is nil)", "expr", expr)
			}
			return "<" + expr + ">"
		}

		formatted := ui.FormatAsYamlLike(val, 0)
		if a.DebugMode {
			slog.Debug("renderString: replaced placeholder", "expr", expr, "formatted", formatted)
		}
		return formatted
	})
}

func (a *AstonishAgent) cleanAndFixJson(input string) string {
	trimmed := strings.TrimSpace(input)

	// Find the first JSON object or array start character
	// This handles both pure JSON and markdown-wrapped JSON (```json ... ```)
	startIdx := -1
	startChar := ""
	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		if ch == '{' || ch == '[' {
			startIdx = i
			startChar = string(ch)
			break
		}
	}

	if startIdx == -1 {
		// No JSON found, return as-is
		return trimmed
	}

	// Find the matching closing bracket with proper string handling
	endChar := "]"
	if startChar == "{" {
		endChar = "}"
	}

	depth := 0
	endIdx := -1
	inString := false
	escapeNext := false

	for i := startIdx; i < len(trimmed); i++ {
		ch := trimmed[i]

		// Handle string escaping
		if escapeNext {
			escapeNext = false
			continue
		}
		if ch == '\\' {
			escapeNext = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}

		// Only count brackets outside of strings
		if !inString {
			if string(ch) == startChar {
				depth++
			} else if string(ch) == endChar {
				depth--
				if depth == 0 {
					endIdx = i
					break
				}
			}
		}
	}

	if endIdx != -1 {
		return strings.TrimSpace(trimmed[startIdx : endIdx+1])
	}

	// If we couldn't find matching bracket, return from startIdx to end
	// This at least gives us partial JSON that might still be parseable
	return strings.TrimSpace(trimmed[startIdx:])
}

// getKeys returns the keys of a map as a slice
func getKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// getKeysStr returns the keys of a map[string]string as a slice
func getKeysStr(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// emitNodeTransition emits a node transition event
func (a *AstonishAgent) emitNodeTransition(nodeName string, state session.State, yield func(*session.Event, error) bool) bool {
	if nodeName == "END" {
		event := &session.Event{
			Actions: session.EventActions{
				StateDelta: map[string]any{
					"current_node": "END",
					"node_type":    "END",
				},
			},
		}
		return yield(event, nil)
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
		// LLMResponse removed to prevent static "--- Node ---" log
		Actions: session.EventActions{
			StateDelta: map[string]any{
				"current_node":      nodeName,
				"temp:node_history": history,
				"temp:node_type":    node.Type,
				"node_type":         node.Type,
				"silent":            node.Silent,
			},
		},
	}

	return yield(event, nil)
}
