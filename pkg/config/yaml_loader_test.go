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

func TestAgentConfigTestSuiteParsing(t *testing.T) {
	input := `
description: "MyApp Integration Tests"
type: test_suite
suite_config:
  template: "@myapp"
  setup:
    - "cd /workspace && npm start &"
  ready_check:
    type: http
    url: "http://localhost:3000/health"
    timeout: 30
    interval: 2
  teardown:
    - "pkill -f 'npm start'"
  environment:
    NODE_ENV: test
    DB_URL: "postgres://localhost/testdb"
`
	var cfg AgentConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if cfg.Type != "test_suite" {
		t.Errorf("Type = %q, want %q", cfg.Type, "test_suite")
	}
	if cfg.Description != "MyApp Integration Tests" {
		t.Errorf("Description = %q, want %q", cfg.Description, "MyApp Integration Tests")
	}
	if cfg.SuiteConfig == nil {
		t.Fatal("SuiteConfig is nil")
	}
	if cfg.SuiteConfig.Template != "@myapp" {
		t.Errorf("Template = %q, want %q", cfg.SuiteConfig.Template, "@myapp")
	}
	if len(cfg.SuiteConfig.Setup) != 1 {
		t.Fatalf("Setup length = %d, want 1", len(cfg.SuiteConfig.Setup))
	}
	if cfg.SuiteConfig.Setup[0] != "cd /workspace && npm start &" {
		t.Errorf("Setup[0] = %q, want %q", cfg.SuiteConfig.Setup[0], "cd /workspace && npm start &")
	}
	if cfg.SuiteConfig.ReadyCheck == nil {
		t.Fatal("ReadyCheck is nil")
	}
	if cfg.SuiteConfig.ReadyCheck.Type != "http" {
		t.Errorf("ReadyCheck.Type = %q, want %q", cfg.SuiteConfig.ReadyCheck.Type, "http")
	}
	if cfg.SuiteConfig.ReadyCheck.URL != "http://localhost:3000/health" {
		t.Errorf("ReadyCheck.URL = %q, want %q", cfg.SuiteConfig.ReadyCheck.URL, "http://localhost:3000/health")
	}
	if cfg.SuiteConfig.ReadyCheck.Timeout != 30 {
		t.Errorf("ReadyCheck.Timeout = %d, want 30", cfg.SuiteConfig.ReadyCheck.Timeout)
	}
	if cfg.SuiteConfig.ReadyCheck.Interval != 2 {
		t.Errorf("ReadyCheck.Interval = %d, want 2", cfg.SuiteConfig.ReadyCheck.Interval)
	}
	if len(cfg.SuiteConfig.Teardown) != 1 {
		t.Fatalf("Teardown length = %d, want 1", len(cfg.SuiteConfig.Teardown))
	}
	if len(cfg.SuiteConfig.Environment) != 2 {
		t.Fatalf("Environment length = %d, want 2", len(cfg.SuiteConfig.Environment))
	}
	if cfg.SuiteConfig.Environment["NODE_ENV"] != "test" {
		t.Errorf("Environment[NODE_ENV] = %q, want %q", cfg.SuiteConfig.Environment["NODE_ENV"], "test")
	}
}

