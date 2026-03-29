package fleet

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const validFleetYAML = `name: test-fleet
description: A test fleet
communication:
  flow:
    - role: po
      talks_to: [customer, dev]
      entry_point: true
    - role: dev
      talks_to: [po, qa]
    - role: qa
      talks_to: [dev, po]
agents:
  po:
    name: Product Owner
    identity: You are a product owner.
    tools:
      - read_file
      - write_file
    behaviors: |
      When receiving a request, gather requirements.
  dev:
    name: Developer
    identity: You are a developer.
    tools: true
    behaviors: |
      When receiving a task, implement it.
  qa:
    name: QA Engineer
    identity: You are a QA engineer.
    tools: true
    behaviors: |
      When receiving code, test it.
settings:
  max_turns_per_agent: 15
`

const delegateFleetYAML = `name: delegate-fleet
description: Fleet with delegate
communication:
  flow:
    - role: dev
      talks_to: [customer]
      entry_point: true
agents:
  dev:
    name: Developer
    identity: You are a developer.
    delegate:
      tool: opencode
      params:
        agent: build
        format: json
      description: OpenCode is a coding agent.
    behaviors: |
      Formulate tasks for the delegate tool.
`

func TestLoadFleet_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(path, []byte(validFleetYAML), 0644); err != nil {
		t.Fatal(err)
		return
	}

	f, err := LoadFleet(path)
	if err != nil {
		t.Fatal(err)
		return
	}

	if f.Name != "test-fleet" {
		t.Errorf("expected name %q, got %q", "test-fleet", f.Name)
	}
	if len(f.Agents) != 3 {
		t.Errorf("expected 3 agents, got %d", len(f.Agents))
	}

	// Check tools parsing
	po := f.Agents["po"]
	if po.Tools.All {
		t.Error("expected po tools.All to be false")
	}
	if len(po.Tools.Names) != 2 {
		t.Errorf("expected 2 tool names for po, got %d", len(po.Tools.Names))
	}

	qa := f.Agents["qa"]
	if !qa.Tools.All {
		t.Error("expected qa tools.All to be true")
	}

	// Check communication graph
	if f.Communication == nil {
		t.Fatal("expected communication to be non-nil")
		return
	}
	if len(f.Communication.Flow) != 3 {
		t.Errorf("expected 3 flow nodes, got %d", len(f.Communication.Flow))
	}

	// Check entry point
	if ep := f.GetEntryPoint(); ep != "po" {
		t.Errorf("expected entry point %q, got %q", "po", ep)
	}

	// Check settings
	if f.Settings.GetMaxTurnsPerAgent() != 15 {
		t.Errorf("expected max_turns_per_agent 15, got %d", f.Settings.GetMaxTurnsPerAgent())
	}
}

func TestLoadFleet_WithDelegate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "delegate.yaml")
	if err := os.WriteFile(path, []byte(delegateFleetYAML), 0644); err != nil {
		t.Fatal(err)
		return
	}

	f, err := LoadFleet(path)
	if err != nil {
		t.Fatal(err)
		return
	}

	dev := f.Agents["dev"]
	if dev.Delegate == nil {
		t.Fatal("expected delegate to be non-nil")
		return
	}
	if dev.Delegate.Tool != "opencode" {
		t.Errorf("expected delegate tool %q, got %q", "opencode", dev.Delegate.Tool)
	}
	if dev.Delegate.Params["agent"] != "build" {
		t.Errorf("expected delegate param agent=%q, got %q", "build", dev.Delegate.Params["agent"])
	}
}

func TestFleetValidate_MissingName(t *testing.T) {
	f := &FleetConfig{
		Agents: map[string]FleetAgentConfig{
			"dev": {Name: "Developer", Identity: "You are a developer.", Tools: ToolsConfig{All: true}, Behaviors: "do stuff"},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for missing name")
	}
}

func TestFleetValidate_NoAgents(t *testing.T) {
	f := &FleetConfig{
		Name:   "empty",
		Agents: map[string]FleetAgentConfig{},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for no agents")
	}
}

func TestFleetValidate_MissingAgentName(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"dev": {Name: "", Identity: "You are a developer.", Tools: ToolsConfig{All: true}, Behaviors: "do stuff"},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for missing agent name")
	}
}

func TestFleetValidate_MissingIdentity(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"dev": {Name: "Developer", Identity: "", Tools: ToolsConfig{All: true}, Behaviors: "do stuff"},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for missing identity")
	}
}

func TestFleetValidate_MissingBehaviors(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"dev": {Name: "Developer", Identity: "You are a developer.", Tools: ToolsConfig{All: true}, Behaviors: ""},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for missing behaviors")
	}
}

func TestFleetValidate_NoToolsOrDelegate(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"dev": {Name: "Developer", Identity: "You are a developer.", Behaviors: "do stuff"},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for no tools or delegate")
	}
}

func TestFleetValidate_DelegateMissingTool(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"dev": {
				Name:      "Developer",
				Identity:  "You are a developer.",
				Delegate:  &DelegateConfig{Tool: ""},
				Behaviors: "do stuff",
			},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for delegate with missing tool")
	}
}

func TestFleetValidate_BadMode(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"dev": {Name: "Developer", Identity: "You are a developer.", Tools: ToolsConfig{All: true}, Behaviors: "do stuff", Mode: "invalid"},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestFleetValidate_CommunicationBadRole(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"dev": {Name: "Developer", Identity: "You are a developer.", Tools: ToolsConfig{All: true}, Behaviors: "do stuff"},
		},
		Communication: &CommunicationConfig{
			Flow: []CommunicationNode{
				{Role: "nonexistent", TalksTo: []string{"dev"}, EntryPoint: true},
			},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for unknown role in communication flow")
	}
}

func TestFleetValidate_CommunicationBadTarget(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"dev": {Name: "Developer", Identity: "You are a developer.", Tools: ToolsConfig{All: true}, Behaviors: "do stuff"},
		},
		Communication: &CommunicationConfig{
			Flow: []CommunicationNode{
				{Role: "dev", TalksTo: []string{"nonexistent"}, EntryPoint: true},
			},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for unknown target in talks_to")
	}
}

func TestFleetValidate_CommunicationNoEntryPoint(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"dev": {Name: "Developer", Identity: "You are a developer.", Tools: ToolsConfig{All: true}, Behaviors: "do stuff"},
		},
		Communication: &CommunicationConfig{
			Flow: []CommunicationNode{
				{Role: "dev", TalksTo: []string{"customer"}},
			},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for no entry point in communication")
	}
}

func TestFleetValidate_CommunicationCustomerAllowed(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"po": {Name: "Product Owner", Identity: "You are a product owner.", Tools: ToolsConfig{All: true}, Behaviors: "requirements"},
		},
		Communication: &CommunicationConfig{
			Flow: []CommunicationNode{
				{Role: "po", TalksTo: []string{"customer"}, EntryPoint: true},
			},
		},
	}
	if err := f.Validate(); err != nil {
		t.Errorf("expected valid communication with customer target, got error: %v", err)
	}
}

func TestCommunicationHelpers(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"po":  {Name: "PO", Identity: "You are a PO.", Tools: ToolsConfig{All: true}, Behaviors: "requirements"},
			"dev": {Name: "Dev", Identity: "You are a dev.", Tools: ToolsConfig{All: true}, Behaviors: "code"},
			"qa":  {Name: "QA", Identity: "You are a QA.", Tools: ToolsConfig{All: true}, Behaviors: "test"},
		},
		Communication: &CommunicationConfig{
			Flow: []CommunicationNode{
				{Role: "po", TalksTo: []string{"customer", "dev"}, EntryPoint: true},
				{Role: "dev", TalksTo: []string{"po", "qa"}},
				{Role: "qa", TalksTo: []string{"dev", "po"}},
			},
		},
	}

	// GetEntryPoint
	if ep := f.GetEntryPoint(); ep != "po" {
		t.Errorf("GetEntryPoint() = %q, want %q", ep, "po")
	}

	// CanTalkTo
	if !f.CanTalkTo("po", "customer") {
		t.Error("expected po to be able to talk to customer")
	}
	if !f.CanTalkTo("po", "dev") {
		t.Error("expected po to be able to talk to dev")
	}
	if f.CanTalkTo("po", "qa") {
		t.Error("expected po NOT to be able to talk to qa")
	}
	if !f.CanTalkTo("dev", "qa") {
		t.Error("expected dev to be able to talk to qa")
	}
	if f.CanTalkTo("dev", "customer") {
		t.Error("expected dev NOT to be able to talk to customer")
	}

	// CanTalkToCustomer
	if !f.CanTalkToCustomer("po") {
		t.Error("expected po to be able to talk to customer")
	}
	if f.CanTalkToCustomer("dev") {
		t.Error("expected dev NOT to be able to talk to customer")
	}

	// GetTalksTo
	poTargets := f.GetTalksTo("po")
	if len(poTargets) != 2 || poTargets[0] != "customer" || poTargets[1] != "dev" {
		t.Errorf("GetTalksTo(po) = %v, want [customer dev]", poTargets)
	}

	// GetFlowOrder
	order := f.GetFlowOrder()
	if len(order) != 3 || order[0] != "po" || order[1] != "dev" || order[2] != "qa" {
		t.Errorf("GetFlowOrder() = %v, want [po dev qa]", order)
	}

	// GetNextInFlow
	if next := f.GetNextInFlow("po"); next != "dev" {
		t.Errorf("GetNextInFlow(po) = %q, want %q", next, "dev")
	}
	if next := f.GetNextInFlow("dev"); next != "qa" {
		t.Errorf("GetNextInFlow(dev) = %q, want %q", next, "qa")
	}
	if next := f.GetNextInFlow("qa"); next != "" {
		t.Errorf("GetNextInFlow(qa) = %q, want empty", next)
	}
}

