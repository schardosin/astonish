package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	prompt := BuildAgentPrompt(agentCfg, fleetCfg, "dev", nil, "", "")

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

	prompt := BuildAgentPrompt(agentCfg, fleetCfg, "qa", nil, "", "")

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

	prompt := BuildAgentPrompt(agentCfg, fleetCfg, "dev", nil, "", "issue-6-payoff-chart", plan)

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

	prompt := BuildAgentPrompt(agentCfg, fleetCfg, "dev", nil, "", "")

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

	prompt := BuildAgentPrompt(agentCfg, fleetCfg, "po", nil, "", "issue-5-add-feature", plan)

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
