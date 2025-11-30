package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
)

// SmartRender detects content type and renders appropriately using glamour.
// It automatically wraps JSON in markdown code blocks for syntax highlighting.
func SmartRender(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}

	// 1. Check if it is valid raw JSON
	// We only care if it looks like a JSON object or array
	if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
		if isJSON(trimmed) {
			// Wrap it in a markdown block so Glamour treats it as code
			input = fmt.Sprintf("```json\n%s\n```", trimmed)
		}
	}

	// 2. Render as Markdown (glamour handles both plain MD and our wrapped JSON)
	// Use Dark style by default as it usually looks better on most terminals
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(100), // Reasonable width for most terminals
	)
	if err != nil {
		return input // Fallback to raw text on error
	}
	
	out, err := renderer.Render(input)
	if err != nil {
		return input // Fallback to raw text on error
	}
	
	// Glamour adds a newline at the beginning sometimes, trim it if it's excessive
	return out
}

// Helper to check validity
func isJSON(s string) bool {
	var js json.RawMessage
	return json.Unmarshal([]byte(s), &js) == nil
}
