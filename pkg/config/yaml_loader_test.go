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

	if cfg.Type != "drill_suite" {
		t.Errorf("Type = %q, want %q", cfg.Type, "drill_suite")
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

	if cfg.Type != "drill" {
		t.Errorf("Type = %q, want %q", cfg.Type, "drill")
	}
	if cfg.Suite != "myapp" {
		t.Errorf("Suite = %q, want %q", cfg.Suite, "myapp")
	}
	if cfg.DrillConfig == nil {
		t.Fatal("DrillConfig is nil")
	}
	if len(cfg.DrillConfig.Tags) != 2 {
		t.Fatalf("Tags length = %d, want 2", len(cfg.DrillConfig.Tags))
	}
	if cfg.DrillConfig.Tags[0] != "smoke" {
		t.Errorf("Tags[0] = %q, want %q", cfg.DrillConfig.Tags[0], "smoke")
	}
	if cfg.DrillConfig.Timeout != 30 {
		t.Errorf("Timeout = %d, want 30", cfg.DrillConfig.Timeout)
	}
	if cfg.DrillConfig.StepTimeout != 10 {
		t.Errorf("StepTimeout = %d, want 10", cfg.DrillConfig.StepTimeout)
	}
	if cfg.DrillConfig.OnFail != "continue" {
		t.Errorf("OnFail = %q, want %q", cfg.DrillConfig.OnFail, "continue")
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
	if cfg.DrillConfig != nil {
		t.Error("DrillConfig should be nil for regular flows")
	}
	if cfg.Nodes[0].Assert != nil {
		t.Error("Assert should be nil for regular flow nodes")
	}
}

func TestMultiServiceSuiteParsing(t *testing.T) {
	input := `
description: "Full-stack E2E Tests"
type: test_suite
suite_config:
  template: "@fullstack"
  base_url: "http://localhost:3000"
  environment:
    NODE_ENV: test
  services:
    - name: database
      setup: "pg_ctl start -D /var/lib/postgresql/data"
      ready_check:
        type: port
        port: 5432
        timeout: 15
      teardown: "pg_ctl stop -D /var/lib/postgresql/data"
      environment:
        POSTGRES_DB: testdb
    - name: backend
      setup: "cd /workspace/api && npm start &"
      ready_check:
        type: http
        url: "http://localhost:8080/health"
        timeout: 30
        interval: 2
      teardown: "pkill -f 'npm start'"
      environment:
        DATABASE_URL: "postgres://localhost:5432/testdb"
    - name: frontend
      setup: "cd /workspace/web && npm run dev &"
      ready_check:
        type: output_contains
        pattern: "ready in"
        timeout: 20
      teardown: "pkill -f 'npm run dev'"
`
	var cfg AgentConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if cfg.Type != "drill_suite" {
		t.Errorf("Type = %q, want %q", cfg.Type, "drill_suite")
	}
	sc := cfg.SuiteConfig
	if sc == nil {
		t.Fatal("SuiteConfig is nil")
	}
	if sc.Template != "@fullstack" {
		t.Errorf("Template = %q, want %q", sc.Template, "@fullstack")
	}
	if sc.BaseURL != "http://localhost:3000" {
		t.Errorf("BaseURL = %q, want %q", sc.BaseURL, "http://localhost:3000")
	}
	if sc.Environment["NODE_ENV"] != "test" {
		t.Errorf("Environment[NODE_ENV] = %q, want %q", sc.Environment["NODE_ENV"], "test")
	}

	// Should have no legacy setup/teardown
	if len(sc.Setup) != 0 {
		t.Errorf("Legacy Setup should be empty, got %d entries", len(sc.Setup))
	}
	if len(sc.Teardown) != 0 {
		t.Errorf("Legacy Teardown should be empty, got %d entries", len(sc.Teardown))
	}
	if sc.ReadyCheck != nil {
		t.Error("Legacy ReadyCheck should be nil")
	}

	// Validate services
	if len(sc.Services) != 3 {
		t.Fatalf("Services length = %d, want 3", len(sc.Services))
	}

	// Database service
	db := sc.Services[0]
	if db.Name != "database" {
		t.Errorf("Services[0].Name = %q, want %q", db.Name, "database")
	}
	if db.Setup != "pg_ctl start -D /var/lib/postgresql/data" {
		t.Errorf("Services[0].Setup = %q, unexpected", db.Setup)
	}
	if db.ReadyCheck == nil {
		t.Fatal("Services[0].ReadyCheck is nil")
	}
	if db.ReadyCheck.Type != "port" {
		t.Errorf("Services[0].ReadyCheck.Type = %q, want %q", db.ReadyCheck.Type, "port")
	}
	if db.ReadyCheck.Port != 5432 {
		t.Errorf("Services[0].ReadyCheck.Port = %d, want 5432", db.ReadyCheck.Port)
	}
	if db.Teardown != "pg_ctl stop -D /var/lib/postgresql/data" {
		t.Errorf("Services[0].Teardown = %q, unexpected", db.Teardown)
	}
	if db.Environment["POSTGRES_DB"] != "testdb" {
		t.Errorf("Services[0].Environment[POSTGRES_DB] = %q, want %q", db.Environment["POSTGRES_DB"], "testdb")
	}

	// Backend service
	be := sc.Services[1]
	if be.Name != "backend" {
		t.Errorf("Services[1].Name = %q, want %q", be.Name, "backend")
	}
	if be.ReadyCheck == nil || be.ReadyCheck.Type != "http" {
		t.Error("Services[1].ReadyCheck should be http type")
	}
	if be.ReadyCheck.URL != "http://localhost:8080/health" {
		t.Errorf("Services[1].ReadyCheck.URL = %q, unexpected", be.ReadyCheck.URL)
	}
	if be.ReadyCheck.Interval != 2 {
		t.Errorf("Services[1].ReadyCheck.Interval = %d, want 2", be.ReadyCheck.Interval)
	}

	// Frontend service
	fe := sc.Services[2]
	if fe.Name != "frontend" {
		t.Errorf("Services[2].Name = %q, want %q", fe.Name, "frontend")
	}
	if fe.ReadyCheck == nil || fe.ReadyCheck.Type != "output_contains" {
		t.Error("Services[2].ReadyCheck should be output_contains type")
	}
	if fe.ReadyCheck.Pattern != "ready in" {
		t.Errorf("Services[2].ReadyCheck.Pattern = %q, want %q", fe.ReadyCheck.Pattern, "ready in")
	}
}

func TestMultiServiceBackwardCompat(t *testing.T) {
	// Legacy single-service suite should still parse with empty Services
	input := `
description: "Legacy Suite"
type: test_suite
suite_config:
  setup:
    - "npm start &"
  ready_check:
    type: http
    url: "http://localhost:3000"
  teardown:
    - "pkill -f npm"
`
	var cfg AgentConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	sc := cfg.SuiteConfig
	if sc == nil {
		t.Fatal("SuiteConfig is nil")
	}
	if len(sc.Services) != 0 {
		t.Errorf("Services should be empty for legacy suite, got %d", len(sc.Services))
	}
	if len(sc.Setup) != 1 {
		t.Fatalf("Legacy Setup length = %d, want 1", len(sc.Setup))
	}
	if sc.ReadyCheck == nil {
		t.Fatal("Legacy ReadyCheck should not be nil")
	}
	if sc.ReadyCheck.Type != "http" {
		t.Errorf("ReadyCheck.Type = %q, want %q", sc.ReadyCheck.Type, "http")
	}
}

func TestServiceConfigMinimal(t *testing.T) {
	// Service with only name and setup (no ready check, no teardown, no env)
	input := `
description: "Minimal Service Suite"
type: test_suite
suite_config:
  services:
    - name: worker
      setup: "python worker.py &"
`
	var cfg AgentConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	sc := cfg.SuiteConfig
	if len(sc.Services) != 1 {
		t.Fatalf("Services length = %d, want 1", len(sc.Services))
	}
	svc := sc.Services[0]
	if svc.Name != "worker" {
		t.Errorf("Name = %q, want %q", svc.Name, "worker")
	}
	if svc.Setup != "python worker.py &" {
		t.Errorf("Setup = %q, unexpected", svc.Setup)
	}
	if svc.ReadyCheck != nil {
		t.Error("ReadyCheck should be nil for minimal service")
	}
	if svc.Teardown != "" {
		t.Errorf("Teardown should be empty, got %q", svc.Teardown)
	}
	if len(svc.Environment) != 0 {
		t.Errorf("Environment should be empty, got %d entries", len(svc.Environment))
	}
}

func TestBaseURLParsing(t *testing.T) {
	input := `
description: "Browser Test Suite"
type: test_suite
suite_config:
  base_url: "http://localhost:5173"
  setup:
    - "npm run dev &"
`
	var cfg AgentConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if cfg.SuiteConfig.BaseURL != "http://localhost:5173" {
		t.Errorf("BaseURL = %q, want %q", cfg.SuiteConfig.BaseURL, "http://localhost:5173")
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