func TestAgentConfigTestParsing(t *testing.T) {
	input := `
description: "Test: Login with valid credentials"
type: test
suite: myapp
test_config:
  tags: ["smoke", "auth"]
  timeout: 30
  step_timeout: 10
  on_fail: continue

nodes:
  - name: check_login
    type: tool
    args:
      tool: shell_command
      command: "curl -s http://localhost:3000/login"
    assert:
      type: contains
      source: output
      expected: "Sign In"

  - name: submit_login
    type: tool
    args:
      tool: shell_command
      command: "curl -s -X POST http://localhost:3000/login"
    assert:
      type: regex
      expected: "Welcome.*Admin"
      on_fail: triage

flow:
  - from: check_login
    to: submit_login
`
	var cfg AgentConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if cfg.Type != "test" {
		t.Errorf("Type = %q, want %q", cfg.Type, "test")
	}
	if cfg.Suite != "myapp" {
		t.Errorf("Suite = %q, want %q", cfg.Suite, "myapp")
	}
	if cfg.TestConfig == nil {
		t.Fatal("TestConfig is nil")
	}
	if len(cfg.TestConfig.Tags) != 2 {
		t.Fatalf("Tags length = %d, want 2", len(cfg.TestConfig.Tags))
	}
	if cfg.TestConfig.Tags[0] != "smoke" {
		t.Errorf("Tags[0] = %q, want %q", cfg.TestConfig.Tags[0], "smoke")
	}
	if cfg.TestConfig.Timeout != 30 {
		t.Errorf("Timeout = %d, want 30", cfg.TestConfig.Timeout)
	}
	if cfg.TestConfig.StepTimeout != 10 {
		t.Errorf("StepTimeout = %d, want 10", cfg.TestConfig.StepTimeout)
	}
	if cfg.TestConfig.OnFail != "continue" {
		t.Errorf("OnFail = %q, want %q", cfg.TestConfig.OnFail, "continue")
	}

	// Check nodes with assertions
	if len(cfg.Nodes) != 2 {
		t.Fatalf("Nodes length = %d, want 2", len(cfg.Nodes))
	}

	node0 := cfg.Nodes[0]
	if node0.Assert == nil {
		t.Fatal("Nodes[0].Assert is nil")
	}
	if node0.Assert.Type != "contains" {
		t.Errorf("Nodes[0].Assert.Type = %q, want %q", node0.Assert.Type, "contains")
	}
	if node0.Assert.Source != "output" {
		t.Errorf("Nodes[0].Assert.Source = %q, want %q", node0.Assert.Source, "output")
	}
	if node0.Assert.Expected != "Sign In" {
		t.Errorf("Nodes[0].Assert.Expected = %q, want %q", node0.Assert.Expected, "Sign In")
	}

	node1 := cfg.Nodes[1]
	if node1.Assert == nil {
		t.Fatal("Nodes[1].Assert is nil")
	}
	if node1.Assert.Type != "regex" {
		t.Errorf("Nodes[1].Assert.Type = %q, want %q", node1.Assert.Type, "regex")
	}
	if node1.Assert.OnFail != "triage" {
		t.Errorf("Nodes[1].Assert.OnFail = %q, want %q", node1.Assert.OnFail, "triage")
	}
	// Source should default to empty (runner treats as "output")
	if node1.Assert.Source != "" {
		t.Errorf("Nodes[1].Assert.Source = %q, want empty", node1.Assert.Source)
	}
}

func TestAgentConfigRegularFlowUnchanged(t *testing.T) {
	// Verify that regular flows (no type field) still parse correctly
	input := `
description: "Regular flow"
nodes:
  - name: start
    type: input
    prompt: "Enter name"
flow:
  - from: start
    to: end
`
	var cfg AgentConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if cfg.Type != "" {
		t.Errorf("Type = %q, want empty", cfg.Type)
	}
	if cfg.Suite != "" {
		t.Errorf("Suite = %q, want empty", cfg.Suite)
	}
	if cfg.SuiteConfig != nil {
		t.Error("SuiteConfig should be nil for regular flows")
	}
	if cfg.TestConfig != nil {
		t.Error("TestConfig should be nil for regular flows")
	}
	if cfg.Nodes[0].Assert != nil {
		t.Error("Assert should be nil for regular flow nodes")
	}
}

func TestReadyCheckPortParsing(t *testing.T) {
	input := `
type: port
host: "127.0.0.1"
port: 5432
timeout: 15
interval: 1
`
	var rc ReadyCheck
	if err := yaml.Unmarshal([]byte(input), &rc); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if rc.Type != "port" {
		t.Errorf("Type = %q, want %q", rc.Type, "port")
	}
	if rc.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want %q", rc.Host, "127.0.0.1")
	}
	if rc.Port != 5432 {
		t.Errorf("Port = %d, want 5432", rc.Port)
	}
	if rc.Timeout != 15 {
		t.Errorf("Timeout = %d, want 15", rc.Timeout)
	}
}

func TestReadyCheckOutputContainsParsing(t *testing.T) {
	input := `
type: output_contains
pattern: "Server listening on"
timeout: 20
`
	var rc ReadyCheck
	if err := yaml.Unmarshal([]byte(input), &rc); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if rc.Type != "output_contains" {
		t.Errorf("Type = %q, want %q", rc.Type, "output_contains")
	}
	if rc.Pattern != "Server listening on" {
		t.Errorf("Pattern = %q, want %q", rc.Pattern, "Server listening on")
	}
}
