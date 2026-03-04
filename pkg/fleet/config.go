package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// FleetConfig represents a fleet definition loaded from YAML.
// Fleets compose personas into teams with behavioral rules, tool/delegate
// assignments, and a suggested workflow.
type FleetConfig struct {
	Name          string                      `yaml:"name" json:"name"`
	Description   string                      `yaml:"description,omitempty" json:"description,omitempty"`
	Leader        *FleetLeaderConfig          `yaml:"leader,omitempty" json:"leader,omitempty"`
	Agents        map[string]FleetAgentConfig `yaml:"agents" json:"agents"`
	SuggestedFlow *FleetSuggestedFlow         `yaml:"suggested_flow,omitempty" json:"suggested_flow,omitempty"`
	Settings      FleetSettings               `yaml:"settings,omitempty" json:"settings,omitempty"`
}

// FleetLeaderConfig defines the orchestrator for the fleet.
// The leader persona is injected into the main ChatAgent's system prompt
// when this fleet is active. The leader manages the team by delegating
// work via run_fleet_phase and reviewing results.
type FleetLeaderConfig struct {
	Persona   string `yaml:"persona" json:"persona"`
	Behaviors string `yaml:"behaviors" json:"behaviors"`
}

// FleetAgentConfig defines a single agent slot within a fleet.
// It references a persona and adds fleet-specific tool access, delegate
// configuration, and behavioral rules.
type FleetAgentConfig struct {
	Persona   string          `yaml:"persona" json:"persona"`
	Tools     ToolsConfig     `yaml:"tools,omitempty" json:"tools,omitempty"`
	Delegate  *DelegateConfig `yaml:"delegate,omitempty" json:"delegate,omitempty"`
	Behaviors string          `yaml:"behaviors" json:"behaviors"`
}

// ToolsConfig handles the polymorphic tools field which can be either
// a boolean (true = all tools) or a list of tool names.
type ToolsConfig struct {
	All   bool     // true means all available tools
	Names []string // specific tool names when All is false
}

// UnmarshalYAML implements custom YAML unmarshalling for the polymorphic tools field.
func (tc *ToolsConfig) UnmarshalYAML(value *yaml.Node) error {
	// Try boolean first
	if value.Kind == yaml.ScalarNode {
		var b bool
		if err := value.Decode(&b); err == nil {
			tc.All = b
			return nil
		}
	}

	// Try string list
	var names []string
	if err := value.Decode(&names); err == nil {
		tc.Names = names
		return nil
	}

	return fmt.Errorf("tools must be a boolean or a list of strings")
}

// MarshalYAML implements custom YAML marshalling.
func (tc ToolsConfig) MarshalYAML() (interface{}, error) {
	if tc.All {
		return true, nil
	}
	if len(tc.Names) > 0 {
		return tc.Names, nil
	}
	return nil, nil
}

// IsEmpty returns true if no tools are configured.
func (tc *ToolsConfig) IsEmpty() bool {
	return !tc.All && len(tc.Names) == 0
}

