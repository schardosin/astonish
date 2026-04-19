package agent

import (
	"strings"
	"sync"
)

// PlanState tracks the ordered steps from an announce_plan call, enabling
// automatic progression driven by sub-task lifecycle events (task_start,
// task_complete) with prefix-based name matching between delegate task
// names and plan step names.
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

// AdvanceOnToolStart is called when a non-delegate tool begins executing.
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

// StartStepByName finds a plan step matching the given task name via prefix
// matching and marks it as "running" if it is currently "pending".
// Returns the matched step name, or "" if no match or already running/complete.
//
// Prefix matching: taskName starts with stepName, or stepName starts with
// taskName. When multiple steps match, the longest step name wins to avoid
// ambiguous matches (e.g., "analyze-astonish" wins over "analyze" for task
// "analyze-astonish-core").
func (ps *PlanState) StartStepByName(taskName string) string {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	idx := ps.matchStepLocked(taskName)
	if idx < 0 {
		return ""
	}
	if ps.steps[idx].status != "pending" {
		return "" // already running or complete
	}
	ps.steps[idx].status = "running"
	return ps.steps[idx].name
}

// CompleteStepByName finds a plan step matching the given task name via prefix
// matching and marks it as "complete" if it is currently "running".
// Returns the matched step name, or "" if no match or not running.
func (ps *PlanState) CompleteStepByName(taskName string) string {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	idx := ps.matchStepLocked(taskName)
	if idx < 0 {
		return ""
	}
	if ps.steps[idx].status != "running" {
		return "" // not currently running — nothing to complete
	}
	ps.steps[idx].status = "complete"
	return ps.steps[idx].name
}

// matchStepLocked finds the best matching plan step index for the given task
// name using prefix matching. Returns -1 if no match.
// Must be called with ps.mu held.
func (ps *PlanState) matchStepLocked(taskName string) int {
	bestIdx := -1
	bestLen := 0

	tn := strings.ToLower(taskName)
	for i, s := range ps.steps {
		sn := strings.ToLower(s.name)
		// Prefix match: task name starts with step name, or vice versa
		if strings.HasPrefix(tn, sn) || strings.HasPrefix(sn, tn) {
			// Prefer the longest matching step name to avoid ambiguity
			if len(sn) > bestLen {
				bestIdx = i
				bestLen = len(sn)
			}
		}
	}
	return bestIdx
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
