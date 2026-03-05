package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schardosin/astonish/pkg/persona"
)

const validFleetYAML = `name: test-fleet
description: A test fleet
communication:
  flow:
    - role: po
      talks_to: [human, dev]
      entry_point: true
    - role: dev
      talks_to: [po, qa]
    - role: qa
      talks_to: [dev, po]
agents:
  po:
    persona: product_owner
    tools:
      - read_file
      - write_file
    behaviors: |
      When receiving a request, gather requirements.
  dev:
    persona: developer
    tools: true
    behaviors: |
      When receiving a task, implement it.
  qa:
    persona: qa_engineer
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
      talks_to: [human]
      entry_point: true
agents:
  dev:
    persona: developer
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
			"dev": {Persona: "developer", Tools: ToolsConfig{All: true}, Behaviors: "do stuff"},
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

func TestFleetValidate_MissingPersona(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"dev": {Persona: "", Tools: ToolsConfig{All: true}, Behaviors: "do stuff"},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for missing persona")
	}
}

func TestFleetValidate_MissingBehaviors(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"dev": {Persona: "developer", Tools: ToolsConfig{All: true}, Behaviors: ""},
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
			"dev": {Persona: "developer", Behaviors: "do stuff"},
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
				Persona:   "developer",
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
			"dev": {Persona: "developer", Tools: ToolsConfig{All: true}, Behaviors: "do stuff", Mode: "invalid"},
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
			"dev": {Persona: "developer", Tools: ToolsConfig{All: true}, Behaviors: "do stuff"},
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
			"dev": {Persona: "developer", Tools: ToolsConfig{All: true}, Behaviors: "do stuff"},
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
			"dev": {Persona: "developer", Tools: ToolsConfig{All: true}, Behaviors: "do stuff"},
		},
		Communication: &CommunicationConfig{
			Flow: []CommunicationNode{
				{Role: "dev", TalksTo: []string{"human"}},
			},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for no entry point in communication")
	}
}

func TestFleetValidate_CommunicationHumanAllowed(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"po": {Persona: "product_owner", Tools: ToolsConfig{All: true}, Behaviors: "requirements"},
		},
		Communication: &CommunicationConfig{
			Flow: []CommunicationNode{
				{Role: "po", TalksTo: []string{"human"}, EntryPoint: true},
			},
		},
	}
	if err := f.Validate(); err != nil {
		t.Errorf("expected valid communication with human target, got error: %v", err)
	}
}

func TestCommunicationHelpers(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"po":  {Persona: "po", Tools: ToolsConfig{All: true}, Behaviors: "requirements"},
			"dev": {Persona: "dev", Tools: ToolsConfig{All: true}, Behaviors: "code"},
			"qa":  {Persona: "qa", Tools: ToolsConfig{All: true}, Behaviors: "test"},
		},
		Communication: &CommunicationConfig{
			Flow: []CommunicationNode{
				{Role: "po", TalksTo: []string{"human", "dev"}, EntryPoint: true},
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
	if !f.CanTalkTo("po", "human") {
		t.Error("expected po to be able to talk to human")
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
	if f.CanTalkTo("dev", "human") {
		t.Error("expected dev NOT to be able to talk to human")
	}

	// CanTalkToHuman
	if !f.CanTalkToHuman("po") {
		t.Error("expected po to be able to talk to human")
	}
	if f.CanTalkToHuman("dev") {
		t.Error("expected dev NOT to be able to talk to human")
	}

	// GetTalksTo
	poTargets := f.GetTalksTo("po")
	if len(poTargets) != 2 || poTargets[0] != "human" || poTargets[1] != "dev" {
		t.Errorf("GetTalksTo(po) = %v, want [human dev]", poTargets)
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

func TestFleetValidatePersonaRefs(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"dev": {Persona: "developer", Tools: ToolsConfig{All: true}, Behaviors: "do stuff"},
			"qa":  {Persona: "missing_persona", Tools: ToolsConfig{All: true}, Behaviors: "test stuff"},
		},
	}

	known := map[string]bool{"developer": true}
	err := f.ValidatePersonaRefs(func(key string) bool {
		return known[key]
	})
	if err == nil {
		t.Error("expected error for missing persona reference")
	}
	if !strings.Contains(err.Error(), "missing_persona") {
		t.Errorf("expected error to mention missing persona, got: %v", err)
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
	personaDir := t.TempDir()

	// Create persona files
	poPersona := `name: Product Owner
description: Gathers requirements
prompt: You are a product owner.
`
	devPersona := `name: Developer
description: Writes code
prompt: You are a developer.
`
	qaPersona := `name: QA Engineer
