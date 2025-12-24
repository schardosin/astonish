package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// AgentConfig represents the top-level structure of the agent YAML.
type AgentConfig struct {
	Description     string          `yaml:"description"`
	Nodes           []Node          `yaml:"nodes"`
	Flow            []FlowItem      `yaml:"flow"`
	MCPDependencies []MCPDependency `yaml:"mcp_dependencies,omitempty"`
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
	Updates           map[string]string      `yaml:"updates,omitempty"`
	Action            string                 `yaml:"action,omitempty"`
	Value             interface{}            `yaml:"value,omitempty"`
	SourceVariable    string                 `yaml:"source_variable,omitempty"`
	Parallel          *ParallelConfig        `yaml:"parallel,omitempty"`
	OutputAction      string                 `yaml:"output_action,omitempty"`  // "append" or other aggregation strategies
	MaxRetries        int                    `yaml:"max_retries,omitempty"`    // Maximum retry attempts (default: 3)
	RetryStrategy     string                 `yaml:"retry_strategy,omitempty"` // "intelligent" or "simple" (default: intelligent)
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
