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
agents:
  dev:
    persona: developer
    tools:
      - read_file
      - write_file
    behaviors: |
      When receiving a task, implement it.
  qa:
    persona: qa_engineer
    tools: true
    behaviors: |
      When receiving code, test it.
suggested_flow:
  phases:
    - name: implementation
      agent: dev
    - name: testing
      agent: qa
  reviews:
    testing: [implementation]
settings:
  max_reviews_per_phase: 3
`

const delegateFleetYAML = `name: delegate-fleet
description: Fleet with delegate
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
	if len(f.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(f.Agents))
	}

	// Check tools parsing
	dev := f.Agents["dev"]
	if dev.Tools.All {
		t.Error("expected dev tools.All to be false")
	}
	if len(dev.Tools.Names) != 2 {
		t.Errorf("expected 2 tool names for dev, got %d", len(dev.Tools.Names))
	}

	qa := f.Agents["qa"]
	if !qa.Tools.All {
		t.Error("expected qa tools.All to be true")
	}

	// Check suggested flow
	if f.SuggestedFlow == nil {
		t.Fatal("expected suggested_flow to be non-nil")
		return
	}
	if len(f.SuggestedFlow.Phases) != 2 {
		t.Errorf("expected 2 phases, got %d", len(f.SuggestedFlow.Phases))
	}

	// Check settings
	if f.Settings.GetMaxReviewsPerPhase() != 3 {
		t.Errorf("expected max_reviews_per_phase 3, got %d", f.Settings.GetMaxReviewsPerPhase())
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

func TestFleetValidate_LeaderMissingPersona(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Leader: &FleetLeaderConfig{
			Persona:   "",
			Behaviors: "lead the team",
		},
		Agents: map[string]FleetAgentConfig{
			"dev": {Persona: "developer", Tools: ToolsConfig{All: true}, Behaviors: "do stuff"},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for leader with missing persona")
	}
}

func TestFleetValidate_LeaderMissingBehaviors(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Leader: &FleetLeaderConfig{
			Persona:   "project_lead",
			Behaviors: "",
		},
		Agents: map[string]FleetAgentConfig{
			"dev": {Persona: "developer", Tools: ToolsConfig{All: true}, Behaviors: "do stuff"},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for leader with missing behaviors")
	}
}

func TestFleetValidate_BadPhaseRef(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"dev": {Persona: "developer", Tools: ToolsConfig{All: true}, Behaviors: "do stuff"},
		},
		SuggestedFlow: &FleetSuggestedFlow{
			Phases: []FleetPhase{
				{Name: "implementation", Agent: "nonexistent"},
			},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for unknown agent reference in phase")
	}
}

func TestFleetValidate_BadReviewRef(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"dev": {Persona: "developer", Tools: ToolsConfig{All: true}, Behaviors: "do stuff"},
		},
		SuggestedFlow: &FleetSuggestedFlow{
			Phases: []FleetPhase{
				{Name: "implementation", Agent: "dev"},
			},
			Reviews: map[string][]string{
				"nonexistent": {"implementation"},
			},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for unknown reviewer phase")
	}
}

func TestFleetValidate_ConversationPhase(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"architect": {Persona: "architect", Tools: ToolsConfig{All: true}, Behaviors: "design"},
			"po":        {Persona: "po", Tools: ToolsConfig{All: true}, Behaviors: "requirements"},
		},
		SuggestedFlow: &FleetSuggestedFlow{
			Phases: []FleetPhase{
				{Name: "design", Primary: "architect", Reviewers: []string{"po"}},
			},
		},
	}
	if err := f.Validate(); err != nil {
		t.Errorf("expected valid conversation phase, got error: %v", err)
	}
}

func TestFleetValidate_ConversationPhase_BadPrimary(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"po": {Persona: "po", Tools: ToolsConfig{All: true}, Behaviors: "requirements"},
		},
		SuggestedFlow: &FleetSuggestedFlow{
			Phases: []FleetPhase{
				{Name: "design", Primary: "nonexistent", Reviewers: []string{"po"}},
			},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for unknown primary agent in conversation phase")
	}
}

