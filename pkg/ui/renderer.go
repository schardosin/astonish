package ui

import (
	"strings"
)

// SmartRender returns the input as-is for terminal output.
// This bypasses glamour markdown rendering to:
// 1. Preserve markdown tags for easy copy/paste
// 2. Let the terminal handle natural line wrapping
// 3. Avoid artificial double-spacing between lines
func SmartRender(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}

	// Return raw text - let the terminal handle display naturally
	return input
}
