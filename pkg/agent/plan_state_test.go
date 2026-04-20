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

func TestPlanState_ExplicitPlanStep_SingleTask(t *testing.T) {
	ps := NewPlanState("test", []PlanStepInfo{
		{Name: "research-news", Description: "Search news"},
		{Name: "write-reports", Description: "Write report files"},
	})

	// task_start with explicit plan_step
	stepName := ps.ResolveStepName("research-news", "apple-search")
	if stepName != "research-news" {
		t.Fatalf("ResolveStepName() = %q, want %q", stepName, "research-news")
	}
	if got := ps.StartStep(stepName, "apple-search"); got != "research-news" {
		t.Errorf("StartStep() = %q, want %q", got, "research-news")
	}

	// Step is now running, second StartStep should return "" (no transition)
	if got := ps.StartStep("research-news", "nvidia-search"); got != "" {
		t.Errorf("StartStep() = %q, want empty (already running)", got)
	}

	// Complete first task — step should NOT complete yet (nvidia-search still pending)
	if got := ps.CompleteTask("research-news", "apple-search"); got != "" {
		t.Errorf("CompleteTask() = %q, want empty (nvidia-search still running)", got)
	}

	// Complete second task — NOW step should complete
	if got := ps.CompleteTask("research-news", "nvidia-search"); got != "research-news" {
		t.Errorf("CompleteTask() = %q, want %q", got, "research-news")
	}
}

func TestPlanState_ExplicitPlanStep_MultipleTasksSameStep(t *testing.T) {
	// Simulates the Apple/NVIDIA news session: 2 tasks both linked to "research-news"
	ps := NewPlanState("Create News Reports", []PlanStepInfo{
		{Name: "research-news", Description: "Search for news in parallel"},
		{Name: "write-reports", Description: "Create two report files"},
	})

	// Round 1 — both tasks start under "research-news"
	step := ps.ResolveStepName("research-news", "apple-search")
	ps.StartStep(step, "apple-search")
	ps.StartStep(step, "nvidia-search")

	// apple finishes first
	if got := ps.CompleteTask("research-news", "apple-search"); got != "" {
		t.Errorf("step should NOT complete when nvidia is still running, got %q", got)
	}

	// nvidia finishes — now step completes
	if got := ps.CompleteTask("research-news", "nvidia-search"); got != "research-news" {
		t.Errorf("step should complete when all tasks done, got %q", got)
	}

	// Round 2 (retry with web tools) — same plan_step, new task names
	step2 := ps.ResolveStepName("research-news", "apple-web")
	// Step is already complete, StartStep should return ""
	if step2 != "research-news" {
		t.Fatalf("ResolveStepName still matches, got %q", step2)
	}
	if got := ps.StartStep(step2, "apple-web"); got != "" {
		t.Errorf("StartStep on already-complete step should return empty, got %q", got)
	}
}

func TestPlanState_FallbackPrefixMatch(t *testing.T) {
	// When plan_step is empty, fall back to prefix matching on task name
	ps := NewPlanState("test", []PlanStepInfo{
		{Name: "analyze-astonish", Description: "Analyze Astonish"},
		{Name: "analyze-openclaw", Description: "Analyze OpenClaw"},
	})

	// Resolve with empty plan_step → prefix match
	step := ps.ResolveStepName("", "analyze-astonish-core")
	if step != "analyze-astonish" {
		t.Errorf("ResolveStepName fallback = %q, want %q", step, "analyze-astonish")
	}

	step2 := ps.ResolveStepName("", "analyze-openclaw-core")
	if step2 != "analyze-openclaw" {
		t.Errorf("ResolveStepName fallback = %q, want %q", step2, "analyze-openclaw")
	}

	// No match
	step3 := ps.ResolveStepName("", "totally-unrelated")
	if step3 != "" {
		t.Errorf("ResolveStepName should return empty for no match, got %q", step3)
	}
}

func TestPlanState_PrefixMatch_LongestWins(t *testing.T) {
	ps := NewPlanState("test", []PlanStepInfo{
		{Name: "analyze", Description: "Generic"},
		{Name: "analyze-astonish", Description: "Specific"},
	})

	step := ps.ResolveStepName("", "analyze-astonish-core")
	if step != "analyze-astonish" {
		t.Errorf("longest prefix should win: got %q, want %q", step, "analyze-astonish")
	}
}

