package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentConfig represents the top-level structure of the agent YAML.
type AgentConfig struct {
	Description     string              `yaml:"description"`
	Type            string              `yaml:"type,omitempty"`         // "drill", "drill_suite" (legacy: "test", "test_suite"), or empty for regular flows
	Template        string              `yaml:"template,omitempty"`     // Sandbox template (also accepted inside suite_config; top-level is reconciled down)
	Suite           string              `yaml:"suite,omitempty"`        // For type: drill — which suite this belongs to
	SuiteConfig     *DrillSuiteConfig   `yaml:"suite_config,omitempty"` // For type: drill_suite — infrastructure config
	DrillConfig     *DrillConfig        `yaml:"drill_config,omitempty"` // For type: drill — drill-specific config
	Parameters      []map[string]string `yaml:"parameters,omitempty"`   // Parameter sets for data-driven tests (each map is one test run)
	Nodes           []Node              `yaml:"nodes"`
	Flow            []FlowItem          `yaml:"flow"`
	MCPDependencies []MCPDependency     `yaml:"mcp_dependencies,omitempty"`
}

// agentConfigRaw is the intermediate struct used for backward-compatible YAML parsing.
// It supports both old (test_config) and new (drill_config) YAML tags.
type agentConfigRaw struct {
	Description     string              `yaml:"description"`
	Type            string              `yaml:"type,omitempty"`
	Template        string              `yaml:"template,omitempty"`
	Suite           string              `yaml:"suite,omitempty"`
	SuiteConfig     *DrillSuiteConfig   `yaml:"suite_config,omitempty"`
	DrillConfig     *DrillConfig        `yaml:"drill_config,omitempty"`
	TestConfig      *DrillConfig        `yaml:"test_config,omitempty"` // backward compat
	Parameters      []map[string]string `yaml:"parameters,omitempty"`
	Nodes           []Node              `yaml:"nodes"`
	Flow            []FlowItem          `yaml:"flow"`
	MCPDependencies []MCPDependency     `yaml:"mcp_dependencies,omitempty"`
}

// UnmarshalYAML implements custom unmarshaling for AgentConfig to support
// backward compatibility: "test_config" YAML tag is accepted and mapped
// to DrillConfig, and "test"/"test_suite" type values are normalized to
// "drill"/"drill_suite".
func (c *AgentConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw agentConfigRaw
	if err := value.Decode(&raw); err != nil {
		return err
	}

	c.Description = raw.Description
	c.Type = raw.Type
	c.Template = raw.Template
	c.Suite = raw.Suite
	c.SuiteConfig = raw.SuiteConfig
	c.Parameters = raw.Parameters
	c.Nodes = raw.Nodes
	c.Flow = raw.Flow
	c.MCPDependencies = raw.MCPDependencies

	// drill_config takes precedence; fall back to test_config for backward compat
	if raw.DrillConfig != nil {
		c.DrillConfig = raw.DrillConfig
	} else if raw.TestConfig != nil {
		c.DrillConfig = raw.TestConfig
	}

	// Normalize legacy type values
	switch c.Type {
	case "test":
		c.Type = "drill"
	case "test_suite":
		c.Type = "drill_suite"
	}

	// Reconcile template: top-level template is accepted as a convenience.
	// If suite_config exists but has no template, and top-level has one, copy it down.
	if c.Template != "" && c.SuiteConfig != nil && c.SuiteConfig.Template == "" {
		c.SuiteConfig.Template = c.Template
	}

	return nil
}