func TestAgentConfig_GetMode(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		expected string
	}{
		{name: "empty defaults to agentic", mode: "", expected: "agentic"},
		{name: "explicit simple", mode: "simple", expected: "simple"},
		{name: "explicit agentic", mode: "agentic", expected: "agentic"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &FleetAgentConfig{Mode: tt.mode}
			if got := a.GetMode(); got != tt.expected {
				t.Errorf("GetMode() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestLoadFleets_Directory(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(validFleetYAML), 0644); err != nil {
		t.Fatal(err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "delegate.yaml"), []byte(delegateFleetYAML), 0644); err != nil {
		t.Fatal(err)
		return
	}
	// Non-YAML file should be ignored
	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("not a fleet"), 0644); err != nil {
		t.Fatal(err)
		return
	}

	fleets, err := LoadFleets(dir)
	if err != nil {
		t.Fatal(err)
		return
	}

	if len(fleets) != 2 {
		t.Errorf("expected 2 fleets, got %d", len(fleets))
	}
}

func TestLoadFleets_NonExistentDir(t *testing.T) {
	fleets, err := LoadFleets("/tmp/nonexistent-fleet-dir-12345")
	if err != nil {
		t.Errorf("expected nil error for non-existent dir, got %v", err)
	}
	if fleets != nil {
		t.Errorf("expected nil map for non-existent dir, got %v", fleets)
	}
}

func TestFleetSettings_Defaults(t *testing.T) {
	s := FleetSettings{}
	if s.GetMaxTurnsPerAgent() != 20 {
		t.Errorf("expected default max_turns_per_agent 20, got %d", s.GetMaxTurnsPerAgent())
	}
}

func TestRegistry_Basic(t *testing.T) {
	fleetDir := t.TempDir()

	// Create fleet file
	if err := os.WriteFile(filepath.Join(fleetDir, "test.yaml"), []byte(validFleetYAML), 0644); err != nil {
		t.Fatal(err)
		return
	}

	fleetReg, err := NewRegistry(fleetDir)
	if err != nil {
		t.Fatal(err)
		return
	}

	if fleetReg.Count() != 1 {
		t.Errorf("expected 1 fleet, got %d", fleetReg.Count())
	}

	f, ok := fleetReg.GetFleet("test")
	if !ok {
		t.Fatal("expected to find fleet 'test'")
		return
	}
	if f.Name != "test-fleet" {
		t.Errorf("expected name %q, got %q", "test-fleet", f.Name)
	}
}

func TestRegistry_SaveAndDelete(t *testing.T) {
	fleetDir := t.TempDir()

	reg, err := NewRegistry(fleetDir)
	if err != nil {
		t.Fatal(err)
		return
	}

	fleet := &FleetConfig{
		Name: "saved-fleet",
		Agents: map[string]FleetAgentConfig{
			"dev": {
				Name:      "Developer",
				Identity:  "You are a developer.",
				Tools:     ToolsConfig{All: true},
				Behaviors: "implement stuff",
			},
		},
	}

	if err := reg.Save("saved", fleet); err != nil {
		t.Fatal(err)
		return
	}

	if reg.Count() != 1 {
		t.Errorf("expected 1 fleet after save, got %d", reg.Count())
	}

	// Verify on disk
	if _, err := os.Stat(filepath.Join(fleetDir, "saved.yaml")); err != nil {
		t.Errorf("expected saved.yaml on disk: %v", err)
	}

	// Delete
	if err := reg.Delete("saved"); err != nil {
		t.Fatal(err)
		return
	}

	if reg.Count() != 0 {
		t.Errorf("expected 0 fleets after delete, got %d", reg.Count())
	}
}

func TestBuildAgentPrompt(t *testing.T) {
	agentCfg := FleetAgentConfig{
		Name:      "Developer",
		Identity:  "You are a developer.",
		Behaviors: "When receiving a task, implement it carefully.",
		Delegate: &DelegateConfig{
			Tool:        "opencode",
			Params:      map[string]any{"agent": "build", "format": "json"},
			Description: "OpenCode is a coding agent.",
		},
	}

	fleetCfg := &FleetConfig{
		Name: "test",
		Communication: &CommunicationConfig{
			Flow: []CommunicationNode{
				{Role: "po", TalksTo: []string{"customer", "dev"}, EntryPoint: true},
				{Role: "dev", TalksTo: []string{"po", "qa"}},
				{Role: "qa", TalksTo: []string{"dev"}},
			},
		},
		Agents: map[string]FleetAgentConfig{
			"po":  agentCfg,
			"dev": agentCfg,
			"qa":  agentCfg,
		},
	}

	prompt := BuildAgentPrompt(agentCfg, fleetCfg, "dev", nil, "", "", "")

	if !strings.Contains(prompt, "You are a developer.") {
		t.Error("expected prompt to contain identity")
	}
	if !strings.Contains(prompt, "Team Behaviors") {
		t.Error("expected prompt to contain behaviors section")
	}
	if !strings.Contains(prompt, "implement it carefully") {
		t.Error("expected prompt to contain behavior content")
	}
	if !strings.Contains(prompt, "Communication Rules") {
		t.Error("expected prompt to contain communication rules section")
	}
	if !strings.Contains(prompt, "@po") {
		t.Error("expected prompt to mention communication targets")
	}
	if !strings.Contains(prompt, "@qa") {
		t.Error("expected prompt to mention communication targets")
	}
	if !strings.Contains(prompt, "Delegate Tool") {
		t.Error("expected prompt to contain delegate execution section")
	}
	if !strings.Contains(prompt, "`opencode` tool") {
		t.Error("expected prompt to instruct use of opencode tool")
	}
	if !strings.Contains(prompt, "How to Use") {
		t.Error("expected prompt to contain usage section")
	}
	if !strings.Contains(prompt, "OpenCode is a coding agent.") {
		t.Error("expected prompt to contain delegate description")
	}
}

func TestBuildAgentPrompt_NoDelegate(t *testing.T) {
	agentCfg := FleetAgentConfig{
		Name:      "QA Engineer",
		Identity:  "You are a QA engineer.",
		Tools:     ToolsConfig{All: true},
		Behaviors: "Test everything.",
	}

	fleetCfg := &FleetConfig{
		Name: "test",
		Communication: &CommunicationConfig{
			Flow: []CommunicationNode{
				{Role: "qa", TalksTo: []string{"dev"}, EntryPoint: true},
				{Role: "dev", TalksTo: []string{"qa"}},
			},
		},
		Agents: map[string]FleetAgentConfig{
			"qa":  agentCfg,
			"dev": {Name: "Dev", Identity: "You are a dev.", Tools: ToolsConfig{All: true}, Behaviors: "code"},
		},
	}

	prompt := BuildAgentPrompt(agentCfg, fleetCfg, "qa", nil, "", "", "")

	if !strings.Contains(prompt, "You are a QA engineer.") {
		t.Error("expected prompt to contain identity")
	}
	if strings.Contains(prompt, "Delegate Tool") {
		t.Error("expected prompt to NOT contain delegate execution section")
	}
	if strings.Contains(prompt, "`opencode` tool") {
		t.Error("expected prompt to NOT contain opencode tool instructions")
	}
}

func TestBuildSystemPromptSection_Empty(t *testing.T) {
	result := BuildSystemPromptSection(nil, nil)
	if result != "" {
		t.Errorf("expected empty string for no fleets, got %q", result)
	}
}

func TestBuildSystemPromptSection_WithCommunication(t *testing.T) {
	fleets := []FleetSummary{
		{Key: "dev", Name: "software-dev", Description: "Dev team", AgentCount: 3},
	}

	fleetConfigs := map[string]*FleetConfig{
		"dev": {
			Name:        "software-dev",
			Description: "Dev team",
			Communication: &CommunicationConfig{
				Flow: []CommunicationNode{
					{Role: "po", TalksTo: []string{"customer", "dev"}, EntryPoint: true},
					{Role: "dev", TalksTo: []string{"po", "qa"}},
					{Role: "qa", TalksTo: []string{"dev", "po"}},
				},
			},
			Agents: map[string]FleetAgentConfig{
				"po":  {Name: "Product Owner", Identity: "You are a PO.", Tools: ToolsConfig{All: true}, Behaviors: "requirements"},
				"dev": {Name: "Developer", Identity: "You are a dev.", Delegate: &DelegateConfig{Tool: "opencode"}, Behaviors: "code"},
				"qa":  {Name: "QA Engineer", Identity: "You are a QA.", Tools: ToolsConfig{All: true}, Behaviors: "test"},
			},
		},
	}

	result := BuildSystemPromptSection(
		fleets,
		func(key string) (*FleetConfig, bool) {
			f, ok := fleetConfigs[key]
			return f, ok
		},
	)

	if !strings.Contains(result, "Fleet-Based Development") {
		t.Error("expected fleet guidance section header")
	}
	if !strings.Contains(result, "software-dev") {
		t.Error("expected fleet name in listing")
	}
	if !strings.Contains(result, "po") {
		t.Error("expected agent names in flow")
	}
}

func TestToolsConfig_Marshal(t *testing.T) {
	tests := []struct {
		name   string
		config ToolsConfig
		empty  bool
	}{
		{name: "all", config: ToolsConfig{All: true}, empty: false},
		{name: "names", config: ToolsConfig{Names: []string{"a", "b"}}, empty: false},
		{name: "empty", config: ToolsConfig{}, empty: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.config.IsEmpty() != tt.empty {
				t.Errorf("IsEmpty() = %v, want %v", tt.config.IsEmpty(), tt.empty)
			}
		})
	}
}

func TestCollectDelegateEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		fleets   map[string]*FleetConfig
		expected map[string]bool // expected env var names (order-independent)
	}{
		{
			name:     "nil fleets",
			fleets:   nil,
			expected: map[string]bool{},
		},
		{
			name: "no delegates",
			fleets: map[string]*FleetConfig{
				"test": {
					Name: "test",
					Agents: map[string]FleetAgentConfig{
						"dev": {Name: "Developer", Identity: "You are a dev.", Tools: ToolsConfig{All: true}, Behaviors: "code"},
					},
				},
			},
			expected: map[string]bool{},
		},
		{
			name: "delegate without env",
			fleets: map[string]*FleetConfig{
				"test": {
					Name: "test",
					Agents: map[string]FleetAgentConfig{
						"dev": {
							Name:      "Developer",
							Identity:  "You are a dev.",
							Delegate:  &DelegateConfig{Tool: "opencode"},
							Behaviors: "code",
						},
					},
				},
			},
			expected: map[string]bool{},
		},
		{
			name: "delegate with env",
			fleets: map[string]*FleetConfig{
				"test": {
					Name: "test",
					Agents: map[string]FleetAgentConfig{
						"dev": {
							Name:      "Developer",
							Identity:  "You are a dev.",
							Delegate:  &DelegateConfig{Tool: "opencode", Env: []string{"BIFROST_API_KEY"}},
							Behaviors: "code",
						},
					},
				},
			},
			expected: map[string]bool{"BIFROST_API_KEY": true},
		},
		{
			name: "deduplicates across agents and fleets",
			fleets: map[string]*FleetConfig{
				"fleet1": {
					Name: "fleet1",
					Agents: map[string]FleetAgentConfig{
						"dev": {
							Name:      "Developer",
							Identity:  "You are a dev.",
							Delegate:  &DelegateConfig{Tool: "opencode", Env: []string{"BIFROST_API_KEY", "CUSTOM_KEY"}},
							Behaviors: "code",
						},
						"qa": {
							Name:      "QA",
							Identity:  "You are QA.",
							Delegate:  &DelegateConfig{Tool: "opencode", Env: []string{"BIFROST_API_KEY"}},
							Behaviors: "test",
						},
					},
				},
				"fleet2": {
					Name: "fleet2",
					Agents: map[string]FleetAgentConfig{
						"arch": {
							Name:      "Architect",
							Identity:  "You are an architect.",
							Delegate:  &DelegateConfig{Tool: "opencode", Env: []string{"BIFROST_API_KEY", "OTHER_KEY"}},
							Behaviors: "design",
						},
					},
				},
			},
			expected: map[string]bool{"BIFROST_API_KEY": true, "CUSTOM_KEY": true, "OTHER_KEY": true},
		},
		{
			name: "trims whitespace and skips empty",
			fleets: map[string]*FleetConfig{
				"test": {
					Name: "test",
					Agents: map[string]FleetAgentConfig{
						"dev": {
							Name:      "Developer",
							Identity:  "You are a dev.",
							Delegate:  &DelegateConfig{Tool: "opencode", Env: []string{"  BIFROST_API_KEY  ", "", "  "}},
							Behaviors: "code",
						},
					},
				},
			},
			expected: map[string]bool{"BIFROST_API_KEY": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CollectDelegateEnvVars(tt.fleets)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d env vars, got %d: %v", len(tt.expected), len(result), result)
				return
			}
			for _, name := range result {
				if !tt.expected[name] {
					t.Errorf("unexpected env var %q in result", name)
				}
			}
		})
	}
}

func TestTaskSlugFromIssue(t *testing.T) {
	tests := []struct {
		number   int
		title    string
		expected string
	}{
		{6, "Improve Payoff Chart to show the Today Line", "issue-6-improve-payoff-chart-to-show-the-today-line"},
		{123, "Fix bug", "issue-123-fix-bug"},
		{1, "A very long title that exceeds the sixty character limit for branch names", "issue-1-a-very-long-title-that-exceeds-the-sixty-character"},
		{42, "Special chars: $100 & 50% off!!!", "issue-42-special-chars-100-50-off"},
		{0, "Edge case zero", "issue-0-edge-case-zero"},
		{7, "  Trim spaces  ", "issue-7-trim-spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			result := TaskSlugFromIssue(tt.number, tt.title)
			if result != tt.expected {
				t.Errorf("TaskSlugFromIssue(%d, %q) = %q, want %q", tt.number, tt.title, result, tt.expected)
			}
		})
	}
}

func TestResolveBranchPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		slug     string
		expected string
	}{
		{"fleet/{task}", "issue-6-payoff-chart", "fleet/issue-6-payoff-chart"},
		{"fleet/<task>", "issue-6-payoff-chart", "fleet/issue-6-payoff-chart"},
		{"feature/{task}/dev", "issue-42-fix", "feature/issue-42-fix/dev"},
		{"fleet/{task}", "", "fleet/{task}"},
		{"main", "issue-1-test", "main"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.slug, func(t *testing.T) {
			result := ResolveBranchPattern(tt.pattern, tt.slug)
			if result != tt.expected {
				t.Errorf("ResolveBranchPattern(%q, %q) = %q, want %q", tt.pattern, tt.slug, result, tt.expected)
			}
		})
	}
}

func TestBuildAgentPrompt_GitWorkflow(t *testing.T) {
	agentCfg := FleetAgentConfig{
		Name:      "Developer",
		Identity:  "You are a developer.",
		Behaviors: "Implement things.",
	}

	fleetCfg := &FleetConfig{
		Name: "test",
		Communication: &CommunicationConfig{
			Flow: []CommunicationNode{
				{Role: "po", TalksTo: []string{"customer", "dev"}, EntryPoint: true},
				{Role: "dev", TalksTo: []string{"po"}},
			},
		},
		Agents: map[string]FleetAgentConfig{
			"po":  agentCfg,
			"dev": agentCfg,
		},
	}

	plan := &FleetPlan{
		FleetConfig: *fleetCfg,
		Artifacts: map[string]PlanArtifactConfig{
			"code": {
				Type:          "git_repo",
				Repo:          "owner/repo",
				BranchPattern: "fleet/{task}",
				AutoPR:        true,
			},
		},
	}

	prompt := BuildAgentPrompt(agentCfg, fleetCfg, "dev", nil, "", "issue-6-payoff-chart", "", plan)

	// Check git workflow section exists
	if !strings.Contains(prompt, "## Git Workflow") {
		t.Error("expected prompt to contain Git Workflow section")
	}
	if !strings.Contains(prompt, "NEVER push directly to the `main` or `master` branch") {
		t.Error("expected prompt to contain never-push-to-main rule")
	}
	// Check resolved branch name
	if !strings.Contains(prompt, "fleet/issue-6-payoff-chart") {
		t.Error("expected prompt to contain resolved branch name")
	}
	if !strings.Contains(prompt, "git checkout -b fleet/issue-6-payoff-chart") {
		t.Error("expected prompt to contain checkout command")
	}
	// Check commit and share instructions
	if !strings.Contains(prompt, "Commit and share every document you write") {
		t.Error("expected prompt to contain commit-and-share instructions")
	}
	// Check GitHub link template with resolved repo and branch
	if !strings.Contains(prompt, "https://github.com/owner/repo/blob/fleet/issue-6-payoff-chart/") {
		t.Error("expected prompt to contain GitHub link template with resolved repo and branch")
	}
}

func TestBuildAgentPrompt_CommunicationMechanism(t *testing.T) {
	agentCfg := FleetAgentConfig{
		Name:      "Dev",
		Identity:  "You are a dev.",
		Behaviors: "Code.",
	}

	fleetCfg := &FleetConfig{
		Name: "test",
		Communication: &CommunicationConfig{
			Flow: []CommunicationNode{
				{Role: "po", TalksTo: []string{"customer", "dev"}, EntryPoint: true},
				{Role: "dev", TalksTo: []string{"po"}},
			},
		},
		Agents: map[string]FleetAgentConfig{
			"po":  agentCfg,
			"dev": agentCfg,
		},
	}

	prompt := BuildAgentPrompt(agentCfg, fleetCfg, "dev", nil, "", "", "")

	// Check improved communication explanation
	if !strings.Contains(prompt, "Your TEXT OUTPUT is how you communicate") {
		t.Error("expected prompt to explain text output IS the communication")
	}
	if !strings.Contains(prompt, "Do NOT use shell_command, echo, or any tool to") {
		t.Error("expected prompt to warn against using tools for communication")
	}
}

func TestBuildAgentPrompt_DocsPathFromArtifact(t *testing.T) {
	agentCfg := FleetAgentConfig{
		Name:      "PO",
		Identity:  "You are a PO.",
		Behaviors: "Manage things.",
	}

	fleetCfg := &FleetConfig{
		Name: "test",
		Communication: &CommunicationConfig{
			Flow: []CommunicationNode{
				{Role: "po", TalksTo: []string{"customer"}, EntryPoint: true},
			},
		},
		Agents: map[string]FleetAgentConfig{
			"po": agentCfg,
		},
	}

	plan := &FleetPlan{
		FleetConfig: *fleetCfg,
		Artifacts: map[string]PlanArtifactConfig{
			"code": {Type: "git_repo", Repo: "owner/repo", BranchPattern: "fleet/{task}"},
			"docs": {Type: "git_repo", Repo: "owner/repo", BranchPattern: "fleet/{task}", SubPath: "documentation"},
		},
		// Set WorkspaceDir directly (the final resolved path) so ResolveWorkspaceDir()
		// returns it as-is without appending a project name derived from the repo.
		WorkspaceDir: "/tmp/test-workspace",
	}

	prompt := BuildAgentPrompt(agentCfg, fleetCfg, "po", nil, "", "issue-5-add-feature", "", plan)

	// Should derive docs path from artifact sub_path, not hardcoded "docs"
	if !strings.Contains(prompt, "/tmp/test-workspace/documentation/issue-5-add-feature/") {
		// Find the docs reference in the prompt for debugging
		idx := strings.Index(prompt, "Fleet documentation")
		if idx < 0 {
			t.Errorf("expected prompt to contain 'Fleet documentation' with docs path from artifact sub_path 'documentation'")
		} else {
			end := idx + 150
			if end > len(prompt) {
				end = len(prompt)
			}
			t.Errorf("expected prompt to derive docs path from artifact sub_path 'documentation', got prompt snippet: %s", prompt[idx:end])
		}
	}
	// Should NOT contain the old hardcoded "docs/" path
	if strings.Contains(prompt, "/tmp/test-workspace/docs/issue-5-add-feature/") {
		t.Error("expected prompt to NOT hardcode 'docs/' when artifact sub_path is 'documentation'")
	}
}

