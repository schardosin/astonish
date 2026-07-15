package tools

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/SAP/astonish/pkg/store"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type FleetTaskPostArgs struct {
	Title                string   `json:"title" jsonschema:"Short task title"`
	Description          string   `json:"description" jsonschema:"Detailed task description"`
	RequiredCapabilities []string `json:"required_capabilities,omitempty" jsonschema:"Capability names required to complete this task"`
	ParentTaskID         string   `json:"parent_task_id,omitempty" jsonschema:"Optional parent task UUID"`
}

type FleetTaskCompleteArgs struct {
	TaskID string         `json:"task_id" jsonschema:"Task UUID to complete"`
	Result map[string]any `json:"result,omitempty" jsonschema:"Structured completion result"`
}

type FleetTaskFailArgs struct {
	TaskID string `json:"task_id" jsonschema:"Task UUID to fail"`
	Reason string `json:"reason" jsonschema:"Reason the task failed"`
}

type FleetTaskResult struct {
	Status  string         `json:"status"`
	TaskID  string         `json:"task_id,omitempty"`
	Message string         `json:"message"`
	Task    map[string]any `json:"task,omitempty"`
}

func fleetTaskPost(ctx tool.Context, args FleetTaskPostArgs) (FleetTaskResult, error) {
	board, sessionID, err := fleetTaskBoardFromContext(ctx)
	if err != nil {
		return FleetTaskResult{Status: "error", Message: err.Error()}, nil
	}
	task, err := board.Post(ctx, store.FleetTask{
		SessionID:            sessionID,
		Title:                args.Title,
		Description:          args.Description,
		RequiredCapabilities: args.RequiredCapabilities,
		ParentTaskID:         args.ParentTaskID,
	})
	if err != nil {
		return FleetTaskResult{Status: "error", Message: err.Error()}, nil
	}
	if h := store.FleetTaskEventHandlerFromContext(ctx); h != nil {
		h("fleet_task_posted", *task)
	}
	return FleetTaskResult{
		Status:  "ok",
		TaskID:  task.ID.String(),
		Message: "Task posted.",
		Task:    fleetTaskResultMap(*task),
	}, nil
}

func fleetTaskComplete(ctx tool.Context, args FleetTaskCompleteArgs) (FleetTaskResult, error) {
	board, _, err := fleetTaskBoardFromContext(ctx)
	if err != nil {
		return FleetTaskResult{Status: "error", Message: err.Error()}, nil
	}
	id, err := uuid.Parse(args.TaskID)
	if err != nil {
		return FleetTaskResult{Status: "error", Message: "invalid task_id"}, nil
	}
	if err := board.Complete(ctx, id, args.Result); err != nil {
		return FleetTaskResult{Status: "error", TaskID: args.TaskID, Message: err.Error()}, nil
	}
	if h := store.FleetTaskEventHandlerFromContext(ctx); h != nil {
		h("fleet_task_completed", store.FleetTask{ID: id, Status: "done", Result: args.Result})
	}
	return FleetTaskResult{Status: "ok", TaskID: args.TaskID, Message: "Task completed."}, nil
}

func fleetTaskFail(ctx tool.Context, args FleetTaskFailArgs) (FleetTaskResult, error) {
	board, _, err := fleetTaskBoardFromContext(ctx)
	if err != nil {
		return FleetTaskResult{Status: "error", Message: err.Error()}, nil
	}
	id, err := uuid.Parse(args.TaskID)
	if err != nil {
		return FleetTaskResult{Status: "error", Message: "invalid task_id"}, nil
	}
	if err := board.Fail(ctx, id, args.Reason); err != nil {
		return FleetTaskResult{Status: "error", TaskID: args.TaskID, Message: err.Error()}, nil
	}
	if h := store.FleetTaskEventHandlerFromContext(ctx); h != nil {
		h("fleet_task_failed", store.FleetTask{ID: id, Status: "failed", Result: map[string]any{"reason": args.Reason}})
	}
	return FleetTaskResult{Status: "ok", TaskID: args.TaskID, Message: "Task failed."}, nil
}

func fleetTaskBoardFromContext(ctx tool.Context) (store.FleetTaskBoardStore, string, error) {
	board := store.FleetTaskBoardStoreFromContext(ctx)
	if board == nil {
		return nil, "", fmt.Errorf("fleet task board store is not available")
	}
	sessionID := store.SessionIDFromContext(ctx)
	if sessionID == "" {
		return nil, "", fmt.Errorf("fleet session id is not available")
	}
	return board, sessionID, nil
}

func fleetTaskResultMap(task store.FleetTask) map[string]any {
	return map[string]any{
		"id":                    task.ID.String(),
		"session_id":            task.SessionID,
		"title":                 task.Title,
		"description":           task.Description,
		"required_capabilities": task.RequiredCapabilities,
		"claimed_by":            task.ClaimedBy,
		"status":                task.Status,
		"parent_task_id":        task.ParentTaskID,
		"created_at":            task.CreatedAt,
		"updated_at":            task.UpdatedAt,
	}
}

func NewFleetTaskPostTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "fleet_task_post",
		Description: "Post a task to the current fleet session's durable task board for trackable work.",
	}, fleetTaskPost)
}

func NewFleetTaskCompleteTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "fleet_task_complete",
		Description: "Mark a task-board task complete with an optional structured result.",
	}, fleetTaskComplete)
}

func NewFleetTaskFailTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "fleet_task_fail",
		Description: "Mark a task-board task failed with a reason.",
	}, fleetTaskFail)
}

func GetFleetTaskTools() ([]tool.Tool, error) {
	postTool, err := NewFleetTaskPostTool()
	if err != nil {
		return nil, fmt.Errorf("fleet_task_post: %w", err)
	}
	completeTool, err := NewFleetTaskCompleteTool()
	if err != nil {
		return nil, fmt.Errorf("fleet_task_complete: %w", err)
	}
	failTool, err := NewFleetTaskFailTool()
	if err != nil {
		return nil, fmt.Errorf("fleet_task_fail: %w", err)
	}
	return []tool.Tool{postTool, completeTool, failTool}, nil
}
