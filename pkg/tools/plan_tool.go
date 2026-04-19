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

// planStateCallback is set by the launcher to store the announced plan in
// ChatAgent.PlanState, enabling auto-progression of plan steps in
// AfterToolCallback without requiring update_plan LLM round-trips.
var planStateCallback func(goal string, steps []agent.PlanStepInfo)

// SetPlanProgressCallback sets the callback used by plan tools to emit SSE events.
func SetPlanProgressCallback(fn func(event agent.SubTaskProgressEvent)) {
	planProgressCallback = fn
}

// SetPlanStateCallback sets the callback used by announce_plan to store the
// plan in ChatAgent for auto-progression.
func SetPlanStateCallback(fn func(goal string, steps []agent.PlanStepInfo)) {
	planStateCallback = fn
}

// --- announce_plan tool ---

// PlanStepInput describes a single step in a plan.
type PlanStepInput struct {
	Name        string `json:"name" jsonschema:"Short identifier for this step (e.g., 'explore-repos', 'analyze-implementations', 'write-report')."`
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
	steps := make([]agent.PlanStepInfo, len(args.Steps))
	for i, s := range args.Steps {
		steps[i] = agent.PlanStepInfo{
			Name:        s.Name,
			Description: s.Description,
		}
	}

	// Store the plan in ChatAgent for auto-progression.
	if planStateCallback != nil {
		planStateCallback(args.Goal, steps)
	}

	// Emit SSE event for frontend rendering.
	if planProgressCallback != nil {
		planProgressCallback(agent.SubTaskProgressEvent{
			Type:      "plan_announced",
			PlanGoal:  args.Goal,
			PlanSteps: steps,
		})
	}

	return AnnouncePlanResult{Status: "ok"}, nil
}

// NewAnnouncePlanTool creates the announce_plan tool.
func NewAnnouncePlanTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "announce_plan",
		Description: `Announce a structured plan before starting multi-step work. Call this BEFORE your first delegate_tasks call to show the user your high-level approach. The plan appears as a visible checklist in the UI — steps are automatically marked running/complete as sub-tasks execute, so you do not need to update them manually. Keep steps high-level (3-7 steps) — each step represents a distinct phase, not individual tool calls.

IMPORTANT: Plan step names are used for progress tracking via prefix matching. When you later call delegate_tasks, use your plan step names as prefixes in the task names. For example, if your plan has a step named "analyze-astonish", name your delegate tasks "analyze-astonish-core", "analyze-astonish-memory", etc. This ensures progress is tracked accurately.`,
	}, announcePlan)
}
