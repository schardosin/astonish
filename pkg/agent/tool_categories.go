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
	"skill_lookup":              true,
	"process_list":              true,
	"process_read":              true,
	// Browser observation tools (read-only)
	"browser_snapshot":         true,
	"browser_take_screenshot":  true,
	"browser_console_messages": true,
	"browser_network_requests": true,
	// Browser navigation & interaction (operates in sandboxed browser)
	"browser_navigate":      true,
	"browser_navigate_back": true,
	"browser_click":         true,
	"browser_type":          true,
	"browser_hover":         true,
	"browser_drag":          true,
	"browser_press_key":     true,
	"browser_select_option": true,
	"browser_fill_form":     true,
	// Browser management
	"browser_tabs":          true,
	"browser_close":         true,
	"browser_resize":        true,
	"browser_wait_for":      true,
	"browser_file_upload":   true,
	"browser_handle_dialog": true,
	// Browser advanced
	"browser_evaluate":      true,
	"browser_run_code":      true,
	"browser_pdf":           true,
	"browser_response_body": true,
	// Browser state & emulation (Phase 2)
	"browser_cookies":         true,
	"browser_storage":         true,
	"browser_set_offline":     true,
	"browser_set_headers":     true,
	"browser_set_credentials": true,
	"browser_set_geolocation": true,
	"browser_set_media":       true,
	"browser_set_timezone":    true,
	"browser_set_locale":      true,
	"browser_set_device":      true,
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
