package drill

import (
	"context"
	"fmt"
	"os"
	"strings"
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
	t.Parallel()
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
			Type:        "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_basic",
			Config: &config.AgentConfig{
				Type:  "drill",
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
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{"stdout": "error occurred"}, nil)

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_fail",
			Config: &config.AgentConfig{
				Type:  "drill",
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
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("shell_command", nil, fmt.Errorf("command not found"))

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_err",
			Config: &config.AgentConfig{
				Type:  "drill",
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
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("shell_command", nil, fmt.Errorf("setup failed"))

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type: "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{
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
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{"stdout": "ok"}, nil)

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test1",
			Config: &config.AgentConfig{
				Type:  "drill",
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
				Type:  "drill",
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
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{"stdout": "bad output"}, nil)

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{},
		},
	}

	// Test with two steps: first fails, second should be skipped (on_fail defaults to "stop")
	tests := []LoadedTest{
		{
			Name: "test_stop",
			Config: &config.AgentConfig{
				Type:  "drill",
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
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{"stdout": "bad output"}, nil)

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_continue",
			Config: &config.AgentConfig{
				Type:        "drill",
				Suite:       "myapp",
				DrillConfig: &config.DrillConfig{OnFail: "continue"},
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
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{"stdout": "whatever"}, nil)

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_noassert",
			Config: &config.AgentConfig{
				Type:  "drill",
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
	t.Parallel()
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
			Type: "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{
				Setup:    []string{"setup_cmd"},
				Teardown: []string{"teardown_cmd"},
			},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_err",
			Config: &config.AgentConfig{
				Type:  "drill",
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

// commandTrackingExecutor tracks commands in order and returns per-command results.
type commandTrackingExecutor struct {
	commands []string
	results  map[string]mockResult
}

func newCommandTrackingExecutor() *commandTrackingExecutor {
	return &commandTrackingExecutor{
		results: make(map[string]mockResult),
	}
}

func (ct *commandTrackingExecutor) SetCommandResult(command string, result any, err error) {
	ct.results[command] = mockResult{result: result, err: err}
}

func (ct *commandTrackingExecutor) Execute(_ context.Context, name string, args map[string]interface{}) (any, error) {
	if cmd, ok := args["command"]; ok {
		ct.commands = append(ct.commands, fmt.Sprintf("%v", cmd))
		if r, found := ct.results[fmt.Sprintf("%v", cmd)]; found {
			return r.result, r.err
		}
	}
	return map[string]interface{}{"stdout": "ok"}, nil
}

func TestRunSuiteMultiServiceBasic(t *testing.T) {
	t.Parallel()
	exec := newCommandTrackingExecutor()
	runner := NewSuiteRunner(exec, nil, false)

	suite := &LoadedSuite{
		Name: "fullstack",
		Config: &config.AgentConfig{
			Type: "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{
				Services: []config.ServiceConfig{
					{Name: "database", Setup: "start_db", Teardown: "stop_db"},
					{Name: "backend", Setup: "start_api", Teardown: "stop_api"},
					{Name: "frontend", Setup: "start_web", Teardown: "stop_web"},
				},
			},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_basic",
			Config: &config.AgentConfig{
				Type:  "drill",
				Suite: "fullstack",
				Nodes: []config.Node{
					{Name: "step1", Type: "tool", Args: map[string]interface{}{"tool": "shell_command", "command": "echo test"}},
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

	// Verify order: setup db → setup api → setup web → test → teardown web → teardown api → teardown db
	expected := []string{"start_db", "start_api", "start_web", "echo test", "stop_web", "stop_api", "stop_db"}
	if len(exec.commands) != len(expected) {
		t.Fatalf("commands = %v, want %v", exec.commands, expected)
	}
	for i, cmd := range expected {
		if exec.commands[i] != cmd {
			t.Errorf("command[%d] = %q, want %q", i, exec.commands[i], cmd)
		}
	}
}

func TestRunSuiteMultiServiceSetupFailure(t *testing.T) {
	t.Parallel()
	exec := newCommandTrackingExecutor()
	exec.SetCommandResult("start_api", nil, fmt.Errorf("api failed to start"))

	runner := NewSuiteRunner(exec, nil, false)

	suite := &LoadedSuite{
		Name: "fullstack",
		Config: &config.AgentConfig{
			Type: "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{
				Services: []config.ServiceConfig{
					{Name: "database", Setup: "start_db", Teardown: "stop_db"},
					{Name: "backend", Setup: "start_api", Teardown: "stop_api"},
					{Name: "frontend", Setup: "start_web", Teardown: "stop_web"},
				},
			},
		},
	}

	report, _ := runner.RunSuite(context.Background(), suite, nil)

	if report.Status != "error" {
		t.Errorf("Status = %q, want %q", report.Status, "error")
	}
	if report.Summary != `service "backend" setup failed: api failed to start` {
		t.Errorf("Summary = %q, unexpected", report.Summary)
	}

	// Should have: start_db (ok) → start_api (fail) → stop_db (reverse teardown of started)
	// "stop_api" should NOT be called because backend never finished starting
	// Only database was fully started, so only "stop_db" should be torn down
	expected := []string{"start_db", "start_api", "stop_db"}
	if len(exec.commands) != len(expected) {
		t.Fatalf("commands = %v, want %v", exec.commands, expected)
	}
	for i, cmd := range expected {
		if exec.commands[i] != cmd {
			t.Errorf("command[%d] = %q, want %q", i, exec.commands[i], cmd)
		}
	}
}

func TestRunSuiteMultiServiceTeardownReverse(t *testing.T) {
	t.Parallel()
	exec := newCommandTrackingExecutor()
	runner := NewSuiteRunner(exec, nil, false)

	suite := &LoadedSuite{
		Name: "trio",
		Config: &config.AgentConfig{
			Type: "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{
				Services: []config.ServiceConfig{
					{Name: "a", Setup: "start_a", Teardown: "stop_a"},
					{Name: "b", Setup: "start_b", Teardown: "stop_b"},
					{Name: "c", Setup: "start_c"}, // no teardown
				},
			},
		},
	}

	report, _ := runner.RunSuite(context.Background(), suite, nil)
	if report.Status != "passed" {
		t.Errorf("Status = %q, want %q (no tests = passed)", report.Status, "passed")
	}

	// Services with no teardown should be skipped in teardown phase
	expected := []string{"start_a", "start_b", "start_c", "stop_b", "stop_a"}
	if len(exec.commands) != len(expected) {
		t.Fatalf("commands = %v, want %v", exec.commands, expected)
	}
	for i, cmd := range expected {
		if exec.commands[i] != cmd {
			t.Errorf("command[%d] = %q, want %q", i, exec.commands[i], cmd)
		}
	}
}

func TestRunSuiteMultiServiceAlwaysTeardownOnTestFailure(t *testing.T) {
	t.Parallel()
	exec := newCommandTrackingExecutor()
	exec.SetCommandResult("echo failing", map[string]interface{}{"stdout": "wrong output"}, nil)

	runner := NewSuiteRunner(exec, nil, false)

	suite := &LoadedSuite{
		Name: "svc",
		Config: &config.AgentConfig{
			Type: "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{
				Services: []config.ServiceConfig{
					{Name: "db", Setup: "start_db", Teardown: "stop_db"},
					{Name: "app", Setup: "start_app", Teardown: "stop_app"},
				},
			},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_fail",
			Config: &config.AgentConfig{
				Type:  "drill",
				Suite: "svc",
				Nodes: []config.Node{
					{Name: "step1", Type: "tool", Args: map[string]interface{}{"tool": "shell_command", "command": "echo failing"},
						Assert: &config.AssertConfig{Type: "contains", Expected: "success"}},
				},
			},
		},
	}

	report, _ := runner.RunSuite(context.Background(), suite, tests)

	if report.Status != "failed" {
		t.Errorf("Status = %q, want %q", report.Status, "failed")
	}

	// Teardown must still happen even when tests fail
	expected := []string{"start_db", "start_app", "echo failing", "stop_app", "stop_db"}
	if len(exec.commands) != len(expected) {
		t.Fatalf("commands = %v, want %v", exec.commands, expected)
	}
	for i, cmd := range expected {
		if exec.commands[i] != cmd {
			t.Errorf("command[%d] = %q, want %q", i, exec.commands[i], cmd)
		}
	}
}

func TestResolveExecutionOrder(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
			t.Parallel()
			got := ExtractOutput(tt.result)
			if got != tt.want {
				t.Errorf("ExtractOutput = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsBackgroundCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		cmd  string
		want bool
	}{
		{"./server &", true},
		{"cd /root/app && npx vite --host 0.0.0.0 &", true},
		{"  npm start &  ", true},
		{"go build ./...", false},
		{"echo hello && echo world", false},  // ends with &&, not &
		{"echo hello && echo world &", true}, // ends with &
		{"", false},
		{"pkill -f server || true", false},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			t.Parallel()
			got := isBackgroundCommand(tt.cmd)
			if got != tt.want {
				t.Errorf("isBackgroundCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestStripBackgroundSuffix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		cmd  string
		want string
	}{
		{"./server &", "./server"},
		{"cd /root/app && npx vite &", "cd /root/app && npx vite"},
		{"  npm start &  ", "npm start"},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			t.Parallel()
			got := stripBackgroundSuffix(tt.cmd)
			if got != tt.want {
				t.Errorf("stripBackgroundSuffix(%q) = %q, want %q", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestExtractSessionID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		result any
		want   string
	}{
		{
			name:   "with session_id",
			result: map[string]interface{}{"stdout": "starting", "session_id": "abc123"},
			want:   "abc123",
		},
		{
			name:   "without session_id",
			result: map[string]interface{}{"stdout": "done", "exit_code": 0},
			want:   "",
		},
		{
			name:   "nil result",
			result: nil,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractSessionID(tt.result)
			if got != tt.want {
				t.Errorf("extractSessionID = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunShellCommandBackground(t *testing.T) {
	t.Parallel()
	// When setup command ends with &, the runner should strip the & and
	// pass background=true to shell_command.
	mock := newMockExecutor()
	mock.SetResult("shell_command", map[string]interface{}{
		"stdout":     "Vite dev server running",
		"session_id": "bg-sess-1",
	}, nil)

	runner := NewSuiteRunner(mock, nil, false)
	output, err := runner.runShellCommand(context.Background(), "npx vite --host 0.0.0.0 &", 120)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "Vite dev server running" {
		t.Errorf("output = %q, want %q", output, "Vite dev server running")
	}

	// Verify the mock received the correct args
	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.calls))
	}
	call := mock.calls[0]
	if call.name != "shell_command" {
		t.Errorf("tool name = %q, want %q", call.name, "shell_command")
	}
	if cmd, ok := call.args["command"].(string); !ok || cmd != "npx vite --host 0.0.0.0" {
		t.Errorf("command = %q, want %q (trailing & should be stripped)", cmd, "npx vite --host 0.0.0.0")
	}
	if bg, ok := call.args["background"].(bool); !ok || !bg {
		t.Errorf("background = %v, want true", call.args["background"])
	}

	// Verify session was tracked
	if len(runner.bgSessions) != 1 || runner.bgSessions[0] != "bg-sess-1" {
		t.Errorf("bgSessions = %v, want [bg-sess-1]", runner.bgSessions)
	}
}

func TestRunShellCommandForeground(t *testing.T) {
	t.Parallel()
	// When setup command does NOT end with &, it should run in normal mode.
	mock := newMockExecutor()
	mock.SetResult("shell_command", map[string]interface{}{
		"stdout":    "build complete",
		"exit_code": 0,
	}, nil)

	runner := NewSuiteRunner(mock, nil, false)
	output, err := runner.runShellCommand(context.Background(), "go build ./...", 120)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "build complete" {
		t.Errorf("output = %q, want %q", output, "build complete")
	}

	call := mock.calls[0]
	if _, hasBg := call.args["background"]; hasBg {
		t.Error("foreground command should not have background arg")
	}
	if cmd := call.args["command"].(string); cmd != "go build ./..." {
		t.Errorf("command = %q, want %q", cmd, "go build ./...")
	}

	if len(runner.bgSessions) != 0 {
		t.Errorf("bgSessions should be empty, got %v", runner.bgSessions)
	}
}

func TestKillBackgroundSessions(t *testing.T) {
	t.Parallel()
	mock := newMockExecutor()
	mock.SetResult("process_kill", map[string]interface{}{"status": "killed"}, nil)

	runner := NewSuiteRunner(mock, nil, false)
	runner.bgSessions = []string{"sess-1", "sess-2", "sess-3"}

	runner.killBackgroundSessions(context.Background())

	// Should have called process_kill for each session
	if len(mock.calls) != 3 {
		t.Fatalf("expected 3 process_kill calls, got %d", len(mock.calls))
	}
	for i, call := range mock.calls {
		if call.name != "process_kill" {
			t.Errorf("call[%d] = %q, want process_kill", i, call.name)
		}
		expected := fmt.Sprintf("sess-%d", i+1)
		if sid := call.args["session_id"].(string); sid != expected {
			t.Errorf("call[%d] session_id = %q, want %q", i, sid, expected)
		}
	}

	// bgSessions should be cleared
	if len(runner.bgSessions) != 0 {
		t.Errorf("bgSessions should be nil/empty after cleanup, got %v", runner.bgSessions)
	}
}

// sequenceMockExecutor returns different results for successive calls to the same tool.
type sequenceMockExecutor struct {
	calls    []mockCall
	sequence []mockResult // returned in order for each call
	idx      int
	fallback mockResult
}

func newSequenceMockExecutor(results []mockResult, fallback mockResult) *sequenceMockExecutor {
	return &sequenceMockExecutor{
		sequence: results,
		fallback: fallback,
	}
}

func (m *sequenceMockExecutor) Execute(_ context.Context, name string, args map[string]interface{}) (any, error) {
	m.calls = append(m.calls, mockCall{name: name, args: args})
	if m.idx < len(m.sequence) {
		r := m.sequence[m.idx]
		m.idx++
		return r.result, r.err
	}
	return m.fallback.result, m.fallback.err
}

func TestRunReadyCheckViaExecutorHTTPSuccess(t *testing.T) {
	t.Parallel()
	mock := newMockExecutor()
	mock.SetResult("shell_command", map[string]interface{}{
		"stdout":    "200",
		"exit_code": 0,
	}, nil)

	runner := NewSuiteRunner(mock, nil, false)
	rc := &config.ReadyCheck{
		Type:    "http",
		URL:     "http://localhost:8080/health",
		Timeout: 5,
	}

	err := runner.runReadyCheckViaExecutor(context.Background(), rc)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	// Verify curl command was constructed correctly
	if len(mock.calls) < 1 {
		t.Fatal("expected at least 1 call")
	}
	cmd := mock.calls[0].args["command"].(string)
	if !strings.Contains(cmd, "curl") || !strings.Contains(cmd, "http://localhost:8080/health") {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestRunReadyCheckViaExecutorHTTPRetry(t *testing.T) {
	t.Parallel()
	// First call returns 503, second returns 200
	mock := newSequenceMockExecutor(
		[]mockResult{
			{result: map[string]interface{}{"stdout": "503", "exit_code": 0}, err: nil},
			{result: map[string]interface{}{"stdout": "200", "exit_code": 0}, err: nil},
		},
		mockResult{result: map[string]interface{}{"stdout": "200", "exit_code": 0}},
	)

	runner := NewSuiteRunner(mock, nil, false)
	rc := &config.ReadyCheck{
		Type:     "http",
		URL:      "http://localhost:8080/health",
		Timeout:  10,
		Interval: 1,
	}

	err := runner.runReadyCheckViaExecutor(context.Background(), rc)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}

	if len(mock.calls) < 2 {
		t.Errorf("expected at least 2 calls (retry), got %d", len(mock.calls))
	}
}

func TestRunReadyCheckViaExecutorPortSuccess(t *testing.T) {
	t.Parallel()
	mock := newMockExecutor()
	mock.SetResult("shell_command", map[string]interface{}{
		"stdout":    "",
		"exit_code": 0,
	}, nil)

	runner := NewSuiteRunner(mock, nil, false)
	rc := &config.ReadyCheck{
		Type:    "port",
		Port:    3000,
		Timeout: 5,
	}

	err := runner.runReadyCheckViaExecutor(context.Background(), rc)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	cmd := mock.calls[0].args["command"].(string)
	if !strings.Contains(cmd, "nc -z") || !strings.Contains(cmd, "3000") {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestRunReadyCheckViaExecutorTimeout(t *testing.T) {
	t.Parallel()
	// Always return 503 — should timeout
	mock := newMockExecutor()
	mock.SetResult("shell_command", map[string]interface{}{
		"stdout":    "503",
		"exit_code": 0,
	}, nil)

	runner := NewSuiteRunner(mock, nil, false)
	rc := &config.ReadyCheck{
		Type:     "http",
		URL:      "http://localhost:8080/health",
		Timeout:  2,
		Interval: 1,
	}

	err := runner.runReadyCheckViaExecutor(context.Background(), rc)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

func TestRunReadyCheckViaExecutorNilConfig(t *testing.T) {
	t.Parallel()
	runner := NewSuiteRunner(newMockExecutor(), nil, false)
	err := runner.runReadyCheckViaExecutor(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected nil config to return nil, got: %v", err)
	}
}

func TestRunReadyCheckViaExecutorMissingURL(t *testing.T) {
	t.Parallel()
	runner := NewSuiteRunner(newMockExecutor(), nil, false)
	rc := &config.ReadyCheck{Type: "http", URL: ""}
	err := runner.runReadyCheckViaExecutor(context.Background(), rc)
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Errorf("expected url required error, got: %v", err)
	}
}

func TestRunReadyCheckViaExecutorMissingPort(t *testing.T) {
	t.Parallel()
	runner := NewSuiteRunner(newMockExecutor(), nil, false)
	rc := &config.ReadyCheck{Type: "port", Port: 0}
	err := runner.runReadyCheckViaExecutor(context.Background(), rc)
	if err == nil || !strings.Contains(err.Error(), "port is required") {
		t.Errorf("expected port required error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests for placeholder substitution
// ---------------------------------------------------------------------------

func TestSubstituteVarsInString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		vars     map[string]string
		expected string
	}{
		{
			name:     "basic replacement",
			input:    "http://{{CONTAINER_IP}}:3000",
			vars:     map[string]string{"CONTAINER_IP": "10.99.0.5"},
			expected: "http://10.99.0.5:3000",
		},
		{
			name:     "no vars",
			input:    "http://{{CONTAINER_IP}}:3000",
			vars:     nil,
			expected: "http://{{CONTAINER_IP}}:3000",
		},
		{
			name:     "empty vars",
			input:    "http://{{CONTAINER_IP}}:3000",
			vars:     map[string]string{},
			expected: "http://{{CONTAINER_IP}}:3000",
		},
		{
			name:     "no placeholders",
			input:    "http://localhost:3000",
			vars:     map[string]string{"CONTAINER_IP": "10.99.0.5"},
			expected: "http://localhost:3000",
		},
		{
			name:     "multiple vars",
			input:    "http://{{CONTAINER_IP}}:{{PORT}}/path",
			vars:     map[string]string{"CONTAINER_IP": "10.99.0.5", "PORT": "8080"},
			expected: "http://10.99.0.5:8080/path",
		},
		{
			name:     "same var used twice",
			input:    "{{CONTAINER_IP}} and {{CONTAINER_IP}}",
			vars:     map[string]string{"CONTAINER_IP": "10.99.0.5"},
			expected: "10.99.0.5 and 10.99.0.5",
		},
		{
			name:     "unknown placeholder left as-is",
			input:    "http://{{UNKNOWN}}:3000",
			vars:     map[string]string{"CONTAINER_IP": "10.99.0.5"},
			expected: "http://{{UNKNOWN}}:3000",
		},
		{
			name:     "single braces fallback",
			input:    "http://{CONTAINER_IP}:3000",
			vars:     map[string]string{"CONTAINER_IP": "10.99.0.5"},
			expected: "http://10.99.0.5:3000",
		},
		{
			name:     "mixed single and double braces",
			input:    "http://{CONTAINER_IP}:{{PORT}}/path",
			vars:     map[string]string{"CONTAINER_IP": "10.99.0.5", "PORT": "8080"},
			expected: "http://10.99.0.5:8080/path",
		},
		{
			name:     "unknown single brace left as-is",
			input:    "http://{UNKNOWN}:3000",
			vars:     map[string]string{"CONTAINER_IP": "10.99.0.5"},
			expected: "http://{UNKNOWN}:3000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := substituteVarsInString(tt.input, tt.vars)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSubstituteVarsInArgs(t *testing.T) {
	t.Parallel()
	vars := map[string]string{"CONTAINER_IP": "10.99.0.5"}

	t.Run("string values", func(t *testing.T) {
		t.Parallel()
		args := map[string]interface{}{
			"url":     "http://{{CONTAINER_IP}}:3000",
			"timeout": 30,
		}
		result := substituteVarsInArgs(args, vars)
		if result["url"] != "http://10.99.0.5:3000" {
			t.Errorf("url not substituted: %v", result["url"])
		}
		if result["timeout"] != 30 {
			t.Errorf("timeout changed: %v", result["timeout"])
		}
	})

	t.Run("nested map", func(t *testing.T) {
		t.Parallel()
		args := map[string]interface{}{
			"headers": map[string]interface{}{
				"Origin": "http://{{CONTAINER_IP}}:3000",
			},
		}
		result := substituteVarsInArgs(args, vars)
		headers := result["headers"].(map[string]interface{})
		if headers["Origin"] != "http://10.99.0.5:3000" {
			t.Errorf("nested value not substituted: %v", headers["Origin"])
		}
	})

	t.Run("array values", func(t *testing.T) {
		t.Parallel()
		args := map[string]interface{}{
			"urls": []interface{}{
				"http://{{CONTAINER_IP}}:3000",
				"http://{{CONTAINER_IP}}:8080",
			},
		}
		result := substituteVarsInArgs(args, vars)
		urls := result["urls"].([]interface{})
		if urls[0] != "http://10.99.0.5:3000" {
			t.Errorf("array[0] not substituted: %v", urls[0])
		}
		if urls[1] != "http://10.99.0.5:8080" {
			t.Errorf("array[1] not substituted: %v", urls[1])
		}
	})

	t.Run("nil vars", func(t *testing.T) {
		t.Parallel()
		args := map[string]interface{}{
			"url": "http://{{CONTAINER_IP}}:3000",
		}
		result := substituteVarsInArgs(args, nil)
		if result["url"] != "http://{{CONTAINER_IP}}:3000" {
			t.Errorf("should not substitute with nil vars: %v", result["url"])
		}
	})

	t.Run("does not modify original", func(t *testing.T) {
		t.Parallel()
		args := map[string]interface{}{
			"url": "http://{{CONTAINER_IP}}:3000",
		}
		_ = substituteVarsInArgs(args, vars)
		if args["url"] != "http://{{CONTAINER_IP}}:3000" {
			t.Errorf("original args modified: %v", args["url"])
		}
	})
}

func TestBaseURLResolution(t *testing.T) {
	t.Parallel()
	// Create a mock executor that records what args it receives
	executor := newMockExecutor()
	executor.SetResult("browser_navigate", map[string]interface{}{"status": "ok"}, nil)

	runner := NewSuiteRunner(executor, nil, false)
	runner.SetVars(map[string]string{"CONTAINER_IP": "10.99.0.5"})

	suite := &LoadedSuite{
		Name: "test-base-url",
		Config: &config.AgentConfig{
			SuiteConfig: &config.DrillSuiteConfig{
				BaseURL: "http://{{CONTAINER_IP}}:3000",
			},
		},
	}

	test := LoadedTest{
		Name: "test-nav",
		Config: &config.AgentConfig{
			Description: "Test base_url resolution",
			Suite:       "test-base-url",
			DrillConfig: &config.DrillConfig{Tags: []string{"test"}},
			Nodes: []config.Node{
				{
					Name: "navigate_relative",
					Args: map[string]interface{}{
						"tool": "browser_navigate",
						"url":  "/dashboard",
					},
				},
				{
					Name: "navigate_absolute",
					Args: map[string]interface{}{
						"tool": "browser_navigate",
						"url":  "http://{{CONTAINER_IP}}:3000/other",
					},
				},
			},
		},
	}

	_, err := runner.RunSuite(context.Background(), suite, []LoadedTest{test})
	if err != nil {
		t.Fatalf("RunSuite failed: %v", err)
	}

	// Check the recorded calls
	browserCalls := []mockCall{}
	for _, c := range executor.calls {
		if c.name == "browser_navigate" {
			browserCalls = append(browserCalls, c)
		}
	}

	if len(browserCalls) != 2 {
		t.Fatalf("expected 2 browser_navigate calls, got %d", len(browserCalls))
	}

	// First call: relative URL should be resolved to base_url + /dashboard
	url1 := fmt.Sprintf("%v", browserCalls[0].args["url"])
	if url1 != "http://10.99.0.5:3000/dashboard" {
		t.Errorf("relative URL not resolved, got: %s", url1)
	}

	// Second call: absolute URL should have {{CONTAINER_IP}} substituted
	url2 := fmt.Sprintf("%v", browserCalls[1].args["url"])
	if url2 != "http://10.99.0.5:3000/other" {
		t.Errorf("absolute URL not substituted, got: %s", url2)
	}
}

func TestBaseURLNotAppliedToNonBrowserTools(t *testing.T) {
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{"stdout": "ok", "exit_code": 0}, nil)

	runner := NewSuiteRunner(executor, nil, false)
	runner.SetVars(map[string]string{"CONTAINER_IP": "10.99.0.5"})

	suite := &LoadedSuite{
		Name: "test-no-base-url",
		Config: &config.AgentConfig{
			SuiteConfig: &config.DrillSuiteConfig{
				BaseURL: "http://{{CONTAINER_IP}}:3000",
			},
		},
	}

	test := LoadedTest{
		Name: "test-shell",
		Config: &config.AgentConfig{
			Description: "Shell command should not get base_url",
			Suite:       "test-no-base-url",
			DrillConfig: &config.DrillConfig{Tags: []string{"test"}},
			Nodes: []config.Node{
				{
					Name: "run_cmd",
					Args: map[string]interface{}{
						"tool":    "shell_command",
						"command": "curl /api/health",
					},
				},
			},
		},
	}

	_, err := runner.RunSuite(context.Background(), suite, []LoadedTest{test})
	if err != nil {
		t.Fatalf("RunSuite failed: %v", err)
	}

	// Verify shell_command was called with the original command (no base_url prepend)
	for _, c := range executor.calls {
		if c.name == "shell_command" {
			cmd := fmt.Sprintf("%v", c.args["command"])
			if cmd != "curl /api/health" {
				t.Errorf("shell_command args should not be modified, got: %s", cmd)
			}
		}
	}
}

func TestContainerIPFallbackToLocalhost(t *testing.T) {
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("browser_navigate", map[string]interface{}{"status": "ok"}, nil)

	runner := NewSuiteRunner(executor, nil, false)
	// Simulate CLI mode: CONTAINER_IP = localhost
	runner.SetVars(map[string]string{"CONTAINER_IP": "localhost"})

	suite := &LoadedSuite{
		Name: "test-localhost-fallback",
		Config: &config.AgentConfig{
			SuiteConfig: &config.DrillSuiteConfig{
				BaseURL: "http://{{CONTAINER_IP}}:3000",
			},
		},
	}

	test := LoadedTest{
		Name: "test-nav",
		Config: &config.AgentConfig{
			Description: "Test localhost fallback",
			Suite:       "test-localhost-fallback",
			DrillConfig: &config.DrillConfig{Tags: []string{"test"}},
			Nodes: []config.Node{
				{
					Name: "navigate",
					Args: map[string]interface{}{
						"tool": "browser_navigate",
						"url":  "/",
					},
				},
			},
		},
	}

	_, err := runner.RunSuite(context.Background(), suite, []LoadedTest{test})
	if err != nil {
		t.Fatalf("RunSuite failed: %v", err)
	}

	for _, c := range executor.calls {
		if c.name == "browser_navigate" {
			url := fmt.Sprintf("%v", c.args["url"])
			if url != "http://localhost:3000/" {
				t.Errorf("expected http://localhost:3000/, got: %s", url)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Tests for parameterized tests (Feature 1)
// ---------------------------------------------------------------------------

func TestParameterizedTestRunsMultipleTimes(t *testing.T) {
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{"stdout": "hello world", "exit_code": 0}, nil)

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_params",
			Config: &config.AgentConfig{
				Type:  "drill",
				Suite: "myapp",
				Parameters: []map[string]string{
					{"greeting": "hello", "target": "world"},
					{"greeting": "hi", "target": "there"},
					{"greeting": "hey", "target": "you"},
				},
				Nodes: []config.Node{
					{
						Name: "step1",
						Type: "tool",
						Args: map[string]interface{}{"tool": "shell_command", "command": "echo {{greeting}} {{target}}"},
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

	// Should have 3 test reports (one per parameter set)
	if len(report.Tests) != 3 {
		t.Fatalf("expected 3 test runs, got %d", len(report.Tests))
	}

	// Each should have the parameter set attached
	for i, tr := range report.Tests {
		if len(tr.ParameterSet) == 0 {
			t.Errorf("test[%d] missing ParameterSet", i)
		}
		if !strings.Contains(tr.Name, "[param") {
			t.Errorf("test[%d] name %q should contain '[param'", i, tr.Name)
		}
	}
}

func TestParameterizedTestSubstitutesVars(t *testing.T) {
	t.Parallel()
	executor := newMockExecutor()
	runner := NewSuiteRunner(executor, nil, false)
	runner.SetVars(map[string]string{"BASE": "original"})

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_subst",
			Config: &config.AgentConfig{
				Type:  "drill",
				Suite: "myapp",
				Parameters: []map[string]string{
					{"user": "alice"},
				},
				Nodes: []config.Node{
					{
						Name: "step1",
						Type: "tool",
						Args: map[string]interface{}{"tool": "shell_command", "command": "echo {{user}} {{BASE}}"},
					},
				},
			},
		},
	}

	report, _ := runner.RunSuite(context.Background(), suite, tests)
	if len(report.Tests) != 1 {
		t.Fatalf("expected 1 test run, got %d", len(report.Tests))
	}

	// Verify the command was substituted with both param and existing var
	if len(executor.calls) == 0 {
		t.Fatal("expected at least 1 call")
	}
	cmd := fmt.Sprintf("%v", executor.calls[0].args["command"])
	if cmd != "echo alice original" {
		t.Errorf("command = %q, want %q", cmd, "echo alice original")
	}

	// Verify original vars are restored (not polluted by params)
	if runner.vars["user"] != "" {
		t.Error("params should not leak into runner vars")
	}
}

func TestNonParameterizedTestUnchanged(t *testing.T) {
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{"stdout": "ok"}, nil)

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_normal",
			Config: &config.AgentConfig{
				Type:  "drill",
				Suite: "myapp",
				Nodes: []config.Node{
					{Name: "s1", Type: "tool", Args: map[string]interface{}{"tool": "shell_command"},
						Assert: &config.AssertConfig{Type: "contains", Expected: "ok"}},
				},
			},
		},
	}

	report, _ := runner.RunSuite(context.Background(), suite, tests)
	if len(report.Tests) != 1 {
		t.Fatalf("expected 1 test, got %d", len(report.Tests))
	}
	if report.Tests[0].ParameterSet != nil {
		t.Error("non-parameterized test should not have ParameterSet")
	}
}

// ---------------------------------------------------------------------------
// Tests for auto-wait (Feature 4)
// ---------------------------------------------------------------------------

func TestAutoWaitInjectsBrowserWaitFor(t *testing.T) {
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("browser_wait_for", map[string]interface{}{"status": "ok"}, nil)
	executor.SetResult("browser_click", map[string]interface{}{"status": "ok"}, nil)

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_autowait",
			Config: &config.AgentConfig{
				Type:        "drill",
				Suite:       "myapp",
				DrillConfig: &config.DrillConfig{AutoWait: true, AutoWaitTimeout: 3000},
				Nodes: []config.Node{
					{
						Name: "click-btn",
						Type: "tool",
						Args: map[string]interface{}{
							"tool":     "browser_click",
							"selector": "button.submit",
						},
					},
				},
			},
		},
	}

	report, _ := runner.RunSuite(context.Background(), suite, tests)
	if report.Tests[0].Status != "passed" {
		t.Errorf("test should pass, got %q", report.Tests[0].Status)
	}

	// Should have 2 calls: browser_wait_for + browser_click
	if len(executor.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d: %v", len(executor.calls), toolNames(executor.calls))
	}
	if executor.calls[0].name != "browser_wait_for" {
		t.Errorf("first call should be browser_wait_for, got %q", executor.calls[0].name)
	}
	if executor.calls[0].args["selector"] != "button.submit" {
		t.Errorf("wait_for selector = %v, want %q", executor.calls[0].args["selector"], "button.submit")
	}
	if executor.calls[0].args["timeout"] != 3000 {
		t.Errorf("wait_for timeout = %v, want 3000", executor.calls[0].args["timeout"])
	}
	if executor.calls[1].name != "browser_click" {
		t.Errorf("second call should be browser_click, got %q", executor.calls[1].name)
	}
}

func TestAutoWaitSkipsNonInteractiveTools(t *testing.T) {
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("browser_snapshot", map[string]interface{}{"snapshot": "page content"}, nil)

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_no_autowait",
			Config: &config.AgentConfig{
				Type:        "drill",
				Suite:       "myapp",
				DrillConfig: &config.DrillConfig{AutoWait: true},
				Nodes: []config.Node{
					{
						Name: "snapshot",
						Type: "tool",
						Args: map[string]interface{}{"tool": "browser_snapshot"},
					},
				},
			},
		},
	}

	runner.RunSuite(context.Background(), suite, tests)

	// Should only have 1 call (no wait_for injected for snapshot)
	if len(executor.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(executor.calls))
	}
	if executor.calls[0].name != "browser_snapshot" {
		t.Errorf("call should be browser_snapshot, got %q", executor.calls[0].name)
	}
}

func TestAutoWaitDisabledByDefault(t *testing.T) {
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("browser_click", map[string]interface{}{"status": "ok"}, nil)

	runner := NewSuiteRunner(executor, nil, false)

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_no_autowait",
			Config: &config.AgentConfig{
				Type:  "drill",
				Suite: "myapp",
				// No DrillConfig or AutoWait = false
				Nodes: []config.Node{
					{
						Name: "click",
						Type: "tool",
						Args: map[string]interface{}{
							"tool":     "browser_click",
							"selector": "button",
						},
					},
				},
			},
		},
	}

	runner.RunSuite(context.Background(), suite, tests)

	// Should only have 1 call (no wait_for when auto_wait is off)
	if len(executor.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(executor.calls))
	}
}

// ---------------------------------------------------------------------------
// Tests for isInteractiveBrowserTool and extractWaitTarget
// ---------------------------------------------------------------------------

func TestIsInteractiveBrowserTool(t *testing.T) {
	t.Parallel()
	interactive := []string{"browser_click", "browser_type", "browser_hover",
		"browser_select_option", "browser_fill_form", "browser_drag"}
	nonInteractive := []string{"browser_navigate", "browser_snapshot",
		"browser_take_screenshot", "browser_wait_for", "shell_command"}

	for _, name := range interactive {
		if !isInteractiveBrowserTool(name) {
			t.Errorf("%q should be interactive", name)
		}
	}
	for _, name := range nonInteractive {
		if isInteractiveBrowserTool(name) {
			t.Errorf("%q should NOT be interactive", name)
		}
	}
}

func TestExtractWaitTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
		want     string
	}{
		{
			name:     "selector arg",
			toolName: "browser_click",
			args:     map[string]interface{}{"selector": "button.submit"},
			want:     "button.submit",
		},
		{
			name:     "ref arg skipped",
			toolName: "browser_click",
			args:     map[string]interface{}{"ref": "ref5"},
			want:     "",
		},
		{
			name:     "no selector or ref",
			toolName: "browser_click",
			args:     map[string]interface{}{},
			want:     "",
		},
		{
			name:     "fill_form with fields",
			toolName: "browser_fill_form",
			args: map[string]interface{}{
				"fields": []interface{}{
					map[string]interface{}{"selector": "input#email", "value": "test@test.com"},
				},
			},
			want: "input#email",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractWaitTarget(tt.toolName, tt.args)
			if got != tt.want {
				t.Errorf("extractWaitTarget = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests for semantic assertions with mock LLM (Feature 2)
// ---------------------------------------------------------------------------

type mockLLMProvider struct {
	response string
	err      error
}

func (m *mockLLMProvider) EvaluateText(_ context.Context, _ string) (string, error) {
	return m.response, m.err
}

func TestSemanticAssertionWithLLM(t *testing.T) {
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{
		"stdout":    `{"status": "healthy", "uptime": "24h"}`,
		"exit_code": 0,
	}, nil)

	runner := NewSuiteRunner(executor, nil, false)
	runner.SetLLMProvider(&mockLLMProvider{response: "YES\nThe output contains a healthy status"})

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_semantic",
			Config: &config.AgentConfig{
				Type:  "drill",
				Suite: "myapp",
				Nodes: []config.Node{
					{
						Name: "check-health",
						Type: "tool",
						Args: map[string]interface{}{"tool": "shell_command", "command": "curl health"},
						Assert: &config.AssertConfig{
							Type:     "semantic",
							Expected: "The response indicates the service is healthy",
						},
					},
				},
			},
		},
	}

	report, _ := runner.RunSuite(context.Background(), suite, tests)
	if report.Tests[0].Status != "passed" {
		t.Errorf("semantic assertion should pass, got %q", report.Tests[0].Status)
	}
	if report.Tests[0].Steps[0].Assertion == nil || !report.Tests[0].Steps[0].Assertion.Passed {
		t.Error("assertion should be passed")
	}
}

func TestSemanticAssertionFailsWithoutLLM(t *testing.T) {
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{"stdout": "data"}, nil)

	runner := NewSuiteRunner(executor, nil, false)
	// No LLM provider set

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_no_llm",
			Config: &config.AgentConfig{
				Type:  "drill",
				Suite: "myapp",
				Nodes: []config.Node{
					{
						Name: "check",
						Type: "tool",
						Args: map[string]interface{}{"tool": "shell_command"},
						Assert: &config.AssertConfig{
							Type:     "semantic",
							Expected: "response is valid",
						},
					},
				},
			},
		},
	}

	report, _ := runner.RunSuite(context.Background(), suite, tests)
	// Without LLM, falls through to deterministic Evaluate() which returns failure
	if report.Tests[0].Status != "failed" {
		t.Errorf("semantic assertion without LLM should fail, got %q", report.Tests[0].Status)
	}
}

func TestSemanticAssertionLLMSaysNo(t *testing.T) {
	t.Parallel()
	executor := newMockExecutor()
	executor.SetResult("shell_command", map[string]interface{}{"stdout": "error: bad request"}, nil)

	runner := NewSuiteRunner(executor, nil, false)
	runner.SetLLMProvider(&mockLLMProvider{response: "NO\nThe output indicates an error, not a success"})

	suite := &LoadedSuite{
		Name: "myapp",
		Config: &config.AgentConfig{
			Type:        "drill_suite",
			SuiteConfig: &config.DrillSuiteConfig{},
		},
	}

	tests := []LoadedTest{
		{
			Name: "test_llm_no",
			Config: &config.AgentConfig{
				Type:  "drill",
				Suite: "myapp",
				Nodes: []config.Node{
					{
						Name: "check",
						Type: "tool",
						Args: map[string]interface{}{"tool": "shell_command"},
						Assert: &config.AssertConfig{
							Type:     "semantic",
							Expected: "The response indicates success",
						},
					},
				},
			},
		},
	}

	report, _ := runner.RunSuite(context.Background(), suite, tests)
	if report.Tests[0].Status != "failed" {
		t.Errorf("semantic assertion with LLM NO should fail, got %q", report.Tests[0].Status)
	}
}

// helper
func toolNames(calls []mockCall) []string {
	var names []string
	for _, c := range calls {
		names = append(names, c.name)
	}
	return names
}
