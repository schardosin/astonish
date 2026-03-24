package testing

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/schardosin/astonish/pkg/config"
)

// mockExecutor is a test double for ToolExecutor.
type mockExecutor struct {
	calls   []mockCall
	results map[string]mockResult
}

type mockCall struct {
	name string
	args map[string]interface{}
}

type mockResult struct {
	result any
	err    error
}

func newMockExecutor() *mockExecutor {
	return &mockExecutor{
		results: make(map[string]mockResult),
	}
}

func (m *mockExecutor) SetResult(toolName string, result any, err error) {
	m.results[toolName] = mockResult{result: result, err: err}
}

func (m *mockExecutor) Execute(_ context.Context, name string, args map[string]interface{}) (any, error) {
	m.calls = append(m.calls, mockCall{name: name, args: args})
	if r, ok := m.results[name]; ok {
		return r.result, r.err
	}
	return map[string]interface{}{"stdout": "ok"}, nil
}

func TestRunSuiteBasicPass(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "runner-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{"stdout": "hello world", "exit_code": 0}, nil)

	am, _ := NewArtifactManager(tmpDir, "test")
	runner := NewSuiteRunner(executor, am, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "test_suite",
			SuiteConfig: &config.TestSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_basic",
			Config: &config.AgentConfig{
				Type:  "test",
				Suite: "myapp",
				Nodes: []config.Node{
					{
						Name: "step1",
						Type: "tool",
						Args: map[string]interface{}{"tool": "shell_command", "command": "echo hello"},
						Assert: &config.AssertConfig{
							Type:     "contains",
							Expected: "hello",
						},
					},
				},
			},
		},
	}

	report, err := runner.RunSuite(context.Background(), suite, tests)
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}

	if report.Status != "passed" {
		t.Errorf("Status = %q, want %q", report.Status, "passed")
	}
	if len(report.Tests) != 1 {
		t.Fatalf("Tests length = %d, want 1", len(report.Tests))
	}
	if report.Tests[0].Status != "passed" {
		t.Errorf("Test status = %q, want %q", report.Tests[0].Status, "passed")
	}
	if len(report.Tests[0].Steps) != 1 {
		t.Fatalf("Steps length = %d, want 1", len(report.Tests[0].Steps))
	}
	if !report.Tests[0].Steps[0].Assertion.Passed {
		t.Error("assertion should have passed")
	}
}

func TestRunSuiteAssertionFail(t *testing.T) {
	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{"stdout": "error occurred"}, nil)

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "test_suite",
			SuiteConfig: &config.TestSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_fail",
			Config: &config.AgentConfig{
				Type:  "test",
				Suite: "myapp",
				Nodes: []config.Node{
					{
						Name: "step1",
						Type: "tool",
						Args: map[string]interface{}{"tool": "shell_command", "command": "echo err"},
						Assert: &config.AssertConfig{
							Type:     "contains",
							Expected: "success",
						},
					},
				},
			},
		},
	}

	report, _ := runner.RunSuite(context.Background(), suite, tests)

	if report.Status != "failed" {
		t.Errorf("Status = %q, want %q", report.Status, "failed")
	}
	if report.Tests[0].Status != "failed" {
		t.Errorf("Test status = %q, want %q", report.Tests[0].Status, "failed")
	}
	if report.Tests[0].Steps[0].Assertion.Passed {
		t.Error("assertion should have failed")
	}
}

func TestRunSuiteToolError(t *testing.T) {
	executor := newMockExecutor()
	executor.SetResult("shell_command", nil, fmt.Errorf("command not found"))

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "test_suite",
			SuiteConfig: &config.TestSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_err",
			Config: &config.AgentConfig{
				Type:  "test",
				Suite: "myapp",
				Nodes: []config.Node{
					{
						Name: "step1",
						Type: "tool",
						Args: map[string]interface{}{"tool": "shell_command"},
					},
				},
			},
		},
	}

	report, _ := runner.RunSuite(context.Background(), suite, tests)
	if report.Tests[0].Steps[0].Status != "error" {
		t.Errorf("Status = %q, want %q", report.Tests[0].Steps[0].Status, "error")
	}
	if report.Tests[0].Steps[0].Error == "" {
		t.Error("expected error message")
	}
}

