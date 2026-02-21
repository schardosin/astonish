package agent

import (
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
)

// SafeTools are read-only tools that auto-approve in chat mode.
// These tools cannot modify the filesystem or execute commands.
var SafeTools = map[string]bool{
	"read_file":                 true,
	"file_tree":                 true,
	"find_files":                true,
	"grep_search":               true,
	"git_diff_add_line_numbers": true,
	"filter_json":               true,
	"web_fetch":                 true,
	"read_pdf":                  true,
	"memory_search":             true,
	"memory_get":                true,
}

// IsToolSafe returns true if the tool is read-only and safe to auto-approve.
func IsToolSafe(name string) bool {
	return SafeTools[name]
}

// WrapToolsForChat wraps tools with approval gates based on their category.
// Safe (read-only) tools are returned unwrapped and will auto-execute.
// Protected (write/exec) tools get wrapped in ProtectedTool for approval.
// If autoApprove is true, all tools are returned unwrapped.
func WrapToolsForChat(allTools []tool.Tool, state session.State,
	chatAgent *AstonishAgent, yieldFunc func(*session.Event, error) bool,
	autoApprove bool) []tool.Tool {

	if autoApprove {
		return allTools
	}

	wrapped := make([]tool.Tool, len(allTools))
	for i, t := range allTools {
		if IsToolSafe(t.Name()) {
			wrapped[i] = t
		} else {
			wrapped[i] = &ProtectedTool{
				Tool:      t,
				State:     state,
				Agent:     chatAgent,
				YieldFunc: yieldFunc,
			}
		}
	}
	return wrapped
}
