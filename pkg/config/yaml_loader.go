package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// AgentConfig represents the top-level structure of the agent YAML.
type AgentConfig struct {
	Description string     `yaml:"description"`
	Nodes       []Node     `yaml:"nodes"`
	Flow        []FlowItem `yaml:"flow"`
}

// Node represents a single step in the agent's execution.
type Node struct {
	Name            string                 `yaml:"name"`
	Type            string                 `yaml:"type"` // "input", "llm", "tool"
	Prompt          string                 `yaml:"prompt,omitempty"`
	System          string                 `yaml:"system,omitempty"`
	OutputModel     map[string]string      `yaml:"output_model,omitempty"`
	Tools           bool                   `yaml:"tools,omitempty"`
	ToolsSelection  []string               `yaml:"tools_selection,omitempty"`
	Options         []string               `yaml:"options,omitempty"` // Simplified for now, assuming string list
	UserMessage     []string               `yaml:"user_message,omitempty"`
	Args            map[string]interface{} `yaml:"args,omitempty"`
	RawToolOutput   map[string]string      `yaml:"raw_tool_output,omitempty"`
	ToolsAutoApproval bool                 `yaml:"tools_auto_approval,omitempty"`
	Updates         map[string]string      `yaml:"updates,omitempty"`
	Action          string                 `yaml:"action,omitempty"`
	Value           interface{}            `yaml:"value,omitempty"`
	SourceVariable  string                 `yaml:"source_variable,omitempty"`
	Parallel        *ParallelConfig        `yaml:"parallel,omitempty"`
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
