package tools

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adrill "github.com/schardosin/astonish/pkg/drill"
)

// ---------------------------------------------------------------------------
// executeRunDrill argument validation tests
// ---------------------------------------------------------------------------

func TestExecuteRunDrill_EmptySuiteName(t *testing.T) {
	deps := &runDrillDeps{}
	result, err := executeRunDrill(nil, deps, RunDrillArgs{SuiteName: ""})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Summary, "suite_name is required") {
		t.Errorf("Summary = %q", result.Summary)
	}
}

func TestExecuteRunDrill_SuiteNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	deps := &runDrillDeps{}
	result, err := executeRunDrill(nil, deps, RunDrillArgs{SuiteName: "nonexistent-suite"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Summary, "not found") {
		t.Errorf("Summary = %q", result.Summary)
	}
}

func TestExecuteRunDrill_StripYAMLExtension(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	deps := &runDrillDeps{}
	// This should strip .yaml and then fail with "not found" (not "empty suite name")
	result, err := executeRunDrill(nil, deps, RunDrillArgs{SuiteName: "someapp.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Summary, "not found") {
		t.Errorf("Summary = %q, expected 'not found' (not 'suite_name is required')", result.Summary)
	}
}

func TestExecuteRunDrill_TestNameNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	flowsDir := filepath.Join(tmpDir, "astonish", "flows")
	os.MkdirAll(flowsDir, 0o755)

	// Create a valid suite with one test
	suiteYAML := "description: App\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	os.WriteFile(filepath.Join(flowsDir, "testapp.yaml"), []byte(suiteYAML), 0o644)
	drillYAML := "type: drill\nsuite: testapp\ndescription: s\nnodes:\n  - name: s\n    type: tool\n    args:\n      tool: shell_command\n      command: echo hi\n    assert:\n      type: contains\n      expected: hi"
	os.WriteFile(filepath.Join(flowsDir, "existing-drill.yaml"), []byte(drillYAML), 0o644)

	deps := &runDrillDeps{}
	result, err := executeRunDrill(nil, deps, RunDrillArgs{
		SuiteName: "testapp",
		TestName:  "nonexistent-drill",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Summary, "not found") {
		t.Errorf("Summary = %q", result.Summary)
	}
}

func TestExecuteRunDrill_TagFilterNoMatch(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	flowsDir := filepath.Join(tmpDir, "astonish", "flows")
	os.MkdirAll(flowsDir, 0o755)

	suiteYAML := "description: App\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	os.WriteFile(filepath.Join(flowsDir, "testapp.yaml"), []byte(suiteYAML), 0o644)
	drillYAML := "type: drill\nsuite: testapp\ndescription: s\ndrill_config:\n  tags: [smoke]\nnodes:\n  - name: s\n    type: tool\n    args:\n      tool: shell_command\n      command: echo hi\n    assert:\n      type: contains\n      expected: hi"
	os.WriteFile(filepath.Join(flowsDir, "smoke-test.yaml"), []byte(drillYAML), 0o644)

	deps := &runDrillDeps{}
	result, err := executeRunDrill(nil, deps, RunDrillArgs{
		SuiteName: "testapp",
		Tag:       "regression",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "passed" {
		t.Errorf("Status = %q, want passed (no matching tests = vacuous pass)", result.Status)
	}
	if !strings.Contains(result.Summary, "No tests matching") {
		t.Errorf("Summary = %q", result.Summary)
	}
}

func TestExecuteRunDrill_RunsLocally(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	flowsDir := filepath.Join(tmpDir, "astonish", "flows")
	os.MkdirAll(flowsDir, 0o755)

	suiteYAML := "description: Echo App\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	os.WriteFile(filepath.Join(flowsDir, "echoapp.yaml"), []byte(suiteYAML), 0o644)
	drillYAML := "type: drill\nsuite: echoapp\ndescription: echo test\nnodes:\n  - name: echo-step\n    type: tool\n    args:\n      tool: shell_command\n      command: \"echo hello-drill-test\"\n    assert:\n      type: contains\n      expected: \"hello-drill-test\""
	os.WriteFile(filepath.Join(flowsDir, "echo-test.yaml"), []byte(drillYAML), 0o644)

	deps := &runDrillDeps{} // no sandbox
	result, err := executeRunDrill(nil, deps, RunDrillArgs{
		SuiteName: "echoapp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "passed" {
		t.Errorf("Status = %q, want passed. Report:\n%s", result.Status, result.Report)
	}
	if !strings.Contains(result.Summary, "1/1") {
		t.Errorf("Summary = %q, want 1/1 passed", result.Summary)
	}
}

func TestExecuteRunDrill_SingleTestFilter(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	flowsDir := filepath.Join(tmpDir, "astonish", "flows")
	os.MkdirAll(flowsDir, 0o755)

	suiteYAML := "description: App\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	os.WriteFile(filepath.Join(flowsDir, "multiapp.yaml"), []byte(suiteYAML), 0o644)

	drill1 := "type: drill\nsuite: multiapp\ndescription: test1\nnodes:\n  - name: s\n    type: tool\n    args:\n      tool: shell_command\n      command: \"echo aaa\"\n    assert:\n      type: contains\n      expected: aaa"
	drill2 := "type: drill\nsuite: multiapp\ndescription: test2\nnodes:\n  - name: s\n    type: tool\n    args:\n      tool: shell_command\n      command: \"echo bbb\"\n    assert:\n      type: contains\n      expected: bbb"
	os.WriteFile(filepath.Join(flowsDir, "drill-one.yaml"), []byte(drill1), 0o644)
	os.WriteFile(filepath.Join(flowsDir, "drill-two.yaml"), []byte(drill2), 0o644)

	deps := &runDrillDeps{}
	result, err := executeRunDrill(nil, deps, RunDrillArgs{
		SuiteName: "multiapp",
		TestName:  "drill-one",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "passed" {
		t.Errorf("Status = %q, want passed. Report:\n%s", result.Status, result.Report)
	}
	// Should only run 1 test, not 2
	if !strings.Contains(result.Summary, "1/1") {
		t.Errorf("Summary = %q, want 1/1 (single test filter)", result.Summary)
	}
}

func TestExecuteRunDrill_AssertionFail(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	flowsDir := filepath.Join(tmpDir, "astonish", "flows")
	os.MkdirAll(flowsDir, 0o755)

	suiteYAML := "description: Fail App\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	os.WriteFile(filepath.Join(flowsDir, "failapp.yaml"), []byte(suiteYAML), 0o644)
	drillYAML := "type: drill\nsuite: failapp\ndescription: failing test\nnodes:\n  - name: step1\n    type: tool\n    args:\n      tool: shell_command\n      command: \"echo actual-output\"\n    assert:\n      type: contains\n      expected: \"expected-but-missing\""
	os.WriteFile(filepath.Join(flowsDir, "fail-test.yaml"), []byte(drillYAML), 0o644)

	deps := &runDrillDeps{}
	result, err := executeRunDrill(nil, deps, RunDrillArgs{
		SuiteName: "failapp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.Summary, "0/1") {
		t.Errorf("Summary = %q, want 0/1", result.Summary)
	}
}

// ---------------------------------------------------------------------------
// testCompositeExecutor routing tests
// ---------------------------------------------------------------------------

func TestCompositeExecutor_RoutesToBrowser(t *testing.T) {
	// Test routing by checking the tool name maps directly.
	// These maps determine how testCompositeExecutor routes calls.

	browserTools := []string{
		"browser_navigate", "browser_snapshot", "browser_click",
		"browser_type", "browser_run_code", "browser_wait_for",
	}
	for _, tool := range browserTools {
		if !testBrowserToolNames[tool] {
			t.Errorf("expected %q in testBrowserToolNames", tool)
		}
	}

	containerTools := []string{
		"shell_command", "read_file", "write_file", "grep_search",
		"http_request", "web_fetch",
	}
	for _, tool := range containerTools {
		if !testContainerTools[tool] {
			t.Errorf("expected %q in testContainerTools", tool)
		}
	}

	// Verify no overlap between browser and container tool lists
	for tool := range testBrowserToolNames {
		if testContainerTools[tool] {
			t.Errorf("tool %q appears in both browser and container lists — routing ambiguity", tool)
		}
	}
}

func TestContainerToolMap_Completeness(t *testing.T) {
	// Verify key tools are in the container tools map
	expected := []string{
		"shell_command", "read_file", "write_file", "edit_file",
		"file_tree", "grep_search", "find_files",
		"process_read", "process_write", "process_list", "process_kill",
		"http_request", "web_fetch",
	}
	for _, tool := range expected {
		if !testContainerTools[tool] {
			t.Errorf("expected %q in testContainerTools", tool)
		}
	}
}

func TestBrowserToolMap_Completeness(t *testing.T) {
	// Verify all core browser tools are present
	expected := []string{
		"browser_navigate", "browser_navigate_back",
		"browser_click", "browser_type", "browser_hover",
		"browser_press_key", "browser_select_option", "browser_fill_form",
		"browser_snapshot", "browser_take_screenshot",
		"browser_wait_for", "browser_evaluate", "browser_run_code",
		"browser_console_messages", "browser_network_requests",
		"browser_tabs", "browser_close", "browser_resize",
		"browser_cookies", "browser_storage",
	}
	for _, tool := range expected {
		if !testBrowserToolNames[tool] {
			t.Errorf("expected %q in testBrowserToolNames", tool)
		}
	}
}

// ---------------------------------------------------------------------------
// templateDisplay tests
// ---------------------------------------------------------------------------

func TestTemplateDisplay_Empty(t *testing.T) {
	if got := templateDisplay(""); got != "@base" {
		t.Errorf("templateDisplay(\"\") = %q, want @base", got)
	}
}

func TestTemplateDisplay_Named(t *testing.T) {
	if got := templateDisplay("juicytrade"); got != "juicytrade" {
		t.Errorf("templateDisplay(\"juicytrade\") = %q", got)
	}
}

// ---------------------------------------------------------------------------
// enrichReportWithFailureContext tests
// ---------------------------------------------------------------------------

func TestEnrichReportWithFailureContext(t *testing.T) {
	report := &adrill.SuiteReport{
		Suite:  "testapp",
		Status: "failed",
		Tests: []adrill.TestReport{
			{
				Name:   "health-check",
				Status: "failed",
				Steps: []adrill.StepResult{
					{
						Name:   "step1",
						Tool:   "shell_command",
						Status: "failed",
						Output: "error: connection refused",
						Assertion: &adrill.AssertionResult{
							Passed:  false,
							Message: `expected "ok" but output does not contain it`,
						},
					},
				},
			},
			{
				Name:   "passing-test",
				Status: "passed",
				Steps: []adrill.StepResult{
					{
						Name:   "ok-step",
						Tool:   "shell_command",
						Status: "passed",
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	enrichReportWithFailureContext(&buf, report)
	output := buf.String()

	if !strings.Contains(output, "health-check") {
		t.Error("enriched report should mention the failing test name")
	}
	if !strings.Contains(output, "step1") {
		t.Error("enriched report should mention the failing step name")
	}
	if !strings.Contains(output, "connection refused") {
		t.Error("enriched report should include the step output")
	}
	// Should NOT include passing test details
	if strings.Contains(output, "ok-step") {
		t.Error("enriched report should not include passing step details")
	}
}

// adrill is imported for report struct types
var _ adrill.SuiteReport
