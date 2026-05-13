package tools

import "fmt"

// ToolResult is the common base for all tool result structs.
// Embed this in tool-specific result types to get consistent Status/Message
// fields and access to the toolError/toolSuccess constructors.
type ToolResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// toolError creates a ToolResult with status "error" and a formatted message.
// Use this for simple error-only tool results (no extra fields needed).
func toolError(format string, args ...any) ToolResult {
	return ToolResult{Status: "error", Message: fmt.Sprintf(format, args...)}
}

// toolSuccess creates a ToolResult with status "success" and a formatted message.
func toolSuccess(format string, args ...any) ToolResult {
	return ToolResult{Status: "success", Message: fmt.Sprintf(format, args...)}
}
