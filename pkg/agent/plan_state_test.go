package agent

import (
	"testing"
)

func TestPlanState_AdvanceOnToolStart(t *testing.T) {
	ps := NewPlanState("test", []PlanStepInfo{
		{Name: "clone-repos", Description: "Clone"},
		{Name: "analyze", Description: "Analyze"},
	})

	// First non-delegate tool starts step 1
	if got := ps.AdvanceOnToolStart(); got != "clone-repos" {
		t.Errorf("AdvanceOnToolStart() = %q, want %q", got, "clone-repos")
	}

	// While step 1 is running, no advancement
	if got := ps.AdvanceOnToolStart(); got != "" {
		t.Errorf("AdvanceOnToolStart() = %q, want empty (step already running)", got)
	}
}

func TestPlanState_StartStepByName_PrefixMatch(t *testing.T) {
	ps := NewPlanState("test", []PlanStepInfo{
		{Name: "clone-repos", Description: "Clone"},
		{Name: "analyze-astonish", Description: "Analyze Astonish"},
		{Name: "analyze-openclaw", Description: "Analyze OpenClaw"},
		{Name: "compare-shared", Description: "Compare"},
		{Name: "write-report", Description: "Report"},
	})

	tests := []struct {
		taskName string
		wantStep string
		note     string
	}{
		{"analyze-astonish-core", "analyze-astonish", "task name starts with step name"},
		{"analyze-astonish-memory", "", "step already running, second task is no-op"},
		{"analyze-openclaw-core", "analyze-openclaw", "different step prefix"},
		{"compare-shared-features", "compare-shared", "compare prefix match"},
		{"write-report", "write-report", "exact match"},
	}

	for _, tt := range tests {
		got := ps.StartStepByName(tt.taskName)
		if got != tt.wantStep {
			t.Errorf("StartStepByName(%q) = %q, want %q (%s)", tt.taskName, got, tt.wantStep, tt.note)
		}
	}
}

func TestPlanState_CompleteStepByName(t *testing.T) {
	ps := NewPlanState("test", []PlanStepInfo{
		{Name: "analyze-astonish", Description: "Analyze Astonish"},
		{Name: "analyze-openclaw", Description: "Analyze OpenClaw"},
	})

	// Start both steps
	ps.StartStepByName("analyze-astonish-core")
	ps.StartStepByName("analyze-openclaw-core")

	// Complete astonish via a different task name with same prefix
	if got := ps.CompleteStepByName("analyze-astonish-memory"); got != "analyze-astonish" {
		t.Errorf("CompleteStepByName() = %q, want %q", got, "analyze-astonish")
	}

	// Completing again should return "" (already complete)
	if got := ps.CompleteStepByName("analyze-astonish-browser"); got != "" {
		t.Errorf("CompleteStepByName() = %q, want empty (already complete)", got)
	}

	// Complete openclaw
	if got := ps.CompleteStepByName("analyze-openclaw-core"); got != "analyze-openclaw" {
		t.Errorf("CompleteStepByName() = %q, want %q", got, "analyze-openclaw")
	}
}

func TestPlanState_MatchStepLongestWins(t *testing.T) {
	// If step names overlap in prefix, the longest match should win.
	ps := NewPlanState("test", []PlanStepInfo{
		{Name: "analyze", Description: "Generic analyze"},
		{Name: "analyze-astonish", Description: "Analyze Astonish specifically"},
	})

	// "analyze-astonish-core" matches both "analyze" and "analyze-astonish",
	// but "analyze-astonish" is longer → wins.
	if got := ps.StartStepByName("analyze-astonish-core"); got != "analyze-astonish" {
		t.Errorf("StartStepByName() = %q, want %q (longest prefix wins)", got, "analyze-astonish")
	}
}

