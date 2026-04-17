package agent

import "sync"

// PlanState tracks the ordered steps from an announce_plan call, enabling
// automatic progression in AfterToolCallback without requiring the LLM
// to call update_plan (which wastes a full context round-trip per update).
type PlanState struct {
	mu    sync.Mutex
	goal  string
	steps []planStep
}

type planStep struct {
	name        string
	description string
	status      string // "pending", "running", "complete", "failed"
}

// NewPlanState creates a PlanState from an announce_plan call's step list.
func NewPlanState(goal string, steps []PlanStepInfo) *PlanState {
	ps := &PlanState{
		goal:  goal,
		steps: make([]planStep, len(steps)),
	}
	for i, s := range steps {
		ps.steps[i] = planStep{
			name:        s.Name,
			description: s.Description,
			status:      "pending",
		}
	}
	return ps
}

// AdvanceOnToolStart is called when a non-plan tool begins executing.
// If no step is currently running, it marks the next pending step as running
// and returns the step name (for SSE emission). Returns "" if no step to advance.
func (ps *PlanState) AdvanceOnToolStart() string {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Check if any step is already running
	for _, s := range ps.steps {
		if s.status == "running" {
			return "" // a step is already in progress
		}
	}

	// Mark the next pending step as running
	for i := range ps.steps {
		if ps.steps[i].status == "pending" {
			ps.steps[i].status = "running"
			return ps.steps[i].name
		}
	}
	return ""
}

// CompleteCurrentAndAdvance marks the current running step as complete,
// then marks the next pending step as running. Returns (completedName, startedName).
// Either or both may be empty if there's nothing to transition.
func (ps *PlanState) CompleteCurrentAndAdvance() (completed string, started string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Find and complete the running step
	for i := range ps.steps {
		if ps.steps[i].status == "running" {
			ps.steps[i].status = "complete"
			completed = ps.steps[i].name
			break
		}
	}

	// Start the next pending step
	for i := range ps.steps {
		if ps.steps[i].status == "pending" {
			ps.steps[i].status = "running"
			started = ps.steps[i].name
			return
		}
	}
	return
}

// CompleteAll marks all remaining running/pending steps as complete.
// Returns the names of steps that were transitioned.
func (ps *PlanState) CompleteAll() []string {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	var completed []string
	for i := range ps.steps {
		if ps.steps[i].status == "running" || ps.steps[i].status == "pending" {
			ps.steps[i].status = "complete"
			completed = append(completed, ps.steps[i].name)
		}
	}
	return completed
}

// HasPendingSteps returns true if any steps are still pending or running.
func (ps *PlanState) HasPendingSteps() bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	for _, s := range ps.steps {
		if s.status == "pending" || s.status == "running" {
			return true
		}
	}
	return false
}
