package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// FleetConfig represents a fleet definition loaded from YAML.
// In v2, fleets define a communication graph (who talks to whom) instead of
// a pipeline. Agents are autonomous actors that react to messages on a shared
// channel and route work to each other via explicit @mentions.
type FleetConfig struct {
	Name           string                      `yaml:"name" json:"name"`
	Description    string                      `yaml:"description,omitempty" json:"description,omitempty"`
	PlanWizard     *PlanWizardConfig           `yaml:"plan_wizard,omitempty" json:"plan_wizard,omitempty"`
	Communication  *CommunicationConfig        `yaml:"communication,omitempty" json:"communication,omitempty"`
	Agents         map[string]FleetAgentConfig `yaml:"agents" json:"agents"`
	Settings       FleetSettings               `yaml:"settings,omitempty" json:"settings,omitempty"`
	ProjectContext *ProjectContextConfig       `yaml:"project_context,omitempty" json:"project_context,omitempty"`
}

// PlanWizardConfig defines how an AI-guided plan creation session should behave
// when creating a fleet plan from this template. Each template can have a
// completely different wizard tailored to its domain and integrations.
type PlanWizardConfig struct {
	// Description is a short user-visible message shown when the wizard starts
	// (e.g., "Let's configure a software development fleet plan").
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// SystemPrompt contains instructions for the LLM that guide the plan creation
	// conversation. These are injected as system context and never shown to the
	// user as a chat message. The prompt should tell the LLM what to ask about,
	// what to validate, and how to call save_fleet_plan.
	SystemPrompt string `yaml:"system_prompt" json:"system_prompt"`
}

// CommunicationConfig defines the communication graph for the fleet.
// It specifies which agents can talk to each other and in what logical order.
type CommunicationConfig struct {
	Flow []CommunicationNode `yaml:"flow" json:"flow"`
}

// CommunicationNode defines one agent's position in the communication graph.
type CommunicationNode struct {
	Role       string   `yaml:"role" json:"role"`                                   // Agent key (e.g., "po", "architect")
	TalksTo    []string `yaml:"talks_to" json:"talks_to"`                           // Who this agent can communicate with (agent keys or "customer")
	EntryPoint bool     `yaml:"entry_point,omitempty" json:"entry_point,omitempty"` // True if this agent receives initial human requests
}

// FleetAgentConfig defines a single agent slot within a fleet.
// It references a persona and adds fleet-specific tool access, delegate
// configuration, behavioral rules, and execution mode.
type FleetAgentConfig struct {
	Persona   string          `yaml:"persona" json:"persona"`
	Mode      string          `yaml:"mode,omitempty" json:"mode,omitempty"` // "simple" or "agentic" (default: "agentic")
	Tools     ToolsConfig     `yaml:"tools,omitempty" json:"tools,omitempty"`
	Delegate  *DelegateConfig `yaml:"delegate,omitempty" json:"delegate,omitempty"`
	Behaviors string          `yaml:"behaviors" json:"behaviors"`
}