func TestPlanState_NoMatchReturnsEmpty(t *testing.T) {
	ps := NewPlanState("test", []PlanStepInfo{
		{Name: "clone-repos", Description: "Clone"},
		{Name: "analyze-astonish", Description: "Analyze"},
	})

	if got := ps.StartStepByName("totally-unrelated-task"); got != "" {
		t.Errorf("StartStepByName() = %q, want empty (no match)", got)
	}

	if got := ps.CompleteStepByName("totally-unrelated-task"); got != "" {
		t.Errorf("CompleteStepByName() = %q, want empty (no match)", got)
	}
}

func TestPlanState_CaseInsensitive(t *testing.T) {
	ps := NewPlanState("test", []PlanStepInfo{
		{Name: "Analyze-Astonish", Description: "Analyze"},
	})

	if got := ps.StartStepByName("analyze-astonish-core"); got != "Analyze-Astonish" {
		t.Errorf("StartStepByName() = %q, want %q (case-insensitive)", got, "Analyze-Astonish")
	}
}

func TestPlanState_RealWorldScenario(t *testing.T) {
	// Simulate the exact scenario from session e1fc4e31
	ps := NewPlanState("Source-Level GitHub Comparison", []PlanStepInfo{
		{Name: "clone-repos", Description: "Clone both repositories"},
		{Name: "analyze-astonish", Description: "Deep analysis of astonish"},
		{Name: "analyze-openclaw", Description: "Deep analysis of openclaw"},
		{Name: "compare-shared", Description: "Compare shared features"},
		{Name: "write-report", Description: "Produce comparison report"},
	})

	// Phase 0: shell_command (clone) starts first step
	if got := ps.AdvanceOnToolStart(); got != "clone-repos" {
		t.Fatalf("AdvanceOnToolStart() = %q, want clone-repos", got)
	}

	// Phase 1: delegate_tasks batch 1 — sub-agents start
	// task_start: analyze-astonish-core → should start analyze-astonish
	// But clone-repos is still "running" — it should NOT be affected
	if got := ps.StartStepByName("analyze-astonish-core"); got != "analyze-astonish" {
		t.Errorf("expected analyze-astonish to start, got %q", got)
	}
	if got := ps.StartStepByName("analyze-openclaw-core"); got != "analyze-openclaw" {
		t.Errorf("expected analyze-openclaw to start, got %q", got)
	}

	// task_complete: analyze-astonish-core
	if got := ps.CompleteStepByName("analyze-astonish-core"); got != "analyze-astonish" {
		t.Errorf("expected analyze-astonish to complete, got %q", got)
	}
	if got := ps.CompleteStepByName("analyze-openclaw-core"); got != "analyze-openclaw" {
		t.Errorf("expected analyze-openclaw to complete, got %q", got)
	}

	// Phase 2: read-astonish-full, read-openclaw-full — no matching plan steps
	if got := ps.StartStepByName("read-astonish-full"); got != "" {
		t.Errorf("read-astonish-full should not match any step, got %q", got)
	}

	// Phase 3: compare-flow-systems → matches compare-shared
	if got := ps.StartStepByName("compare-shared-features"); got != "compare-shared" {
		t.Errorf("expected compare-shared to start, got %q", got)
	}
	if got := ps.CompleteStepByName("compare-shared-features"); got != "compare-shared" {
		t.Errorf("expected compare-shared to complete, got %q", got)
	}

	// Phase 4: write_file (not delegation) → AdvanceOnToolStart starts write-report
	// clone-repos is still "running" though — let's check HasPendingSteps
	if !ps.HasPendingSteps() {
		t.Error("expected pending steps (clone-repos still running, write-report pending)")
	}

	// End of turn: CompleteAll sweeps remaining
	completed := ps.CompleteAll()
	if len(completed) != 2 {
		t.Errorf("CompleteAll() returned %d steps, want 2 (clone-repos + write-report)", len(completed))
	}

	if ps.HasPendingSteps() {
		t.Error("expected no pending steps after CompleteAll")
	}
}