func TestPlanState_CaseInsensitive(t *testing.T) {
	ps := NewPlanState("test", []PlanStepInfo{
		{Name: "Research-News", Description: "Search"},
	})

	// Exact match is case-insensitive
	step := ps.ResolveStepName("research-news", "some-task")
	if step != "Research-News" {
		t.Errorf("case-insensitive resolve: got %q, want %q", step, "Research-News")
	}

	// Prefix match is also case-insensitive
	step2 := ps.ResolveStepName("", "research-news-apple")
	if step2 != "Research-News" {
		t.Errorf("case-insensitive prefix: got %q, want %q", step2, "Research-News")
	}
}

func TestPlanState_CompleteAll(t *testing.T) {
	ps := NewPlanState("test", []PlanStepInfo{
		{Name: "step-1", Description: "First"},
		{Name: "step-2", Description: "Second"},
		{Name: "step-3", Description: "Third"},
	})

	// Start step-1 via tool, start step-2 via explicit binding
	ps.AdvanceOnToolStart()
	ps.StartStep("step-2", "some-task")

	// CompleteAll should sweep all 3 (step-1 running, step-2 running, step-3 pending)
	completed := ps.CompleteAll()
	if len(completed) != 3 {
		t.Errorf("CompleteAll() returned %d, want 3", len(completed))
	}
	if ps.HasPendingSteps() {
		t.Error("expected no pending steps after CompleteAll")
	}
}

func TestPlanState_RealWorldScenario_ExplicitBinding(t *testing.T) {
	// Simulate the improved Apple/NVIDIA news session with explicit plan_step
	ps := NewPlanState("Create News Reports for Apple and NVIDIA", []PlanStepInfo{
		{Name: "research-news", Description: "Search for latest Apple and NVIDIA news in parallel"},
		{Name: "write-reports", Description: "Create two separate report files with findings"},
	})

	// Agent calls delegate_tasks with plan_step: "research-news" on both tasks

	// task_start: apple-search (plan_step: "research-news")
	step := ps.ResolveStepName("research-news", "apple-search")
	if emitted := ps.StartStep(step, "apple-search"); emitted != "research-news" {
		t.Errorf("first task should start the step, got %q", emitted)
	}

	// task_start: nvidia-search (plan_step: "research-news")
	step = ps.ResolveStepName("research-news", "nvidia-search")
	if emitted := ps.StartStep(step, "nvidia-search"); emitted != "" {
		t.Errorf("second task should not re-emit step start, got %q", emitted)
	}

	// task_complete: apple-search (plan_step: "research-news")
	if emitted := ps.CompleteTask("research-news", "apple-search"); emitted != "" {
		t.Errorf("step should NOT complete yet (nvidia still running), got %q", emitted)
	}

	// task_complete: nvidia-search (plan_step: "research-news")
	if emitted := ps.CompleteTask("research-news", "nvidia-search"); emitted != "research-news" {
		t.Errorf("step should complete now, got %q", emitted)
	}

	// Agent calls write_file directly (not delegation) → AdvanceOnToolStart
	if emitted := ps.AdvanceOnToolStart(); emitted != "write-reports" {
		t.Errorf("write_file should advance to write-reports, got %q", emitted)
	}

	// End of turn
	completed := ps.CompleteAll()
	if len(completed) != 1 { // only write-reports was still running
		t.Errorf("CompleteAll() returned %d, want 1", len(completed))
	}
}

func TestPlanState_MixedExplicitAndFallback(t *testing.T) {
	// Some tasks have plan_step, some don't
	ps := NewPlanState("test", []PlanStepInfo{
		{Name: "explore", Description: "Explore repos"},
		{Name: "analyze-code", Description: "Analyze code"},
	})

	// Task with explicit plan_step
	step := ps.ResolveStepName("explore", "fetch-tree")
	if step != "explore" {
		t.Fatalf("explicit resolve: got %q, want 'explore'", step)
	}
	ps.StartStep(step, "fetch-tree")
	ps.CompleteTask(step, "fetch-tree")

	// Task without plan_step (fallback to prefix match)
	step2 := ps.ResolveStepName("", "analyze-code-astonish")
	if step2 != "analyze-code" {
		t.Fatalf("fallback resolve: got %q, want 'analyze-code'", step2)
	}
	ps.StartStep(step2, "analyze-code-astonish")
	if got := ps.CompleteTask(step2, "analyze-code-astonish"); got != "analyze-code" {
		t.Errorf("single task completion: got %q, want 'analyze-code'", got)
	}
}
