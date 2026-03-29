package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// saveDrill tests
// ---------------------------------------------------------------------------

func TestSaveDrill_EmptySuiteName(t *testing.T) {
	result, err := saveDrill(nil, SaveDrillArgs{
		SuiteName: "",
		Tests:     []DrillFileArg{{Name: "test1", YAML: "foo"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, "suite_name is required") {
		t.Errorf("Message = %q", result.Message)
	}
}

func TestSaveDrill_NoTests(t *testing.T) {
	result, err := saveDrill(nil, SaveDrillArgs{
		SuiteName: "myapp",
		Tests:     nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, "at least one drill") {
		t.Errorf("Message = %q", result.Message)
	}
}

func TestSaveDrill_InvalidSuiteYAML(t *testing.T) {
	result, err := saveDrill(nil, SaveDrillArgs{
		SuiteName: "myapp",
		SuiteYAML: ":::invalid yaml",
		Tests:     []DrillFileArg{{Name: "test1", YAML: "foo"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, "invalid suite YAML") {
		t.Errorf("Message = %q", result.Message)
	}
}

func TestSaveDrill_WrongSuiteType(t *testing.T) {
	result, err := saveDrill(nil, SaveDrillArgs{
		SuiteName: "myapp",
		SuiteYAML: "type: drill\ndescription: oops",
		Tests:     []DrillFileArg{{Name: "test1", YAML: "foo"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, "drill_suite") {
		t.Errorf("Message = %q", result.Message)
	}
}

func TestSaveDrill_MissingSuiteConfig(t *testing.T) {
	result, err := saveDrill(nil, SaveDrillArgs{
		SuiteName: "myapp",
		SuiteYAML: "type: drill_suite\ndescription: test",
		Tests:     []DrillFileArg{{Name: "test1", YAML: "foo"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, "suite_config") {
		t.Errorf("Message = %q", result.Message)
	}
}

func TestSaveDrill_TestMissingName(t *testing.T) {
	result, err := saveDrill(nil, SaveDrillArgs{
		SuiteName: "myapp",
		SuiteYAML: "",
		Tests:     []DrillFileArg{{Name: "", YAML: "some yaml"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, "name is required") {
		t.Errorf("Message = %q", result.Message)
	}
}

func TestSaveDrill_TestMissingYAML(t *testing.T) {
	result, err := saveDrill(nil, SaveDrillArgs{
		SuiteName: "myapp",
		SuiteYAML: "",
		Tests:     []DrillFileArg{{Name: "test1", YAML: ""}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, "yaml is required") {
		t.Errorf("Message = %q", result.Message)
	}
}

func TestSaveDrill_TestInvalidYAML(t *testing.T) {
	result, err := saveDrill(nil, SaveDrillArgs{
		SuiteName: "myapp",
		SuiteYAML: "",
		Tests:     []DrillFileArg{{Name: "test1", YAML: ":::bad"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, "invalid YAML") {
		t.Errorf("Message = %q", result.Message)
	}
}

func TestSaveDrill_TestWrongType(t *testing.T) {
	result, err := saveDrill(nil, SaveDrillArgs{
		SuiteName: "myapp",
		SuiteYAML: "",
		Tests: []DrillFileArg{{
			Name: "test1",
			YAML: "type: drill_suite\nsuite: myapp\nnodes:\n  - name: s\n    type: tool\n    args:\n      tool: shell_command\n      command: echo hi",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, "must have type: drill") {
		t.Errorf("Message = %q", result.Message)
	}
}

func TestSaveDrill_TestWrongSuiteRef(t *testing.T) {
	result, err := saveDrill(nil, SaveDrillArgs{
		SuiteName: "myapp",
		SuiteYAML: "",
		Tests: []DrillFileArg{{
			Name: "test1",
			YAML: "type: drill\nsuite: other_suite\nnodes:\n  - name: s\n    type: tool\n    args:\n      tool: shell_command\n      command: echo hi",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, `suite field must be "myapp"`) {
		t.Errorf("Message = %q", result.Message)
	}
}

func TestSaveDrill_TestNoNodes(t *testing.T) {
	result, err := saveDrill(nil, SaveDrillArgs{
		SuiteName: "myapp",
		SuiteYAML: "",
		Tests: []DrillFileArg{{
			Name: "test1",
			YAML: "type: drill\nsuite: myapp",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, "at least one node") {
		t.Errorf("Message = %q", result.Message)
	}
}

func TestSaveDrill_AppendMode_SavesFiles(t *testing.T) {
	// Set up a temp directory and override XDG_CONFIG_HOME so flowstore
	// resolves there. This avoids touching the real config.
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	flowsDir := filepath.Join(tmpDir, "astonish", "flows")
	os.MkdirAll(flowsDir, 0o755)

	// Write a stub suite so save_drill's append mode works
	suiteYAML := "description: test suite\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	os.WriteFile(filepath.Join(flowsDir, "myapp.yaml"), []byte(suiteYAML), 0o644)

	drillYAML := "type: drill\nsuite: myapp\nnodes:\n  - name: step1\n    type: tool\n    args:\n      tool: shell_command\n      command: echo hi\n    assert:\n      type: contains\n      expected: hi"
	result, err := saveDrill(nil, SaveDrillArgs{
		SuiteName: "myapp",
		SuiteYAML: "", // append mode
		Tests:     []DrillFileArg{{Name: "health-check", YAML: drillYAML}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "saved" {
		t.Fatalf("Status = %q, want saved. Message = %q", result.Status, result.Message)
	}
	if len(result.TestPaths) != 1 {
		t.Fatalf("TestPaths len = %d, want 1", len(result.TestPaths))
	}
	if result.SuitePath != "" {
		t.Errorf("SuitePath should be empty in append mode, got %q", result.SuitePath)
	}

	// Verify file was written
	content, err := os.ReadFile(result.TestPaths[0])
	if err != nil {
		t.Fatalf("failed to read drill file: %v", err)
	}
	if !strings.Contains(string(content), "type: drill") {
		t.Error("drill file does not contain expected content")
	}
}

func TestSaveDrill_FullMode_SavesSuiteAndTests(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	suiteYAML := "description: test suite\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	drillYAML := "type: drill\nsuite: newapp\nnodes:\n  - name: step1\n    type: tool\n    args:\n      tool: shell_command\n      command: echo hi\n    assert:\n      type: contains\n      expected: hi"

	result, err := saveDrill(nil, SaveDrillArgs{
		SuiteName: "newapp",
		SuiteYAML: suiteYAML,
		Tests:     []DrillFileArg{{Name: "test1", YAML: drillYAML}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "saved" {
		t.Fatalf("Status = %q, Message = %q", result.Status, result.Message)
	}
	if result.SuitePath == "" {
		t.Error("SuitePath should be set in full mode")
	}
	if len(result.TestPaths) != 1 {
		t.Fatalf("TestPaths len = %d, want 1", len(result.TestPaths))
	}

	// Verify suite was written
	content, err := os.ReadFile(result.SuitePath)
	if err != nil {
		t.Fatalf("failed to read suite file: %v", err)
	}
	if !strings.Contains(string(content), "drill_suite") {
		t.Error("suite file does not contain expected content")
	}
}

// ---------------------------------------------------------------------------
// validateDrill tests
// ---------------------------------------------------------------------------

func TestValidateDrill_ValidSuiteAndTest(t *testing.T) {
	suiteYAML := `
type: drill_suite
description: Test suite
suite_config:
  setup:
    - echo hi
  teardown:
    - echo bye
`
	testYAML := `
type: drill
suite: test-suite
description: health check
nodes:
  - name: check
    type: tool
    args:
      tool: shell_command
      command: "curl localhost:8080/health"
    assert:
      type: contains
      expected: "ok"
`
	result, err := validateDrill(nil, ValidateDrillArgs{
		SuiteYAML: suiteYAML,
		TestYAMLs: []string{testYAML},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "passed" {
		t.Errorf("Status = %q, want passed", result.Status)
		for _, c := range result.Checks {
			if c.Status == "failed" {
				t.Logf("  FAIL: %s — %s", c.Name, c.Message)
			}
		}
	}
}

func TestValidateDrill_InvalidSuiteYAML(t *testing.T) {
	result, err := validateDrill(nil, ValidateDrillArgs{
		SuiteYAML: ":::invalid",
		TestYAMLs: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed", result.Status)
	}
	hasParseFail := false
	for _, c := range result.Checks {
		if c.Name == "suite_parse" && c.Status == "failed" {
			hasParseFail = true
		}
	}
	if !hasParseFail {
		t.Error("expected suite_parse failed check")
	}
}

func TestValidateDrill_WrongSuiteType(t *testing.T) {
	result, err := validateDrill(nil, ValidateDrillArgs{
		SuiteYAML: "type: drill\ndescription: wrong",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed", result.Status)
	}
}

func TestValidateDrill_MissingSuiteConfig(t *testing.T) {
	result, err := validateDrill(nil, ValidateDrillArgs{
		SuiteYAML: "type: drill_suite\ndescription: no config",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed", result.Status)
	}
}

func TestValidateDrill_InvalidTestYAML(t *testing.T) {
	suiteYAML := "type: drill_suite\ndescription: s\nsuite_config:\n  setup: []\n"
	result, err := validateDrill(nil, ValidateDrillArgs{
		SuiteYAML: suiteYAML,
		TestYAMLs: []string{":::bad"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed", result.Status)
	}
}

func TestValidateDrill_TestWrongType(t *testing.T) {
	suiteYAML := "type: drill_suite\ndescription: s\nsuite_config:\n  setup: []\n"
	testYAML := "type: workflow\nnodes:\n  - name: s\n    type: tool\n    args:\n      tool: shell_command\n      command: echo"
	result, err := validateDrill(nil, ValidateDrillArgs{
		SuiteYAML: suiteYAML,
		TestYAMLs: []string{testYAML},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed", result.Status)
	}
}

func TestValidateDrill_TestNoNodes(t *testing.T) {
	suiteYAML := "type: drill_suite\ndescription: s\nsuite_config:\n  setup: []\n"
	testYAML := "type: drill\nsuite: s\ndescription: empty"
	result, err := validateDrill(nil, ValidateDrillArgs{
		SuiteYAML: suiteYAML,
		TestYAMLs: []string{testYAML},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed", result.Status)
	}
}

func TestValidateDrill_TestNoAssertions(t *testing.T) {
	suiteYAML := "type: drill_suite\ndescription: s\nsuite_config:\n  setup: []\n"
	testYAML := "type: drill\nsuite: s\nnodes:\n  - name: step1\n    type: tool\n    args:\n      tool: shell_command\n      command: echo hi"
	result, err := validateDrill(nil, ValidateDrillArgs{
		SuiteYAML: suiteYAML,
		TestYAMLs: []string{testYAML},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed — no assertions should be flagged", result.Status)
	}
}

func TestValidateDrill_NodeWrongType(t *testing.T) {
	suiteYAML := "type: drill_suite\ndescription: s\nsuite_config:\n  setup: []\n"
	testYAML := "type: drill\nsuite: s\nnodes:\n  - name: step1\n    type: shell\n    args:\n      command: echo hi\n    assert:\n      type: contains\n      expected: hi"
	result, err := validateDrill(nil, ValidateDrillArgs{
		SuiteYAML: suiteYAML,
		TestYAMLs: []string{testYAML},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed — node type 'shell' should fail", result.Status)
	}
}

func TestValidateDrill_NodeMissingArgsTool(t *testing.T) {
	suiteYAML := "type: drill_suite\ndescription: s\nsuite_config:\n  setup: []\n"
	testYAML := "type: drill\nsuite: s\nnodes:\n  - name: step1\n    type: tool\n    args:\n      command: echo hi\n    assert:\n      type: contains\n      expected: hi"
	result, err := validateDrill(nil, ValidateDrillArgs{
		SuiteYAML: suiteYAML,
		TestYAMLs: []string{testYAML},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed — missing args.tool", result.Status)
	}
}

func TestValidateDrill_InvalidAssertionType(t *testing.T) {
	suiteYAML := "type: drill_suite\ndescription: s\nsuite_config:\n  setup: []\n"
	testYAML := "type: drill\nsuite: s\nnodes:\n  - name: step1\n    type: tool\n    args:\n      tool: shell_command\n      command: echo hi\n    assert:\n      type: fuzzy_match\n      expected: hi"
	result, err := validateDrill(nil, ValidateDrillArgs{
		SuiteYAML: suiteYAML,
		TestYAMLs: []string{testYAML},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed — unknown assertion type", result.Status)
	}
}

func TestValidateDrill_ServiceValidation(t *testing.T) {
	suiteYAML := `
type: drill_suite
description: multi-service
suite_config:
  services:
    - name: backend
      setup: "./server &"
      ready_check:
        type: http
        url: "http://localhost:8080/health"
      teardown: "pkill server"
    - name: frontend
      setup: "npm start &"
      ready_check:
        type: port
        port: 3000
      teardown: "pkill node"
`
	result, err := validateDrill(nil, ValidateDrillArgs{
		SuiteYAML: suiteYAML,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "passed" {
		t.Errorf("Status = %q, want passed", result.Status)
		for _, c := range result.Checks {
			if c.Status == "failed" {
				t.Logf("  FAIL: %s — %s", c.Name, c.Message)
			}
		}
	}
}

func TestValidateDrill_ServiceMissingName(t *testing.T) {
	suiteYAML := `
type: drill_suite
description: s
suite_config:
  services:
    - setup: "./server &"
`
	result, err := validateDrill(nil, ValidateDrillArgs{SuiteYAML: suiteYAML})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed — service missing name", result.Status)
	}
}

func TestValidateDrill_ServiceDuplicateName(t *testing.T) {
	suiteYAML := `
type: drill_suite
description: s
suite_config:
  services:
    - name: backend
      setup: "./a"
    - name: backend
      setup: "./b"
`
	result, err := validateDrill(nil, ValidateDrillArgs{SuiteYAML: suiteYAML})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed — duplicate service name", result.Status)
	}
}

func TestValidateDrill_ServiceHTTPReadyCheckMissingURL(t *testing.T) {
	suiteYAML := `
type: drill_suite
description: s
suite_config:
  services:
    - name: api
      setup: "./server &"
      ready_check:
        type: http
`
	result, err := validateDrill(nil, ValidateDrillArgs{SuiteYAML: suiteYAML})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed", result.Status)
	}
}

func TestValidateDrill_ServicePortReadyCheckMissingPort(t *testing.T) {
	suiteYAML := `
type: drill_suite
description: s
suite_config:
  services:
    - name: api
      setup: "./server &"
      ready_check:
        type: port
`
	result, err := validateDrill(nil, ValidateDrillArgs{SuiteYAML: suiteYAML})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed", result.Status)
	}
}

func TestValidateDrill_LegacyReadyCheckHTTP(t *testing.T) {
	suiteYAML := `
type: drill_suite
description: s
suite_config:
  setup:
    - "./server &"
  ready_check:
    type: http
    url: "http://localhost:8080/health"
  teardown:
    - "pkill server"
`
	result, err := validateDrill(nil, ValidateDrillArgs{SuiteYAML: suiteYAML})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "passed" {
		t.Errorf("Status = %q, want passed", result.Status)
		for _, c := range result.Checks {
			if c.Status == "failed" {
				t.Logf("  FAIL: %s — %s", c.Name, c.Message)
			}
		}
	}
}

func TestValidateDrill_LegacyReadyCheckMissingType(t *testing.T) {
	suiteYAML := `
type: drill_suite
description: s
suite_config:
  setup: []
  ready_check:
    url: "http://localhost:8080/health"
`
	result, err := validateDrill(nil, ValidateDrillArgs{SuiteYAML: suiteYAML})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" {
		t.Errorf("Status = %q, want failed — ready_check missing type", result.Status)
	}
}

// ---------------------------------------------------------------------------
// deleteDrill tests
// ---------------------------------------------------------------------------

func TestDeleteDrill_NoArgs(t *testing.T) {
	result, err := deleteDrill(nil, DeleteDrillArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, "suite_name or test_name is required") {
		t.Errorf("Message = %q", result.Message)
	}
}

func TestDeleteDrill_NonexistentTest(t *testing.T) {
	result, err := deleteDrill(nil, DeleteDrillArgs{TestName: "nonexistent-drill-99"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error for nonexistent drill", result.Status)
	}
}

func TestDeleteDrill_NonexistentSuite(t *testing.T) {
	result, err := deleteDrill(nil, DeleteDrillArgs{SuiteName: "nonexistent-suite-99"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error for nonexistent suite", result.Status)
	}
}

// ---------------------------------------------------------------------------
// listDrills tests
// ---------------------------------------------------------------------------

func TestListDrills_NoSuites(t *testing.T) {
	// Point to empty temp dir
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	result, err := listDrills(nil, ListDrillsArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want ok", result.Status)
	}
	if !strings.Contains(result.Message, "No drill suites") {
		t.Errorf("Message = %q, want no suites message", result.Message)
	}
}

func TestListDrills_NonexistentSuite(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	result, err := listDrills(nil, ListDrillsArgs{SuiteName: "nonexistent"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
}

func TestListDrills_WithSuiteAndDrills(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	flowsDir := filepath.Join(tmpDir, "astonish", "flows")
	os.MkdirAll(flowsDir, 0o755)

	// Write a suite
	suiteYAML := "description: Test App\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	os.WriteFile(filepath.Join(flowsDir, "testapp.yaml"), []byte(suiteYAML), 0o644)

	// Write a drill
	drillYAML := "type: drill\nsuite: testapp\ndescription: health check\nnodes:\n  - name: step1\n    type: tool\n    args:\n      tool: shell_command\n      command: echo"
	os.WriteFile(filepath.Join(flowsDir, "health-check.yaml"), []byte(drillYAML), 0o644)

	result, err := listDrills(nil, ListDrillsArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want ok", result.Status)
	}
	if len(result.Suites) != 1 {
		t.Fatalf("Suites len = %d, want 1", len(result.Suites))
	}
	if result.Suites[0].Name != "testapp" {
		t.Errorf("Suite name = %q, want testapp", result.Suites[0].Name)
	}
	if result.Suites[0].DrillCount != 1 {
		t.Errorf("DrillCount = %d, want 1", result.Suites[0].DrillCount)
	}
	if len(result.Suites[0].Drills) != 1 {
		t.Fatalf("Drills len = %d, want 1", len(result.Suites[0].Drills))
	}
	if result.Suites[0].Drills[0].Name != "health-check" {
		t.Errorf("Drill name = %q, want health-check", result.Suites[0].Drills[0].Name)
	}
}

func TestListDrills_SpecificSuite(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	flowsDir := filepath.Join(tmpDir, "astonish", "flows")
	os.MkdirAll(flowsDir, 0o755)

	suiteYAML := "description: My App\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	os.WriteFile(filepath.Join(flowsDir, "myapp.yaml"), []byte(suiteYAML), 0o644)

	drillYAML := "type: drill\nsuite: myapp\ndescription: api check\nnodes:\n  - name: s\n    type: tool\n    args:\n      tool: shell_command\n      command: echo"
	os.WriteFile(filepath.Join(flowsDir, "api-check.yaml"), []byte(drillYAML), 0o644)

	result, err := listDrills(nil, ListDrillsArgs{SuiteName: "myapp"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want ok", result.Status)
	}
	if len(result.Suites) != 1 {
		t.Fatalf("Suites len = %d, want 1", len(result.Suites))
	}
	if result.Suites[0].Name != "myapp" {
		t.Errorf("Suite name = %q", result.Suites[0].Name)
	}
}

// ---------------------------------------------------------------------------
// readDrill tests
// ---------------------------------------------------------------------------

func TestReadDrill_EmptyName(t *testing.T) {
	result, err := readDrill(nil, ReadDrillArgs{Name: ""})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, "name is required") {
		t.Errorf("Message = %q", result.Message)
	}
}

func TestReadDrill_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	result, err := readDrill(nil, ReadDrillArgs{Name: "nonexistent"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, "No drill or suite") {
		t.Errorf("Message = %q", result.Message)
	}
}

func TestReadDrill_ReadDrillYAML(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	flowsDir := filepath.Join(tmpDir, "astonish", "flows")
	os.MkdirAll(flowsDir, 0o755)

	// Suite
	suiteYAML := "description: App\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	os.WriteFile(filepath.Join(flowsDir, "testapp.yaml"), []byte(suiteYAML), 0o644)

	// Drill
	drillYAML := "type: drill\nsuite: testapp\ndescription: the health check\nnodes:\n  - name: step1\n    type: tool\n    args:\n      tool: shell_command\n      command: echo hi\n    assert:\n      type: contains\n      expected: hi"
	os.WriteFile(filepath.Join(flowsDir, "health-check.yaml"), []byte(drillYAML), 0o644)

	result, err := readDrill(nil, ReadDrillArgs{Name: "health-check"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" {
		t.Fatalf("Status = %q, Message = %q", result.Status, result.Message)
	}
	if result.Type != "drill_yaml" {
		t.Errorf("Type = %q, want drill_yaml", result.Type)
	}
	if !strings.Contains(result.Content, "type: drill") {
		t.Error("Content does not contain expected drill YAML")
	}
}

func TestReadDrill_ReadSuiteOverview(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	flowsDir := filepath.Join(tmpDir, "astonish", "flows")
	os.MkdirAll(flowsDir, 0o755)

	suiteYAML := "description: My App\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	os.WriteFile(filepath.Join(flowsDir, "myapp.yaml"), []byte(suiteYAML), 0o644)

	drillYAML := "type: drill\nsuite: myapp\ndescription: api test\nnodes:\n  - name: s\n    type: tool\n    args:\n      tool: shell_command\n      command: echo"
	os.WriteFile(filepath.Join(flowsDir, "api-test.yaml"), []byte(drillYAML), 0o644)

	result, err := readDrill(nil, ReadDrillArgs{Name: "myapp"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" {
		t.Fatalf("Status = %q, Message = %q", result.Status, result.Message)
	}
	if result.Type != "suite_overview" {
		t.Errorf("Type = %q, want suite_overview", result.Type)
	}
	if !strings.Contains(result.Content, "myapp") {
		t.Error("Content does not contain suite name")
	}
}

func TestReadDrill_StripsYAMLExtension(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	flowsDir := filepath.Join(tmpDir, "astonish", "flows")
	os.MkdirAll(flowsDir, 0o755)

	suiteYAML := "description: App\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	os.WriteFile(filepath.Join(flowsDir, "xapp.yaml"), []byte(suiteYAML), 0o644)

	result, err := readDrill(nil, ReadDrillArgs{Name: "xapp.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" {
		t.Fatalf("Status = %q — should find suite after stripping .yaml. Message = %q", result.Status, result.Message)
	}
}
