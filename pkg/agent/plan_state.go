package agent

import (
	"strings"
	"sync"
)

// PlanState tracks the ordered steps from an announce_plan call, enabling
// automatic progression driven by sub-task lifecycle events (task_start,
// task_complete) with explicit plan_step binding between delegate tasks
// and plan steps.
//
// Each delegate task carries a plan_step field identifying which plan step
// it belongs to. A plan step is marked "running" when its first task starts,
// and "complete" only when ALL registered tasks for that step have completed.
type PlanState struct {
	mu    sync.Mutex
	goal  string
	steps []planStep

	// taskRegistry tracks which tasks belong to each plan step.
	// Key: step name (lowercase), Value: set of task names (lowercase).
	taskRegistry map[string]map[string]bool

	// completedTasks tracks which tasks have finished.
	// Key: step name (lowercase), Value: set of completed task names (lowercase).
	completedTasks map[string]map[string]bool
}

type planStep struct {
	name        string
	description string
	status      string // "pending", "running", "complete", "failed"
}

// NewPlanState creates a PlanState from an announce_plan call's step list.
func NewPlanState(goal string, steps []PlanStepInfo) *PlanState {
	ps := &PlanState{
		goal:           goal,
		steps:          make([]planStep, len(steps)),
		taskRegistry:   make(map[string]map[string]bool),
		completedTasks: make(map[string]map[string]bool),
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

// StartStep registers a task under a plan step and marks the step as "running"
// if it is currently "pending". The stepName is resolved either from the
// explicit plan_step field or via fallback prefix matching on taskName.
//
// Returns the matched step name (for SSE emission), or "" if no match or
// the step is already running/complete.
func (ps *PlanState) StartStep(stepName, taskName string) string {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	idx := ps.findStepLocked(stepName)
	if idx < 0 {
		return ""
	}

	sn := strings.ToLower(ps.steps[idx].name)
	tn := strings.ToLower(taskName)

	// Register this task under the step
	if ps.taskRegistry[sn] == nil {
		ps.taskRegistry[sn] = make(map[string]bool)
	}
	ps.taskRegistry[sn][tn] = true

	// Mark step running if pending
	if ps.steps[idx].status == "pending" {
		ps.steps[idx].status = "running"
		return ps.steps[idx].name
	}
	return "" // already running or complete — no transition to emit
}

// CompleteTask marks a task as done within its plan step. If ALL registered
// tasks for that step are now complete, the step itself is marked "complete".
//
// Returns the step name if the step transitioned to "complete", or "" otherwise.
func (ps *PlanState) CompleteTask(stepName, taskName string) string {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	idx := ps.findStepLocked(stepName)
	if idx < 0 {
		return ""
	}
	if ps.steps[idx].status != "running" {
		return "" // not running — nothing to complete
	}

	sn := strings.ToLower(ps.steps[idx].name)
	tn := strings.ToLower(taskName)

	// Record this task as completed
	if ps.completedTasks[sn] == nil {
		ps.completedTasks[sn] = make(map[string]bool)
	}
	ps.completedTasks[sn][tn] = true

	// Check if ALL registered tasks for this step are done
	registered := ps.taskRegistry[sn]
	completed := ps.completedTasks[sn]
	if len(registered) > 0 && len(completed) >= len(registered) {
		allDone := true
		for task := range registered {
			if !completed[task] {
				allDone = false
				break
			}
		}
		if allDone {
			ps.steps[idx].status = "complete"
			return ps.steps[idx].name
		}
	}
	return "" // not all tasks done yet
}

// ResolveStepName returns the plan step name for a given explicit plan_step
// value and task name. If planStep is non-empty, it uses exact matching.
// If planStep is empty, it falls back to prefix matching on taskName.
// Returns "" if no match found.
func (ps *PlanState) ResolveStepName(planStep, taskName string) string {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if planStep != "" {
		// Exact match on plan_step
		idx := ps.findStepExactLocked(planStep)
		if idx >= 0 {
			return ps.steps[idx].name
		}
		return ""
	}

	// Fallback: prefix matching on taskName
	idx := ps.matchStepByPrefixLocked(taskName)
	if idx >= 0 {
		return ps.steps[idx].name
	}
	return ""
}

// findStepLocked finds a step by exact case-insensitive name match.
// Must be called with ps.mu held.
func (ps *PlanState) findStepExactLocked(name string) int {
	target := strings.ToLower(name)
	for i, s := range ps.steps {
		if strings.ToLower(s.name) == target {
			return i
		}
	}
	return -1
}

// findStepLocked tries exact match first, then prefix match.
// Must be called with ps.mu held.
func (ps *PlanState) findStepLocked(name string) int {
	// Try exact match first
	idx := ps.findStepExactLocked(name)
	if idx >= 0 {
		return idx
	}
	// Fall back to prefix match
	return ps.matchStepByPrefixLocked(name)
}

// matchStepByPrefixLocked finds the best matching plan step index using
// prefix matching on task name. Returns -1 if no match.
// Must be called with ps.mu held.
func (ps *PlanState) matchStepByPrefixLocked(taskName string) int {
	bestIdx := -1
	bestLen := 0

	tn := strings.ToLower(taskName)
	for i, s := range ps.steps {
		sn := strings.ToLower(s.name)
		if strings.HasPrefix(tn, sn) || strings.HasPrefix(sn, tn) {
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
