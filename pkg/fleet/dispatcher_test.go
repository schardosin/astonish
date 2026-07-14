package fleet

import (
	"testing"
)

func TestCollectActivationTargets_Serial(t *testing.T) {
	t.Parallel()
	cfg := &FleetConfig{
		Agents: map[string]FleetAgentConfig{
			"po": baseAgent("PO"),
			"qa": func() FleetAgentConfig {
				a := baseAgent("QA")
				a.Execution = &AgentExecutionConfig{Parallelizable: true}
				return a
			}(),
			"e2e": func() FleetAgentConfig {
				a := baseAgent("E2E")
				a.Execution = &AgentExecutionConfig{Parallelizable: true}
				return a
			}(),
		},
		Communication: &CommunicationConfig{Flow: []CommunicationNode{
			{Role: "po", TalksTo: []string{"qa", "e2e"}, EntryPoint: true},
			{Role: "qa", TalksTo: []string{"po"}},
			{Role: "e2e", TalksTo: []string{"po"}},
		}},
		Settings: FleetSettings{MaxParallelAgents: 1},
	}
	msg := Message{Sender: "po", Text: "Please @qa and @e2e review", Mentions: []string{"qa", "e2e"}}
	got := collectActivationTargets(msg, RoutingResult{Target: "qa"}, cfg)
	if len(got) != 1 || got[0] != "qa" {
		t.Fatalf("serial mode should return single routing target, got %v", got)
	}
}

func TestCollectActivationTargets_ParallelFanOut(t *testing.T) {
	t.Parallel()
	cfg := &FleetConfig{
		Agents: map[string]FleetAgentConfig{
			"po": baseAgent("PO"),
			"qa": func() FleetAgentConfig {
				a := baseAgent("QA")
				a.Execution = &AgentExecutionConfig{Parallelizable: true}
				return a
			}(),
			"e2e": func() FleetAgentConfig {
				a := baseAgent("E2E")
				a.Execution = &AgentExecutionConfig{Parallelizable: true}
				return a
			}(),
		},
		Communication: &CommunicationConfig{Flow: []CommunicationNode{
			{Role: "po", TalksTo: []string{"qa", "e2e"}, EntryPoint: true},
			{Role: "qa", TalksTo: []string{"po"}},
			{Role: "e2e", TalksTo: []string{"po"}},
		}},
		Settings: FleetSettings{MaxParallelAgents: 2},
	}
	msg := Message{Sender: "po", Text: "Please @qa and @e2e review", Mentions: []string{"qa", "e2e"}}
	got := collectActivationTargets(msg, RoutingResult{Target: "qa"}, cfg)
	if len(got) != 2 {
		t.Fatalf("expected 2 parallel targets, got %v", got)
	}
}

func TestPartitionPending(t *testing.T) {
	t.Parallel()
	cfg := &FleetConfig{
		Agents: map[string]FleetAgentConfig{
			"po": baseAgent("PO"),
			"qa": func() FleetAgentConfig {
				a := baseAgent("QA")
				a.Execution = &AgentExecutionConfig{Parallelizable: true}
				return a
			}(),
			"e2e": func() FleetAgentConfig {
				a := baseAgent("E2E")
				a.Execution = &AgentExecutionConfig{Parallelizable: true}
				return a
			}(),
		},
	}
	serial, parallel, rest := partitionPending([]string{"po", "qa"}, cfg, 2)
	if serial != "po" || len(parallel) != 0 || len(rest) != 1 || rest[0] != "qa" {
		t.Fatalf("non-parallelizable head: serial=%q parallel=%v rest=%v", serial, parallel, rest)
	}
	serial, parallel, rest = partitionPending([]string{"qa", "e2e"}, cfg, 2)
	if serial != "" || len(parallel) != 2 || len(rest) != 0 {
		t.Fatalf("parallel batch: serial=%q parallel=%v rest=%v", serial, parallel, rest)
	}
}
