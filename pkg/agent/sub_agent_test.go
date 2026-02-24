package agent

import (
	"context"
	"testing"
	"time"

	"google.golang.org/adk/tool"
)

func TestNewSubAgentManager_Defaults(t *testing.T) {
	mgr := NewSubAgentManager(SubAgentConfig{})

	if mgr.Config.MaxDepth != 2 {
		t.Errorf("MaxDepth = %d, want 2", mgr.Config.MaxDepth)
	}
	if mgr.Config.MaxConcurrent != 5 {
		t.Errorf("MaxConcurrent = %d, want 5", mgr.Config.MaxConcurrent)
	}
	if mgr.Config.TaskTimeout != 5*time.Minute {
		t.Errorf("TaskTimeout = %v, want 5m", mgr.Config.TaskTimeout)
	}
}

func TestNewSubAgentManager_CustomConfig(t *testing.T) {
	mgr := NewSubAgentManager(SubAgentConfig{
		MaxDepth:      3,
		MaxConcurrent: 10,
		TaskTimeout:   10 * time.Minute,
	})

	if mgr.Config.MaxDepth != 3 {
		t.Errorf("MaxDepth = %d, want 3", mgr.Config.MaxDepth)
	}
	if mgr.Config.MaxConcurrent != 10 {
		t.Errorf("MaxConcurrent = %d, want 10", mgr.Config.MaxConcurrent)
	}
	if mgr.Config.TaskTimeout != 10*time.Minute {
		t.Errorf("TaskTimeout = %v, want 10m", mgr.Config.TaskTimeout)
	}
}

func TestSubAgentManager_FilterTools(t *testing.T) {
	mgr := NewSubAgentManager(SubAgentConfig{})
	mgr.Tools = mockTools(
		"read_file", "write_file", "shell_command",
		"memory_save",       // should be excluded
		"delegate_tasks",    // should be excluded
		"schedule_job",      // should be excluded
		"save_credential",   // should be excluded
		"remove_credential", // should be excluded
		"grep_search",
	)

	// Filter with empty allow list (all non-excluded)
	filtered := mgr.filterTools(nil)
	if len(filtered) != 4 {
		t.Errorf("filterTools(nil) returned %d tools, want 4", len(filtered))
	}

	// Verify excluded tools are not present
	for _, ft := range filtered {
		if excludedChildTools[ft.Name()] {
			t.Errorf("filterTools returned excluded tool %q", ft.Name())
		}
	}

	// Filter with specific allow list
	filtered = mgr.filterTools([]string{"read_file", "grep_search"})
	if len(filtered) != 2 {
		t.Errorf("filterTools([read_file, grep_search]) returned %d tools, want 2", len(filtered))
	}

	// Allow list that includes an excluded tool — should still be excluded
	filtered = mgr.filterTools([]string{"read_file", "memory_save"})
	if len(filtered) != 1 {
		t.Errorf("filterTools([read_file, memory_save]) returned %d tools, want 1", len(filtered))
	}
}

func TestSubAgentManager_FilterToolsEmptyInput(t *testing.T) {
	mgr := NewSubAgentManager(SubAgentConfig{})
	mgr.Tools = nil

	filtered := mgr.filterTools(nil)
	if len(filtered) != 0 {
		t.Errorf("filterTools with nil tools len = %d, want 0", len(filtered))
	}
}

func TestSubAgentManager_BuildChildPrompt(t *testing.T) {
	mgr := NewSubAgentManager(SubAgentConfig{})

	task := SubAgentTask{
		Name:         "researcher",
		Description:  "Find all references to function X",
		Instructions: "Check only .go files",
	}

	prompt := mgr.buildChildPrompt(task)

	// Check key sections are present
	if !contains(prompt, "researcher") {
		t.Error("prompt missing agent name")
	}
	if !contains(prompt, "Find all references to function X") {
		t.Error("prompt missing task description")
	}
	if !contains(prompt, "Check only .go files") {
		t.Error("prompt missing instructions")
	}
	if !contains(prompt, "Behavior Rules") {
		t.Error("prompt missing behavior rules")
	}
}

func TestSubAgentManager_BuildChildPromptNoInstructions(t *testing.T) {
	mgr := NewSubAgentManager(SubAgentConfig{})

	task := SubAgentTask{
		Name:        "worker",
		Description: "Do something",
	}

	prompt := mgr.buildChildPrompt(task)

	if contains(prompt, "## Instructions") {
		t.Error("prompt should NOT contain Instructions section when no instructions provided")
	}
}

func TestSubAgentManager_BuildChildPromptWithHTTPTools(t *testing.T) {
	mgr := NewSubAgentManager(SubAgentConfig{})
	mgr.Tools = mockTools("http_request", "list_credentials", "resolve_credential", "read_file")

	task := SubAgentTask{
		Name:        "api-caller",
		Description: "Call an API",
	}

	prompt := mgr.buildChildPrompt(task)

	if !contains(prompt, "## HTTP Requests") {
		t.Error("prompt missing HTTP Requests section when http_request tool is available")
	}
	if !contains(prompt, "## Credentials") {
		t.Error("prompt missing Credentials section when resolve_credential tool is available")
	}
	if !contains(prompt, "Do NOT write scripts") {
		t.Error("prompt missing anti-script guidance")
	}
}

func TestSubAgentManager_BuildChildPromptWithoutHTTPTools(t *testing.T) {
	mgr := NewSubAgentManager(SubAgentConfig{})
	mgr.Tools = mockTools("read_file", "grep_search")

	task := SubAgentTask{
		Name:        "searcher",
		Description: "Search files",
	}

	prompt := mgr.buildChildPrompt(task)

	if contains(prompt, "## HTTP Requests") {
		t.Error("prompt should NOT contain HTTP Requests section when http_request tool is not available")
	}
	if contains(prompt, "## Credentials") {
		t.Error("prompt should NOT contain Credentials section when resolve_credential tool is not available")
	}
}

