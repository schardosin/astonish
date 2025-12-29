package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestNodeSilentParsing verifies that the silent flag is correctly parsed from YAML
func TestNodeSilentParsing(t *testing.T) {
	tests := []struct {
		name           string
		yaml           string
		expectedSilent bool
	}{
		{
			name: "silent true should be parsed",
			yaml: `
name: init_vars
type: update_state
silent: true
updates:
  counter: "0"
`,
			expectedSilent: true,
		},
		{
			name: "silent false should be parsed",
			yaml: `
name: init_vars
type: update_state
silent: false
updates:
  counter: "0"
`,
			expectedSilent: false,
		},
		{
			name: "silent not specified should default to false",
			yaml: `
name: init_vars
type: update_state
updates:
  counter: "0"
`,
			expectedSilent: false,
		},
		{
			name: "llm node with silent should be parsed",
			yaml: `
name: process
type: llm
silent: true
prompt: "Do something"
`,
			expectedSilent: true,
		},
		{
			name: "tool node with silent should be parsed",
			yaml: `
name: run_tool
type: tool
silent: true
tools_selection:
  - shell_command
`,
			expectedSilent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var node Node
			err := yaml.Unmarshal([]byte(tt.yaml), &node)
			if err != nil {
				t.Fatalf("failed to unmarshal YAML: %v", err)
			}

			if node.Silent != tt.expectedSilent {
				t.Errorf("Silent = %v, expected %v", node.Silent, tt.expectedSilent)
			}
		})
	}
}
