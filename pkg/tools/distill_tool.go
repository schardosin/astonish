package tools

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// DistillAccess provides access to flow distillation operations without
// importing the agent package directly (breaking the import cycle).
// ChatAgent satisfies this interface.
type DistillAccess interface {
	// PreviewDistill analyzes the session traces and identifies the primary task.
	// Returns a description of the identified task.
	PreviewDistill(ctx context.Context, ds DistillSession) (string, error)
	// ConfirmAndDistill runs distillation using traces identified by PreviewDistill.
	// The print function receives status/result text.
	ConfirmAndDistill(ctx context.Context, ds DistillSession, print func(string)) error
}

// DistillSession identifies a session for distillation, providing the
// minimum information needed to locate session traces.
type DistillSession struct {
	SessionID string
	AppName   string
	UserID    string
}

// distillAccessVar holds the DistillAccess implementation.
// Set by the daemon/launcher via SetDistillAccess.
var distillAccessVar DistillAccess

// SetDistillAccess registers the distill access implementation.
// Called after ChatAgent initialization.
func SetDistillAccess(da DistillAccess) {
	distillAccessVar = da
}

// --- distill_flow tool ---

type DistillFlowArgs struct {
	// No arguments needed — distills from current session traces.
}

type DistillFlowResult struct {
	Status   string `json:"status"`
	FlowName string `json:"flow_name,omitempty"`
	Message  string `json:"message"`
}

func distillFlow(ctx tool.Context, args DistillFlowArgs) (DistillFlowResult, error) {
	if distillAccessVar == nil {
		return DistillFlowResult{
			Status:  "error",
			Message: "Flow distillation is not available.",
		}, nil
	}

	// Build session info from the tool context
	ds := DistillSession{
		SessionID: ctx.SessionID(),
		AppName:   ctx.AppName(),
		UserID:    ctx.UserID(),
	}

	// Phase 1: Preview — identify the task from traces
	description, err := distillAccessVar.PreviewDistill(ctx, ds)
	if err != nil {
		return DistillFlowResult{
			Status:  "error",
			Message: fmt.Sprintf("Nothing to distill: %v", err),
		}, nil
	}

	// Phase 2: Distill — generate the flow YAML
	var output strings.Builder
	distillErr := distillAccessVar.ConfirmAndDistill(ctx, ds, func(s string) {
		output.WriteString(s)
	})
	if distillErr != nil {
		return DistillFlowResult{
			Status:  "error",
			Message: fmt.Sprintf("Distillation failed: %v. Task identified: %s", distillErr, description),
		}, nil
	}

	// Extract the flow name from the output (the output contains "Flow saved as `path`")
	flowName := extractFlowName(output.String())

	return DistillFlowResult{
		Status:   "success",
		FlowName: flowName,
		Message:  output.String(),
	}, nil
}

// extractFlowName extracts the flow name from the distillation output.
// The output typically contains: "Flow saved as `/path/to/flow_name.yaml`"
// and a run command: "astonish flows run flow_name ..."
func extractFlowName(output string) string {
	// Look for "astonish flows run <name>" pattern — most reliable
	if idx := strings.Index(output, "astonish flows run "); idx >= 0 {
		rest := output[idx+len("astonish flows run "):]
		// The flow name ends at the next space or newline
		end := strings.IndexAny(rest, " \n\t")
		if end > 0 {
			return rest[:end]
		}
		return strings.TrimSpace(rest)
	}

	// Fallback: look for "Flow saved as `<path>`"
	if idx := strings.Index(output, "Flow saved as `"); idx >= 0 {
		rest := output[idx+len("Flow saved as `"):]
		end := strings.Index(rest, "`")
		if end > 0 {
			path := rest[:end]
			// Extract just the filename without extension and path
			parts := strings.Split(path, "/")
			filename := parts[len(parts)-1]
			return strings.TrimSuffix(filename, ".yaml")
		}
	}

	return ""
}

// NewDistillFlowTool creates the distill_flow tool.
func NewDistillFlowTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "distill_flow",
		Description: `Distill the current conversation into a reusable flow.

This analyzes the tool calls made in the current session and creates a saved flow YAML file that can be replayed deterministically.

Use this tool when:
- The user wants to schedule a task as "routine" but no saved flow exists yet for what was just done in the conversation.
- You need to create a flow from a task you just performed so it can be reused.

The tool automatically identifies the task from the conversation traces, extracts the tool calls, and generates a reusable flow file.

Returns the flow name which can then be used with schedule_job in routine mode.`,
	}, distillFlow)
}

// GetDistillTools returns the distill_flow tool.
func GetDistillTools() ([]tool.Tool, error) {
	t, err := NewDistillFlowTool()
	if err != nil {
		return nil, fmt.Errorf("distill_flow: %w", err)
	}
	return []tool.Tool{t}, nil
}
