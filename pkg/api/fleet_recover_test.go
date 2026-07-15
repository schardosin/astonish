package api

import (
	"testing"

	"github.com/SAP/astonish/pkg/agent"
	"github.com/SAP/astonish/pkg/fleet"
)

func testResumeFleetConfig() *fleet.FleetConfig {
	return &fleet.FleetConfig{
		Name: "Test",
		Agents: map[string]fleet.FleetAgentConfig{
			"po":  {Name: "PO", Identity: "PO", Behaviors: "Lead"},
			"dev": {Name: "Dev", Identity: "Dev", Behaviors: "Code"},
		},
		Communication: &fleet.CommunicationConfig{
			Flow: []fleet.CommunicationNode{
				{Role: "po", EntryPoint: true, TalksTo: []string{"dev", "customer"}},
				{Role: "dev", TalksTo: []string{"po", "customer"}},
			},
		},
	}
}

func TestDetermineResumeTarget_CustomerMessageRoutesToEntry(t *testing.T) {
	cfg := testResumeFleetConfig()
	got := determineResumeTarget(fleet.Message{
		Sender: "customer",
		Text:   "looks good, continue",
	}, cfg, &agent.SubAgentManager{}, false)
	if got != "po" {
		t.Fatalf("resume = %q, want po", got)
	}
}

func TestDetermineResumeTarget_WaitingOnCustomerStaysQuiet(t *testing.T) {
	cfg := testResumeFleetConfig()
	got := determineResumeTarget(fleet.Message{
		Sender:   "po",
		Text:     "Please review the PR @customer",
		Mentions: []string{"customer"},
	}, cfg, &agent.SubAgentManager{}, false)
	if got != "" {
		t.Fatalf("resume = %q, want empty (stay quiet)", got)
	}
}

func TestDetermineResumeTarget_WaitingOnCustomerWithIncompleteTasksStaysQuiet(t *testing.T) {
	cfg := testResumeFleetConfig()
	got := determineResumeTarget(fleet.Message{
		Sender:   "po",
		Text:     "Please review the PR @customer",
		Mentions: []string{"customer"},
	}, cfg, &agent.SubAgentManager{}, true)
	if got != "" {
		t.Fatalf("resume = %q, want empty (customer wait overrides incomplete tasks)", got)
	}
}

func TestDetermineResumeTarget_NoneWithNoTasksStaysQuiet(t *testing.T) {
	cfg := testResumeFleetConfig()
	// No mentions and no actionable handoff → fallbackRoute returns "none".
	got := determineResumeTarget(fleet.Message{
		Sender: "po",
		Text:   "Nothing else for anyone to do.",
	}, cfg, &agent.SubAgentManager{}, false)
	if got != "" {
		t.Fatalf("resume = %q, want empty", got)
	}
}

func TestDetermineResumeTarget_SystemWithoutTasksStaysQuiet(t *testing.T) {
	cfg := testResumeFleetConfig()
	got := determineResumeTarget(fleet.Message{
		Sender: "system",
		Text:   "Fleet session resumed after daemon restart.",
	}, cfg, &agent.SubAgentManager{}, false)
	if got != "" {
		t.Fatalf("resume = %q, want empty", got)
	}
}
