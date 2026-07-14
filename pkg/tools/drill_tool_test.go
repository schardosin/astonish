package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/schardosin/astonish/pkg/store"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/toolconfirmation"
	"google.golang.org/genai"
)

// ---------------------------------------------------------------------------
// Test helpers: in-memory FlowStore and minimal tool.Context mock
// ---------------------------------------------------------------------------

// memFlowStore is a minimal in-memory implementation of store.FlowStore for tests.
type memFlowStore struct {
	mu    sync.Mutex
	flows map[string]string // name -> yaml content
	types map[string]string // name -> flow type (parsed from yaml)
}

func newMemFlowStore() *memFlowStore {
	return &memFlowStore{
		flows: make(map[string]string),
		types: make(map[string]string),
	}
}

func (m *memFlowStore) ListAllFlows(_ context.Context) []store.FlowSummary {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.FlowSummary
	for name := range m.flows {
		out = append(out, m.flowSummary(name))
	}
	return out
}

func (m *memFlowStore) ListFlowsByType(_ context.Context, types []string) []store.FlowSummary {
	m.mu.Lock()
	defer m.mu.Unlock()
	typeSet := make(map[string]bool, len(types))
	for _, t := range types {
		typeSet[t] = true
	}
	var out []store.FlowSummary
	for name := range m.flows {
		if typeSet[m.types[name]] {
			out = append(out, m.flowSummary(name))
		}
	}
	return out
}

func (m *memFlowStore) GetFlow(_ context.Context, name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if yaml, ok := m.flows[name]; ok {
		return yaml, nil
	}
	return "", fmt.Errorf("flow %q not found", name)
}

func (m *memFlowStore) SaveFlow(_ context.Context, name string, yamlContent string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flows[name] = yamlContent
	// Extract type from YAML (crude but sufficient for tests).
	for _, line := range strings.Split(yamlContent, "\n") {
		if strings.HasPrefix(line, "type:") {
			m.types[name] = strings.TrimSpace(strings.TrimPrefix(line, "type:"))
			break
		}
	}
	// Also extract suite field for drill flows.
	return nil
}

func (m *memFlowStore) DeleteFlow(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.flows[name]; !ok {
		return fmt.Errorf("flow %q not found", name)
	}
	delete(m.flows, name)
	delete(m.types, name)
	return nil
}

func (m *memFlowStore) GetTaps(_ context.Context) []store.FlowTap { return nil }
func (m *memFlowStore) AddTap(_ context.Context, _ string, _ string) (string, error) {
	return "", nil
}
func (m *memFlowStore) RemoveTap(_ context.Context, _ string) error { return nil }
func (m *memFlowStore) GetStoreDir(_ context.Context) string       { return "" }

// flowSummary builds a FlowSummary from stored data (must hold lock).
func (m *memFlowStore) flowSummary(name string) store.FlowSummary {
	yaml := m.flows[name]
	summary := store.FlowSummary{
		Name: name,
		Type: m.types[name],
	}
	// Extract description and suite from YAML.
	for _, line := range strings.Split(yaml, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "description:") {
			summary.Description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
		if strings.HasPrefix(line, "suite:") {
			summary.Suite = strings.TrimSpace(strings.TrimPrefix(line, "suite:"))
		}
	}
	return summary
}

// mockToolCtx is a minimal implementation of tool.Context for unit tests.
// It delegates context.Context methods to a wrapped context and stubs all
// ADK-specific methods.
type mockToolCtx struct {
	context.Context
}

var _ tool.Context = (*mockToolCtx)(nil)

func (m *mockToolCtx) UserContent() *genai.Content                                    { return nil }
func (m *mockToolCtx) InvocationID() string                                           { return "test" }
func (m *mockToolCtx) AgentName() string                                              { return "test" }
func (m *mockToolCtx) ReadonlyState() session.ReadonlyState                           { return nil }
func (m *mockToolCtx) UserID() string                                                 { return "" }
func (m *mockToolCtx) AppName() string                                                { return "" }
func (m *mockToolCtx) SessionID() string                                              { return "" }
func (m *mockToolCtx) Branch() string                                                 { return "" }
func (m *mockToolCtx) Artifacts() agent.Artifacts                                     { return nil }
func (m *mockToolCtx) State() session.State                                           { return nil }
func (m *mockToolCtx) FunctionCallID() string                                         { return "" }
func (m *mockToolCtx) Actions() *session.EventActions                                 { return nil }
func (m *mockToolCtx) SearchMemory(_ context.Context, _ string) (*memory.SearchResponse, error) {
	return nil, nil
}
func (m *mockToolCtx) ToolConfirmation() *toolconfirmation.ToolConfirmation { return nil }
func (m *mockToolCtx) RequestConfirmation(_ string, _ any) error            { return nil }

