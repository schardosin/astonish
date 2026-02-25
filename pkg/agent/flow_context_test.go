package agent

import (
	"strings"
	"testing"
)

func TestBuildExecutionPlan_BasicFlow(t *testing.T) {
	b := &FlowContextBuilder{}

	flowYAML := `
name: proxmox_vm_overview
description: Check Proxmox VM and container status

nodes:
  - name: get_connection_info
    type: input
    prompt: "Enter SSH connection details:"
    output_model:
      ssh_user: str
      ssh_ip: str
  - name: fetch_resources
    type: llm
    system: |
      You are a Proxmox assistant.
      Use shell_command to SSH and run pvesh.
    prompt: "Connect to {ssh_ip} as {ssh_user} and get resources"
    tools: true
    tools_selection:
      - shell_command
  - name: format_output
    type: llm
    system: |
      Format the data into a markdown table.
      Include columns: VMID, Name, Status, CPUs, RAM, Disk.
    prompt: "Format the resources"

flow:
  - from: START
    to: get_connection_info
  - from: get_connection_info
    to: fetch_resources
  - from: fetch_resources
    to: format_output
  - from: format_output
    to: END
`

	plan := b.BuildExecutionPlan(flowYAML, "proxmox_vm_overview", "")

	if plan == "" {
		t.Fatal("expected non-empty plan")
	}

	// Should mention the flow description
	if !strings.Contains(plan, "Check Proxmox VM and container status") {
		t.Errorf("plan should mention flow description:\n%s", plan)
	}

	// Should have 3 steps
	if !strings.Contains(plan, "Step 1") || !strings.Contains(plan, "Step 2") || !strings.Contains(plan, "Step 3") {
		t.Errorf("plan should have 3 steps:\n%s", plan)
	}

	// Step 1 should mention input parameters
	if !strings.Contains(plan, "ssh_user") || !strings.Contains(plan, "ssh_ip") {
		t.Errorf("plan should list input parameters:\n%s", plan)
	}

	// Step 2 should mention shell_command tool
	if !strings.Contains(plan, "shell_command") {
		t.Errorf("plan should mention tools:\n%s", plan)
	}

	// Step 3 should include format instructions
	if !strings.Contains(plan, "markdown table") {
		t.Errorf("plan should include format instructions:\n%s", plan)
	}
}

func TestBuildExecutionPlan_WithMemory(t *testing.T) {
	b := &FlowContextBuilder{}

	flowYAML := `
name: test_flow
description: Test flow

nodes:
  - name: get_info
    type: input
    prompt: "Enter connection details:"
    output_model:
      ssh_user: str
      ssh_ip: str
  - name: run_cmd
    type: llm
    prompt: "Run command"
    tools: true
    tools_selection:
      - shell_command

flow:
  - from: START
    to: get_info
  - from: get_info
    to: run_cmd
  - from: run_cmd
    to: END
`

	memoryContent := `## Infrastructure
- SSH user: root
- SSH IP: 192.168.1.200
- Auth method: SSH key (passwordless)
`

	plan := b.BuildExecutionPlan(flowYAML, "test_flow", memoryContent)

	// Should resolve ssh_user from memory
	if !strings.Contains(plan, "from memory") {
		t.Errorf("plan should resolve parameters from memory:\n%s", plan)
	}
}

func TestBuildExecutionPlan_InvalidYAML(t *testing.T) {
	b := &FlowContextBuilder{}
	plan := b.BuildExecutionPlan("not: valid: yaml: [", "test", "")
	if plan != "" {
		t.Errorf("expected empty plan for invalid YAML, got: %s", plan)
	}
}

func TestBuildExecutionPlan_EmptyYAML(t *testing.T) {
	b := &FlowContextBuilder{}
	plan := b.BuildExecutionPlan("", "test", "")
	if plan != "" {
		t.Errorf("expected empty plan for empty YAML, got: %s", plan)
	}
}