description: Tests code
prompt: You are a QA engineer.
`
	if err := os.WriteFile(filepath.Join(personaDir, "product_owner.yaml"), []byte(poPersona), 0644); err != nil {
		t.Fatal(err)
		return
	}
	if err := os.WriteFile(filepath.Join(personaDir, "developer.yaml"), []byte(devPersona), 0644); err != nil {
		t.Fatal(err)
		return
	}
	if err := os.WriteFile(filepath.Join(personaDir, "qa_engineer.yaml"), []byte(qaPersona), 0644); err != nil {
		t.Fatal(err)
		return
	}

	// Create fleet file
	if err := os.WriteFile(filepath.Join(fleetDir, "test.yaml"), []byte(validFleetYAML), 0644); err != nil {
		t.Fatal(err)
		return
	}

	// Create registries
	personaReg, err := persona.NewRegistry(personaDir)
	if err != nil {
		t.Fatal(err)
		return
	}

	fleetReg, err := NewRegistry(fleetDir, personaReg)
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

	// Test persona resolution
	p, err := fleetReg.ResolvePersona("developer")
	if err != nil {
		t.Fatal(err)
		return
	}
	if p.Name != "Developer" {
		t.Errorf("expected resolved persona name %q, got %q", "Developer", p.Name)
	}

	// Validate persona references
	results := fleetReg.ValidateAll()
	for key, valErr := range results {
		if valErr != nil {
			t.Errorf("fleet %q validation failed: %v", key, valErr)
		}
	}
}

func TestRegistry_SaveAndDelete(t *testing.T) {
	fleetDir := t.TempDir()

	reg, err := NewRegistry(fleetDir, nil)
	if err != nil {
		t.Fatal(err)
		return
	}

	fleet := &FleetConfig{
		Name: "saved-fleet",
		Agents: map[string]FleetAgentConfig{
			"dev": {
				Persona:   "developer",
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
	p := &persona.PersonaConfig{
		Name:   "Developer",
		Prompt: "You are a developer.",
	}

	agentCfg := FleetAgentConfig{
		Persona:   "developer",
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
				{Role: "po", TalksTo: []string{"human", "dev"}, EntryPoint: true},
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

	prompt := BuildAgentPrompt(p, agentCfg, fleetCfg, "dev")

	if !strings.Contains(prompt, "You are a developer.") {
		t.Error("expected prompt to contain persona prompt")
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
	p := &persona.PersonaConfig{
		Name:   "QA",
		Prompt: "You are a QA engineer.",
	}

	agentCfg := FleetAgentConfig{
		Persona:   "qa_engineer",
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
			"dev": {Persona: "dev", Tools: ToolsConfig{All: true}, Behaviors: "code"},
		},
	}

	prompt := BuildAgentPrompt(p, agentCfg, fleetCfg, "qa")

	if !strings.Contains(prompt, "You are a QA engineer.") {
		t.Error("expected prompt to contain persona prompt")
	}
	if strings.Contains(prompt, "Delegate Tool") {
		t.Error("expected prompt to NOT contain delegate execution section")
	}
	if strings.Contains(prompt, "`opencode` tool") {
		t.Error("expected prompt to NOT contain opencode tool instructions")
	}
}

func TestBuildSystemPromptSection_Empty(t *testing.T) {
	result := BuildSystemPromptSection(nil, nil, nil)
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
					{Role: "po", TalksTo: []string{"human", "dev"}, EntryPoint: true},
					{Role: "dev", TalksTo: []string{"po", "qa"}},
					{Role: "qa", TalksTo: []string{"dev", "po"}},
				},
			},
			Agents: map[string]FleetAgentConfig{
				"po":  {Persona: "product_owner", Tools: ToolsConfig{All: true}, Behaviors: "requirements"},
				"dev": {Persona: "developer", Delegate: &DelegateConfig{Tool: "opencode"}, Behaviors: "code"},
				"qa":  {Persona: "qa_engineer", Tools: ToolsConfig{All: true}, Behaviors: "test"},
			},
		},
	}

	personaConfigs := map[string]*persona.PersonaConfig{
		"product_owner": {Name: "Product Owner", Prompt: "You are a PO."},
	}

	result := BuildSystemPromptSection(
		fleets,
		func(key string) (*FleetConfig, bool) {
			f, ok := fleetConfigs[key]
			return f, ok
		},
		func(key string) (*persona.PersonaConfig, bool) {
			p, ok := personaConfigs[key]
			return p, ok
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
						"dev": {Persona: "developer", Tools: ToolsConfig{All: true}, Behaviors: "code"},
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
							Persona:   "developer",
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
							Persona:   "developer",
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
							Persona:   "developer",
							Delegate:  &DelegateConfig{Tool: "opencode", Env: []string{"BIFROST_API_KEY", "CUSTOM_KEY"}},
							Behaviors: "code",
						},
						"qa": {
							Persona:   "qa",
							Delegate:  &DelegateConfig{Tool: "opencode", Env: []string{"BIFROST_API_KEY"}},
							Behaviors: "test",
						},
					},
				},
				"fleet2": {
					Name: "fleet2",
					Agents: map[string]FleetAgentConfig{
						"arch": {
							Persona:   "architect",
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
							Persona:   "developer",
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
