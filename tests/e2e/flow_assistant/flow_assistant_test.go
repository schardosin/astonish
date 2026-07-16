//go:build e2e

// Package flow_assistant tests the Flow AI assistant end-to-end.
// It exercises the full path: HTTP → auth middleware → AIChatHandler → real LLM → SSE response.
//
// These tests run sequentially (Bootstrap uses t.Setenv for XDG_CONFIG_HOME,
// which is incompatible with t.Parallel in Go's testing framework).
//
// Run:
//
//	go test -tags=e2e -count=1 -v -timeout=5m ./tests/e2e/flow_assistant/
package flow_assistant

import (
	"strings"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/api"
	"github.com/SAP/astonish/tests/e2eboot"
)

// COVERS: FLOWS-001
func TestE2E_FlowAssistant_CreateFlow(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	reqBody := api.AIChatRequest{
		Message:       "Create a simple flow that lists files in a directory the user provides and prints them. Keep it minimal - just an input node, an llm node to process, and an output node.",
		Context:       "create_flow",
		CurrentYAML:   "",
		SelectedNodes: nil,
		History:       nil,
	}

	events := h.SSE(t, "/api/ai/chat", reqBody, 90*time.Second)

	// Find the complete event
	completeEvent := e2eboot.FindEvent(events, "complete")
	if completeEvent == nil {
		t.Fatal("no 'complete' SSE event found — LLM may have failed or timed out")
	}

	var resp api.AIChatResponse
	e2eboot.DecodeEventData(t, completeEvent, &resp)

	// Validate: response should contain proposed YAML
	if resp.ProposedYAML == "" {
		t.Fatalf("AI did not produce a flow YAML.\nMessage: %s", resp.Message)
	}

	// Run the production flow validator.
	// Validation errors are logged but non-fatal — LLM output is non-deterministic
	// and may reference tools or patterns the validator doesn't recognize.
	// The critical assertion is that a YAML was produced and SSE transport worked.
	validation := api.ValidateFlowYAML(resp.ProposedYAML, nil)
	if !validation.Valid {
		t.Logf("Validation errors (non-fatal): %s", strings.Join(validation.Errors, "; "))
	}

	// Structural checks
	if !strings.Contains(resp.ProposedYAML, "name:") {
		t.Error("generated YAML should have node names")
	}
	if !strings.Contains(resp.ProposedYAML, "nodes:") {
		t.Error("generated YAML should have a nodes section")
	}

	t.Logf("Created flow YAML (%d bytes), validation: valid=%v", len(resp.ProposedYAML), validation.Valid)
}

