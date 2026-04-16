package tools

import (
	"github.com/schardosin/astonish/pkg/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// planProgressCallback is set by the launcher to emit plan events through
// the same SubTaskProgress pipeline used by delegate_tasks. This allows
// plan events to flow through ChatAgent.SubTaskProgressCallback → ChatRunner
// → SSE → frontend without any new callback wiring.
var planProgressCallback func(event agent.SubTaskProgressEvent)

// SetPlanProgressCallback sets the callback used by plan tools to emit SSE events.
func SetPlanProgressCallback(fn func(event agent.SubTaskProgressEvent)) {
	planProgressCallback = fn
}

// --- announce_plan tool ---

// PlanStepInput describes a single step in a plan.
type PlanStepInput struct {
	Name        string `json:"name" jsonschema:"Short identifier for this step (e.g., 'explore-repos', 'analyze-implementations', 'write-report'). Used to reference the step in update_plan calls."`
	Description string `json:"description" jsonschema:"Human-readable description of what this step accomplishes (e.g., 'Explore both repository structures and dependencies')."`
}

// AnnouncePlanArgs is the input schema for the announce_plan tool.
type AnnouncePlanArgs struct {
	Goal  string          `json:"goal" jsonschema:"A concise title for the overall plan (e.g., 'Source-Level GitHub Comparison: astonish vs openclaw'). Displayed as the plan header."`
	Steps []PlanStepInput `json:"steps" jsonschema:"Ordered list of high-level steps to complete the goal. Each step should represent a distinct phase of work. Keep it to 3-7 steps."`
}

// AnnouncePlanResult is the output of the announce_plan tool.
type AnnouncePlanResult struct {
	Status string `json:"status"`
}

func announcePlan(_ tool.Context, args AnnouncePlanArgs) (AnnouncePlanResult, error) {
	if planProgressCallback == nil {
		return AnnouncePlanResult{Status: "ok"}, nil
	}

	steps := make([]agent.PlanStepInfo, len(args.Steps))
	for i, s := range args.Steps {
		steps[i] = agent.PlanStepInfo{
			Name:        s.Name,
			Description: s.Description,
		}
	}

	planProgressCallback(agent.SubTaskProgressEvent{
		Type:      "plan_announced",
		PlanGoal:  args.Goal,
		PlanSteps: steps,
	})

	return AnnouncePlanResult{Status: "ok"}, nil
}

// NewAnnouncePlanTool creates the announce_plan tool.
func NewAnnouncePlanTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "announce_plan",
		Description: `Announce a structured plan before starting multi-step work. Call this BEFORE your first delegate_tasks call to show the user your high-level approach. The plan appears as a visible checklist in the UI. Keep steps high-level (3-7 steps) — each step represents a distinct phase, not individual tool calls. Use update_plan to mark steps as running/complete as you work through them.`,
	}, announcePlan)
}

// --- update_plan tool ---

// UpdatePlanArgs is the input schema for the update_plan tool.
type UpdatePlanArgs struct {
	Step   string `json:"step" jsonschema:"The step name to update (must match a name from the announce_plan call)."`
	Status string `json:"status" jsonschema:"New status: 'running', 'complete', or 'failed'."`
}

// UpdatePlanResult is the output of the update_plan tool.
type UpdatePlanResult struct {
	Status string `json:"status"`
}

func updatePlan(_ tool.Context, args UpdatePlanArgs) (UpdatePlanResult, error) {
	if planProgressCallback == nil {
		return UpdatePlanResult{Status: "ok"}, nil
	}

	planProgressCallback(agent.SubTaskProgressEvent{
		Type:       "plan_step_update",
		StepName:   args.Step,
		StepStatus: args.Status,
	})

	return UpdatePlanResult{Status: "ok"}, nil
}

// NewUpdatePlanTool creates the update_plan tool.
func NewUpdatePlanTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "update_plan",
		Description: `Update the status of a plan step. Use this to mark steps as 'running' when you start working on them, 'complete' when done, or 'failed' if they cannot be completed. Only needed for steps you handle yourself (non-delegated work like synthesis or report writing) — delegated steps are auto-tracked.`,
	}, updatePlan)
}