// DrillSuiteConfig defines infrastructure for running drills.
// Used by type: drill_suite flows.
//
// run_drill only injects credentials and executes tests. Prep fields below are
// instruction sources for Studio chat / agents (template switch, git sync,
// start services, ready poll) — they are NOT executed by SuiteRunner.
type DrillSuiteConfig struct {
	Template            string                    `yaml:"template,omitempty" json:"template,omitempty"`                         // Container template name (e.g., "@myapp")
	Workspace           string                    `yaml:"workspace,omitempty" json:"workspace,omitempty"`                       // Absolute path to the app workspace inside the sandbox
	Branch              string                    `yaml:"branch,omitempty" json:"branch,omitempty"`                             // Git branch to sync before Studio runs (default main when workspace set)
	RunInstructions     string                    `yaml:"run_instructions,omitempty" json:"run_instructions,omitempty"`         // Optional full chat prep override; else auto-generated
	Services            []ServiceConfig           `yaml:"services,omitempty" json:"services,omitempty"`                         // Multi-service defs (instruction source; not auto-started by runner)
	Configure           []string                  `yaml:"configure,omitempty" json:"configure,omitempty"`                       // Offline/file prep steps (instruction source)
	Setup               []string                  `yaml:"setup,omitempty" json:"setup,omitempty"`                               // Start-service commands (instruction source)
	ReadyCheck          *ReadyCheck               `yaml:"ready_check,omitempty" json:"ready_check,omitempty"`                   // Readiness hint for agent prep (not auto-polled by runner)
	Teardown            []string                  `yaml:"teardown,omitempty" json:"teardown,omitempty"`                         // Cleanup hint for agents (not auto-run by runner)
	Environment         map[string]string         `yaml:"environment,omitempty" json:"environment,omitempty"`                   // Shared environment variables
	BaseURL             string                    `yaml:"base_url,omitempty" json:"base_url,omitempty"`                         // Base URL for browser tests (e.g., "http://localhost:3000")
	Credentials         map[string]string         `yaml:"credentials,omitempty" json:"credentials,omitempty"`                   // logical name → credential store entry
	CredentialInjection *SuiteCredentialInjection `yaml:"credential_injection,omitempty" json:"credential_injection,omitempty"` // inject_drill_credentials before start; run_drill also injects before tests
}

// SuiteCredentialInjection mirrors fleet credential_injection for drill suites.
// Kept in config (not fleet) to avoid config→fleet imports.
type SuiteCredentialInjection struct {
	Env   []SuiteEnvInjection  `yaml:"env,omitempty" json:"env,omitempty"`
	Files []SuiteFileInjection `yaml:"files,omitempty" json:"files,omitempty"`
}

// SuiteEnvInjection maps a logical suite credential to a container env var.
type SuiteEnvInjection struct {
	Credential string `yaml:"credential" json:"credential"`
	Var        string `yaml:"var" json:"var"`
	Field      string `yaml:"field" json:"field"`
}