// COVERS: FLOWS-002
func TestE2E_FlowAssistant_ModifyFlow(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	// Known-good simple flow as starting point
	baseFlow := `name: list_files
description: Lists files in a user-provided directory
nodes:
  - name: get_directory
    type: input
    prompt: "Enter the directory path to list files from:"
    output_model:
      directory: string
  - name: process_listing
    type: llm
    model: gemini-2.0-flash
    prompt: "List the files in the directory: {{get_directory.directory}}. Format them nicely."
  - name: show_results
    type: output
    user_message:
      - "Here are the files:"
      - "{{process_listing}}"
flow:
  - from: START
    to: get_directory
  - from: get_directory
    to: process_listing
  - from: process_listing
    to: show_results
  - from: show_results
    to: END
`

	reqBody := api.AIChatRequest{
		Message:       "Add a step at the end that asks the user if they want to check another folder. If yes, loop back to the get_directory step. If no, end the flow.",
		Context:       "modify_flow",
		CurrentYAML:   baseFlow,
		SelectedNodes: nil,
		History:       nil,
	}

	events := h.SSE(t, "/api/ai/chat", reqBody, 90*time.Second)

	completeEvent := e2eboot.FindEvent(events, "complete")
	if completeEvent == nil {
		t.Fatal("no 'complete' SSE event found — LLM may have failed or timed out")
	}

	var resp api.AIChatResponse
	e2eboot.DecodeEventData(t, completeEvent, &resp)

	if resp.ProposedYAML == "" {
		t.Fatalf("AI did not produce a modified flow YAML.\nMessage: %s", resp.Message)
	}

	// Run the production flow validator.
	// Non-fatal — LLM output is non-deterministic.
	validation := api.ValidateFlowYAML(resp.ProposedYAML, nil)
	if !validation.Valid {
		t.Logf("Validation errors (non-fatal): %s", strings.Join(validation.Errors, "; "))
	}

	// Structural checks: modified flow should be larger than the original
	if len(resp.ProposedYAML) <= len(baseFlow) {
		t.Errorf("modified flow (%d bytes) should be larger than base flow (%d bytes)",
			len(resp.ProposedYAML), len(baseFlow))
	}

	// The modified flow should still contain the original nodes
	if !strings.Contains(resp.ProposedYAML, "get_directory") {
		t.Error("modified flow should still contain 'get_directory' node")
	}
	if !strings.Contains(resp.ProposedYAML, "process_listing") {
		t.Error("modified flow should still contain 'process_listing' node")
	}

	// The modified flow should have more nodes than the original (3 → at least 4)
	originalNodeCount := strings.Count(baseFlow, "- name:")
	modifiedNodeCount := strings.Count(resp.ProposedYAML, "- name:")
	if modifiedNodeCount <= originalNodeCount {
		t.Errorf("modified flow should have more nodes: original=%d, modified=%d",
			originalNodeCount, modifiedNodeCount)
	}

	// Check for loop-back evidence
	flowSection := ""
	if idx := strings.Index(resp.ProposedYAML, "flow:"); idx != -1 {
		flowSection = resp.ProposedYAML[idx:]
	}
	if flowSection != "" {
		getDirectoryRefs := strings.Count(flowSection, "get_directory")
		if getDirectoryRefs < 2 {
			t.Logf("WARNING: expected loop-back to get_directory in flow edges (found %d references)", getDirectoryRefs)
		}
	}

	t.Logf("Modified flow YAML (%d bytes, %d nodes), validation: valid=%v",
		len(resp.ProposedYAML), modifiedNodeCount, validation.Valid)
}

// TestE2E_FlowAssistant_LargeFlowSSE validates that a real LLM producing a
// substantial flow results in a complete SSE event being delivered intact.
// This is the E2E counterpart of the SSE parser regression test.
// COVERS: FLOWS-003
func TestE2E_FlowAssistant_LargeFlowSSE(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	reqBody := api.AIChatRequest{
		Message: `Create a comprehensive customer onboarding flow with at least 8 steps:
1. Collect user name and email
2. Verify email format
3. Ask for company details
4. Determine account tier (free/pro/enterprise)
5. Based on tier, set different feature flags
6. Generate welcome message personalized to their tier
7. Show a summary of their account
8. Ask if they want to make changes or confirm

Use conditional routing based on the account tier. Include proper flow edges.`,
		Context:       "create_flow",
		CurrentYAML:   "",
		SelectedNodes: nil,
		History:       nil,
	}

	events := h.SSE(t, "/api/ai/chat", reqBody, 120*time.Second)

	completeEvent := e2eboot.FindEvent(events, "complete")
	if completeEvent == nil {
		t.Fatal("no 'complete' SSE event — large flow may have been dropped")
	}

	var resp api.AIChatResponse
	e2eboot.DecodeEventData(t, completeEvent, &resp)

	if resp.ProposedYAML == "" {
		t.Fatalf("no YAML in response.\nMessage: %s", resp.Message)
	}

	// Should be a substantial flow
	nodeCount := strings.Count(resp.ProposedYAML, "- name:")
	if nodeCount < 5 {
		t.Errorf("expected at least 5 nodes for a complex flow, got %d", nodeCount)
	}

	// Validate
	validation := api.ValidateFlowYAML(resp.ProposedYAML, nil)
	t.Logf("Large flow: %d bytes, %d nodes, valid=%v, errors=%d",
		len(resp.ProposedYAML), nodeCount, validation.Valid, len(validation.Errors))

	if !validation.Valid {
		// Log errors but don't fail — LLM output isn't perfectly deterministic.
		// The important assertion is that the SSE event was delivered intact.
		t.Logf("Validation errors (non-fatal for E2E): %s", strings.Join(validation.Errors, "; "))
	}
}
