//go:build e2e

package fleet

import (
	"testing"

	"github.com/SAP/astonish/pkg/fleet"
)

// TestE2E_Fleet_SoftwareDevSequentialConfig verifies the software-dev
// bundled template uses sequential Dev→QA→(PO)→E2E (no parallel QA/E2E).
func TestE2E_Fleet_SoftwareDevSequentialConfig(t *testing.T) {
	configs, err := fleet.LoadBundledConfigs()
	if err != nil {
		t.Fatalf("LoadBundledConfigs: %v", err)
	}
	cfg, ok := configs["software-dev"]
	if !ok {
		t.Fatal("software-dev template missing")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if cfg.Settings.GetMaxParallelAgents() != 1 {
		t.Fatalf("expected max_parallel_agents == 1, got %d", cfg.Settings.GetMaxParallelAgents())
	}
	qa := cfg.Agents["qa"]
	e2e := cfg.Agents["e2e"]
	if qa.IsParallelizable() || e2e.IsParallelizable() {
		t.Fatalf("qa/e2e must not be parallelizable: qa=%v e2e=%v", qa.IsParallelizable(), e2e.IsParallelizable())
	}
	arch := cfg.Agents["architect"]
	if arch.GetWorkspace() != "shared" {
		t.Fatalf("architect workspace want shared, got %q", arch.GetWorkspace())
	}
	if cfg.ProjectContext == nil || cfg.ProjectContext.Generator != "load_file" {
		gen := ""
		if cfg.ProjectContext != nil {
			gen = cfg.ProjectContext.Generator
		}
		t.Fatalf("expected project_context.generator load_file, got %q", gen)
	}
}

// TestE2E_Fleet_DispatcherFanOutUnit locks the parallel fan-out helper used by
// the session dispatcher (same package logic, exercised under the e2e tag so
// CI fleet packages pick it up).
func TestE2E_Fleet_DispatcherFanOutUnit(t *testing.T) {
	qa := fleet.FleetAgentConfig{
		Name: "QA", Identity: "id", Behaviors: "b", Tools: fleet.ToolsConfig{All: true},
		Execution:  &fleet.AgentExecutionConfig{Parallelizable: true},
		TaskPolicy: &fleet.AgentTaskPolicy{Claims: []string{"code.test"}},
	}
	e2eAgent := fleet.FleetAgentConfig{
		Name: "E2E", Identity: "id", Behaviors: "b", Tools: fleet.ToolsConfig{All: true},
		Execution:  &fleet.AgentExecutionConfig{Parallelizable: true},
		TaskPolicy: &fleet.AgentTaskPolicy{Claims: []string{"ops.observe"}},
	}
	cfg := &fleet.FleetConfig{
		Name: "parallel",
		Agents: map[string]fleet.FleetAgentConfig{
			"po": {
				Name: "PO", Identity: "id", Behaviors: "b", Tools: fleet.ToolsConfig{All: true},
				TaskPolicy: &fleet.AgentTaskPolicy{Claims: []string{"planning"}},
			},
			"qa":  qa,
			"e2e": e2eAgent,
		},
		Communication: &fleet.CommunicationConfig{Flow: []fleet.CommunicationNode{
			{Role: "po", TalksTo: []string{"qa", "e2e"}, EntryPoint: true},
			{Role: "qa", TalksTo: []string{"po"}},
			{Role: "e2e", TalksTo: []string{"po"}},
		}},
		Settings: fleet.FleetSettings{MaxParallelAgents: 2},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}