func TestGetConfigStringSlice(t *testing.T) {
	tests := []struct {
		name     string
		config   map[string]any
		key      string
		expected []string
	}{
		{
			name:     "nil config",
			config:   nil,
			key:      "labels",
			expected: nil,
		},
		{
			name:     "key not found",
			config:   map[string]any{"other": "value"},
			key:      "labels",
			expected: nil,
		},
		{
			name:     "single string value",
			config:   map[string]any{"label": "astonish_fleet"},
			key:      "label",
			expected: []string{"astonish_fleet"},
		},
		{
			name:     "empty string value",
			config:   map[string]any{"label": ""},
			key:      "label",
			expected: nil,
		},
		{
			name:     "string slice value",
			config:   map[string]any{"labels": []string{"bug", "fleet"}},
			key:      "labels",
			expected: []string{"bug", "fleet"},
		},
		{
			name:     "any slice value (from YAML)",
			config:   map[string]any{"labels": []any{"bug", "fleet"}},
			key:      "labels",
			expected: []string{"bug", "fleet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getConfigStringSlice(tt.config, tt.key)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}
			if len(result) != len(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("expected[%d] = %q, got %q", i, tt.expected[i], v)
				}
			}
		})
	}
}

func TestNewGitHubMonitor_LabelFallback(t *testing.T) {
	// Test that singular "label" key works as fallback for "labels"
	stateDir := t.TempDir()

	// Singular key (common in YAML configs)
	m := NewGitHubMonitor("test-plan", map[string]any{
		"repo":  "owner/repo",
		"label": "astonish_fleet",
	}, stateDir)

	if len(m.Labels) != 1 || m.Labels[0] != "astonish_fleet" {
		t.Errorf("expected labels=[astonish_fleet] from singular 'label' key, got %v", m.Labels)
	}

	// Plural key takes precedence
	m2 := NewGitHubMonitor("test-plan", map[string]any{
		"repo":   "owner/repo",
		"labels": []any{"bug", "fleet"},
		"label":  "ignored",
	}, stateDir)

	if len(m2.Labels) != 2 || m2.Labels[0] != "bug" || m2.Labels[1] != "fleet" {
		t.Errorf("expected labels=[bug, fleet] from plural 'labels' key, got %v", m2.Labels)
	}
}

// ---------------------------------------------------------------------------
// Stateless GitHub Monitor tests
// ---------------------------------------------------------------------------

func TestGitHubMonitor_MarkSeenAndGetState(t *testing.T) {
	stateDir := t.TempDir()
	m := NewGitHubMonitor("test-plan", map[string]any{"repo": "owner/repo"}, stateDir)

	m.MarkSeen(42, "session-abc", "Fix the bug")

	s := m.GetIssueState(42)
	if s == nil {
		t.Fatal("expected state for issue 42")
	}
	if s.SessionID != "session-abc" {
		t.Errorf("expected session_id %q, got %q", "session-abc", s.SessionID)
	}
	if s.IssueTitle != "Fix the bug" {
		t.Errorf("expected title %q, got %q", "Fix the bug", s.IssueTitle)
	}

	// Unknown issue should return nil
	if m.GetIssueState(999) != nil {
		t.Error("expected nil for unknown issue")
	}
}

func TestGitHubMonitor_UpdateCursorOnlyAdvances(t *testing.T) {
	stateDir := t.TempDir()
	m := NewGitHubMonitor("test-plan", map[string]any{"repo": "owner/repo"}, stateDir)

	m.MarkSeen(10, "sess-1", "Issue 10")

	m.UpdateCursor(10, 100)
	s := m.GetIssueState(10)
	if s.LastCommentID != 100 {
		t.Errorf("expected cursor 100, got %d", s.LastCommentID)
	}

	// Cursor should not regress
	m.UpdateCursor(10, 50)
	s = m.GetIssueState(10)
	if s.LastCommentID != 100 {
		t.Errorf("expected cursor to stay at 100, got %d", s.LastCommentID)
	}

	// Cursor should advance
	m.UpdateCursor(10, 200)
	s = m.GetIssueState(10)
	if s.LastCommentID != 200 {
		t.Errorf("expected cursor 200, got %d", s.LastCommentID)
	}
}

func TestGitHubMonitor_RetryCountAndBackoff(t *testing.T) {
	stateDir := t.TempDir()
	m := NewGitHubMonitor("test-plan", map[string]any{"repo": "owner/repo"}, stateDir)

	m.MarkSeen(10, "sess-1", "Issue 10")

	// First failure
	m.IncrementRetryCount(10, "connection timeout")
	s := m.GetIssueState(10)
	if s.RetryCount != 1 {
		t.Fatalf("expected retry_count 1, got %d", s.RetryCount)
	}
	if s.LastError != "connection timeout" {
		t.Errorf("expected error %q, got %q", "connection timeout", s.LastError)
	}

	// Second failure
	m.IncrementRetryCount(10, "model error")
	s = m.GetIssueState(10)
	if s.RetryCount != 2 {
		t.Fatalf("expected retry_count 2, got %d", s.RetryCount)
	}

	// Third failure — should now appear in GetIssuesNeedingAttention
	m.IncrementRetryCount(10, "final error")
	s = m.GetIssueState(10)
	if s.RetryCount != 3 {
		t.Fatalf("expected retry_count 3, got %d", s.RetryCount)
	}

	issues := m.GetIssuesNeedingAttention()
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue needing attention, got %d", len(issues))
	}
	if issues[0].IssueNumber != 10 {
		t.Errorf("expected issue #10, got #%d", issues[0].IssueNumber)
	}
	if issues[0].RetryCount != 3 {
		t.Errorf("expected retry_count 3, got %d", issues[0].RetryCount)
	}
}

func TestGitHubMonitor_ResetRetryCount(t *testing.T) {
	stateDir := t.TempDir()
	m := NewGitHubMonitor("test-plan", map[string]any{"repo": "owner/repo"}, stateDir)

	m.MarkSeen(10, "sess-1", "Issue 10")
	m.IncrementRetryCount(10, "error 1")
	m.IncrementRetryCount(10, "error 2")
	m.IncrementRetryCount(10, "error 3")

	// Should be in "needs attention" now
	if len(m.GetIssuesNeedingAttention()) != 1 {
		t.Fatal("expected issue to need attention before reset")
	}

	// Reset
	if err := m.ResetRetryCount(10); err != nil {
		t.Fatal(err)
	}

	s := m.GetIssueState(10)
	if s.RetryCount != 0 {
		t.Errorf("expected retry_count 0 after reset, got %d", s.RetryCount)
	}
	if s.LastError != "" {
		t.Errorf("expected empty error after reset, got %q", s.LastError)
	}

	// Should no longer need attention
	if len(m.GetIssuesNeedingAttention()) != 0 {
		t.Fatal("expected no issues needing attention after reset")
	}

	// Reset on unknown issue should error
	if err := m.ResetRetryCount(999); err == nil {
		t.Error("expected error when resetting unknown issue")
	}
}

func TestGitHubMonitor_ClearRetryOnSuccess(t *testing.T) {
	stateDir := t.TempDir()
	m := NewGitHubMonitor("test-plan", map[string]any{"repo": "owner/repo"}, stateDir)

	m.MarkSeen(10, "sess-1", "Issue 10")
	m.IncrementRetryCount(10, "transient error")

	s := m.GetIssueState(10)
	if s.RetryCount != 1 {
		t.Fatalf("expected retry_count 1, got %d", s.RetryCount)
	}

	m.ClearRetryOnSuccess(10)

	s = m.GetIssueState(10)
	if s.RetryCount != 0 {
		t.Errorf("expected retry_count 0 after success, got %d", s.RetryCount)
	}
}

func TestGitHubMonitor_StatePersistence(t *testing.T) {
	stateDir := t.TempDir()

	// Create and populate a monitor
	m1 := NewGitHubMonitor("test-plan", map[string]any{"repo": "owner/repo"}, stateDir)
	m1.MarkSeen(10, "sess-1", "Issue 10")
	m1.UpdateCursor(10, 500)
	m1.IncrementRetryCount(10, "some error")

	// Create a new monitor and load state from disk
	m2 := NewGitHubMonitor("test-plan", map[string]any{"repo": "owner/repo"}, stateDir)
	if err := m2.LoadState(); err != nil {
		t.Fatal(err)
	}

	s := m2.GetIssueState(10)
	if s == nil {
		t.Fatal("expected state for issue 10 after reload")
	}
	if s.SessionID != "sess-1" {
		t.Errorf("expected session_id %q, got %q", "sess-1", s.SessionID)
	}
	if s.LastCommentID != 500 {
		t.Errorf("expected cursor 500, got %d", s.LastCommentID)
	}
	if s.RetryCount != 1 {
		t.Errorf("expected retry_count 1, got %d", s.RetryCount)
	}
	if s.LastError != "some error" {
		t.Errorf("expected error %q, got %q", "some error", s.LastError)
	}
}