// GetMode returns the agent execution mode, defaulting to "agentic".
func (a *FleetAgentConfig) GetMode() string {
	if a.Mode == "" {
		return "agentic"
	}
	return a.Mode
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

// ProjectContextConfig defines how project context (e.g., AGENTS.md) is
// generated and injected into agent prompts. This is template-level config:
// not all fleet types need project context, and those that do can choose
// different generation strategies.
type ProjectContextConfig struct {
	// Generator is the strategy for producing the context file.
	// Supported values: "opencode_init" (runs OpenCode /init to analyze the
	// codebase), "load_file" (reads an existing file without generating).
	// Empty or omitted means no project context.
	Generator string `yaml:"generator,omitempty" json:"generator,omitempty"`

	// OutputFile is the filename to write/read in the workspace directory
	// (e.g., "AGENTS.md"). Relative to the workspace root.
	OutputFile string `yaml:"output_file,omitempty" json:"output_file,omitempty"`

	// MaxSizeKB caps the content injected into agent prompts.
	// Default: 10 (10KB). Larger files are truncated.
	MaxSizeKB int `yaml:"max_size_kb,omitempty" json:"max_size_kb,omitempty"`
}

// GetMaxSizeBytes returns the max size in bytes, defaulting to 10KB.
func (p *ProjectContextConfig) GetMaxSizeBytes() int {
	if p.MaxSizeKB <= 0 {
		return 10 * 1024
	}
	return p.MaxSizeKB * 1024
}

// FleetSettings holds fleet-level configuration.
type FleetSettings struct {
	MaxTurnsPerAgent int `yaml:"max_turns_per_agent,omitempty" json:"max_turns_per_agent,omitempty"` // Max LLM turns when an agent is activated (0 = use system default)
}

// GetMaxTurnsPerAgent returns the configured max or a default of 20.
func (s *FleetSettings) GetMaxTurnsPerAgent() int {
	if s.MaxTurnsPerAgent <= 0 {
		return 20
	}
	return s.MaxTurnsPerAgent
}

// --- Communication graph helpers ---

// GetEntryPoint returns the agent key marked as entry_point in the communication graph.
// If no entry point is defined, returns the first agent in the flow.
func (f *FleetConfig) GetEntryPoint() string {
	if f.Communication == nil || len(f.Communication.Flow) == 0 {
		return ""
	}
	for _, node := range f.Communication.Flow {
		if node.EntryPoint {
			return node.Role
		}
	}
	// Fallback: first agent in the flow
	return f.Communication.Flow[0].Role
}

// CanTalkTo checks whether agent 'from' is allowed to talk to agent 'to'
// according to the communication graph.
func (f *FleetConfig) CanTalkTo(from, to string) bool {
	if f.Communication == nil {
		return false
	}
	for _, node := range f.Communication.Flow {
		if node.Role == from {
			for _, target := range node.TalksTo {
				if target == to {
					return true
				}
			}
			return false
		}
	}
	return false
}

// GetTalksTo returns the list of targets an agent can communicate with.
func (f *FleetConfig) GetTalksTo(agentKey string) []string {
	if f.Communication == nil {
		return nil
	}
	for _, node := range f.Communication.Flow {
		if node.Role == agentKey {
			return node.TalksTo
		}
	}
	return nil
}

// GetFlowOrder returns the logical order of agents from the communication graph.
// This is the order in which agents appear in the flow, representing the
// natural progression (e.g., PO -> architect -> dev -> QA -> security).
func (f *FleetConfig) GetFlowOrder() []string {
	if f.Communication == nil {
		return nil
	}
	order := make([]string, 0, len(f.Communication.Flow))
	for _, node := range f.Communication.Flow {
		if node.Role != "customer" {
			order = append(order, node.Role)
		}
	}
	return order
}

// GetNextInFlow returns the next agent in the logical flow after the given agent.
// Returns empty string if the agent is last or not found.
func (f *FleetConfig) GetNextInFlow(agentKey string) string {
	order := f.GetFlowOrder()
	for i, role := range order {
		if role == agentKey && i+1 < len(order) {
			return order[i+1]
		}
	}
	return ""
}

// CanTalkToCustomer checks whether an agent is allowed to talk to the customer.
func (f *FleetConfig) CanTalkToCustomer(agentKey string) bool {
	return f.CanTalkTo(agentKey, "customer")
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

	for key, agent := range f.Agents {
		if strings.TrimSpace(agent.Persona) == "" {
			return fmt.Errorf("fleet %q agent %q: persona reference is required", f.Name, key)
		}
		if strings.TrimSpace(agent.Behaviors) == "" {
			return fmt.Errorf("fleet %q agent %q: behaviors are required", f.Name, key)
		}
		// Agent must have either tools or a delegate (or both)
		if agent.Tools.IsEmpty() && agent.Delegate == nil {
			return fmt.Errorf("fleet %q agent %q: must have tools or a delegate configured", f.Name, key)
		}
		if agent.Delegate != nil {
			if strings.TrimSpace(agent.Delegate.Tool) == "" {
				return fmt.Errorf("fleet %q agent %q: delegate tool name is required", f.Name, key)
			}
		}
		// Validate mode if specified
		if agent.Mode != "" && agent.Mode != "simple" && agent.Mode != "agentic" {
			return fmt.Errorf("fleet %q agent %q: mode must be 'simple' or 'agentic', got %q", f.Name, key, agent.Mode)
		}
	}

	// Validate communication graph
	if f.Communication != nil {
		hasEntryPoint := false
		for _, node := range f.Communication.Flow {
			if strings.TrimSpace(node.Role) == "" {
				return fmt.Errorf("fleet %q communication: role is required for each flow node", f.Name)
			}
			// Role must reference a defined agent (unless it's "customer")
			if node.Role != "customer" {
				if _, ok := f.Agents[node.Role]; !ok {
					return fmt.Errorf("fleet %q communication: role %q references unknown agent", f.Name, node.Role)
				}
			}
			// Validate talks_to targets
			for _, target := range node.TalksTo {
				if target != "customer" {
					if _, ok := f.Agents[target]; !ok {
						return fmt.Errorf("fleet %q communication: %q talks_to unknown agent %q", f.Name, node.Role, target)
					}
				}
			}
			if node.EntryPoint {
				hasEntryPoint = true
			}
		}
		if len(f.Communication.Flow) > 0 && !hasEntryPoint {
			return fmt.Errorf("fleet %q communication: at least one agent must be marked as entry_point", f.Name)
		}
	}

	return nil
}

// ValidatePersonaRefs checks that all persona references in the fleet
// can be resolved. The lookup function should return true if the persona key exists.
func (f *FleetConfig) ValidatePersonaRefs(personaExists func(key string) bool) error {
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