// DelegateConfig configures external tool delegation for an agent.
type DelegateConfig struct {
	Tool        string         `yaml:"tool" json:"tool"`
	Params      map[string]any `yaml:"params,omitempty" json:"params,omitempty"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Env         []string       `yaml:"env,omitempty" json:"env,omitempty"` // Environment variable names to forward to the delegate
}

// FleetSuggestedFlow defines a template workflow.
type FleetSuggestedFlow struct {
	Phases  []FleetPhase        `yaml:"phases" json:"phases"`
	Reviews map[string][]string `yaml:"reviews,omitempty" json:"reviews,omitempty"`
}

// FleetPhase is a single step in the suggested workflow.
// Every phase has a Primary agent. If Reviewers are present, it becomes a
// multi-agent conversation phase; otherwise it runs as a single-agent phase.
//
// The Agent field is a deprecated alias for Primary (for backward compatibility
// with older YAML files). On load, Agent is normalized to Primary.
type FleetPhase struct {
	Name      string   `yaml:"name" json:"name"`
	Agent     string   `yaml:"agent,omitempty" json:"agent,omitempty"`         // Deprecated: use Primary instead
	Primary   string   `yaml:"primary,omitempty" json:"primary,omitempty"`     // Agent that executes/produces deliverables
	Reviewers []string `yaml:"reviewers,omitempty" json:"reviewers,omitempty"` // Optional: agents that review/discuss (enables conversation mode)
}

// NormalizePrimary copies Agent to Primary if Primary is empty (backward compat).
func (p *FleetPhase) NormalizePrimary() {
	if p.Primary == "" && p.Agent != "" {
		p.Primary = p.Agent
	}
}

// IsConversation returns true if this phase uses the multi-agent conversation model.
func (p *FleetPhase) IsConversation() bool {
	return len(p.Reviewers) > 0
}

// GetPrimaryAgent returns the primary agent key for this phase.
func (p *FleetPhase) GetPrimaryAgent() string {
	if p.Primary != "" {
		return p.Primary
	}
	return p.Agent // backward compat fallback
}

// FleetSettings holds fleet-level configuration.
type FleetSettings struct {
	MaxReviewsPerPhase int `yaml:"max_reviews_per_phase,omitempty" json:"max_reviews_per_phase,omitempty"`
}

// GetMaxReviewsPerPhase returns the configured max or a default of 2.
func (s *FleetSettings) GetMaxReviewsPerPhase() int {
	if s.MaxReviewsPerPhase <= 0 {
		return 2
	}
	return s.MaxReviewsPerPhase
}

// Validate checks that the fleet config is internally consistent.
// It does not validate persona references (that requires the persona registry).
func (f *FleetConfig) Validate() error {
	if strings.TrimSpace(f.Name) == "" {
		return fmt.Errorf("fleet name is required")
	}

	if len(f.Agents) == 0 {
		return fmt.Errorf("fleet %q: at least one agent is required", f.Name)
	}

	// Validate leader if present
	if f.Leader != nil {
		if strings.TrimSpace(f.Leader.Persona) == "" {
			return fmt.Errorf("fleet %q: leader persona reference is required", f.Name)
		}
		if strings.TrimSpace(f.Leader.Behaviors) == "" {
			return fmt.Errorf("fleet %q: leader behaviors are required", f.Name)
		}
	}

	for key, agent := range f.Agents {
		if strings.TrimSpace(agent.Persona) == "" {
			return fmt.Errorf("fleet %q agent %q: persona reference is required", f.Name, key)
		}
		if strings.TrimSpace(agent.Behaviors) == "" {
			return fmt.Errorf("fleet %q agent %q: behaviors are required", f.Name, key)
		}
		// Agent must have either tools or a delegate (or both for validation tools)
		if agent.Tools.IsEmpty() && agent.Delegate == nil {
			return fmt.Errorf("fleet %q agent %q: must have tools or a delegate configured", f.Name, key)
		}
		if agent.Delegate != nil {
			if strings.TrimSpace(agent.Delegate.Tool) == "" {
				return fmt.Errorf("fleet %q agent %q: delegate tool name is required", f.Name, key)
			}
		}
	}

	// Validate suggested flow references
	if f.SuggestedFlow != nil {
		phaseNames := make(map[string]bool)
		for i := range f.SuggestedFlow.Phases {
			phase := &f.SuggestedFlow.Phases[i]

			// Normalize deprecated Agent -> Primary
			phase.NormalizePrimary()

			if strings.TrimSpace(phase.Name) == "" {
				return fmt.Errorf("fleet %q: phase name is required", f.Name)
			}

			// Primary is required for all phases
			if strings.TrimSpace(phase.Primary) == "" {
				return fmt.Errorf("fleet %q phase %q: primary agent is required", f.Name, phase.Name)
			}
			if _, ok := f.Agents[phase.Primary]; !ok {
				return fmt.Errorf("fleet %q phase %q: primary references unknown agent %q", f.Name, phase.Name, phase.Primary)
			}

			// Validate reviewers if present (conversation mode)
			for _, r := range phase.Reviewers {
				if _, ok := f.Agents[r]; !ok {
					return fmt.Errorf("fleet %q phase %q: reviewer references unknown agent %q", f.Name, phase.Name, r)
				}
			}

			phaseNames[phase.Name] = true
		}

		// Validate review targets
		for reviewer, targets := range f.SuggestedFlow.Reviews {
			if !phaseNames[reviewer] {
				return fmt.Errorf("fleet %q reviews: reviewer %q is not a defined phase", f.Name, reviewer)
			}
			for _, target := range targets {
				if !phaseNames[target] {
					return fmt.Errorf("fleet %q reviews: review target %q is not a defined phase", f.Name, target)
				}
			}
		}
	}

	return nil
}

// ValidatePersonaRefs checks that all persona references in the fleet
// can be resolved. The lookup function should return true if the persona key exists.
func (f *FleetConfig) ValidatePersonaRefs(personaExists func(key string) bool) error {
	// Check leader persona
	if f.Leader != nil {
		if !personaExists(f.Leader.Persona) {
			return fmt.Errorf("fleet %q leader: persona %q not found", f.Name, f.Leader.Persona)
		}
	}

	for agentKey, agent := range f.Agents {
		if !personaExists(agent.Persona) {
			return fmt.Errorf("fleet %q agent %q: persona %q not found", f.Name, agentKey, agent.Persona)
		}
	}
	return nil
}

// LoadFleet reads and parses a single fleet YAML file.
func LoadFleet(path string) (*FleetConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading fleet file %s: %w", path, err)
	}

	var fleet FleetConfig
	if err := yaml.Unmarshal(data, &fleet); err != nil {
		return nil, fmt.Errorf("parsing fleet file %s: %w", path, err)
	}

	if err := fleet.Validate(); err != nil {
		return nil, fmt.Errorf("validating fleet file %s: %w", path, err)
	}

	return &fleet, nil
}

// LoadFleets reads all .yaml/.yml files from the given directory
// and returns a map keyed by filename stem.
func LoadFleets(dir string) (map[string]*FleetConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading fleets directory %s: %w", dir, err)
	}

	fleets := make(map[string]*FleetConfig)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		path := filepath.Join(dir, name)
		fleet, err := LoadFleet(path)
		if err != nil {
			return nil, err
		}

		key := strings.TrimSuffix(name, filepath.Ext(name))
		fleets[key] = fleet
	}

	return fleets, nil
}

// SaveFleet writes a fleet config to a YAML file.
func SaveFleet(dir string, key string, fleet *FleetConfig) error {
	if err := fleet.Validate(); err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating fleets directory: %w", err)
	}

	data, err := yaml.Marshal(fleet)
	if err != nil {
		return fmt.Errorf("marshalling fleet %q: %w", fleet.Name, err)
	}

	path := filepath.Join(dir, key+".yaml")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing fleet file %s: %w", path, err)
	}

	return nil
}

// CollectDelegateEnvVars returns the unique set of environment variable names
// declared across all delegate configs in the given fleets. This is used by
// the daemon installer and runtime to ensure delegate tools (e.g. OpenCode)
// have the env vars they need.
func CollectDelegateEnvVars(fleets map[string]*FleetConfig) []string {
	seen := make(map[string]bool)
	for _, f := range fleets {
		for _, agent := range f.Agents {
			if agent.Delegate == nil {
				continue
			}
			for _, envName := range agent.Delegate.Env {
				envName = strings.TrimSpace(envName)
				if envName != "" {
					seen[envName] = true
				}
			}
		}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	return result
}

// DeleteFleet removes a fleet YAML file from the directory.
func DeleteFleet(dir string, key string) error {
	path := filepath.Join(dir, key+".yaml")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("fleet %q not found", key)
		}
		return fmt.Errorf("deleting fleet %q: %w", key, err)
	}
	return nil
}
