package drill

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// triageToolExecutor is a package-level reference set by BuildTriageTools.
// Each triage tool handler closes over this to dispatch calls.
// This is the same pattern used by other host-side tools (e.g., sandbox_template_list_tool.go).
var triageToolExecutor ToolExecutor
var triageToolCtx context.Context

// BuildTriageTools creates ADK tool.Tool adapters for the triage agent.
// The tools delegate to the provided ToolExecutor, which routes calls to
// the appropriate backend (sandbox container for shell/file, host for browser).
func BuildTriageTools(ctx context.Context, executor ToolExecutor) ([]tool.Tool, error) {
	triageToolExecutor = executor
	triageToolCtx = ctx

	var tools []tool.Tool

	defs := []struct {
		name string
		desc string
	}{
		// Browser diagnostic tools
		{"browser_snapshot", "Get the accessibility tree of the current page. Returns the page structure as text. Use this to understand what the page looks like and find elements."},
		{"browser_take_screenshot", "Take a screenshot of the current page. Returns base64-encoded PNG image data."},
		{"browser_console_messages", "Get JavaScript console messages (errors, warnings, logs) from the browser. Essential for diagnosing JS errors."},
		{"browser_network_requests", "Get failed network requests from the browser. Essential for diagnosing API failures, 404s, CORS errors, etc."},
		{"browser_evaluate", "Evaluate a JavaScript expression in the browser page context. Use for inspecting DOM state, checking JS variables, etc."},

		// File/source tools
		{"read_file", "Read a file from the filesystem. Use to inspect source code, config files, or log files. Args: path (string), optional offset/limit."},
		{"grep_search", "Search file contents using a regex pattern. Args: pattern (string), optional path (directory to search in), optional include (file glob). Returns matching files and line numbers."},
		{"file_tree", "List files and directories as a tree. Args: path (string), optional depth (int)."},

		// Shell tools
		{"shell_command", "Run a shell command. Use to check process status, read logs, inspect system state. Args: command (string), optional timeout (int)."},
		{"process_list", "List running processes/PTY sessions."},
		{"process_read", "Read output from a running PTY session. Args: session_id (string), optional lines (int)."},
	}

	for _, d := range defs {
		t, err := newTriageTool(d.name, d.desc)
		if err != nil {
			return nil, fmt.Errorf("build triage tool %s: %w", d.name, err)
		}
		tools = append(tools, t)
	}

	return tools, nil
}

// TriageToolArgs is a generic args struct for triage tool adapters.
// All fields are optional — the triage agent provides whichever are relevant
// for the specific tool being called.
type TriageToolArgs struct {
	// Shell tools
	Command string `json:"command,omitempty" jsonschema:"Shell command to execute"`
	Timeout int    `json:"timeout,omitempty" jsonschema:"Command timeout in seconds"`

	// File tools
	Path    string `json:"path,omitempty" jsonschema:"File or directory path"`
	Pattern string `json:"pattern,omitempty" jsonschema:"Search pattern (regex for grep_search)"`
	Include string `json:"include,omitempty" jsonschema:"File glob pattern to include in search"`
	Offset  int    `json:"offset,omitempty" jsonschema:"Line offset for read_file"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Max lines for read_file"`
	Depth   int    `json:"depth,omitempty" jsonschema:"Tree depth for file_tree"`

	// Browser tools
	Expression string `json:"expression,omitempty" jsonschema:"JavaScript expression for browser_evaluate"`

	// Process tools
	SessionID string `json:"session_id,omitempty" jsonschema:"PTY session ID for process_read"`
	Lines     int    `json:"lines,omitempty" jsonschema:"Number of lines for process_read"`

	// Screenshot tools
	Ref string `json:"ref,omitempty" jsonschema:"Element ref for browser_take_screenshot"`
}

// TriageToolResult is the generic result from a triage tool.
type TriageToolResult struct {
	Status string `json:"status"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// newTriageTool creates a single triage tool adapter using functiontool.New.
func newTriageTool(name, description string) (tool.Tool, error) {
	// Create a handler that closes over the tool name
	toolName := name
	handler := func(_ tool.Context, args TriageToolArgs) (TriageToolResult, error) {
		if triageToolExecutor == nil {
			return TriageToolResult{
				Status: "error",
				Error:  "triage tool executor not initialized",
			}, nil
		}

		// Convert args struct to map for the executor
		argsMap := triageArgsToMap(toolName, args)

		result, err := triageToolExecutor.Execute(triageToolCtx, toolName, argsMap)
		if err != nil {
			return TriageToolResult{
				Status: "error",
				Error:  err.Error(),
			}, nil
		}

		// Convert result to string for the LLM
		output := ExtractOutput(result)
		if len(output) > 20480 {
			output = output[:20480] + "\n... (truncated to 20KB)"
		}

		return TriageToolResult{
			Status: "ok",
			Output: output,
		}, nil
	}

	return functiontool.New(functiontool.Config{
		Name:        name,
		Description: description,
	}, handler)
}

// triageArgsToMap converts a TriageToolArgs struct to a map, including only
// the fields relevant to the specific tool being called.
func triageArgsToMap(toolName string, args TriageToolArgs) map[string]interface{} {
	m := make(map[string]interface{})

	switch toolName {
	case "shell_command":
		if args.Command != "" {
			m["command"] = args.Command
		}
		if args.Timeout > 0 {
			m["timeout"] = args.Timeout
		}

	case "read_file":
		if args.Path != "" {
			m["path"] = args.Path
		}
		if args.Offset > 0 {
			m["offset"] = args.Offset
		}
		if args.Limit > 0 {
			m["limit"] = args.Limit
		}

	case "grep_search":
		if args.Pattern != "" {
			m["pattern"] = args.Pattern
		}
		if args.Path != "" {
			m["path"] = args.Path
		}
		if args.Include != "" {
			m["include"] = args.Include
		}

	case "file_tree":
		if args.Path != "" {
			m["path"] = args.Path
		}
		if args.Depth > 0 {
			m["depth"] = args.Depth
		}

	case "browser_snapshot":
		// No required args

	case "browser_take_screenshot":
		if args.Ref != "" {
			m["ref"] = args.Ref
		}

	case "browser_console_messages":
		// No required args

	case "browser_network_requests":
		// No required args

	case "browser_evaluate":
		if args.Expression != "" {
			m["expression"] = args.Expression
		}

	case "process_list":
		// No required args

	case "process_read":
		if args.SessionID != "" {
			m["session_id"] = args.SessionID
		}
		if args.Lines > 0 {
			m["lines"] = args.Lines
		}

	default:
		// Generic: marshal and unmarshal to get non-zero fields
		data, err := json.Marshal(args)
		if err == nil {
			json.Unmarshal(data, &m)
		}
	}

	return m
}