func TestGitHubMonitor_BackoffLogic(t *testing.T) {
	stateDir := t.TempDir()
	m := NewGitHubMonitor("test-plan", map[string]any{"repo": "owner/repo"}, stateDir)

	// No failures — backoff expired (should proceed)
	s := &SeenIssueState{RetryCount: 0}
	if !m.isBackoffExpired(s) {
		t.Error("expected backoff to be expired with no failures")
	}

	// Recent failure — should NOT be expired
	s = &SeenIssueState{RetryCount: 1, LastFailedAt: time.Now()}
	if m.isBackoffExpired(s) {
		t.Error("expected backoff to NOT be expired for recent failure")
	}

	// Old failure — should be expired (backoff for retry 1 = 1min)
	s = &SeenIssueState{RetryCount: 1, LastFailedAt: time.Now().Add(-2 * time.Minute)}
	if !m.isBackoffExpired(s) {
		t.Error("expected backoff to be expired for failure 2 minutes ago")
	}
}

func TestStripInlineToolCalls(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no tool calls",
			input:    "Here is the requirements document for the feature.",
			expected: "Here is the requirements document for the feature.",
		},
		{
			name:     "tool call at end",
			input:    "Let me write the file.\nwrite_file{\"file_path\":\"/tmp/test.md\",\"content\":\"# Hello\\nWorld\"}",
			expected: "Let me write the file.",
		},
		{
			name:     "tool call in middle",
			input:    "Let me search first.\ngrep_search{\"pattern\":\"foo\",\"search_path\":\"/src\"}\nNow I found it.",
			expected: "Let me search first.\n\nNow I found it.",
		},
		{
			name:     "multiple tool calls",
			input:    "Step 1.\nread_file{\"file_path\":\"/a.go\"}\nStep 2.\nwrite_file{\"file_path\":\"/b.go\",\"content\":\"package main\"}\nDone.",
			expected: "Step 1.\n\nStep 2.\n\nDone.",
		},
		{
			name:     "nested braces in content",
			input:    "Creating file.\nwrite_file{\"file_path\":\"/t.go\",\"content\":\"func main() {\\n  if true {\\n    fmt.Println()\\n  }\\n}\"}",
			expected: "Creating file.",
		},
		{
			name:     "tool call only",
			input:    "grep_search{\"pattern\":\"test\"}",
			expected: "",
		},
		{
			name:     "not a tool call (normal text with braces)",
			input:    "The config uses {key: value} format.",
			expected: "The config uses {key: value} format.",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripInlineToolCalls(tt.input)
			if result != tt.expected {
				t.Errorf("stripInlineToolCalls(%q)\n  got:  %q\n  want: %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGitHubIssueChannelImplementsExternalPoster(t *testing.T) {
	// Verify GitHubIssueChannel satisfies the ExternalPoster interface.
	var ch interface{} = &GitHubIssueChannel{}
	if _, ok := ch.(ExternalPoster); !ok {
		t.Fatal("GitHubIssueChannel does not implement ExternalPoster")
	}
}

func TestChatChannelDoesNotImplementExternalPoster(t *testing.T) {
	// ChatChannel should NOT implement ExternalPoster (no external system).
	var ch interface{} = &ChatChannel{}
	if _, ok := ch.(ExternalPoster); ok {
		t.Fatal("ChatChannel should not implement ExternalPoster")
	}
}

func TestPostExternalSkipsCustomerMessages(t *testing.T) {
	// PostExternal should silently skip customer messages
	// (they originate FROM GitHub, posting them back would duplicate).
	ch := &GitHubIssueChannel{
		repo:        "test/repo",
		issueNumber: 1,
	}
	ch.cond = sync.NewCond(&ch.mu)

	// This should not panic or call postCommentAsync (which would fail
	// without a valid ghToken/repo). If it tries to post, the gh command
	// will fail and we'll get an error log, but the test is that it
	// returns without attempting.
	msg := Message{
		Sender: "customer",
		Text:   "Hello from GitHub",
	}
	ch.PostExternal(msg) // Should be a no-op
}

func TestPostExternalSkipsIntermediateMessages(t *testing.T) {
	ch := &GitHubIssueChannel{
		repo:        "test/repo",
		issueNumber: 1,
	}
	ch.cond = sync.NewCond(&ch.mu)

	msg := Message{
		Sender: "po",
		Text:   "Let me search for...",
		Metadata: map[string]any{
			"intermediate": true,
		},
	}
	ch.PostExternal(msg) // Should be a no-op
}

func TestPostMessageDoesNotPostToGitHub(t *testing.T) {
	// After the refactor, PostMessage should only add to internal list
	// and notify subscribers. It should NOT attempt GitHub posting.
	// We verify by checking that a message is added to the internal list
	// without any GitHub API errors (since we have no valid token).
	ch := &GitHubIssueChannel{
		repo:        "test/repo",
		issueNumber: 1,
	}
	ch.cond = sync.NewCond(&ch.mu)
	ch.subscribers = make(map[string]chan Message)

	msg := Message{
		Sender: "po",
		Text:   "This is a final agent message",
	}
	err := ch.PostMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("PostMessage returned error: %v", err)
	}

	// Verify message was added to internal list
	ch.mu.RLock()
	if len(ch.messages) != 1 {
		t.Fatalf("Expected 1 message in internal list, got %d", len(ch.messages))
	}
	if ch.messages[0].Text != "This is a final agent message" {
		t.Fatalf("Message text mismatch: %s", ch.messages[0].Text)
	}
	ch.mu.RUnlock()

	// If PostMessage tried to post to GitHub, it would have logged an error
	// (no valid ghToken). The absence of panics and the message being in the
	// internal list confirms PostMessage only does internal operations.
}

// ---------------------------------------------------------------------------
// Thread context building tests
// ---------------------------------------------------------------------------

func makeMessages(count int, charsPer int) []Message {
	msgs := make([]Message, count)
	for i := 0; i < count; i++ {
		text := strings.Repeat("x", charsPer)
		msgs[i] = Message{
			Sender: "po",
			Text:   fmt.Sprintf("Message %d: %s", i, text),
		}
	}
	return msgs
}

func TestBuildThreadWithBudget_LastMessageNeverTruncated(t *testing.T) {
	// Create a thread where the last message is very long (10K chars).
	// It should ALWAYS appear in full, never truncated.
	msgs := makeMessages(50, 500) // 50 messages × ~510 chars = ~25K
	lastMsgText := strings.Repeat("IMPORTANT ", 1000)
	msgs = append(msgs, Message{
		Sender: "customer",
		Text:   lastMsgText,
	})

	result := buildThreadWithBudget(msgs)

	// The full last message text must appear in the result.
	if !strings.Contains(result, lastMsgText) {
		t.Fatal("Last message was truncated — it should ALWAYS be included in full")
	}

	// It should be in the "Message you must respond to" section.
	if !strings.Contains(result, "### Message you must respond to") {
		t.Fatal("Missing 'Message you must respond to' section header")
	}
}

func TestBuildThreadWithBudget_SmallThread(t *testing.T) {
	// Small thread should include everything in full, no summaries.
	msgs := []Message{
		{Sender: "customer", Text: "Please implement feature X"},
		{Sender: "po", Text: "I'll analyze the requirements and get started."},
		{Sender: "po", Text: "Here are the requirements for @dev to implement."},
	}

	result := buildThreadWithBudget(msgs)

	// All messages should be present in full.
	if !strings.Contains(result, "Please implement feature X") {
		t.Fatal("Missing first message")
	}
	if !strings.Contains(result, "I'll analyze the requirements") {
		t.Fatal("Missing second message")
	}
	if !strings.Contains(result, "Here are the requirements for @dev") {
		t.Fatal("Missing last message")
	}
}

func TestBuildThreadWithBudget_LargeThreadSummarizesOlder(t *testing.T) {
	// Create a thread large enough to trigger summarization.
	// 100 messages × 1000 chars each = 100K >> 50K budget.
	msgs := makeMessages(100, 1000)

	result := buildThreadWithBudget(msgs)

	// Should have summary section for older messages.
	if !strings.Contains(result, "### Earlier in the conversation (summary)") {
		t.Fatal("Missing summary section for large thread")
	}

	// Last message should be in full.
	if !strings.Contains(result, "Message 99:") {
		t.Fatal("Last message missing")
	}

	// Total should not exceed budget (with the last message exception).
	// The budget is soft for the last message, but the non-last portion should fit.
	if !strings.Contains(result, "### Message you must respond to") {
		t.Fatal("Missing last message section")
	}
}

func TestDeduplicateRecoverySummaries(t *testing.T) {
	thread := []Message{
		{Sender: "customer", Text: "Initial request"},
		{Sender: "po", Text: "Working on it"},
		{Sender: "system", Text: "Fleet session resumed after daemon restart. Summary 1"},
		{Sender: "po", Text: "Continuing work"},
		{Sender: "system", Text: "Fleet session resumed after daemon restart. Summary 2"},
		{Sender: "dev", Text: "Implementing now"},
		{Sender: "system", Text: "Fleet session resumed after daemon restart. Summary 3"},
		{Sender: "po", Text: "Final report"},
	}

	result := deduplicateRecoverySummaries(thread)

	// Should have removed 2 of the 3 recovery summaries.
	recoveryCount := 0
	for _, msg := range result {
		if isRecoverySummary(msg) {
			recoveryCount++
		}
	}
	if recoveryCount != 1 {
		t.Fatalf("Expected 1 recovery summary, got %d", recoveryCount)
	}

	// The kept one should be the LAST one (Summary 3).
	for _, msg := range result {
		if isRecoverySummary(msg) {
			if !strings.Contains(msg.Text, "Summary 3") {
				t.Fatalf("Expected the last recovery summary to be kept, got: %s", msg.Text)
			}
		}
	}

	// Non-recovery messages should all be preserved.
	if len(result) != 6 { // 8 - 2 removed = 6
		t.Fatalf("Expected 6 messages after dedup, got %d", len(result))
	}
}

func TestDeduplicateRecoverySummaries_NoSummaries(t *testing.T) {
	thread := []Message{
		{Sender: "customer", Text: "Hello"},
		{Sender: "po", Text: "Hi there"},
	}

	result := deduplicateRecoverySummaries(thread)
	if len(result) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(result))
	}
}

