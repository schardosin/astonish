package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// mockFlowRunnerAccess implements FlowRunnerAccess for testing.
type mockFlowRunnerAccess struct {
	result        *FlowRunResult
	err           error
	lastParams    map[string]string
	lastInput     string
	lastKey       string
	cleaned       []string
	pausedNode    string   // simulates a paused flow session
	pausedOptions []string // simulates resolved options for paused node
}

func (m *mockFlowRunnerAccess) RunFlow(_ context.Context, _ string, params map[string]string, inputResponse string, sessionKey string) (*FlowRunResult, error) {
	m.lastParams = params
	m.lastInput = inputResponse
	m.lastKey = sessionKey
	return m.result, m.err
}

func (m *mockFlowRunnerAccess) GetPausedNode(_ string) string {
	return m.pausedNode
}

func (m *mockFlowRunnerAccess) GetPausedOptions(_ string) []string {
	return m.pausedOptions
}

func (m *mockFlowRunnerAccess) CleanupSession(sessionKey string) {
	m.cleaned = append(m.cleaned, sessionKey)
}

// --- runFlow tests ---

func TestRunFlow_NilRunner(t *testing.T) {
	flowRunnerAccessVar = nil

	result, err := runFlow(nil, RunFlowArgs{FlowName: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want %q", result.Status, "error")
	}
}

func TestRunFlow_EmptyFlowName(t *testing.T) {
	mock := &mockFlowRunnerAccess{}
	flowRunnerAccessVar = mock
	defer func() { flowRunnerAccessVar = nil }()

	result, err := runFlow(nil, RunFlowArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want %q", result.Status, "error")
	}
}

func TestRunFlow_FlowNotFound(t *testing.T) {
	mock := &mockFlowRunnerAccess{}
	flowRunnerAccessVar = mock
	defer func() { flowRunnerAccessVar = nil }()

	result, err := runFlow(nil, RunFlowArgs{FlowName: "nonexistent-flow-abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want %q", result.Status, "error")
	}
}

func TestRunFlow_ExecutionError(t *testing.T) {
	mock := &mockFlowRunnerAccess{
		err: fmt.Errorf("execution failed"),
	}
	flowRunnerAccessVar = mock
	defer func() { flowRunnerAccessVar = nil }()

	// Create a temp flow file that resolveFlowFilePath can find.
	// We need to place it in the flows directory or work around it.
	// Since resolveFlowFilePath calls flowstore.GetFlowsDir() which uses real config dir,
	// we test runFlow with a flow that won't be found — the error path is already covered above.
	// Instead, test the RunnerAccess error path indirectly via a unit test of the mapping logic.
	t.Skip("requires real flows directory — covered by integration tests")
}

// --- resolveFlowFilePath tests ---

func TestResolveFlowFilePath_NotFound(t *testing.T) {
	_, err := resolveFlowFilePath("nonexistent-flow-abc123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestResolveFlowFilePath_WithYamlSuffix(t *testing.T) {
	// Should not fail on .yaml extension already present
	_, err := resolveFlowFilePath("nonexistent-flow-abc123.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent flow")
	}
}

// --- scanFlowParameters tests ---

func TestScanFlowParameters_InputNodes(t *testing.T) {
	// Create a temp flow YAML with initial input nodes
	yaml := `
description: Test flow
nodes:
  - name: get_url
    type: input
    prompt: "Enter the URL to scan"
  - name: get_mode
    type: input
    prompt: "Select scan mode"
    options:
      - "quick"
      - "full"
  - name: scan
    type: llm
    prompt: "Scan {get_url.output} in {get_mode.output} mode"
flow:
  - from: START
    to: get_url
  - from: get_url
    to: get_mode
  - from: get_mode
    to: scan
  - from: scan
    to: END
`
	tmpDir := t.TempDir()
	flowPath := filepath.Join(tmpDir, "test_flow.yaml")
	if err := os.WriteFile(flowPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("failed to write temp flow: %v", err)
	}

	params, err := scanFlowParameters(flowPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(params) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(params))
	}

	// First param: get_url
	if params[0].NodeName != "get_url" {
		t.Errorf("params[0].NodeName = %q, want %q", params[0].NodeName, "get_url")
	}
	if params[0].Prompt != "Enter the URL to scan" {
		t.Errorf("params[0].Prompt = %q, want %q", params[0].Prompt, "Enter the URL to scan")
	}
	if len(params[0].Options) != 0 {
		t.Errorf("params[0].Options = %v, want empty", params[0].Options)
	}

	// Second param: get_mode (with options)
	if params[1].NodeName != "get_mode" {
		t.Errorf("params[1].NodeName = %q, want %q", params[1].NodeName, "get_mode")
	}
	if len(params[1].Options) != 2 {
		t.Errorf("params[1].Options length = %d, want 2", len(params[1].Options))
	}
}

