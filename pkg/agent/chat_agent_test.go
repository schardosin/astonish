package agent

import (
	"context"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestExtractInputParams_NilTrace(t *testing.T) {
	ca := &ChatAgent{}
	params := ca.extractInputParams(nil, "nodes: []", nil)
	if params != nil {
		t.Errorf("expected nil for nil trace, got %v", params)
	}
}

func TestExtractInputParams_EmptyYAML(t *testing.T) {
	ca := &ChatAgent{}
	trace := NewExecutionTrace("test")
	trace.Finalize()
	params := ca.extractInputParams(nil, "", trace)
	if params != nil {
		t.Errorf("expected nil for empty YAML, got %v", params)
	}
}

func TestExtractInputParams_NoDistiller(t *testing.T) {
	ca := &ChatAgent{}
	trace := NewExecutionTrace("test")
	trace.Finalize()
	// No FlowDistiller set — should return nil gracefully
	params := ca.extractInputParams(nil, `
nodes:
  - name: get_host
    type: input
    prompt: "Enter host:"
    output_model:
      host: str
`, trace)
	if params != nil {
		t.Errorf("expected nil when no distiller, got %v", params)
	}
}

func TestExtractInputParams_NoInputNodes(t *testing.T) {
	ca := &ChatAgent{
		FlowDistiller: &FlowDistiller{
			LLM: func(ctx context.Context, prompt string) (string, error) {
				t.Fatal("LLM should not be called when there are no input nodes")
				return "", nil
			},
		},
	}
	trace := NewExecutionTrace("test")
	trace.Finalize()
	params := ca.extractInputParams(nil, `
nodes:
  - name: do_stuff
    type: llm
    prompt: "Do something"
`, trace)
	if params != nil {
		t.Errorf("expected nil for no input nodes, got %v", params)
	}
}

func TestExtractInputParams_ParsesLLMResponse(t *testing.T) {
	// Mock the LLM to return known parameter values
	ca := &ChatAgent{
		FlowDistiller: &FlowDistiller{
			LLM: func(ctx context.Context, prompt string) (string, error) {
				return "get_connection_info=192.168.1.200\nget_ssh_user=root\n", nil
			},
		},
	}

	trace := NewExecutionTrace("show proxmox VMs")
	trace.RecordStep("shell_command", map[string]any{
		"command": `ssh root@192.168.1.200 "qm list"`,
	}, nil, nil)
	trace.Finalize()

	params := ca.extractInputParams(nil, `
nodes:
  - name: get_connection_info
    type: input
    prompt: "Enter IP:"
    output_model:
      server_ip: str
  - name: get_ssh_user
    type: input
    prompt: "Enter user:"
    output_model:
      ssh_user: str
  - name: fetch
    type: llm
    prompt: "SSH as {ssh_user} to {server_ip}"
    tools: true
`, trace)

	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d: %v", len(params), params)
	}

	paramMap := make(map[string]string)
	for _, p := range params {
		parts := splitFirst(p, "=")
		paramMap[parts[0]] = parts[1]
	}

	if paramMap["get_connection_info"] != "192.168.1.200" {
		t.Errorf("expected get_connection_info=192.168.1.200, got %s", paramMap["get_connection_info"])
	}
	if paramMap["get_ssh_user"] != "root" {
		t.Errorf("expected get_ssh_user=root, got %s", paramMap["get_ssh_user"])
	}
}

func TestExtractInputParams_IgnoresInvalidLLMResponse(t *testing.T) {
	ca := &ChatAgent{
		FlowDistiller: &FlowDistiller{
			LLM: func(ctx context.Context, prompt string) (string, error) {
				// LLM returns some correct and some garbage lines
				return "get_host=10.0.0.1\n# comment\ngarbage line\nunknown_param=nope\n", nil
			},
		},
	}

	trace := NewExecutionTrace("test")
	trace.RecordStep("some_tool", map[string]any{"x": "y"}, nil, nil)
	trace.Finalize()

	params := ca.extractInputParams(nil, `
nodes:
  - name: get_host
    type: input
    prompt: "Enter host:"
  - name: get_port
    type: input
    prompt: "Enter port:"
`, trace)

	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d: %v", len(params), params)
	}

	paramMap := make(map[string]string)
	for _, p := range params {
		parts := splitFirst(p, "=")
		paramMap[parts[0]] = parts[1]
	}

	if paramMap["get_host"] != "10.0.0.1" {
		t.Errorf("expected get_host=10.0.0.1, got %s", paramMap["get_host"])
	}
	// get_port was not in the LLM response — should fall back to <value>
	if paramMap["get_port"] != "<value>" {
		t.Errorf("expected get_port=<value>, got %s", paramMap["get_port"])
	}
}