func TestFleetValidate_ConversationPhase_NoReviewers(t *testing.T) {
	// Primary without reviewers is now a valid single-agent phase
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"architect": {Persona: "architect", Tools: ToolsConfig{All: true}, Behaviors: "design"},
		},
		SuggestedFlow: &FleetSuggestedFlow{
			Phases: []FleetPhase{
				{Name: "design", Primary: "architect"},
			},
		},
	}
	if err := f.Validate(); err != nil {
		t.Errorf("expected valid single-agent phase with primary, got error: %v", err)
	}
}

func TestFleetValidate_ConversationPhase_BadReviewer(t *testing.T) {
	f := &FleetConfig{
		Name: "test",
		Agents: map[string]FleetAgentConfig{
			"architect": {Persona: "architect", Tools: ToolsConfig{All: true}, Behaviors: "design"},
		},
		SuggestedFlow: &FleetSuggestedFlow{
			Phases: []FleetPhase{
				{Name: "design", Primary: "architect", Reviewers: []string{"nonexistent"}},
			},
		},
	}
	if err := f.Validate(); err == nil {
		t.Error("expected error for unknown reviewer agent in conversation phase")
	}
}

func TestFleetPhase_IsConversation(t *testing.T) {
	// Single-agent via Agent field (backward compat)
	singleAgent := FleetPhase{Name: "impl", Agent: "dev"}
	if singleAgent.IsConversation() {
		t.Error("expected single-agent phase (Agent field) not to be a conversation")
	}
	singleAgent.NormalizePrimary()
	if singleAgent.GetPrimaryAgent() != "dev" {
		t.Errorf("expected GetPrimaryAgent() = %q, got %q", "dev", singleAgent.GetPrimaryAgent())
	}

	// Single-agent via Primary field (no reviewers)
	singlePrimary := FleetPhase{Name: "impl", Primary: "dev"}
	if singlePrimary.IsConversation() {
		t.Error("expected single-agent phase (Primary, no reviewers) not to be a conversation")
	}
	if singlePrimary.GetPrimaryAgent() != "dev" {
		t.Errorf("expected GetPrimaryAgent() = %q, got %q", "dev", singlePrimary.GetPrimaryAgent())
	}

	// Conversation phase (Primary + Reviewers)
	conv := FleetPhase{Name: "design", Primary: "architect", Reviewers: []string{"po"}}
	if !conv.IsConversation() {
		t.Error("expected conversation phase to be identified as conversation")
	}
	if conv.GetPrimaryAgent() != "architect" {
		t.Errorf("expected GetPrimaryAgent() = %q, got %q", "architect", conv.GetPrimaryAgent())
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
	if s.GetMaxReviewsPerPhase() != 2 {
		t.Errorf("expected default max_reviews_per_phase 2, got %d", s.GetMaxReviewsPerPhase())
	}
}

func TestRegistry_Basic(t *testing.T) {
	fleetDir := t.TempDir()
	personaDir := t.TempDir()

	// Create persona files
	devPersona := `name: Developer
description: Writes code
prompt: You are a developer.
`
	qaPersona := `name: QA Engineer
description: Tests code
prompt: You are a QA engineer.
`
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

func TestBuildSubAgentPrompt(t *testing.T) {
	p := &persona.PersonaConfig{
		Name:   "Developer",
		Prompt: "You are a developer.",
	}

	agent := FleetAgentConfig{
		Persona:   "developer",
		Behaviors: "When receiving a task, implement it carefully.",
		Delegate: &DelegateConfig{
			Tool:        "opencode",
			Params:      map[string]any{"agent": "build", "format": "json"},
			Description: "OpenCode is a coding agent.",
		},
	}

	prompt := BuildSubAgentPrompt(p, agent)

	if !strings.Contains(prompt, "You are a developer.") {
		t.Error("expected prompt to contain persona prompt")
	}
	if !strings.Contains(prompt, "Team Behaviors") {
		t.Error("expected prompt to contain behaviors section")
	}
	if !strings.Contains(prompt, "implement it carefully") {
		t.Error("expected prompt to contain behavior content")
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

func TestBuildSubAgentPrompt_NoDelegate(t *testing.T) {
	p := &persona.PersonaConfig{
		Name:   "QA",
		Prompt: "You are a QA engineer.",
	}

	agent := FleetAgentConfig{
		Persona:   "qa_engineer",
		Tools:     ToolsConfig{All: true},
		Behaviors: "Test everything.",
	}

	prompt := BuildSubAgentPrompt(p, agent)

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

func TestBuildSystemPromptSection_WithLeader(t *testing.T) {
	fleets := []FleetSummary{
		{Key: "dev", Name: "software-dev", Description: "Dev team", AgentCount: 2},
	}

	fleetConfigs := map[string]*FleetConfig{
		"dev": {
			Name:        "software-dev",
			Description: "Dev team",
			Leader: &FleetLeaderConfig{
				Persona:   "project_lead",
				Behaviors: "Delegate ALL work via run_fleet_phase.",
			},
			Agents: map[string]FleetAgentConfig{
				"developer": {Persona: "developer", Tools: ToolsConfig{All: true}, Behaviors: "code"},
				"qa":        {Persona: "qa_engineer", Delegate: &DelegateConfig{Tool: "opencode"}, Behaviors: "test"},
			},
			SuggestedFlow: &FleetSuggestedFlow{
				Phases: []FleetPhase{
					{Name: "implementation", Agent: "developer"},
					{Name: "testing", Agent: "qa"},
				},
			},
		},
	}

	personaConfigs := map[string]*persona.PersonaConfig{
		"project_lead": {
			Name:   "Project Lead",
			Prompt: "You are a Project Lead. You delegate all work.",
		},
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

	// With the new approach, the system prompt uses lightweight fleet listing
	// (leader persona is only injected into the orchestrator sub-agent, not here)
	if !strings.Contains(result, "Fleet-Based Development") {
		t.Error("expected fleet guidance section header")
	}
	if !strings.Contains(result, "fleet_plan") {
		t.Error("expected fleet_plan tool reference")
	}
	if !strings.Contains(result, "fleet_execute") {
		t.Error("expected fleet_execute tool reference")
	}
	if !strings.Contains(result, "software-dev") {
		t.Error("expected fleet name in listing")
	}
	if !strings.Contains(result, "implementation") {
		t.Error("expected workflow phase names")
	}
}

func TestBuildSystemPromptSection_NoLeader(t *testing.T) {
	fleets := []FleetSummary{
		{Key: "dev", Name: "software-dev", Description: "Dev team", AgentCount: 2},
	}

	fleetConfigs := map[string]*FleetConfig{
		"dev": {
			Name:        "software-dev",
			Description: "Dev team",
			Agents: map[string]FleetAgentConfig{
				"developer": {Persona: "developer", Tools: ToolsConfig{All: true}, Behaviors: "code"},
				"qa":        {Persona: "qa_engineer", Tools: ToolsConfig{All: true}, Behaviors: "test"},
			},
			SuggestedFlow: &FleetSuggestedFlow{
				Phases: []FleetPhase{
					{Name: "implementation", Agent: "developer"},
					{Name: "testing", Agent: "qa"},
				},
			},
		},
	}

	result := BuildSystemPromptSection(
		fleets,
		func(key string) (*FleetConfig, bool) {
			f, ok := fleetConfigs[key]
			return f, ok
		},
		nil,
	)

	// Both leader and non-leader fleets use the same lightweight listing now
	if !strings.Contains(result, "Fleet-Based Development") {
		t.Error("expected fleet guidance section header")
	}
	if !strings.Contains(result, "software-dev") {
		t.Error("expected fleet name")
	}
	if !strings.Contains(result, "implementation → testing") {
		t.Error("expected workflow phases")
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
