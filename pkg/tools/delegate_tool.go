package tools

import (
	"fmt"
	"strings"

	"github.com/schardosin/astonish/pkg/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// subAgentManagerVar holds the sub-agent manager reference.
// Set by the launcher via SetSubAgentManager.
var subAgentManagerVar *agent.SubAgentManager

// SetSubAgentManager registers the sub-agent manager for the delegate_tasks tool.
func SetSubAgentManager(mgr *agent.SubAgentManager) {
	subAgentManagerVar = mgr
}

// GetSubAgentManager returns the sub-agent manager.
// Used by the fleet session API handlers to create fleet sessions.
func GetSubAgentManager() *agent.SubAgentManager {
	return subAgentManagerVar
}

// --- delegate_tasks tool ---

// SubTaskInput describes a single sub-task to delegate.
type SubTaskInput struct {
	Name         string   `json:"name" jsonschema:"Short identifier for this sub-agent (e.g., 'researcher', 'code-reviewer', 'api-tester'). Must be unique within the delegation call."`
	Task         string   `json:"task" jsonschema:"Clear description of what the sub-agent should accomplish. Be specific about the expected output."`
	Instructions string   `json:"instructions,omitempty" jsonschema:"Additional context or constraints for this sub-agent. Include relevant file paths, API details, or formatting requirements."`
	Tools        []string `json:"tools,omitempty" jsonschema:"Specific tool names this sub-agent should use (e.g., ['grep_search', 'read_file']). If empty, the sub-agent gets all safe tools."`
}

// DelegateTasksArgs is the input schema for the delegate_tasks tool.
type DelegateTasksArgs struct {
	Tasks []SubTaskInput `json:"tasks" jsonschema:"Array of sub-tasks to execute in parallel. Each task gets its own isolated session and tool context."`
}

// SubTaskResultItem holds the result of a single delegated sub-task.
type SubTaskResultItem struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Result    string `json:"result,omitempty"`
	ToolCalls int    `json:"tool_calls"`
	Duration  string `json:"duration"`
	Error     string `json:"error,omitempty"`
}

// DelegateTasksResult is the output schema for the delegate_tasks tool.
type DelegateTasksResult struct {
	Status  string              `json:"status"`
	Results []SubTaskResultItem `json:"results"`
	Summary string              `json:"summary"`
}

func delegateTasks(ctx tool.Context, args DelegateTasksArgs) (DelegateTasksResult, error) {
	if subAgentManagerVar == nil {
		return DelegateTasksResult{
			Status:  "error",
			Summary: "Sub-agent system is not available",
		}, nil
	}

	if len(args.Tasks) == 0 {
		return DelegateTasksResult{
			Status:  "error",
			Summary: "No tasks provided",
		}, nil
	}

	if len(args.Tasks) > 10 {
		return DelegateTasksResult{
			Status:  "error",
			Summary: "Too many tasks (max 10 per delegation call)",
		}, nil
	}

	// Convert tool inputs to SubAgentTasks
	tasks := make([]agent.SubAgentTask, len(args.Tasks))
	for i, input := range args.Tasks {
		tasks[i] = agent.SubAgentTask{
			Name:         input.Name,
			Description:  input.Task,
			Instructions: input.Instructions,
			ToolFilter:   input.Tools,
			ParentID:     ctx.SessionID(),
			ParentDepth:  0, // delegate_tasks creates top-level sub-agents
		}
	}

	// Execute all tasks via the SubAgentManager
	results := subAgentManagerVar.RunTasks(ctx, tasks)

	// Build response
	resultItems := make([]SubTaskResultItem, len(results))
	successCount := 0
	for i, r := range results {
		resultItems[i] = SubTaskResultItem{
			Name:      r.Name,
			Status:    r.Status,
			Result:    r.Result,
			ToolCalls: r.ToolCalls,
			Duration:  r.Duration.Round(100 * 1e6).String(), // Round to 100ms
			Error:     r.Error,
		}
		if r.Status == "success" {
			successCount++
		}
	}

	// Build summary
	var summaryParts []string
	summaryParts = append(summaryParts, fmt.Sprintf("%d/%d tasks completed successfully", successCount, len(results)))
	for _, r := range results {
		if r.Status != "success" {
			summaryParts = append(summaryParts, fmt.Sprintf("  %s: %s — %s", r.Name, r.Status, r.Error))
		}
	}

	overallStatus := "success"
	if successCount == 0 {
		overallStatus = "error"
	} else if successCount < len(results) {
		overallStatus = "partial"
	}

	return DelegateTasksResult{
		Status:  overallStatus,
		Results: resultItems,
		Summary: strings.Join(summaryParts, "\n"),
	}, nil
}

// NewDelegateTasksTool creates the delegate_tasks tool.
func NewDelegateTasksTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "delegate_tasks",
		Description: `Delegate multiple tasks to parallel sub-agents. Only use for 3+ independent tasks that each require multiple tool calls. For 1-2 tasks, call tools directly. Each sub-agent gets an isolated session, read-only memory, filtered tools, and a 5-minute timeout. Max 10 tasks per call.`,
	}, delegateTasks)
}

// GetDelegateTasksTool returns the delegate_tasks tool (or nil if unavailable).
func GetDelegateTasksTool() (tool.Tool, error) {
	return NewDelegateTasksTool()
}