func TestSubAgentManager_DepthCheck(t *testing.T) {
	mgr := NewSubAgentManager(SubAgentConfig{MaxDepth: 2})

	// At depth 2 (equal to max), should be blocked
	result := mgr.RunTask(context.Background(), SubAgentTask{
		Name:        "blocked",
		Description: "should fail",
		ParentDepth: 2,
	})

	if result.Status != "error" {
		t.Errorf("Status = %q, want 'error'", result.Status)
	}
	if !contains(result.Error, "max delegation depth") {
		t.Errorf("Error = %q, want to contain 'max delegation depth'", result.Error)
	}
}

func TestExcludedChildTools(t *testing.T) {
	expected := map[string]bool{
		"memory_save":       true,
		"delegate_tasks":    true,
		"schedule_job":      true,
		"save_credential":   true,
		"remove_credential": true,
	}

	for name := range expected {
		if !excludedChildTools[name] {
			t.Errorf("excludedChildTools missing %q", name)
		}
	}

	if len(excludedChildTools) != len(expected) {
		t.Errorf("excludedChildTools has %d entries, want %d", len(excludedChildTools), len(expected))
	}
}

// --- helpers ---

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && containsString(s, substr)
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// mockTool implements tool.Tool for testing filterTools.
type mockTool struct {
	name string
}

func (m mockTool) Name() string        { return m.name }
func (m mockTool) Description() string { return "mock " + m.name }
func (m mockTool) IsLongRunning() bool { return false }

// mockTools creates a []tool.Tool from a list of names.
func mockTools(names ...string) []tool.Tool {
	var result []tool.Tool
	for _, name := range names {
		result = append(result, mockTool{name: name})
	}
	return result
}

// --- flattenTraces tests ---

func TestFlattenTraces_Nil(t *testing.T) {
	result := flattenTraces(nil)
	if result != nil {
		t.Error("flattenTraces(nil) should return nil")
	}
}

func TestFlattenTraces_NoSubAgentSteps(t *testing.T) {
	trace := &ExecutionTrace{
		Steps: []TraceStep{
			{ToolName: "read_file", Success: true},
			{ToolName: "grep_search", Success: true},
		},
	}

	flattenTraces(trace)

	if len(trace.Steps) != 2 {
		t.Errorf("Steps len = %d, want 2 (no change)", len(trace.Steps))
	}
}

func TestFlattenTraces_ReplaceDelegateTasks(t *testing.T) {
	childTrace1 := &ExecutionTrace{
		Steps: []TraceStep{
			{ToolName: "read_file", Success: true, ToolArgs: map[string]any{"path": "a.go"}},
			{ToolName: "grep_search", Success: true},
		},
	}
	childTrace2 := &ExecutionTrace{
		Steps: []TraceStep{
			{ToolName: "shell_command", Success: true},
		},
	}

	trace := &ExecutionTrace{
		Steps: []TraceStep{
			{ToolName: "read_file", Success: true},
			{
				ToolName:       "delegate_tasks",
				Success:        true,
				SubAgentName:   "test-delegation",
				SubAgentTraces: []*ExecutionTrace{childTrace1, childTrace2},
			},
			{ToolName: "write_file", Success: true},
		},
	}

	flattenTraces(trace)

	// Should be: read_file, read_file(from child1), grep_search(from child1), shell_command(from child2), write_file
	if len(trace.Steps) != 5 {
		t.Errorf("Steps len = %d, want 5", len(trace.Steps))
		for i, s := range trace.Steps {
			t.Logf("  step %d: %s", i, s.ToolName)
		}
		return
	}

	expected := []string{"read_file", "read_file", "grep_search", "shell_command", "write_file"}
	for i, exp := range expected {
		if trace.Steps[i].ToolName != exp {
			t.Errorf("Step[%d].ToolName = %q, want %q", i, trace.Steps[i].ToolName, exp)
		}
	}
}

func TestFlattenTraces_SkipsInternalToolsFromChildren(t *testing.T) {
	childTrace := &ExecutionTrace{
		Steps: []TraceStep{
			{ToolName: "read_file", Success: true},
			{ToolName: "memory_save", Success: true},    // should be filtered
			{ToolName: "delegate_tasks", Success: true}, // should be filtered
		},
	}

	trace := &ExecutionTrace{
		Steps: []TraceStep{
			{
				ToolName:       "delegate_tasks",
				Success:        true,
				SubAgentTraces: []*ExecutionTrace{childTrace},
			},
		},
	}

	flattenTraces(trace)

	if len(trace.Steps) != 1 {
		t.Errorf("Steps len = %d, want 1 (only read_file)", len(trace.Steps))
	}
	if len(trace.Steps) > 0 && trace.Steps[0].ToolName != "read_file" {
		t.Errorf("Step[0].ToolName = %q, want 'read_file'", trace.Steps[0].ToolName)
	}
}

func TestFlattenTraces_DelegateWithoutTraces(t *testing.T) {
	trace := &ExecutionTrace{
		Steps: []TraceStep{
			{ToolName: "read_file", Success: true},
			{
				ToolName:       "delegate_tasks",
				Success:        true,
				SubAgentTraces: nil, // no child traces
			},
		},
	}

	flattenTraces(trace)

	// delegate_tasks with no traces is kept as-is
	if len(trace.Steps) != 2 {
		t.Errorf("Steps len = %d, want 2", len(trace.Steps))
	}
}
