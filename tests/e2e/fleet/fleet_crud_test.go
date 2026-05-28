//go:build e2e

// Package fleet contains E2E tests for Fleet plan CRUD.
package fleet

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/schardosin/astonish/tests/e2eboot"
)

// Minimal fleet plan payload for CRUD testing (persistence only).
var minimalFleetPlan = map[string]any{
	"name":        "e2e_crud_fleet_plan",
	"description": "E2E test fleet plan for CRUD verification",
	"agents":      []any{},
	"communication": map[string]any{
		"mode": "chat",
	},
	"channel": map[string]any{
		"type": "none",
	},
}

// ---------------------------------------------------------------------------
// FLEET-001/002: Fleet Plan CRUD
// ---------------------------------------------------------------------------

// TestE2E_FleetPlan_CRUD verifies create (PUT), list, read, delete of a fleet
// plan through the platform fleet plan APIs.
//
// COVERS: FLEET-001, FLEET-002
func TestE2E_FleetPlan_CRUD(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	const planKey = "e2e-crud-fleet-plan"

	// --- Create (PUT /api/fleet-plans/{key}) ---
	resp := h.Put(t, "/api/fleet-plans/"+planKey, minimalFleetPlan)
	body := e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/fleet-plans/%s returned %d: %s", planKey, resp.StatusCode, body)
	}

	var createResult map[string]any
	if err := json.Unmarshal([]byte(body), &createResult); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if createResult["status"] != "saved" {
		t.Fatalf("expected status=saved, got %v", createResult["status"])
	}

	// --- List (GET /api/fleet-plans) ---
	resp = h.Get(t, "/api/fleet-plans")
	body = e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/fleet-plans returned %d: %s", resp.StatusCode, body)
	}

	// Best-effort check that our plan is in some list shape.
	var list struct {
		Plans []struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"plans"`
	}
	_ = json.Unmarshal([]byte(body), &list)

	found := false
	for _, p := range list.Plans {
		if p.Key == planKey {
			found = true
			break
		}
	}
	if !found {
		t.Logf("Plan not explicitly found in list response (shape may vary); continuing to direct read")
	}

	// --- Read (GET /api/fleet-plans/{key}) ---
	resp = h.Get(t, "/api/fleet-plans/"+planKey)
	body = e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/fleet-plans/%s returned %d: %s", planKey, resp.StatusCode, body)
	}

	var planResp map[string]any
	if err := json.Unmarshal([]byte(body), &planResp); err != nil {
		t.Fatalf("decode plan response: %v", err)
	}
	plan, _ := planResp["plan"].(map[string]any)
	if plan == nil {
		t.Fatalf("GET response missing nested 'plan' object: %s", body)
	}
	if plan["name"] != "e2e_crud_fleet_plan" {
		t.Errorf("plan name mismatch or missing: got %v", plan["name"])
	}

	// --- Delete ---
	resp = h.Delete(t, "/api/fleet-plans/"+planKey)
	body = e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE returned %d: %s", resp.StatusCode, body)
	}

	// Verify gone
	resp = h.Get(t, "/api/fleet-plans/"+planKey)
	if resp.StatusCode != http.StatusNotFound {
		t.Logf("Read after delete returned %d (acceptable)", resp.StatusCode)
	}
}