func TestRunSuiteSetupFailure(t *testing.T) {
	executor := newMockExecutor()
	executor.SetResult("shell_command", nil, fmt.Errorf("setup failed"))

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type: "test_suite",
			SuiteConfig: &config.TestSuiteConfig{
				Setup: []string{"failing_command"},
			},
		},
	}

	report, _ := runner.RunSuite(context.Background(), suite, nil)
	if report.Status != "error" {
		t.Errorf("Status = %q, want %q", report.Status, "error")
	}
	if report.Summary == "" {
		t.Error("expected summary with setup failure")
	}
}

func TestRunSuiteMultipleTests(t *testing.T) {
	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{"stdout": "ok"}, nil)

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "test_suite",
			SuiteConfig: &config.TestSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test1",
			Config: &config.AgentConfig{
				Type:  "test",
				Suite: "myapp",
				Nodes: []config.Node{
					{Name: "s1", Type: "tool", Args: map[string]interface{}{"tool": "shell_command"},
						Assert: &config.AssertConfig{Type: "contains", Expected: "ok"}},
				},
			},
		},
		{
			Name: "test2",
			Config: &config.AgentConfig{
				Type:  "test",
				Suite: "myapp",
				Nodes: []config.Node{
					{Name: "s1", Type: "tool", Args: map[string]interface{}{"tool": "shell_command"},
						Assert: &config.AssertConfig{Type: "contains", Expected: "ok"}},
				},
			},
		},
	}

	report, _ := runner.RunSuite(context.Background(), suite, tests)
	if len(report.Tests) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(report.Tests))
	}
	if report.Status != "passed" {
		t.Errorf("Status = %q, want %q", report.Status, "passed")
	}
	if report.Summary != "2/2 tests passed" {
		t.Errorf("Summary = %q, want %q", report.Summary, "2/2 tests passed")
	}
}

func TestRunSuiteOnFailStop(t *testing.T) {
	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{"stdout": "bad output"}, nil)

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "test_suite",
			SuiteConfig: &config.TestSuiteConfig{},
		},
	}

	// Test with two steps: first fails, second should be skipped (on_fail defaults to "stop")
	tests := []LoadedTest{
		{
			Name: "test_stop",
			Config: &config.AgentConfig{
				Type:  "test",
				Suite: "myapp",
				Nodes: []config.Node{
					{Name: "step1", Type: "tool", Args: map[string]interface{}{"tool": "shell_command"},
						Assert: &config.AssertConfig{Type: "contains", Expected: "good output"}},
					{Name: "step2", Type: "tool", Args: map[string]interface{}{"tool": "shell_command"},
						Assert: &config.AssertConfig{Type: "contains", Expected: "ok"}},
				},
				Flow: []config.FlowItem{{From: "step1", To: "step2"}},
			},
		},
	}

	report, _ := runner.RunSuite(context.Background(), suite, tests)
	// Should stop after step1 fails
	if len(report.Tests[0].Steps) != 1 {
		t.Errorf("expected 1 step (stopped after failure), got %d", len(report.Tests[0].Steps))
	}
}

func TestRunSuiteOnFailContinue(t *testing.T) {
	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{"stdout": "bad output"}, nil)

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "test_suite",
			SuiteConfig: &config.TestSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_continue",
			Config: &config.AgentConfig{
				Type:       "test",
				Suite:      "myapp",
				TestConfig: &config.TestConfig{OnFail: "continue"},
				Nodes: []config.Node{
					{Name: "step1", Type: "tool", Args: map[string]interface{}{"tool": "shell_command"},
						Assert: &config.AssertConfig{Type: "contains", Expected: "good output"}},
					{Name: "step2", Type: "tool", Args: map[string]interface{}{"tool": "shell_command"}},
				},
				Flow: []config.FlowItem{{From: "step1", To: "step2"}},
			},
		},
	}

	report, _ := runner.RunSuite(context.Background(), suite, tests)
	// Should continue after step1 fails
	if len(report.Tests[0].Steps) != 2 {
		t.Errorf("expected 2 steps (continued after failure), got %d", len(report.Tests[0].Steps))
	}
}

