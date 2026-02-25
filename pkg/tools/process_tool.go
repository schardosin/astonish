package tools

import (
	"fmt"
	"strings"
	"time"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// --- process_read tool ---

type ProcessReadArgs struct {
	SessionID string `json:"session_id" jsonschema:"The session ID returned by shell_command (from session_id field)"`
	Offset    int    `json:"offset,omitempty" jsonschema:"Byte offset to start reading from (0 = beginning). Use the total_bytes from a previous read to get only new output."`
}

type ProcessReadResult struct {
	Output     string `json:"output"`
	TotalBytes int    `json:"total_bytes"`
	Running    bool   `json:"running"`
	ExitCode   *int   `json:"exit_code,omitempty"`
}

func processRead(_ tool.Context, args ProcessReadArgs) (ProcessReadResult, error) {
	pm := GetProcessManager()
	sess := pm.Get(args.SessionID)
	if sess == nil {
		return ProcessReadResult{}, fmt.Errorf("session %q not found", args.SessionID)
	}

	data := sess.Output.Bytes()
	running := sess.IsRunning()

	var output string
	if args.Offset > 0 && args.Offset < len(data) {
		output = string(data[args.Offset:])
	} else if args.Offset >= len(data) {
		output = ""
	} else {
		output = string(data)
	}

	result := ProcessReadResult{
		Output:     output,
		TotalBytes: len(data),
		Running:    running,
	}

	if !running {
		result.ExitCode = sess.ExitCode
	}

	return result, nil
}

// --- process_write tool ---

type ProcessWriteArgs struct {
	SessionID string `json:"session_id" jsonschema:"The session ID of the process to write to"`
	Input     string `json:"input" jsonschema:"Text to send to the process stdin. Include newline (\\n) to press Enter."`
}

type ProcessWriteResult struct {
	BytesWritten int    `json:"bytes_written"`
	Output       string `json:"output"`
	TotalBytes   int    `json:"total_bytes"`
	Running      bool   `json:"running"`
	ExitCode     *int   `json:"exit_code,omitempty"`
}

func processWrite(_ tool.Context, args ProcessWriteArgs) (ProcessWriteResult, error) {
	pm := GetProcessManager()
	sess := pm.Get(args.SessionID)
	if sess == nil {
		return ProcessWriteResult{}, fmt.Errorf("session %q not found", args.SessionID)
	}

	if !sess.IsRunning() {
		return ProcessWriteResult{}, fmt.Errorf("process has exited (session %q)", args.SessionID)
	}

	n, err := sess.Write([]byte(args.Input))
	if err != nil {
		return ProcessWriteResult{}, fmt.Errorf("write to process failed: %w", err)
	}

	// Wait briefly for the process to respond to the input
	time.Sleep(500 * time.Millisecond)

	data := sess.Output.Bytes()
	running := sess.IsRunning()

	result := ProcessWriteResult{
		BytesWritten: n,
		Output:       string(data),
		TotalBytes:   len(data),
		Running:      running,
	}

	if !running {
		result.ExitCode = sess.ExitCode
	}

	return result, nil
}

// --- process_list tool ---

type ProcessListArgs struct {
	Filter string `json:"filter,omitempty" jsonschema:"Optional filter to match sessions by command or ID"`
}

type ProcessSummary struct {
	SessionID string `json:"session_id"`
	Command   string `json:"command"`
	PID       int    `json:"pid"`
	Running   bool   `json:"running"`
	Duration  string `json:"duration"`
	ExitCode  *int   `json:"exit_code,omitempty"`
}

type ProcessListResult struct {
	Sessions []ProcessSummary `json:"sessions"`
	Count    int              `json:"count"`
}

func processList(_ tool.Context, args ProcessListArgs) (ProcessListResult, error) {
	pm := GetProcessManager()
	sessions := pm.List()

	summaries := make([]ProcessSummary, 0, len(sessions))
	for _, sess := range sessions {
		if args.Filter != "" && !strings.Contains(sess.Command, args.Filter) && !strings.Contains(sess.ID, args.Filter) {
			continue
		}

		var duration string
		if sess.IsRunning() {
			duration = time.Since(sess.StartedAt).Truncate(time.Second).String()
		} else if sess.EndedAt != nil {
			duration = sess.EndedAt.Sub(sess.StartedAt).Truncate(time.Second).String()
		}

		summary := ProcessSummary{
			SessionID: sess.ID,
			Command:   sess.Command,
			PID:       sess.PID,
			Running:   sess.IsRunning(),
			Duration:  duration,
		}
		if !sess.IsRunning() {
			summary.ExitCode = sess.ExitCode
		}
		summaries = append(summaries, summary)
	}

	return ProcessListResult{
		Sessions: summaries,
		Count:    len(summaries),
	}, nil
}

// --- process_kill tool ---

type ProcessKillArgs struct {
	SessionID string `json:"session_id" jsonschema:"The session ID of the process to kill"`
}

type ProcessKillResult struct {
	Status   string `json:"status"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

func processKill(_ tool.Context, args ProcessKillArgs) (ProcessKillResult, error) {
	pm := GetProcessManager()
	sess := pm.Get(args.SessionID)
	if sess == nil {
		return ProcessKillResult{}, fmt.Errorf("session %q not found", args.SessionID)
	}

	if !sess.IsRunning() {
		return ProcessKillResult{
			Status:   "already_exited",
			ExitCode: sess.ExitCode,
		}, nil
	}

	if err := pm.Kill(args.SessionID); err != nil {
		return ProcessKillResult{}, fmt.Errorf("failed to kill process: %w", err)
	}

	return ProcessKillResult{
		Status:   "killed",
		ExitCode: sess.ExitCode,
	}, nil
}

// --- Tool constructors ---

func NewProcessReadTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "process_read",
		Description: `Read output from a running or completed process session.

Use this after shell_command returns waiting_for_input=true or when checking on a background process.
Pass offset=<total_bytes from previous read> to get only new output since your last read.`,
	}, processRead)
}

func NewProcessWriteTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "process_write",
		Description: `Send input to a running process session.

Use this to respond to interactive prompts (SSH host key verification, passwords, confirmations, etc.).
Always include a newline (\n) at the end to press Enter.
Returns the current output after writing, so you can see the process's response.`,
	}, processWrite)
}

func NewProcessListTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "process_list",
		Description: "List all active and recent process sessions. Shows session IDs, commands, running status, and duration. Optionally filter by command or session ID.",
	}, processList)
}

func NewProcessKillTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "process_kill",
		Description: "Kill a running process session. Sends SIGTERM first, then SIGKILL after 5 seconds if the process doesn't exit.",
	}, processKill)
}

// GetProcessTools returns all process management tools.
func GetProcessTools() ([]tool.Tool, error) {
	var tools []tool.Tool

	readTool, err := NewProcessReadTool()
	if err != nil {
		return nil, fmt.Errorf("process_read: %w", err)
	}
	tools = append(tools, readTool)

	writeTool, err := NewProcessWriteTool()
	if err != nil {
		return nil, fmt.Errorf("process_write: %w", err)
	}
	tools = append(tools, writeTool)

	listTool, err := NewProcessListTool()
	if err != nil {
		return nil, fmt.Errorf("process_list: %w", err)
	}
	tools = append(tools, listTool)

	killTool, err := NewProcessKillTool()
	if err != nil {
		return nil, fmt.Errorf("process_kill: %w", err)
	}
	tools = append(tools, killTool)

	return tools, nil
}