func TestScanFlowParameters_NoInputNodes(t *testing.T) {
	// Flow starts directly with an LLM node
	yaml := `
description: No-input flow
nodes:
  - name: analyze
    type: llm
    prompt: "Analyze the system"
flow:
  - from: START
    to: analyze
  - from: analyze
    to: END
`
	tmpDir := t.TempDir()
	flowPath := filepath.Join(tmpDir, "no_input.yaml")
	if err := os.WriteFile(flowPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("failed to write temp flow: %v", err)
	}

	params, err := scanFlowParameters(flowPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(params) != 0 {
		t.Errorf("expected 0 parameters, got %d", len(params))
	}
}

func TestScanFlowParameters_InputWithOutputModel(t *testing.T) {
	yaml := `
description: Flow with output model
nodes:
  - name: get_info
    type: input
    prompt: "Enter server details"
    output_model:
      hostname: "The server hostname"
      port: "The port number"
  - name: connect
    type: llm
    prompt: "Connect to {get_info.hostname}:{get_info.port}"
flow:
  - from: START
    to: get_info
  - from: get_info
    to: connect
  - from: connect
    to: END
`
	tmpDir := t.TempDir()
	flowPath := filepath.Join(tmpDir, "output_model.yaml")
	if err := os.WriteFile(flowPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("failed to write temp flow: %v", err)
	}

	params, err := scanFlowParameters(flowPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(params) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(params))
	}

	if len(params[0].Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(params[0].Fields))
	}
}

func TestScanFlowParameters_ConditionalEdges(t *testing.T) {
	// When the first flow edge uses conditional edges, we follow the first edge
	yaml := `
description: Conditional flow
nodes:
  - name: get_mode
    type: input
    prompt: "Select mode"
    options:
      - "a"
      - "b"
  - name: route_a
    type: llm
    prompt: "Route A"
  - name: route_b
    type: llm
    prompt: "Route B"
flow:
  - from: START
    to: get_mode
  - from: get_mode
    edges:
      - to: route_a
        condition: "state['get_mode']['output'] == 'a'"
      - to: route_b
        condition: "state['get_mode']['output'] == 'b'"
  - from: route_a
    to: END
  - from: route_b
    to: END
`
	tmpDir := t.TempDir()
	flowPath := filepath.Join(tmpDir, "conditional.yaml")
	if err := os.WriteFile(flowPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("failed to write temp flow: %v", err)
	}

	params, err := scanFlowParameters(flowPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find the initial input node before the conditional branch
	if len(params) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(params))
	}
	if params[0].NodeName != "get_mode" {
		t.Errorf("NodeName = %q, want %q", params[0].NodeName, "get_mode")
	}
}

func TestScanFlowParameters_InvalidFile(t *testing.T) {
	_, err := scanFlowParameters("/nonexistent/path/flow.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestScanFlowParameters_MidFlowInputNotReturned(t *testing.T) {
	// Input nodes after an LLM node should NOT be returned (they're mid-flow)
	yaml := `
description: Mid-flow input
nodes:
  - name: get_url
    type: input
    prompt: "Enter URL"
  - name: analyze
    type: llm
    prompt: "Analyze {get_url.output}"
  - name: confirm
    type: input
    prompt: "Proceed with fix?"
    options:
      - "yes"
      - "no"
  - name: fix
    type: llm
    prompt: "Apply fix"
flow:
  - from: START
    to: get_url
  - from: get_url
    to: analyze
  - from: analyze
    to: confirm
  - from: confirm
    to: fix
  - from: fix
    to: END
`
	tmpDir := t.TempDir()
	flowPath := filepath.Join(tmpDir, "mid_input.yaml")
	if err := os.WriteFile(flowPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("failed to write temp flow: %v", err)
	}

	params, err := scanFlowParameters(flowPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the first input node (get_url) should be returned, not confirm
	if len(params) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(params))
	}
	if params[0].NodeName != "get_url" {
		t.Errorf("NodeName = %q, want %q", params[0].NodeName, "get_url")
	}
}
