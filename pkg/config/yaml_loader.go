package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// AgentConfig represents the top-level structure of the agent YAML.
type AgentConfig struct {
	Description     string           `yaml:"description"`
	Type            string           `yaml:"type,omitempty"`         // "test", "test_suite", or empty for regular flows
	Suite           string           `yaml:"suite,omitempty"`        // For type: test — which suite this belongs to
	SuiteConfig     *TestSuiteConfig `yaml:"suite_config,omitempty"` // For type: test_suite — infrastructure config
	TestConfig      *TestConfig      `yaml:"test_config,omitempty"`  // For type: test — test-specific config
	Nodes           []Node           `yaml:"nodes"`
	Flow            []FlowItem       `yaml:"flow"`
	MCPDependencies []MCPDependency  `yaml:"mcp_dependencies,omitempty"`
}

// TestSuiteConfig defines infrastructure for running tests.
// Used by type: test_suite flows.
type TestSuiteConfig struct {
	Template    string            `yaml:"template,omitempty"`    // Container template name (e.g., "@myapp")
	Services    []ServiceConfig   `yaml:"services,omitempty"`    // Multi-service definitions (started in order, stopped in reverse)
	Setup       []string          `yaml:"setup,omitempty"`       // Shell commands to run before tests (legacy single-service)
	ReadyCheck  *ReadyCheck       `yaml:"ready_check,omitempty"` // Wait for application readiness (legacy single-service)
	Teardown    []string          `yaml:"teardown,omitempty"`    // Shell commands after all tests (legacy single-service)
	Environment map[string]string `yaml:"environment,omitempty"` // Shared environment variables
	BaseURL     string            `yaml:"base_url,omitempty"`    // Base URL for browser tests (e.g., "http://localhost:3000")
}

// ServiceConfig defines a single service in a multi-service test suite.
// Services are started in declaration order and stopped in reverse order.
type ServiceConfig struct {
	Name        string            `yaml:"name"`                  // Service identifier (e.g., "database", "backend", "frontend")
	Setup       string            `yaml:"setup"`                 // Shell command to start this service
	ReadyCheck  *ReadyCheck       `yaml:"ready_check,omitempty"` // Per-service readiness check
	Teardown    string            `yaml:"teardown,omitempty"`    // Shell command to stop this service
	Environment map[string]string `yaml:"environment,omitempty"` // Per-service environment variables
}

// ReadyCheck defines how to verify the application under test is ready.
type ReadyCheck struct {
	Type     string `yaml:"type"`               // "http", "port", "output_contains"
	URL      string `yaml:"url,omitempty"`      // For http type: URL to poll
	Host     string `yaml:"host,omitempty"`     // For port type (default: "localhost")
	Port     int    `yaml:"port,omitempty"`     // For port type: TCP port number
	Pattern  string `yaml:"pattern,omitempty"`  // For output_contains type: string to match
	Timeout  int    `yaml:"timeout,omitempty"`  // Max wait in seconds (default: 30)
	Interval int    `yaml:"interval,omitempty"` // Poll interval in seconds (default: 2)
}

// TestConfig holds per-test configuration (lightweight — infrastructure is in the suite).
type TestConfig struct {
	Tags        []string `yaml:"tags,omitempty"`         // For filtering (e.g., "smoke", "regression")
	Timeout     int      `yaml:"timeout,omitempty"`      // Per-test timeout in seconds (default: 120)
	StepTimeout int      `yaml:"step_timeout,omitempty"` // Per-step timeout in seconds (default: 30)
	OnFail      string   `yaml:"on_fail,omitempty"`      // "stop" (default), "continue", "triage"
	MaxRetries  int      `yaml:"max_retries,omitempty"`  // Max auto-retries for transient failures (default: 1 when triage is active)
}

// AssertConfig defines what to check after a step executes.
type AssertConfig struct {
	Type     string `yaml:"type"`              // "contains", "not_contains", "regex", "exit_code", "element_exists", "semantic"
	Source   string `yaml:"source,omitempty"`  // "output" (default), "snapshot", "screenshot", "pty_buffer"
	Expected string `yaml:"expected"`          // Expected value (string, regex, or natural language for semantic)
	OnFail   string `yaml:"on_fail,omitempty"` // Override per-step: "stop", "continue", "triage"
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
	Name              string                 `yaml:"name"`
	Type              string                 `yaml:"type"` // "input", "llm", "tool"
	Prompt            string                 `yaml:"prompt,omitempty"`
	System            string                 `yaml:"system,omitempty"`
	OutputModel       map[string]string      `yaml:"output_model,omitempty"`
	Tools             bool                   `yaml:"tools,omitempty"`
	ToolsSelection    []string               `yaml:"tools_selection,omitempty"`
	Options           []string               `yaml:"options,omitempty"` // Simplified for now, assuming string list
	UserMessage       []string               `yaml:"user_message,omitempty"`
	Args              map[string]interface{} `yaml:"args,omitempty"`
	RawToolOutput     map[string]string      `yaml:"raw_tool_output,omitempty"`
	ToolsAutoApproval bool                   `yaml:"tools_auto_approval,omitempty"`
	ContinueOnError   bool                   `yaml:"continue_on_error,omitempty"` // If true, tool errors are captured in output instead of stopping flow
	Updates           map[string]string      `yaml:"updates,omitempty"`
	Action            string                 `yaml:"action,omitempty"`
	Value             interface{}            `yaml:"value,omitempty"`
	SourceVariable    string                 `yaml:"source_variable,omitempty"`
	Parallel          *ParallelConfig        `yaml:"parallel,omitempty"`
	OutputAction      string                 `yaml:"output_action,omitempty"`  // "append" or other aggregation strategies
	MaxRetries        int                    `yaml:"max_retries,omitempty"`    // Maximum retry attempts (default: 3)
	RetryStrategy     string                 `yaml:"retry_strategy,omitempty"` // "intelligent" or "simple" (default: intelligent)
	Silent            bool                   `yaml:"silent,omitempty"`         // If true, node execution is not shown in UI/CLI
	Assert            *AssertConfig          `yaml:"assert,omitempty"`         // Assertion for test flows (Spec 17)
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
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config AgentConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
