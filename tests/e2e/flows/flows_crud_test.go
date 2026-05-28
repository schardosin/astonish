//go:build e2e

// Package flows contains E2E tests for Flow (Agent) CRUD via the platform APIs.
package flows

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/schardosin/astonish/tests/e2eboot"
)

// Minimal valid flow YAML for CRUD testing.
// This is a known-good structure used in other E2E flow tests.
const minimalFlowYAML = `name: e2e_crud_flow
description: E2E test flow for CRUD verification
nodes:
  - name: get_input
    type: input
    prompt: "Enter a message:"
    output_model:
      message: string
  - name: echo
    type: llm
    prompt: "Echo this back: {{get_input.message}}"
  - name: show
    type: output
    user_message:
      - "Result:"
      - "{{echo}}"
flow:
  - from: START
    to: get_input
  - from: get_input
    to: echo
  - from: echo
    to: show
  - from: show
    to: END
`

// ---------------------------------------------------------------------------
// FLOWS-007: Flow CRUD — create, list, read, delete via /api/agents
// ---------------------------------------------------------------------------

// TestE2E_Flows_CRUD verifies the full flow (agent) lifecycle through the
// REST API in platform mode: create via PUT, verify in list, read it back,
// delete it, and confirm it's gone from the list.
//
// COVERS: FLOWS-007
func TestE2E_Flows_CRUD(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	const flowName = "e2e_crud_flow"

	// --- Create (PUT) ---
	createBody := map[string]any{
		"yaml": minimalFlowYAML,
	}
	resp := h.Put(t, "/api/agents/"+flowName, createBody)
	body := e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/agents/%s returned %d: %s", flowName, resp.StatusCode, body)
	}

	var createResult map[string]any
	if err := json.Unmarshal([]byte(body), &createResult); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createResult["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v", createResult["status"])
	}

	// --- List (GET /api/agents) ---
	resp = h.Get(t, "/api/agents")
	body = e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/agents returned %d: %s", resp.StatusCode, body)
	}

	var listResult struct {
		Agents []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Scope       string `json:"scope"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(body), &listResult); err != nil {
		t.Fatalf("decode agents list: %v", err)
	}

	found := false
	for _, a := range listResult.Agents {
		if a.Name == flowName {
			found = true
			if a.Description != "E2E test flow for CRUD verification" {
				t.Errorf("list description mismatch: got %q", a.Description)
			}
			break
		}
	}
	if !found {
		t.Fatalf("flow %q not found in agents list after create", flowName)
	}

	// --- Read (GET /api/agents/{name}) ---
	resp = h.Get(t, "/api/agents/"+flowName)
	body = e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/agents/%s returned %d: %s", flowName, resp.StatusCode, body)
	}

	var flow map[string]any
	if err := json.Unmarshal([]byte(body), &flow); err != nil {
		t.Fatalf("decode flow: %v", err)
	}
	// The read endpoint returns the YAML under "yaml".
	if yamlStr, ok := flow["yaml"].(string); ok {
		if !strings.Contains(yamlStr, "name: e2e_crud_flow") {
			t.Errorf("read flow yaml missing expected name")
		}
	} else if name, ok := flow["name"].(string); ok {
		if name != flowName {
			t.Errorf("read flow name mismatch: got %q", name)
		}
	}

	// --- Delete (DELETE /api/agents/{name}) ---
	resp = h.Delete(t, "/api/agents/"+flowName)
	body = e2eboot.ReadBody(t, resp)
	// Accept 200 or 204 for successful delete.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /api/agents/%s returned %d: %s", flowName, resp.StatusCode, body)
	}

	// --- Verify gone from list ---
	resp = h.Get(t, "/api/agents")
	body = e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/agents (post-delete) returned %d: %s", resp.StatusCode, body)
	}

	var listAfter struct {
		Agents []struct {
			Name string `json:"name"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(body), &listAfter); err != nil {
		t.Fatalf("decode agents list after delete: %v", err)
	}

	for _, a := range listAfter.Agents {
		if a.Name == flowName {
			t.Fatalf("flow %q still present in list after delete", flowName)
		}
	}
}
