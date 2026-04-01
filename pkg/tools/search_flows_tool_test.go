package tools

import (
	"testing"

	"github.com/schardosin/astonish/pkg/agent"
)

func TestSearchFlows_NilRegistry(t *testing.T) {
	flowRegistryVar = nil

	result, err := searchFlows(nil, SearchFlowsArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "error" {
		t.Errorf("Status = %q, want %q", result.Status, "error")
	}
}

func TestSearchFlows_EmptyRegistry(t *testing.T) {
	reg := newTestRegistry(t)
	flowRegistryVar = reg
	defer func() { flowRegistryVar = nil }()

	result, err := searchFlows(nil, SearchFlowsArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want %q", result.Status, "ok")
	}
	if result.Count != 0 {
		t.Errorf("Count = %d, want 0", result.Count)
	}
}

func TestSearchFlows_ListAll(t *testing.T) {
	reg := newTestRegistry(t,
		agent.FlowRegistryEntry{FlowFile: "backup.yaml", Description: "Run daily backup", Tags: []string{"infra"}},
		agent.FlowRegistryEntry{FlowFile: "report.yaml", Description: "Generate weekly report", Tags: []string{"analytics"}},
	)
	flowRegistryVar = reg
	defer func() { flowRegistryVar = nil }()

	result, err := searchFlows(nil, SearchFlowsArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want %q", result.Status, "ok")
	}
	if result.Count != 2 {
		t.Errorf("Count = %d, want 2", result.Count)
	}
	// Verify .yaml is stripped from names
	for _, f := range result.Flows {
		if f.Name == "backup.yaml" || f.Name == "report.yaml" {
			t.Errorf("flow name should not include .yaml extension, got %q", f.Name)
		}
	}
}

func TestSearchFlows_QueryByName(t *testing.T) {
	reg := newTestRegistry(t,
		agent.FlowRegistryEntry{FlowFile: "backup.yaml", Description: "Run daily backup"},
		agent.FlowRegistryEntry{FlowFile: "report.yaml", Description: "Generate weekly report"},
		agent.FlowRegistryEntry{FlowFile: "deploy.yaml", Description: "Deploy to production"},
	)
	flowRegistryVar = reg
	defer func() { flowRegistryVar = nil }()

	result, err := searchFlows(nil, SearchFlowsArgs{Query: "backup"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Count != 1 {
		t.Fatalf("Count = %d, want 1", result.Count)
	}
	if result.Flows[0].Name != "backup" {
		t.Errorf("Name = %q, want %q", result.Flows[0].Name, "backup")
	}
}

func TestSearchFlows_QueryByDescription(t *testing.T) {
	reg := newTestRegistry(t,
		agent.FlowRegistryEntry{FlowFile: "backup.yaml", Description: "Run daily backup"},
		agent.FlowRegistryEntry{FlowFile: "report.yaml", Description: "Generate weekly report"},
	)
	flowRegistryVar = reg
	defer func() { flowRegistryVar = nil }()

	result, err := searchFlows(nil, SearchFlowsArgs{Query: "weekly"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Count != 1 {
		t.Fatalf("Count = %d, want 1", result.Count)
	}
	if result.Flows[0].Name != "report" {
		t.Errorf("Name = %q, want %q", result.Flows[0].Name, "report")
	}
}

func TestSearchFlows_QueryByTag(t *testing.T) {
	reg := newTestRegistry(t,
		agent.FlowRegistryEntry{FlowFile: "backup.yaml", Description: "Run backup", Tags: []string{"infra", "cron"}},
		agent.FlowRegistryEntry{FlowFile: "report.yaml", Description: "Generate report", Tags: []string{"analytics"}},
	)
	flowRegistryVar = reg
	defer func() { flowRegistryVar = nil }()

	result, err := searchFlows(nil, SearchFlowsArgs{Query: "analytics"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Count != 1 {
		t.Fatalf("Count = %d, want 1", result.Count)
	}
	if result.Flows[0].Name != "report" {
		t.Errorf("Name = %q, want %q", result.Flows[0].Name, "report")
	}
}

func TestSearchFlows_MultiWordQuery(t *testing.T) {
	reg := newTestRegistry(t,
		agent.FlowRegistryEntry{FlowFile: "daily-backup.yaml", Description: "Run daily backup of Proxmox VMs"},
		agent.FlowRegistryEntry{FlowFile: "weekly-report.yaml", Description: "Generate weekly analytics report"},
		agent.FlowRegistryEntry{FlowFile: "daily-report.yaml", Description: "Generate daily summary report"},
	)
	flowRegistryVar = reg
	defer func() { flowRegistryVar = nil }()

	// "daily report" should match flows containing BOTH words
	result, err := searchFlows(nil, SearchFlowsArgs{Query: "daily report"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Count != 1 {
		t.Fatalf("Count = %d, want 1", result.Count)
	}
	if result.Flows[0].Name != "daily-report" {
		t.Errorf("Name = %q, want %q", result.Flows[0].Name, "daily-report")
	}
}

func TestSearchFlows_CaseInsensitive(t *testing.T) {
	reg := newTestRegistry(t,
		agent.FlowRegistryEntry{FlowFile: "Backup-VMs.yaml", Description: "Backup Proxmox VMs"},
	)
	flowRegistryVar = reg
	defer func() { flowRegistryVar = nil }()

	result, err := searchFlows(nil, SearchFlowsArgs{Query: "PROXMOX"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Count != 1 {
		t.Fatalf("Count = %d, want 1", result.Count)
	}
}

func TestSearchFlows_NoMatch(t *testing.T) {
	reg := newTestRegistry(t,
		agent.FlowRegistryEntry{FlowFile: "backup.yaml", Description: "Run daily backup"},
	)
	flowRegistryVar = reg
	defer func() { flowRegistryVar = nil }()

	result, err := searchFlows(nil, SearchFlowsArgs{Query: "nonexistent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want %q", result.Status, "ok")
	}
	if result.Count != 0 {
		t.Errorf("Count = %d, want 0", result.Count)
	}
}

func TestSearchFlows_FiltersDrills(t *testing.T) {
	reg := newTestRegistry(t,
		agent.FlowRegistryEntry{FlowFile: "backup.yaml", Description: "Run daily backup"},
		agent.FlowRegistryEntry{FlowFile: "login-test.yaml", Description: "Login drill", Type: "drill"},
		agent.FlowRegistryEntry{FlowFile: "e2e-suite.yaml", Description: "End-to-end suite", Type: "drill_suite"},
		agent.FlowRegistryEntry{FlowFile: "report.yaml", Description: "Generate weekly report"},
	)
	flowRegistryVar = reg
	defer func() { flowRegistryVar = nil }()

	// List all — should only return the 2 regular flows
	result, err := searchFlows(nil, SearchFlowsArgs{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Count != 2 {
		t.Errorf("Count = %d, want 2 (drills should be filtered out)", result.Count)
	}
	for _, f := range result.Flows {
		if f.Name == "login-test" || f.Name == "e2e-suite" {
			t.Errorf("drill %q should not appear in search results", f.Name)
		}
	}

	// Query that could match a drill — should still not return it
	result, err = searchFlows(nil, SearchFlowsArgs{Query: "login"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Count != 0 {
		t.Errorf("Count = %d, want 0 (drill should not match)", result.Count)
	}
}

// newTestRegistry creates a FlowRegistry with pre-populated entries for testing.
func newTestRegistry(t *testing.T, entries ...agent.FlowRegistryEntry) *agent.FlowRegistry {
	t.Helper()
	tmpDir := t.TempDir()
	reg, err := agent.NewFlowRegistry(tmpDir + "/test_registry.json")
	if err != nil {
		t.Fatalf("failed to create test registry: %v", err)
	}
	for _, e := range entries {
		if err := reg.Register(e); err != nil {
			t.Fatalf("failed to register test entry: %v", err)
		}
	}
	return reg
}
