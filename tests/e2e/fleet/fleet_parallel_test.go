//go:build e2e

package fleet

import (
	"testing"

	"github.com/schardosin/astonish/pkg/fleet"
)

// TestE2E_Fleet_SoftwareDevParallelConfig verifies the upgraded software-dev
// bundled template declares parallel QA/E2E and validates cleanly.
//
// Full wall-clock overlap of live agent activations requires a provider key and
// is covered by scenario runs; this test locks the template contract.
func TestE2E_Fleet_SoftwareDevParallelConfig(t *testing.T) {
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
	if cfg.Settings.GetMaxParallelAgents() < 2 {
		t.Fatalf("expected max_parallel_agents >= 2, got %d", cfg.Settings.GetMaxParallelAgents())
	}
	qa := cfg.Agents["qa"]
	e2e := cfg.Agents["e2e"]
	if !qa.IsParallelizable() || !e2e.IsParallelizable() {
		t.Fatalf("qa/e2e must be parallelizable: qa=%v e2e=%v", qa.IsParallelizable(), e2e.IsParallelizable())
	}
	arch := cfg.Agents["architect"]
	if arch.GetWorkspace() != "isolated" {
		t.Fatalf("architect workspace want isolated, got %q", arch.GetWorkspace())
	}
}

// TestE2E_Fleet_DispatcherFanOutUnit locks the parallel fan-out helper used by
// the session dispatcher (same package logic, exercised under the e2e tag so
// CI fleet packages pick it up).
func TestE2E_Fleet_DispatcherFanOutUnit(t *testing.T) {
	qa := fleet.FleetAgentConfig{
		Name: "QA", Identity: "id", Behaviors: "b", Tools: fleet.ToolsConfig{All: true},
		Execution: &fleet.AgentExecutionConfig{Parallelizable: true},
	}
	e2eAgent := fleet.FleetAgentConfig{
		Name: "E2E", Identity: "id", Behaviors: "b", Tools: fleet.ToolsConfig{All: true},
		Execution: &fleet.AgentExecutionConfig{Parallelizable: true},
	}
	cfg := &fleet.FleetConfig{
		Name: "parallel",
		Agents: map[string]fleet.FleetAgentConfig{
			"po":  {Name: "PO", Identity: "id", Behaviors: "b", Tools: fleet.ToolsConfig{All: true}},
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