func TestDeduplicateRecoverySummaries_OneSummary(t *testing.T) {
	thread := []Message{
		{Sender: "customer", Text: "Hello"},
		{Sender: "system", Text: "Fleet session resumed after daemon restart. Only one."},
		{Sender: "po", Text: "Back to work"},
	}

	result := deduplicateRecoverySummaries(thread)
	if len(result) != 3 {
		t.Fatalf("Expected 3 messages (single summary kept), got %d", len(result))
	}
}

func TestWriteSummarizedMessage_PreservesMentions(t *testing.T) {
	var sb strings.Builder
	msg := Message{
		Sender: "po",
		Text:   "I'm delegating this to @dev for implementation. @qa should review afterward. " + strings.Repeat("Details here. ", 50),
	}
	writeSummarizedMessage(&sb, msg)
	result := sb.String()

	if !strings.Contains(result, "[mentions: dev, qa]") {
		t.Fatalf("Expected mentions to be preserved, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Pairwise conversation threads
// ---------------------------------------------------------------------------

func TestMakeThreadKey(t *testing.T) {
	tests := []struct {
		a, b string
		want string
	}{
		{"po", "dev", "dev+po"},
		{"dev", "po", "dev+po"}, // reversed order → same key
		{"customer", "po", "customer+po"},
		{"po", "customer", "customer+po"},
		{"a", "a", "a+a"}, // same participant (degenerate case)
		{"architect", "dev", "architect+dev"},
		{"qa", "dev", "dev+qa"},
	}
	for _, tt := range tests {
		got := MakeThreadKey(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("MakeThreadKey(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestMakeThreadKey_Symmetric(t *testing.T) {
	// Verify symmetry: MakeThreadKey(a, b) == MakeThreadKey(b, a) for all pairs
	agents := []string{"po", "dev", "qa", "architect", "customer", "system"}
	for _, a := range agents {
		for _, b := range agents {
			if MakeThreadKey(a, b) != MakeThreadKey(b, a) {
				t.Errorf("MakeThreadKey not symmetric: MakeThreadKey(%q,%q)=%q != MakeThreadKey(%q,%q)=%q",
					a, b, MakeThreadKey(a, b), b, a, MakeThreadKey(b, a))
			}
		}
	}
}

func TestResolveMemoryKeys(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want []string
	}{
		{
			name: "new message with MemoryKeys",
			msg:  Message{MemoryKeys: []string{"po", "dev"}},
			want: []string{"po", "dev"},
		},
		{
			name: "single agent memory",
			msg:  Message{MemoryKeys: []string{"dev"}},
			want: []string{"dev"},
		},
		{
			name: "old message with ThreadKey only",
			msg:  Message{ThreadKey: "dev+po"},
			want: []string{"dev", "po"},
		},
		{
			name: "old message with customer ThreadKey",
			msg:  Message{ThreadKey: "customer+po"},
			want: []string{"customer", "po"},
		},
		{
			name: "MemoryKeys takes precedence over ThreadKey",
			msg:  Message{MemoryKeys: []string{"architect"}, ThreadKey: "dev+po"},
			want: []string{"architect"},
		},
		{
			name: "system message — no keys at all",
			msg:  Message{Sender: "system", Text: "Session started"},
			want: nil,
		},
		{
			name: "empty MemoryKeys with empty ThreadKey",
			msg:  Message{},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.msg.ResolveMemoryKeys()
			if tt.want == nil {
				if got != nil {
					t.Errorf("ResolveMemoryKeys() = %v, want nil", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ResolveMemoryKeys() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ResolveMemoryKeys()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestInAgentMemory(t *testing.T) {
	tests := []struct {
		name     string
		msg      Message
		agentKey string
		want     bool
	}{
		{
			name:     "agent in MemoryKeys",
			msg:      Message{MemoryKeys: []string{"po", "dev"}},
			agentKey: "dev",
			want:     true,
		},
		{
			name:     "agent not in MemoryKeys",
			msg:      Message{MemoryKeys: []string{"po", "dev"}},
			agentKey: "architect",
			want:     false,
		},
		{
			name:     "system message visible to all",
			msg:      Message{Sender: "system", Text: "Session started"},
			agentKey: "dev",
			want:     true,
		},
		{
			name:     "no keys at all visible to all",
			msg:      Message{},
			agentKey: "qa",
			want:     true,
		},
		{
			name:     "single agent memory — match",
			msg:      Message{MemoryKeys: []string{"dev"}},
			agentKey: "dev",
			want:     true,
		},
		{
			name:     "single agent memory — no match",
			msg:      Message{MemoryKeys: []string{"dev"}},
			agentKey: "po",
			want:     false,
		},
		{
			name:     "backward compat — agent in ThreadKey",
			msg:      Message{ThreadKey: "dev+po"},
			agentKey: "po",
			want:     true,
		},
		{
			name:     "backward compat — agent not in ThreadKey",
			msg:      Message{ThreadKey: "dev+po"},
			agentKey: "architect",
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.msg.InAgentMemory(tt.agentKey)
			if got != tt.want {
				t.Errorf("InAgentMemory(%q) = %v, want %v", tt.agentKey, got, tt.want)
			}
		})
	}
}

func TestGetAgentMemory_ChatChannel(t *testing.T) {
	ch := NewChatChannel("test-session")
	ctx := context.Background()

	// Simulate a typical session with per-agent memory keys:
	// - System messages (no keys) → visible to all
	// - Customer → po (only po remembers)
	// - po self-work (only po remembers)
	// - po → dev handoff (both po and dev remember)
	// - dev self-work (only dev remembers)
	// - dev → po handoff (both dev and po remember)
	// - po → customer (only po remembers)
	msgs := []Message{
		{Sender: "system", Text: "Session started"},                                         // global
		{Sender: "customer", Text: "Please build X", MemoryKeys: []string{"po"}},            // customer→po: po's memory
		{Sender: "po", Text: "Analyzing requirements", MemoryKeys: []string{"po"}},          // po self-work
		{Sender: "po", Text: "@dev implement X", MemoryKeys: []string{"po", "dev"}},         // po→dev handoff: shared
		{Sender: "dev", Text: "Reading codebase", MemoryKeys: []string{"dev"}},              // dev self-work
		{Sender: "dev", Text: "Working on it", MemoryKeys: []string{"dev"}},                 // dev self-work
		{Sender: "dev", Text: "Done, @po please review", MemoryKeys: []string{"dev", "po"}}, // dev→po handoff: shared
		{Sender: "po", Text: "@customer, X is ready", MemoryKeys: []string{"po"}},           // po→customer: po's memory
		{Sender: "system", Text: "Milestone: implementation complete"},                      // global
	}
	for _, msg := range msgs {
		if err := ch.PostMessage(ctx, msg); err != nil {
			t.Fatalf("PostMessage: %v", err)
		}
	}

	// Empty agentKey → returns all messages (same as GetThread)
	all, err := ch.GetAgentMemory(ctx, "")
	if err != nil {
		t.Fatalf("GetAgentMemory empty: %v", err)
	}
	if len(all) != 9 {
		t.Errorf("GetAgentMemory('') returned %d messages, want 9", len(all))
	}

	// po's memory: system(2) + customer→po(1) + po-self(1) + po→dev handoff(1) + dev→po handoff(1) + po→customer(1) = 7
	poMem, err := ch.GetAgentMemory(ctx, "po")
	if err != nil {
		t.Fatalf("GetAgentMemory po: %v", err)
	}
	expectedPO := 7
	if len(poMem) != expectedPO {
		t.Errorf("GetAgentMemory('po') returned %d messages, want %d", len(poMem), expectedPO)
		for i, m := range poMem {
			t.Logf("  [%d] sender=%s keys=%v text=%s", i, m.Sender, m.MemoryKeys, m.Text[:min(40, len(m.Text))])
		}
	}

	// dev's memory: system(2) + po→dev handoff(1) + dev-self(2) + dev→po handoff(1) = 6
	devMem, err := ch.GetAgentMemory(ctx, "dev")
	if err != nil {
		t.Fatalf("GetAgentMemory dev: %v", err)
	}
	expectedDev := 6
	if len(devMem) != expectedDev {
		t.Errorf("GetAgentMemory('dev') returned %d messages, want %d", len(devMem), expectedDev)
		for i, m := range devMem {
			t.Logf("  [%d] sender=%s keys=%v text=%s", i, m.Sender, m.MemoryKeys, m.Text[:min(40, len(m.Text))])
		}
	}

	// Verify dev does NOT see customer→po message or po self-work
	for _, m := range devMem {
		if m.Text == "Please build X" {
			t.Error("dev should NOT see customer→po message")
		}
		if m.Text == "Analyzing requirements" {
			t.Error("dev should NOT see po's self-work")
		}
		if m.Text == "@customer, X is ready" {
			t.Error("dev should NOT see po→customer message")
		}
	}

	// Verify handoff messages appear in both memories
	foundHandoffInPO := false
	foundHandoffInDev := false
	for _, m := range poMem {
		if m.Text == "@dev implement X" {
			foundHandoffInPO = true
		}
	}
	for _, m := range devMem {
		if m.Text == "@dev implement X" {
			foundHandoffInDev = true
		}
	}
	if !foundHandoffInPO {
		t.Error("po→dev handoff message should appear in po's memory")
	}
	if !foundHandoffInDev {
		t.Error("po→dev handoff message should appear in dev's memory")
	}

	// architect has no messages → only system messages
	archMem, err := ch.GetAgentMemory(ctx, "architect")
	if err != nil {
		t.Fatalf("GetAgentMemory architect: %v", err)
	}
	if len(archMem) != 2 {
		t.Errorf("GetAgentMemory('architect') returned %d messages, want 2 (system only)", len(archMem))
	}
}

func TestBuildThreadContext_AgentMemory(t *testing.T) {
	ch := NewChatChannel("test-session")
	ctx := context.Background()

	// Post messages with per-agent memory keys
	msgs := []Message{
		{Sender: "system", Text: "Session started"},
		{Sender: "customer", Text: "Build feature X", MemoryKeys: []string{"po"}},
		{Sender: "po", Text: "@dev, implement feature X", MemoryKeys: []string{"po", "dev"}},
		{Sender: "dev", Text: "I'll read the codebase first", MemoryKeys: []string{"dev"}},
		{Sender: "dev", Text: "Implementation complete, @po please review", MemoryKeys: []string{"dev", "po"}},
	}
	for _, msg := range msgs {
		if err := ch.PostMessage(ctx, msg); err != nil {
			t.Fatalf("PostMessage: %v", err)
		}
	}

	// Build context for dev — should see system, po→dev handoff, dev self-work, dev→po handoff
	result, err := BuildThreadContext(ctx, ch, "dev")
	if err != nil {
		t.Fatalf("BuildThreadContext: %v", err)
	}

	if !strings.Contains(result, "Session started") {
		t.Error("Expected system message in dev's memory context")
	}
	if !strings.Contains(result, "implement feature X") {
		t.Error("Expected po→dev handoff message in dev's memory context")
	}
	if !strings.Contains(result, "Implementation complete") {
		t.Error("Expected dev's final message in dev's memory context")
	}

	// Should NOT include customer→po message (not in dev's memory)
	if strings.Contains(result, "Build feature X") {
		t.Error("Customer→po message should NOT appear in dev's memory context")
	}

	// Build context for po — should see everything except dev's self-work
	poResult, err := BuildThreadContext(ctx, ch, "po")
	if err != nil {
		t.Fatalf("BuildThreadContext po: %v", err)
	}
	if !strings.Contains(poResult, "Build feature X") {
		t.Error("Expected customer→po message in po's memory context")
	}
	if !strings.Contains(poResult, "implement feature X") {
		t.Error("Expected po→dev handoff in po's memory context")
	}
	if !strings.Contains(poResult, "Implementation complete") {
		t.Error("Expected dev→po handoff in po's memory context")
	}

	// po should NOT see dev's self-work
	if strings.Contains(poResult, "I'll read the codebase first") {
		t.Error("Dev's self-work should NOT appear in po's memory context")
	}
}

func TestBuildThreadContext_BackwardCompatible(t *testing.T) {
	// Old messages with ThreadKey (pairwise model) should be correctly resolved
	// to per-agent memory via ResolveMemoryKeys.
	ch := NewChatChannel("test-session")
	ctx := context.Background()

	msgs := []Message{
		{Sender: "system", Text: "Session started"},
		{Sender: "customer", Text: "Old message from customer", ThreadKey: "customer+po"},
		{Sender: "po", Text: "Old po response", ThreadKey: "customer+po"},
		{Sender: "po", Text: "Old delegation to dev", ThreadKey: "dev+po"},
		{Sender: "dev", Text: "Old dev work", ThreadKey: "dev+po"},
	}
	for _, msg := range msgs {
		if err := ch.PostMessage(ctx, msg); err != nil {
			t.Fatalf("PostMessage: %v", err)
		}
	}

	// dev's memory via backward compat: system + dev+po thread messages
	devResult, err := BuildThreadContext(ctx, ch, "dev")
	if err != nil {
		t.Fatalf("BuildThreadContext dev: %v", err)
	}
	if !strings.Contains(devResult, "Session started") {
		t.Error("System message should be in dev's memory")
	}
	if !strings.Contains(devResult, "Old delegation to dev") {
		t.Error("po→dev message (ThreadKey=dev+po) should be in dev's memory")
	}
	if !strings.Contains(devResult, "Old dev work") {
		t.Error("dev's own message (ThreadKey=dev+po) should be in dev's memory")
	}

	// dev should NOT see customer+po thread messages
	if strings.Contains(devResult, "Old message from customer") {
		t.Error("customer→po message should NOT be in dev's memory")
	}
	if strings.Contains(devResult, "Old po response") {
		t.Error("po→customer response should NOT be in dev's memory")
	}

	// po sees everything (in both customer+po and dev+po threads)
	poResult, err := BuildThreadContext(ctx, ch, "po")
	if err != nil {
		t.Fatalf("BuildThreadContext po: %v", err)
	}
	if !strings.Contains(poResult, "Old message from customer") {
		t.Error("customer→po message should be in po's memory (backward compat)")
	}
	if !strings.Contains(poResult, "Old delegation to dev") {
		t.Error("po→dev message should be in po's memory (backward compat)")
	}
	if !strings.Contains(poResult, "Old dev work") {
		t.Error("dev's message on dev+po thread should be in po's memory (backward compat)")
	}
}

func TestBuildThreadContext_NoKeysMessages(t *testing.T) {
	// Messages with no MemoryKeys and no ThreadKey (truly old or system messages)
	// should be visible to ALL agents.
	ch := NewChatChannel("test-session")
	ctx := context.Background()

	msgs := []Message{
		{Sender: "customer", Text: "Very old message 1"},
		{Sender: "po", Text: "Very old message 2"},
		{Sender: "dev", Text: "Very old message 3"},
	}
	for _, msg := range msgs {
		if err := ch.PostMessage(ctx, msg); err != nil {
			t.Fatalf("PostMessage: %v", err)
		}
	}

	// All agents should see all messages (no memory scoping)
	for _, agent := range []string{"dev", "po", "architect", "qa"} {
		result, err := BuildThreadContext(ctx, ch, agent)
		if err != nil {
			t.Fatalf("BuildThreadContext %s: %v", agent, err)
		}
		if !strings.Contains(result, "Very old message 1") {
			t.Errorf("Agent %s: message 1 (no keys) should be visible", agent)
		}
		if !strings.Contains(result, "Very old message 2") {
			t.Errorf("Agent %s: message 2 (no keys) should be visible", agent)
		}
		if !strings.Contains(result, "Very old message 3") {
			t.Errorf("Agent %s: message 3 (no keys) should be visible", agent)
		}
	}
}

// --- Per-Session Workspace Tests ---

func TestResolveSessionWorkspaceDir(t *testing.T) {
	tests := []struct {
		name       string
		baseDir    string
		sessionID  string
		taskSlug   string
		wantSuffix string
	}{
		{
			name:       "task slug used as container name",
			baseDir:    "/home/user/.config/astonish/sessions",
			sessionID:  "abc12345-6789-0000-1111-222233334444",
			taskSlug:   "issue-6-payoff-chart",
			wantSuffix: "/workspaces/issue-6-payoff-chart",
		},
		{
			name:       "session ID fallback (first 8 chars)",
			baseDir:    "/home/user/.config/astonish/sessions",
			sessionID:  "abc12345-6789-0000-1111-222233334444",
			taskSlug:   "",
			wantSuffix: "/workspaces/abc12345",
		},
		{
			name:       "short session ID (less than 8 chars)",
			baseDir:    "/tmp/sess",
			sessionID:  "abc",
			taskSlug:   "",
			wantSuffix: "/workspaces/abc",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveSessionWorkspaceDir(tc.baseDir, tc.sessionID, tc.taskSlug)
			if !strings.HasSuffix(got, tc.wantSuffix) {
				t.Errorf("got %q, want suffix %q", got, tc.wantSuffix)
			}
			if !strings.HasPrefix(got, tc.baseDir) {
				t.Errorf("got %q, want prefix %q", got, tc.baseDir)
			}
		})
	}
}

func TestResolveProjectSource(t *testing.T) {
	t.Run("explicit project source returned as-is", func(t *testing.T) {
		plan := &FleetPlan{
			ProjectSource: &ProjectSourceConfig{
				Type: "git_repo",
				Repo: "owner/myrepo",
			},
			Artifacts: map[string]PlanArtifactConfig{
				"code": {Type: "git_repo", Repo: "owner/other-repo"},
			},
		}
		src := plan.ResolveProjectSource()
		if src == nil {
			t.Fatal("expected non-nil ProjectSource")
		}
		if src.Type != "git_repo" || src.Repo != "owner/myrepo" {
			t.Errorf("expected git_repo/owner/myrepo, got %s/%s", src.Type, src.Repo)
		}
	})

	t.Run("backward compat: derive from git_repo artifact", func(t *testing.T) {
		plan := &FleetPlan{
			Artifacts: map[string]PlanArtifactConfig{
				"code": {Type: "git_repo", Repo: "owner/legacy-repo"},
			},
		}
		src := plan.ResolveProjectSource()
		if src == nil {
			t.Fatal("expected non-nil ProjectSource from artifact")
		}
		if src.Type != "git_repo" || src.Repo != "owner/legacy-repo" {
			t.Errorf("expected git_repo/owner/legacy-repo, got %s/%s", src.Type, src.Repo)
		}
	})

	t.Run("backward compat: derive from local artifact", func(t *testing.T) {
		plan := &FleetPlan{
			Artifacts: map[string]PlanArtifactConfig{
				"code": {Type: "local", Path: "/home/user/myproject"},
			},
		}
		src := plan.ResolveProjectSource()
		if src == nil {
			t.Fatal("expected non-nil ProjectSource from local artifact")
		}
		if src.Type != "local" || src.Path != "/home/user/myproject" {
			t.Errorf("expected local//home/user/myproject, got %s/%s", src.Type, src.Path)
		}
	})

	t.Run("no artifacts returns nil", func(t *testing.T) {
		plan := &FleetPlan{}
		src := plan.ResolveProjectSource()
		if src != nil {
			t.Errorf("expected nil ProjectSource, got %+v", src)
		}
	})
}

func TestSetupSessionWorkspace_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "workspace")

	// No source: should create empty directory
	err := SetupSessionWorkspace(wsDir, nil, "")
	if err != nil {
		t.Fatalf("SetupSessionWorkspace with nil source: %v", err)
	}
	info, err := os.Stat(wsDir)
	if err != nil {
		t.Fatalf("workspace dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("workspace is not a directory")
	}
}

func TestSetupSessionWorkspace_AlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "existing")
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Write a marker file to verify the directory is NOT re-created
	marker := filepath.Join(wsDir, "marker.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}

	// Should return nil and leave the directory as-is
	err := SetupSessionWorkspace(wsDir, &ProjectSourceConfig{Type: "git_repo", Repo: "owner/repo"}, "")
	if err != nil {
		t.Fatalf("SetupSessionWorkspace for existing dir: %v", err)
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "keep" {
		t.Error("existing workspace was modified when it should have been left alone")
	}
}

func TestSetupSessionWorkspace_CopyLocal(t *testing.T) {
	// Create a source directory with some files
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(srcDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("deep"), 0644); err != nil {
		t.Fatal(err)
	}

	// Setup workspace via local copy
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "ws-copy")
	err := SetupSessionWorkspace(wsDir, &ProjectSourceConfig{Type: "local", Path: srcDir}, "")
	if err != nil {
		t.Fatalf("SetupSessionWorkspace local copy: %v", err)
	}

	// Verify files were copied
	data, err := os.ReadFile(filepath.Join(wsDir, "hello.txt"))
	if err != nil || string(data) != "world" {
		t.Error("hello.txt not copied correctly")
	}
	data, err = os.ReadFile(filepath.Join(wsDir, "sub", "nested.txt"))
	if err != nil || string(data) != "deep" {
		t.Error("sub/nested.txt not copied correctly")
	}
}

func TestSetupSessionWorkspace_GitCloneLocal(t *testing.T) {
	// Create a base git repo with some content
	baseDir := t.TempDir()
	cmds := [][]string{
		{"git", "init", baseDir},
		{"git", "-C", baseDir, "config", "user.email", "test@test.com"},
		{"git", "-C", baseDir, "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(baseDir, "README.md"), []byte("# Test"), 0644); err != nil {
		t.Fatal(err)
	}
	addCmd := exec.Command("git", "-C", baseDir, "add", ".")
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	commitCmd := exec.Command("git", "-C", baseDir, "commit", "-m", "init")
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	// Setup session workspace via --local clone from the base
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "session-ws")
	err := SetupSessionWorkspace(wsDir, &ProjectSourceConfig{Type: "git_repo", Repo: "owner/repo"}, baseDir)
	if err != nil {
		t.Fatalf("SetupSessionWorkspace with --local: %v", err)
	}

	// Verify the clone has the file
	data, err := os.ReadFile(filepath.Join(wsDir, "README.md"))
	if err != nil || string(data) != "# Test" {
		t.Error("README.md not present in --local clone")
	}

	// Verify it's a git repo
	if !isGitRepo(wsDir) {
		t.Error("session workspace should be a git repo")
	}
}

func TestSetupSessionWorkspace_GitCloneLocalFallsBackToRemote(t *testing.T) {
	// When baseDir doesn't exist, it should attempt remote clone.
	// We can't test a real remote clone, but we can verify the error mentions
	// the remote URL (proving it fell through to the remote path).
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "session-ws")
	err := SetupSessionWorkspace(wsDir, &ProjectSourceConfig{
		Type: "git_repo",
		Repo: "owner/nonexistent-test-repo-12345",
	}, "/nonexistent/base/dir")

	// Should fail because the remote repo doesn't exist, but the error should
	// mention the remote URL (not --local)
	if err == nil {
		t.Fatal("expected error when both --local and remote fail")
	}
	if !strings.Contains(err.Error(), "git clone") {
		t.Errorf("expected error to mention git clone, got: %v", err)
	}
}

func TestSetupSessionWorkspace_LocalGitSource(t *testing.T) {
	// Local source that IS a git repo should use --local clone
	srcDir := t.TempDir()
	cmds := [][]string{
		{"git", "init", srcDir},
		{"git", "-C", srcDir, "config", "user.email", "test@test.com"},
		{"git", "-C", srcDir, "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(srcDir, "app.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	addCmd := exec.Command("git", "-C", srcDir, "add", ".")
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	commitCmd := exec.Command("git", "-C", srcDir, "commit", "-m", "init")
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "ws-local-git")
	err := SetupSessionWorkspace(wsDir, &ProjectSourceConfig{Type: "local", Path: srcDir}, "")
	if err != nil {
		t.Fatalf("SetupSessionWorkspace local git source: %v", err)
	}

	// Verify the file was cloned
	data, err := os.ReadFile(filepath.Join(wsDir, "app.go"))
	if err != nil || string(data) != "package main" {
		t.Error("app.go not present in local git clone")
	}

	// Verify it's a git repo (--local was used)
	if !isGitRepo(wsDir) {
		t.Error("session workspace from local git source should be a git repo")
	}
}

func TestCleanupSessionWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "workspaces", "workspace-to-delete")
	if err := os.MkdirAll(filepath.Join(wsDir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "file.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	err := CleanupSessionWorkspace(wsDir)
	if err != nil {
		t.Fatalf("CleanupSessionWorkspace: %v", err)
	}
	if _, err := os.Stat(wsDir); !os.IsNotExist(err) {
		t.Error("workspace directory should have been removed")
	}
}

func TestCleanupSessionWorkspace_EmptyPath(t *testing.T) {
	// Should be a no-op
	err := CleanupSessionWorkspace("")
	if err != nil {
		t.Fatalf("CleanupSessionWorkspace with empty path: %v", err)
	}
}

func TestCleanupSessionWorkspace_NonExistent(t *testing.T) {
	// Should be a no-op
	err := CleanupSessionWorkspace("/nonexistent/path/that/doesnt/exist")
	if err != nil {
		t.Fatalf("CleanupSessionWorkspace with nonexistent path: %v", err)
	}
}

func TestCleanupSessionWorkspace_SafetyGuard(t *testing.T) {
	// Paths that look like container-internal paths (not under workspaces/)
	// should be refused even if they exist on the host.
	tmpDir := t.TempDir()
	dangerousDir := filepath.Join(tmpDir, "astonish")
	if err := os.MkdirAll(dangerousDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dangerousDir, "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	err := CleanupSessionWorkspace(dangerousDir)
	if err != nil {
		t.Fatalf("CleanupSessionWorkspace should not error on blocked path: %v", err)
	}
	// The directory should still exist — safety guard blocked deletion
	if _, err := os.Stat(dangerousDir); os.IsNotExist(err) {
		t.Error("safety guard should have prevented deletion of non-workspace path")
	}
}

func TestBuildAgentPromptWithWorkspaceDir(t *testing.T) {
	agentCfg := FleetAgentConfig{
		Name:     "Dev",
		Identity: "You are a developer.",
		Tools:    ToolsConfig{All: true},
	}

	fleetCfg := &FleetConfig{
		Agents: map[string]FleetAgentConfig{
			"dev": agentCfg,
		},
	}

	plan := &FleetPlan{
		Channel: PlanChannelConfig{Type: "chat"},
		Artifacts: map[string]PlanArtifactConfig{
			"code": {Type: "local", Path: "/old/shared/path"},
		},
	}

	// When workspaceDir is provided, it should appear in the prompt
	// instead of the plan's ResolveWorkspaceDir result
	prompt := BuildAgentPrompt(agentCfg, fleetCfg, "dev", nil, "", "", "/tmp/per-session-workspace", plan)

	if !strings.Contains(prompt, "/tmp/per-session-workspace") {
		t.Error("expected prompt to contain the per-session workspace dir")
	}
}

func TestProjectSourceConfigYAML(t *testing.T) {
	plan := &FleetPlan{
		Name: "Test Plan",
		Key:  "test",
		FleetConfig: FleetConfig{
			Agents: map[string]FleetAgentConfig{
				"dev": {Name: "Dev", Identity: "You are a dev.", Tools: ToolsConfig{All: true}},
			},
		},
		Channel: PlanChannelConfig{Type: "chat"},
		ProjectSource: &ProjectSourceConfig{
			Type: "git_repo",
			Repo: "owner/myrepo",
		},
	}

	// Marshal to YAML
	data, err := yaml.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	yamlStr := string(data)

	if !strings.Contains(yamlStr, "project_source:") {
		t.Error("YAML should contain project_source key")
	}
	if !strings.Contains(yamlStr, "type: git_repo") {
		t.Error("YAML should contain type: git_repo")
	}
	if !strings.Contains(yamlStr, "repo: owner/myrepo") {
		t.Error("YAML should contain repo: owner/myrepo")
	}

	// Unmarshal back
	var parsed FleetPlan
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.ProjectSource == nil {
		t.Fatal("parsed ProjectSource is nil")
	}
	if parsed.ProjectSource.Type != "git_repo" || parsed.ProjectSource.Repo != "owner/myrepo" {
		t.Errorf("round-trip failed: got %+v", parsed.ProjectSource)
	}
}