// SuiteFileInjection materializes a credential field as a file in the sandbox.
type SuiteFileInjection struct {
	Credential string `yaml:"credential" json:"credential"`
	Path       string `yaml:"path" json:"path"`
	Format     string `yaml:"format,omitempty" json:"format,omitempty"`
	Field      string `yaml:"field" json:"field"`
	Mode       string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

// ServiceConfig defines a single service in a multi-service drill suite.
// Services are started in declaration order and stopped in reverse order.
type ServiceConfig struct {
	Name        string            `yaml:"name" json:"name"`                                   // Service identifier (e.g., "database", "backend", "frontend")
	Setup       string            `yaml:"setup" json:"setup"`                                 // Shell command to start this service
	ReadyCheck  *ReadyCheck       `yaml:"ready_check,omitempty" json:"ready_check,omitempty"` // Per-service readiness check
	Teardown    string            `yaml:"teardown,omitempty" json:"teardown,omitempty"`       // Shell command to stop this service
	Environment map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"` // Per-service environment variables
}

// ReadyCheck defines how to verify the application under test is ready.
type ReadyCheck struct {
	Type        string `yaml:"type" json:"type"`                                     // "http", "port", "output_contains"
	URL         string `yaml:"url,omitempty" json:"url,omitempty"`                   // For http type: URL to poll
	Host        string `yaml:"host,omitempty" json:"host,omitempty"`                 // For port type (default: "localhost")
	Port        int    `yaml:"port,omitempty" json:"port,omitempty"`                 // For port type: TCP port number
	Pattern     string `yaml:"pattern,omitempty" json:"pattern,omitempty"`           // For output_contains type: string to match
	Timeout     int    `yaml:"timeout,omitempty" json:"timeout,omitempty"`           // Max wait in seconds (default: 30)
	Interval    int    `yaml:"interval,omitempty" json:"interval,omitempty"`         // Poll interval in seconds (default: 2)
	StableCount int    `yaml:"stable_count,omitempty" json:"stable_count,omitempty"` // Consecutive successes required (default: 3)
}

// DrillConfig holds per-drill configuration (lightweight — infrastructure is in the suite).
type DrillConfig struct {
	Tags            []string `yaml:"tags,omitempty"`              // For filtering (e.g., "smoke", "regression")
	Timeout         int      `yaml:"timeout,omitempty"`           // Per-test timeout in seconds (default: 120)
	StepTimeout     int      `yaml:"step_timeout,omitempty"`      // Per-step timeout in seconds (default: 30)
	OnFail          string   `yaml:"on_fail,omitempty"`           // "stop" (default), "continue", "triage"
	MaxRetries      int      `yaml:"max_retries,omitempty"`       // Max auto-retries for transient failures (default: 1 when triage is active)
	AutoWait        bool     `yaml:"auto_wait,omitempty"`         // Auto-wait for elements before browser interaction steps
	AutoWaitTimeout int      `yaml:"auto_wait_timeout,omitempty"` // Auto-wait timeout in milliseconds (default: 5000)
	// Mode is ""/"test" for deterministic assertion drills (default), or "tutorial"
	// for paced training-video scripts (narration, hold_ms, scene recording).
	Mode string `yaml:"mode,omitempty"`
}

// AssertConfig defines what to check after a step executes.
type AssertConfig struct {
	Type      string  `yaml:"type" json:"type"`                               // "contains", "not_contains", "regex", "exit_code", "element_exists", "semantic", "visual_match"
	Source    string  `yaml:"source,omitempty" json:"source,omitempty"`       // "output" (default), "snapshot", "screenshot", "pty_buffer"
	Expected  string  `yaml:"expected" json:"expected"`                       // Expected value (string, regex, or natural language for semantic)
	OnFail    string  `yaml:"on_fail,omitempty" json:"on_fail,omitempty"`     // Override per-step: "stop", "continue", "triage"
	Threshold float64 `yaml:"threshold,omitempty" json:"threshold,omitempty"` // For visual_match: max allowed diff percentage (default: 0.01 = 1%)
}

// MCPDependency represents a required MCP server for the flow.
// Source can be: "store" (official MCP store), "tap" (same tap repo), or "inline" (embedded config).
type MCPDependency struct {
	Server  string           `yaml:"server" json:"server"`                         // MCP server name
	Tools   []string         `yaml:"tools,omitempty" json:"tools,omitempty"`       // Which tools from this server are used
	Source  string           `yaml:"source" json:"source"`                         // "store", "tap", or "inline"
	StoreID string           `yaml:"store_id,omitempty" json:"store_id,omitempty"` // For store source: the store entry ID
	Config  *MCPServerConfig `yaml:"config,omitempty" json:"config,omitempty"`     // For inline source: uses MCPServerConfig from mcp_config.go
}

// Node represents a single step in the agent's execution.
type Node struct {
	Name              string                 `yaml:"name" json:"name"`
	Type              string                 `yaml:"type" json:"type"` // "input", "llm", "tool"
	Prompt            string                 `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	System            string                 `yaml:"system,omitempty" json:"system,omitempty"`
	RawContext        string                 `yaml:"raw_context,omitempty" json:"raw_context,omitempty"` // Verbatim context appended to system instruction (no state interpolation)
	OutputModel       map[string]string      `yaml:"output_model,omitempty" json:"output_model,omitempty"`
	Tools             bool                   `yaml:"tools,omitempty" json:"tools,omitempty"`
	ToolsSelection    []string               `yaml:"tools_selection,omitempty" json:"tools_selection,omitempty"`
	Options           []string               `yaml:"options,omitempty" json:"options,omitempty"`
	UserMessage       []string               `yaml:"user_message,omitempty" json:"user_message,omitempty"`
	Args              map[string]interface{} `yaml:"args,omitempty" json:"args,omitempty"`
	RawToolOutput     map[string]string      `yaml:"raw_tool_output,omitempty" json:"raw_tool_output,omitempty"`
	ToolsAutoApproval bool                   `yaml:"tools_auto_approval,omitempty" json:"tools_auto_approval,omitempty"`
	ContinueOnError   bool                   `yaml:"continue_on_error,omitempty" json:"continue_on_error,omitempty"`
	Updates           map[string]string      `yaml:"updates,omitempty" json:"updates,omitempty"`
	Action            string                 `yaml:"action,omitempty" json:"action,omitempty"`
	Value             interface{}            `yaml:"value,omitempty" json:"value,omitempty"`
	SourceVariable    string                 `yaml:"source_variable,omitempty" json:"source_variable,omitempty"`
	Parallel          *ParallelConfig        `yaml:"parallel,omitempty" json:"parallel,omitempty"`
	OutputAction      string                 `yaml:"output_action,omitempty" json:"output_action,omitempty"`   // "append" or other aggregation strategies
	MaxRetries        int                    `yaml:"max_retries,omitempty" json:"max_retries,omitempty"`       // Maximum retry attempts (default: 3)
	RetryStrategy     string                 `yaml:"retry_strategy,omitempty" json:"retry_strategy,omitempty"` // "intelligent" or "simple" (default: intelligent)
	Silent            bool                   `yaml:"silent,omitempty" json:"silent,omitempty"`                 // If true, node execution is not shown in UI/CLI
	Assert            *AssertConfig          `yaml:"assert,omitempty" json:"assert,omitempty"`                 // Assertion for drill flows (Spec 17)
	// Tutorial / scene fields (used when drill_config.mode is "tutorial")
	Narration string `yaml:"narration,omitempty" json:"narration,omitempty"` // Spoken script for this beat
	HoldMs    int    `yaml:"hold_ms,omitempty" json:"hold_ms,omitempty"`     // Pause after the tool succeeds (pacing)
	Record    string `yaml:"record,omitempty" json:"record,omitempty"`       // "", "start", "stop", or "segment"
}

// ParallelConfig defines configuration for parallel execution.
type ParallelConfig struct {
	ForEach        string `yaml:"forEach"`
	As             string `yaml:"as"`
	IndexAs        string `yaml:"index_as,omitempty"`
	MaxConcurrency int    `yaml:"maxConcurrency,omitempty"`
}

// FlowItem represents a transition in the flow.
type FlowItem struct {
	From  string `yaml:"from"`
	To    string `yaml:"to,omitempty"`
	Edges []Edge `yaml:"edges,omitempty"`
}

// Edge represents a conditional transition.
type Edge struct {
	To        string `yaml:"to"`
	Condition string `yaml:"condition"`
}

// LoadAgent loads an AgentConfig from a YAML file.
func LoadAgent(path string) (*AgentConfig, error) {
	// Sanitize the path: resolve to absolute and ensure it doesn't escape
	// via path traversal (e.g., "../../etc/passwd").
	cleaned := filepath.Clean(path)
	if strings.Contains(cleaned, "..") {
		return nil, fmt.Errorf("invalid agent path: must not contain '..'")
	}
	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return nil, fmt.Errorf("invalid agent path: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	return LoadAgentFromBytes(data)
}

// LoadAgentFromBytes parses an AgentConfig from raw YAML bytes.
func LoadAgentFromBytes(data []byte) (*AgentConfig, error) {
	var config AgentConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}