func TestResolveFromMemory_MatchesKeywords(t *testing.T) {
	mem := `## Infrastructure
- SSH user: root
- SSH IP: 192.168.1.200
- Server port: 8006
`

	tests := []struct {
		field    string
		expected string
	}{
		{"ssh_user", "root"},
		{"ssh_ip", "192.168.1.200"},
		{"server_port", "8006"},
		{"unknown_field", ""},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			val := resolveFromMemory(tt.field, mem)
			if val != tt.expected {
				t.Errorf("resolveFromMemory(%q) = %q, want %q", tt.field, val, tt.expected)
			}
		})
	}
}

func TestResolveFromMemory_EmptyMemory(t *testing.T) {
	val := resolveFromMemory("ssh_user", "")
	if val != "" {
		t.Errorf("expected empty for empty memory, got: %q", val)
	}
}

func TestWalkFlow_LinearPath(t *testing.T) {
	b := &FlowContextBuilder{}
	edges := []flowContextEdge{
		{From: "START", To: "a"},
		{From: "a", To: "b"},
		{From: "b", To: "c"},
		{From: "c", To: "END"},
	}
	nodeMap := map[string]*flowContextNode{
		"a": {Name: "a", Type: "input"},
		"b": {Name: "b", Type: "llm"},
		"c": {Name: "c", Type: "llm"},
	}

	ordered := b.walkFlow(edges, nodeMap)
	if len(ordered) != 3 {
		t.Fatalf("expected 3 nodes, got %d: %v", len(ordered), ordered)
	}
	if ordered[0] != "a" || ordered[1] != "b" || ordered[2] != "c" {
		t.Errorf("expected [a, b, c], got %v", ordered)
	}
}

func TestExtractKeyInstruction_SkipsPreamble(t *testing.T) {
	system := `You are a Proxmox assistant.
Your job is to help with server management.
Use shell_command to SSH and run pvesh commands.
Always format output as markdown.`

	instruction := extractKeyInstruction(system)
	if !strings.Contains(instruction, "shell_command") {
		t.Errorf("should skip preamble and return actionable line, got: %q", instruction)
	}
}

func TestSummarizePrompt_Truncates(t *testing.T) {
	long := strings.Repeat("x", 300)
	result := summarizePrompt(long)
	if len(result) > 210 { // 200 + "..."
		t.Errorf("expected truncation, got length %d", len(result))
	}
	if !strings.HasSuffix(result, "...") {
		t.Errorf("expected ... suffix, got: %q", result)
	}
}

func TestSummarizePrompt_CollapsesNewlines(t *testing.T) {
	prompt := "Line one\nLine two\nLine three"
	result := summarizePrompt(prompt)
	if strings.Contains(result, "\n") {
		t.Errorf("should collapse newlines, got: %q", result)
	}
}

func TestBuildExecutionPlan_EscapesCurlyBraces(t *testing.T) {
	b := &FlowContextBuilder{}

	flowYAML := `
name: test_flow
description: Test flow with variables

nodes:
  - name: get_info
    type: input
    prompt: "Enter details:"
    output_model:
      ssh_user: str
      ssh_ip: str
  - name: run_cmd
    type: llm
    system: "SSH as {ssh_user} to {ssh_ip} and run pvesh."
    prompt: "Connect to {ssh_ip} as {ssh_user}"
    tools: true
    tools_selection:
      - shell_command
  - name: format
    type: llm
    system: "Format {raw_resources_json} into a table."
    prompt: "Format the output"

flow:
  - from: START
    to: get_info
  - from: get_info
    to: run_cmd
  - from: run_cmd
    to: format
  - from: format
    to: END
`

	plan := b.BuildExecutionPlan(flowYAML, "test_flow", "")

	if plan == "" {
		t.Fatal("expected non-empty plan")
	}

	// Must NOT contain {variable} patterns (ADK would try to resolve them)
	if strings.Contains(plan, "{ssh_user}") || strings.Contains(plan, "{ssh_ip}") || strings.Contains(plan, "{raw_resources_json}") {
		t.Errorf("plan must not contain {variable} patterns, ADK will crash:\n%s", plan)
	}

	// Should contain escaped <variable> patterns instead
	if !strings.Contains(plan, "<ssh_user>") || !strings.Contains(plan, "<ssh_ip>") {
		t.Errorf("plan should contain <variable> escaped patterns:\n%s", plan)
	}
}
