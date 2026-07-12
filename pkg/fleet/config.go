package fleet

import (
	"encoding/json"
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
	Name             string                      `yaml:"name" json:"name"`
	Description      string                      `yaml:"description,omitempty" json:"description,omitempty"`
	SetupProfileKey  string                      `yaml:"setup_profile,omitempty" json:"setup_profile,omitempty"`
	PlanWizard       *PlanWizardConfig           `yaml:"plan_wizard,omitempty" json:"plan_wizard,omitempty"` // deprecated: use setup_profile
	Communication    *CommunicationConfig        `yaml:"communication,omitempty" json:"communication,omitempty"`
	Agents           map[string]FleetAgentConfig `yaml:"agents" json:"agents"`
	Settings         FleetSettings               `yaml:"settings,omitempty" json:"settings,omitempty"`
	ProjectContext   *ProjectContextConfig       `yaml:"project_context,omitempty" json:"project_context,omitempty"`
	WorkspaceBaseDir string                      `yaml:"workspace_base_dir,omitempty" json:"workspace_base_dir,omitempty"` // Default base directory for project workspaces (e.g., "~/astonish_projects")
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
	// PinnedToolGroups lists tool group names that should always be available
	// during the wizard conversation, regardless of ToolIndex scoring.
	// This prevents critical tools (e.g., save_sandbox_template, save_fleet_plan)
	// from disappearing mid-conversation when user messages don't semantically
	// match those tools. Example: ["fleet", "sandbox_templates", "drill"]
	PinnedToolGroups []string `yaml:"pinned_tool_groups,omitempty" json:"pinned_tool_groups,omitempty"`
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
// The agent's identity, tools, delegation, and behavioral rules are all
// defined inline. No external persona reference is needed.
type FleetAgentConfig struct {
	Name        string          `yaml:"name" json:"name"`
	Description string          `yaml:"description,omitempty" json:"description,omitempty"`
	Identity    string          `yaml:"identity" json:"identity"`
	Mode        string          `yaml:"mode,omitempty" json:"mode,omitempty"` // "simple" or "agentic" (default: "agentic")
	Tools       ToolsConfig     `yaml:"tools,omitempty" json:"tools,omitempty"`
	Delegate    *DelegateConfig `yaml:"delegate,omitempty" json:"delegate,omitempty"`
	Behaviors   string          `yaml:"behaviors" json:"behaviors"`

	// Capabilities are free-form capability declarations (domain-neutral).
	// The runtime never rejects an unknown capability; CapabilityRegistry is advisory only.
	Capabilities map[string]bool `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`

	// Execution holds per-agent execution knobs (timeout, parallelizable, workspace).
	Execution *AgentExecutionConfig `yaml:"execution,omitempty" json:"execution,omitempty"`

	// Memory holds per-agent memory policy (overrides FleetSettings.MemoryVisibility).
	Memory *AgentMemoryConfig `yaml:"memory,omitempty" json:"memory,omitempty"`

	// TaskPolicy holds task-board claim policy for this agent.
	TaskPolicy *AgentTaskPolicy `yaml:"task_policy,omitempty" json:"task_policy,omitempty"`
}

// AgentExecutionConfig holds per-agent execution knobs.
type AgentExecutionConfig struct {
	MaxTurns       int    `yaml:"max_turns,omitempty" json:"max_turns,omitempty"`             // overrides FleetSettings.MaxTurnsPerAgent
	TimeoutMinutes int    `yaml:"timeout_minutes,omitempty" json:"timeout_minutes,omitempty"` // overrides the 60-min hardcode in activateAgent
	Parallelizable bool   `yaml:"parallelizable,omitempty" json:"parallelizable,omitempty"`   // MUST be true for concurrent scheduling
	Workspace      string `yaml:"workspace,omitempty" json:"workspace,omitempty"`             // "shared" | "isolated" | "none"
}

// AgentMemoryConfig holds per-agent memory policy.
type AgentMemoryConfig struct {
	Receives    []string `yaml:"receives,omitempty" json:"receives,omitempty"`         // agent keys whose outputs THIS agent sees
	PrivateWork bool     `yaml:"private_work,omitempty" json:"private_work,omitempty"` // true = private turns never enter shared memory
}

// AgentTaskPolicy holds task-board claim policy for this agent.
type AgentTaskPolicy struct {
	Claims        []string `yaml:"claims,omitempty" json:"claims,omitempty"`                 // capability names this agent will claim
	MaxConcurrent int      `yaml:"max_concurrent,omitempty" json:"max_concurrent,omitempty"` // 0 = 1 (single-slot)
}

// CapabilityRegistry is an advisory list of domain-neutral capability names surfaced
// in the editor UI. The runtime never rejects an unknown capability — templates may
// declare domain-specific tags (e.g. code.write, genetics.analysis) beyond this list.
var CapabilityRegistry = []string{
	// Discovery & reasoning
	"research", "analysis", "synthesis",
	// Planning & orchestration
	"planning", "coordination", "supervisor",
	// Content lifecycle
	"writing", "review", "editing", "publishing",
	// Creation & delivery
	"design", "implementation", "prototyping",
	// Quality
	"validation", "quality-assurance",
	// Data
	"data-collection", "data-processing",
	// Interaction
	"customer-facing",
}

// GetMode returns the agent execution mode, defaulting to "agentic".
func (a *FleetAgentConfig) GetMode() string {
	if a.Mode == "" {
		return "agentic"
	}
	return a.Mode
}

// IsParallelizable returns true if this agent may run concurrently with another.
func (a *FleetAgentConfig) IsParallelizable() bool {
	return a.Execution != nil && a.Execution.Parallelizable
}

// GetWorkspace returns the workspace mode, defaulting to "shared".
func (a *FleetAgentConfig) GetWorkspace() string {
	if a.Execution == nil || a.Execution.Workspace == "" {
		return "shared"
	}
	return a.Execution.Workspace
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

// MarshalJSON implements custom JSON marshalling for the polymorphic tools field.
func (tc ToolsConfig) MarshalJSON() ([]byte, error) {
	if tc.All {
		return json.Marshal(true)
	}
	if len(tc.Names) > 0 {
		return json.Marshal(tc.Names)
	}
	return []byte("null"), nil
}

// UnmarshalJSON implements custom JSON unmarshalling for the polymorphic tools field.
// Handles: true (all tools), ["tool1", "tool2"] (specific tools), null (no tools),
// or the struct form {"All": true, "Names": [...]} for backward compat.
func (tc *ToolsConfig) UnmarshalJSON(data []byte) error {
	// Handle null
	if string(data) == "null" {
		return nil
	}

	// Try boolean first
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		tc.All = b
		return nil
	}

	// Try string list
	var names []string
	if err := json.Unmarshal(data, &names); err == nil {
		tc.Names = names
		return nil
	}

	// Try struct form (backward compat with default Go JSON encoder output)
	type toolsConfigRaw struct {
		All   bool     `json:"All"`
		Names []string `json:"Names"`
	}
	var raw toolsConfigRaw
	if err := json.Unmarshal(data, &raw); err == nil {
		tc.All = raw.All
		tc.Names = raw.Names
		return nil
	}

	return fmt.Errorf("tools must be a boolean or a list of strings, got: %s", string(data))
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

	// MaxParallelAgents bounds concurrent agent activation.
	// 0 or 1 = serial (today's behavior); >=2 enables the parallel dispatcher.
	MaxParallelAgents int `yaml:"max_parallel_agents,omitempty" json:"max_parallel_agents,omitempty"`

	// MaxWallClockMinutes is a global session budget. 0 = unlimited.
	MaxWallClockMinutes int `yaml:"max_wall_clock_minutes,omitempty" json:"max_wall_clock_minutes,omitempty"`

	// RoutingMode: "llm_mentions" (default) | "explicit_queue" | "supervisor"
	RoutingMode string `yaml:"routing_mode,omitempty" json:"routing_mode,omitempty"`

	// TaskBoard configures the durable task board (always on).
	TaskBoard *TaskBoardConfig `yaml:"task_board,omitempty" json:"task_board,omitempty"`

	// MemoryVisibility: "scoped" (default) | "shared" | "private_plus_handoffs"
	MemoryVisibility string `yaml:"memory_visibility,omitempty" json:"memory_visibility,omitempty"`
}

// TaskBoardConfig configures the durable task board claim policy.
type TaskBoardConfig struct {
	ClaimPolicy string `yaml:"claim_policy,omitempty" json:"claim_policy,omitempty"` // "first_come" | "capability_match" | "supervisor_assigned"
}

// GetMaxTurnsPerAgent returns the configured max or a default of 20.
func (s *FleetSettings) GetMaxTurnsPerAgent() int {
	if s.MaxTurnsPerAgent <= 0 {
		return 20
	}
	return s.MaxTurnsPerAgent
}

// GetMaxParallelAgents returns the parallel agent bound; 0 or negative → 1 (serial).
func (s *FleetSettings) GetMaxParallelAgents() int {
	if s.MaxParallelAgents <= 0 {
		return 1
	}
	return s.MaxParallelAgents
}

// GetRoutingMode returns the routing mode, defaulting to "llm_mentions".
func (s *FleetSettings) GetRoutingMode() string {
	if s.RoutingMode == "" {
		return "llm_mentions"
	}
	return s.RoutingMode
}

// GetClaimPolicy returns the task board claim policy, defaulting to "capability_match".
func (s *FleetSettings) GetClaimPolicy() string {
	if s.TaskBoard == nil || s.TaskBoard.ClaimPolicy == "" {
		return "capability_match"
	}
	return s.TaskBoard.ClaimPolicy
}

// GetMemoryVisibility returns the memory visibility policy, defaulting to "scoped".
func (s *FleetSettings) GetMemoryVisibility() string {
	if s.MemoryVisibility == "" {
		return "scoped"
	}
	return s.MemoryVisibility
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
		if strings.TrimSpace(agent.Name) == "" {
			return fmt.Errorf("fleet %q agent %q: name is required", f.Name, key)
		}
		if strings.TrimSpace(agent.Identity) == "" {
			return fmt.Errorf("fleet %q agent %q: identity is required", f.Name, key)
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
		if agent.Execution != nil {
			ws := agent.Execution.Workspace
			if ws != "" && ws != "shared" && ws != "isolated" && ws != "none" {
				return fmt.Errorf("fleet %q agent %q: execution.workspace must be 'shared', 'isolated', or 'none', got %q", f.Name, key, ws)
			}
		}
	}

	// Validate settings enums and orchestration constraints
	if err := f.validateSettings(); err != nil {
		return err
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

func (f *FleetConfig) validateSettings() error {
	s := f.Settings

	switch s.MemoryVisibility {
	case "", "scoped", "shared", "private_plus_handoffs":
	default:
		return fmt.Errorf("fleet %q: memory_visibility must be 'scoped', 'shared', or 'private_plus_handoffs', got %q", f.Name, s.MemoryVisibility)
	}

	switch s.RoutingMode {
	case "", "llm_mentions", "explicit_queue", "supervisor":
	default:
		return fmt.Errorf("fleet %q: routing_mode must be 'llm_mentions', 'explicit_queue', or 'supervisor', got %q", f.Name, s.RoutingMode)
	}

	if s.MaxParallelAgents > 1 {
		parallelCount := 0
		for _, agent := range f.Agents {
			if agent.IsParallelizable() {
				parallelCount++
			}
		}
		if parallelCount < 2 {
			return fmt.Errorf("fleet %q: max_parallel_agents > 1 requires at least two agents with execution.parallelizable=true", f.Name)
		}
		if err := f.validateParallelizableSiblingsSharePredecessor(); err != nil {
			return err
		}
	}

	if s.GetRoutingMode() == "supervisor" {
		supervisors := 0
		var supervisorKey string
		for key, agent := range f.Agents {
			if agent.Capabilities != nil && agent.Capabilities["supervisor"] {
				supervisors++
				supervisorKey = key
			}
		}
		if supervisors != 1 {
			return fmt.Errorf("fleet %q: routing_mode 'supervisor' requires exactly one agent with capabilities.supervisor=true, found %d", f.Name, supervisors)
		}
		if err := f.validateSupervisorReachability(supervisorKey); err != nil {
			return err
		}
	}

	hasClaims := false
	for _, agent := range f.Agents {
		if agent.TaskPolicy != nil && len(agent.TaskPolicy.Claims) > 0 {
			hasClaims = true
			break
		}
	}
	if !hasClaims {
		return fmt.Errorf("fleet %q: at least one agent must declare task_policy.claims (task board is always on)", f.Name)
	}
	switch s.GetClaimPolicy() {
	case "first_come", "capability_match", "supervisor_assigned":
	default:
		policy := ""
		if s.TaskBoard != nil {
			policy = s.TaskBoard.ClaimPolicy
		}
		return fmt.Errorf("fleet %q: task_board.claim_policy must be 'first_come', 'capability_match', or 'supervisor_assigned', got %q", f.Name, policy)
	}

	return nil
}

// validateSupervisorReachability checks that the supervisor can reach every other
// agent either directly or via one hop in the communication graph.
func (f *FleetConfig) validateSupervisorReachability(supervisorKey string) error {
	if f.Communication == nil || len(f.Communication.Flow) == 0 {
		return nil
	}
	direct := make(map[string]bool)
	for _, t := range f.GetTalksTo(supervisorKey) {
		if t != "customer" {
			direct[t] = true
		}
	}
	for key := range f.Agents {
		if key == supervisorKey {
			continue
		}
		if direct[key] {
			continue
		}
		// One-hop: supervisor → intermediary → key
		reachable := false
		for mid := range direct {
			if f.CanTalkTo(mid, key) {
				reachable = true
				break
			}
		}
		if !reachable {
			return fmt.Errorf("fleet %q: supervisor %q cannot reach agent %q (directly or via one hop)", f.Name, supervisorKey, key)
		}
	}
	return nil
}

// validateParallelizableSiblingsSharePredecessor checks that every pair of
// parallelizable agents shares at least one common predecessor in the
// communication graph (someone who can talk to both). This ensures the
// dispatcher has a coherent fan-out point.
func (f *FleetConfig) validateParallelizableSiblingsSharePredecessor() error {
	if f.Communication == nil {
		return nil
	}
	var parallelKeys []string
	for key, agent := range f.Agents {
		if agent.IsParallelizable() {
			parallelKeys = append(parallelKeys, key)
		}
	}
	if len(parallelKeys) < 2 {
		return nil
	}

	// Build reverse edges: agent → set of agents that can talk to it
	predecessors := make(map[string]map[string]bool)
	for _, node := range f.Communication.Flow {
		for _, target := range node.TalksTo {
			if target == "customer" {
				continue
			}
			if predecessors[target] == nil {
				predecessors[target] = make(map[string]bool)
			}
			if node.Role != "customer" {
				predecessors[target][node.Role] = true
			}
		}
	}

	for i := 0; i < len(parallelKeys); i++ {
		for j := i + 1; j < len(parallelKeys); j++ {
			a, b := parallelKeys[i], parallelKeys[j]
			shared := false
			for pred := range predecessors[a] {
				if predecessors[b][pred] {
					shared = true
					break
				}
			}
			if !shared {
				return fmt.Errorf("fleet %q: parallelizable agents %q and %q must share a common predecessor in the communication graph", f.Name, a, b)
			}
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