// testCtxWithStore creates a tool.Context backed by a memFlowStore in the
// team flow store slot.
func testCtxWithStore(fs store.FlowStore) tool.Context {
	ctx := store.WithTeamFlowStore(context.Background(), fs)
	return &mockToolCtx{Context: ctx}
}

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
	fs := newMemFlowStore()
	// Pre-populate suite so append mode works.
	suiteYAML := "description: test suite\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	fs.SaveFlow(context.Background(), "myapp", suiteYAML)

	ctx := testCtxWithStore(fs)

	drillYAML := "type: drill\nsuite: myapp\nnodes:\n  - name: step1\n    type: tool\n    args:\n      tool: shell_command\n      command: echo hi\n    assert:\n      type: contains\n      expected: hi"
	result, err := saveDrill(ctx, SaveDrillArgs{
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

	// Verify drill was stored
	content, err := fs.GetFlow(context.Background(), "health-check")
	if err != nil {
		t.Fatalf("failed to read drill from store: %v", err)
	}
	if !strings.Contains(content, "type: drill") {
		t.Error("drill content does not contain expected content")
	}
}

func TestSaveDrill_FullMode_SavesSuiteAndTests(t *testing.T) {
	fs := newMemFlowStore()
	ctx := testCtxWithStore(fs)

	suiteYAML := "description: test suite\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	drillYAML := "type: drill\nsuite: newapp\nnodes:\n  - name: step1\n    type: tool\n    args:\n      tool: shell_command\n      command: echo hi\n    assert:\n      type: contains\n      expected: hi"

	result, err := saveDrill(ctx, SaveDrillArgs{
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

	// Verify suite was stored
	content, err := fs.GetFlow(context.Background(), "newapp")
	if err != nil {
		t.Fatalf("failed to read suite from store: %v", err)
	}
	if !strings.Contains(content, "drill_suite") {
		t.Error("suite content does not contain expected content")
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
	fs := newMemFlowStore()
	ctx := testCtxWithStore(fs)
	result, err := deleteDrill(ctx, DeleteDrillArgs{})
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
	fs := newMemFlowStore()
	ctx := testCtxWithStore(fs)
	result, err := deleteDrill(ctx, DeleteDrillArgs{TestName: "nonexistent-drill-99"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error for nonexistent drill", result.Status)
	}
}

func TestDeleteDrill_NonexistentSuite(t *testing.T) {
	fs := newMemFlowStore()
	ctx := testCtxWithStore(fs)
	result, err := deleteDrill(ctx, DeleteDrillArgs{SuiteName: "nonexistent-suite-99"})
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
	fs := newMemFlowStore()
	ctx := testCtxWithStore(fs)

	result, err := listDrills(ctx, ListDrillsArgs{})
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
	fs := newMemFlowStore()
	ctx := testCtxWithStore(fs)

	result, err := listDrills(ctx, ListDrillsArgs{SuiteName: "nonexistent"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want error", result.Status)
	}
}

func TestListDrills_WithSuiteAndDrills(t *testing.T) {
	fs := newMemFlowStore()
	ctx := testCtxWithStore(fs)

	// Store a suite and a drill
	suiteYAML := "description: Test App\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	fs.SaveFlow(context.Background(), "testapp", suiteYAML)

	drillYAML := "type: drill\nsuite: testapp\ndescription: health check\nnodes:\n  - name: step1\n    type: tool\n    args:\n      tool: shell_command\n      command: echo"
	fs.SaveFlow(context.Background(), "health-check", drillYAML)

	result, err := listDrills(ctx, ListDrillsArgs{})
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
	fs := newMemFlowStore()
	ctx := testCtxWithStore(fs)

	suiteYAML := "description: My App\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	fs.SaveFlow(context.Background(), "myapp", suiteYAML)

	drillYAML := "type: drill\nsuite: myapp\ndescription: api check\nnodes:\n  - name: s\n    type: tool\n    args:\n      tool: shell_command\n      command: echo"
	fs.SaveFlow(context.Background(), "api-check", drillYAML)

	result, err := listDrills(ctx, ListDrillsArgs{SuiteName: "myapp"})
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
	fs := newMemFlowStore()
	ctx := testCtxWithStore(fs)

	result, err := readDrill(ctx, ReadDrillArgs{Name: "nonexistent"})
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
	fs := newMemFlowStore()
	ctx := testCtxWithStore(fs)

	// Suite
	suiteYAML := "description: App\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	fs.SaveFlow(context.Background(), "testapp", suiteYAML)

	// Drill
	drillYAML := "type: drill\nsuite: testapp\ndescription: the health check\nnodes:\n  - name: step1\n    type: tool\n    args:\n      tool: shell_command\n      command: echo hi\n    assert:\n      type: contains\n      expected: hi"
	fs.SaveFlow(context.Background(), "health-check", drillYAML)

	result, err := readDrill(ctx, ReadDrillArgs{Name: "health-check"})
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
	fs := newMemFlowStore()
	ctx := testCtxWithStore(fs)

	suiteYAML := "description: My App\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	fs.SaveFlow(context.Background(), "myapp", suiteYAML)

	drillYAML := "type: drill\nsuite: myapp\ndescription: api test\nnodes:\n  - name: s\n    type: tool\n    args:\n      tool: shell_command\n      command: echo"
	fs.SaveFlow(context.Background(), "api-test", drillYAML)

	result, err := readDrill(ctx, ReadDrillArgs{Name: "myapp"})
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
	fs := newMemFlowStore()
	ctx := testCtxWithStore(fs)

	suiteYAML := "description: App\ntype: drill_suite\nsuite_config:\n  setup: []\n"
	fs.SaveFlow(context.Background(), "xapp", suiteYAML)

	result, err := readDrill(ctx, ReadDrillArgs{Name: "xapp.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" {
		t.Fatalf("Status = %q — should find suite after stripping .yaml. Message = %q", result.Status, result.Message)
	}
}