func TestExtractInputParams_LLMPromptContainsTrace(t *testing.T) {
	// Verify that the prompt sent to the LLM includes the trace data and output_model fields
	var capturedPrompt string
	ca := &ChatAgent{
		FlowDistiller: &FlowDistiller{
			LLM: func(ctx context.Context, prompt string) (string, error) {
				capturedPrompt = prompt
				return "get_host=10.0.0.1\n", nil
			},
		},
	}

	trace := NewExecutionTrace("connect to my server")
	trace.RecordStep("shell_command", map[string]any{
		"command": "ssh root@10.0.0.1",
	}, nil, nil)
	trace.Finalize()

	ca.extractInputParams(nil, `
nodes:
  - name: get_host
    type: input
    prompt: "Enter host:"
    output_model:
      server_ip: str
`, trace)

	// The prompt should contain the trace tool name and args
	if capturedPrompt == "" {
		t.Fatal("LLM was not called")
	}
	if !containsAll(capturedPrompt, "shell_command", "ssh root@10.0.0.1", "get_host", "connect to my server") {
		t.Errorf("LLM prompt missing expected content:\n%s", capturedPrompt)
	}
	// The prompt should include output_model field names as context
	if !containsAll(capturedPrompt, "server_ip") {
		t.Errorf("LLM prompt should include output_model fields:\n%s", capturedPrompt)
	}
	// The prompt should include conciseness guidance
	if !containsAll(capturedPrompt, "SHORT", "EXACT LITERAL") {
		t.Errorf("LLM prompt should include conciseness instructions:\n%s", capturedPrompt)
	}
}

func TestExtractInputParams_OutputModelMultipleFields(t *testing.T) {
	// Verify output_model with multiple fields is included in the prompt
	var capturedPrompt string
	ca := &ChatAgent{
		FlowDistiller: &FlowDistiller{
			LLM: func(ctx context.Context, prompt string) (string, error) {
				capturedPrompt = prompt
				return "get_connection_info=root@192.168.1.200\n", nil
			},
		},
	}

	trace := NewExecutionTrace("show proxmox VMs")
	trace.RecordStep("shell_command", map[string]any{
		"command": `ssh root@192.168.1.200 "pvesh get /cluster/resources"`,
	}, nil, nil)
	trace.Finalize()

	params := ca.extractInputParams(nil, `
nodes:
  - name: get_connection_info
    type: input
    prompt: "Enter SSH connection details:"
    output_model:
      ssh_user: str
      ssh_ip: str
  - name: fetch
    type: llm
    prompt: "SSH to {ssh_ip}"
    tools: true
`, trace)

	if len(params) != 1 {
		t.Fatalf("expected 1 param, got %d: %v", len(params), params)
	}
	if params[0] != "get_connection_info=root@192.168.1.200" {
		t.Errorf("expected get_connection_info=root@192.168.1.200, got %s", params[0])
	}
	// Prompt should mention the output_model fields
	if !containsAll(capturedPrompt, "ssh_user", "ssh_ip") {
		t.Errorf("LLM prompt should include output_model field names:\n%s", capturedPrompt)
	}
}

func TestFlowYAML_ParsesInputNodes(t *testing.T) {
	// Verify the YAML parsing correctly identifies input nodes
	yamlStr := `
nodes:
  - name: get_ip
    type: input
    prompt: "Enter IP:"
    output_model:
      ip: str
  - name: process
    type: llm
    prompt: "Process {ip}"
  - name: get_user
    type: input
    prompt: "Enter user:"
    output_model:
      user: str
`
	var flow flowYAML
	if err := yaml.Unmarshal([]byte(yamlStr), &flow); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	var inputNames []string
	for _, node := range flow.Nodes {
		if node.Type == "input" {
			inputNames = append(inputNames, node.Name)
		}
	}

	if len(inputNames) != 2 {
		t.Fatalf("expected 2 input nodes, got %d: %v", len(inputNames), inputNames)
	}
	if inputNames[0] != "get_ip" || inputNames[1] != "get_user" {
		t.Errorf("expected [get_ip, get_user], got %v", inputNames)
	}
}

// containsAll checks if s contains all of the given substrings.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// splitFirst splits s on the first occurrence of sep.
func splitFirst(s, sep string) [2]string {
	idx := len(sep)
	for i := 0; i < len(s)-len(sep)+1; i++ {
		if s[i:i+len(sep)] == sep {
			idx = i
			break
		}
	}
	return [2]string{s[:idx], s[idx+len(sep):]}
}