func TestRunSuiteNoAssertionPass(t *testing.T) {
	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{"stdout": "whatever"}, nil)

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "test_suite",
			SuiteConfig: &config.TestSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_noassert",
			Config: &config.AgentConfig{
				Type:  "test",
				Suite: "myapp",
				Nodes: []config.Node{
					{Name: "step1", Type: "tool", Args: map[string]interface{}{"tool": "shell_command"}},
				},
			},
		},
	}

	report, _ := runner.RunSuite(context.Background(), suite, tests)
	if report.Tests[0].Steps[0].Status != "passed" {
		t.Errorf("step without assertion should pass, got %q", report.Tests[0].Steps[0].Status)
	}
}

func TestRunSuiteTeardownAlwaysRuns(t *testing.T) {
	// First call (setup) succeeds, test tool fails
	customExec := &countingExecutor{
		results: []mockResult{
			{result: map[string]interface{}{"stdout": "setup ok"}, err: nil},    // setup
			{result: nil, err: fmt.Errorf("tool failed")},                       // test step
			{result: map[string]interface{}{"stdout": "teardown ok"}, err: nil}, // teardown
		},
	}

	runner := NewSuiteRunner(customExec, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type: "test_suite",
			SuiteConfig: &config.TestSuiteConfig{
				Setup:    []string{"setup_cmd"},
				Teardown: []string{"teardown_cmd"},
			},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_err",
			Config: &config.AgentConfig{
				Type:  "test",
				Suite: "myapp",
				Nodes: []config.Node{
					{Name: "step1", Type: "tool", Args: map[string]interface{}{"tool": "shell_command"}},
				},
			},
		},
	}

	runner.RunSuite(context.Background(), suite, tests)

	// Verify teardown was called (3 total: setup + test step + teardown)
	if customExec.callCount != 3 {
		t.Errorf("expected 3 calls (setup + test + teardown), got %d", customExec.callCount)
	}
}

// countingExecutor tracks call count and returns pre-defined results.
type countingExecutor struct {
	callCount int
	results   []mockResult
}

func (ce *countingExecutor) Execute(_ context.Context, name string, args map[string]interface{}) (any, error) {
	idx := ce.callCount
	ce.callCount++
	if idx < len(ce.results) {
		return ce.results[idx].result, ce.results[idx].err
	}
	return map[string]interface{}{"stdout": "default"}, nil
}

func TestResolveExecutionOrder(t *testing.T) {
	cfg := &config.AgentConfig{
		Nodes: []config.Node{
			{Name: "c"},
			{Name: "a"},
			{Name: "b"},
		},
		Flow: []config.FlowItem{
			{From: "a", To: "b"},
			{From: "b", To: "c"},
		},
	}

	ordered := resolveExecutionOrder(cfg)
	if len(ordered) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(ordered))
	}
	if ordered[0].Name != "a" {
		t.Errorf("first node = %q, want %q", ordered[0].Name, "a")
	}
	if ordered[1].Name != "b" {
		t.Errorf("second node = %q, want %q", ordered[1].Name, "b")
	}
	if ordered[2].Name != "c" {
		t.Errorf("third node = %q, want %q", ordered[2].Name, "c")
	}
}

func TestResolveExecutionOrderNoFlow(t *testing.T) {
	cfg := &config.AgentConfig{
		Nodes: []config.Node{
			{Name: "first"},
			{Name: "second"},
		},
	}

	ordered := resolveExecutionOrder(cfg)
	if ordered[0].Name != "first" {
		t.Errorf("first = %q, want %q", ordered[0].Name, "first")
	}
}

func TestExtractOutput(t *testing.T) {
	tests := []struct {
		name   string
		result any
		want   string
	}{
		{
			name:   "shell command result",
			result: map[string]interface{}{"stdout": "hello world", "exit_code": 0},
			want:   "hello world",
		},
		{
			name:   "nil result",
			result: nil,
			want:   "",
		},
		{
			name:   "content field",
			result: map[string]interface{}{"content": "file contents"},
			want:   "file contents",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractOutput(tt.result)
			if got != tt.want {
				t.Errorf("extractOutput = %q, want %q", got, tt.want)
			}
		})
	}
}
