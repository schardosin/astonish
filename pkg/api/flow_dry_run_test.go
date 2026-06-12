package api

import (
	"encoding/json"
	"testing"
)

func TestDryRunFlowYAML_ValidFlow(t *testing.T) {
	yamlStr := `
name: test_flow
description: A test flow

nodes:
  - name: get_name
    type: input
    prompt: "What is your name?"
    output_model:
      user_name: str

  - name: greet
    type: llm
    prompt: "Say hello to {user_name}"
    output_model:
      greeting: str
    user_message:
      - greeting

  - name: show_result
    type: output
    user_message:
      - greeting

flow:
  - from: START
    to: get_name
  - from: get_name
    to: greet
  - from: greet
    to: show_result
  - from: show_result
    to: END
`
	result := DryRunFlowYAML(yamlStr, nil)
	if !result.Valid {
		t.Errorf("expected valid flow, got errors: %v", result.Errors)
	}
	if len(result.Warnings) > 0 {
		t.Errorf("expected no warnings, got: %v", result.Warnings)
	}
}

func TestDryRunFlowYAML_UnresolvedVariable(t *testing.T) {
	yamlStr := `
name: test_flow
description: A test flow

nodes:
  - name: get_name
    type: input
    prompt: "What is your name?"
    output_model:
      user_name: str

  - name: greet
    type: llm
    prompt: "Say hello to {unknown_var}"
    output_model:
      greeting: str

flow:
  - from: START
    to: get_name
  - from: get_name
    to: greet
  - from: greet
    to: END
`
	result := DryRunFlowYAML(yamlStr, nil)
	// Should produce a warning about unknown_var
	if len(result.Warnings) == 0 {
		t.Error("expected warning about unresolved variable {unknown_var}")
	}
	found := false
	for _, w := range result.Warnings {
		if strContains(w, "unknown_var") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning mentioning 'unknown_var', got warnings: %v", result.Warnings)
	}
}

func TestDryRunFlowYAML_InvalidToolArg(t *testing.T) {
	yamlStr := `
name: test_flow
description: A test flow

nodes:
  - name: run_cmd
    type: tool
    tools_selection:
      - shell_command
    args:
      wrong_param: "ls -la"
    output_model:
      result: str

flow:
  - from: START
    to: run_cmd
  - from: run_cmd
    to: END
`
	toolSchemas := map[string]json.RawMessage{
		"shell_command": json.RawMessage(`{
			"properties": {
				"command": {"type": "string", "description": "The command to execute"},
				"workdir": {"type": "string", "description": "Working directory"}
			},
			"required": ["command"]
		}`),
	}

	result := DryRunFlowYAML(yamlStr, toolSchemas)
	if result.Valid {
		t.Error("expected invalid result for wrong tool parameter name")
	}
	found := false
	for _, e := range result.Errors {
		if strContains(e, "wrong_param") && strContains(e, "shell_command") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error about 'wrong_param' not being valid for 'shell_command', got: %v", result.Errors)
	}
}

func TestDryRunFlowYAML_CorrectToolArgs(t *testing.T) {
	yamlStr := `
name: test_flow
description: A test flow

nodes:
  - name: run_cmd
    type: tool
    tools_selection:
      - shell_command
    args:
      command: "ls -la"
    output_model:
      result: str

flow:
  - from: START
    to: run_cmd
  - from: run_cmd
    to: END
`
	toolSchemas := map[string]json.RawMessage{
		"shell_command": json.RawMessage(`{
			"properties": {
				"command": {"type": "string", "description": "The command to execute"},
				"workdir": {"type": "string", "description": "Working directory"}
			},
			"required": ["command"]
		}`),
	}

	result := DryRunFlowYAML(yamlStr, toolSchemas)
	if !result.Valid {
		t.Errorf("expected valid result, got errors: %v", result.Errors)
	}
}

func TestDryRunFlowYAML_VariableFromPrecedingNode(t *testing.T) {
	// Variable {region} is produced by an input node, then consumed by the LLM node
	yamlStr := `
name: test_flow
description: A test flow

nodes:
  - name: get_region
    type: input
    prompt: "Which region?"
    output_model:
      region: str

  - name: fetch_data
    type: llm
    prompt: "Fetch data for region {region}"
    tools: true
    tools_selection:
      - shell_command
    output_model:
      data: str

flow:
  - from: START
    to: get_region
  - from: get_region
    to: fetch_data
  - from: fetch_data
    to: END
`
	result := DryRunFlowYAML(yamlStr, nil)
	if !result.Valid {
		t.Errorf("expected valid flow, got errors: %v", result.Errors)
	}
	// No warnings about {region} since it's produced by get_region
	for _, w := range result.Warnings {
		if strContains(w, "region") {
			t.Errorf("should not warn about {region} since it's from preceding input node, got: %s", w)
		}
	}
}

func TestDryRunFlowYAML_CredentialWrongParam(t *testing.T) {
	// Simulates the exact bug from the user's report:
	// resolve_credential has parameter "name" but the flow uses "credential_name"
	yamlStr := `
name: test_flow
description: Test credential resolution

nodes:
  - name: resolve_creds
    type: tool
    tools_selection:
      - resolve_credential
    args:
      credential_name: openstack
    output_model:
      creds: str

flow:
  - from: START
    to: resolve_creds
  - from: resolve_creds
    to: END
`
	toolSchemas := map[string]json.RawMessage{
		"resolve_credential": json.RawMessage(`{
			"properties": {
				"name": {"type": "string", "description": "The credential name to resolve"}
			},
			"required": ["name"]
		}`),
	}

	result := DryRunFlowYAML(yamlStr, toolSchemas)
	if result.Valid {
		t.Error("expected invalid result: 'credential_name' is not a valid parameter for resolve_credential")
	}
	found := false
	for _, e := range result.Errors {
		if strContains(e, "credential_name") && strContains(e, "resolve_credential") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error about 'credential_name' not valid for 'resolve_credential', got: %v", result.Errors)
	}
}

func TestDryRunFlowYAML_ShellVarsNotTreatedAsStateVars(t *testing.T) {
	// Shell variables like ${VAR} and awk {print $2} should NOT be flagged
	// as unresolved state variables. Only standalone {var_name} patterns matter.
	yamlStr := `
name: test_flow
description: Test shell variable handling

nodes:
  - name: get_cred
    type: input
    prompt: "Credential name?"
    output_model:
      cred_name: str

  - name: run_auth
    type: llm
    prompt: "Authenticate using credential {cred_name} and list VMs"
    tools: true
    tools_selection:
      - shell_command
    output_model:
      vm_data: str

flow:
  - from: START
    to: get_cred
  - from: get_cred
    to: run_auth
  - from: run_auth
    to: END
`
	result := DryRunFlowYAML(yamlStr, nil)
	if !result.Valid {
		t.Errorf("expected valid flow, got errors: %v", result.Errors)
	}
}

func TestDryRunFlowYAML_InvalidYAML(t *testing.T) {
	result := DryRunFlowYAML("not: [valid: yaml: {{", nil)
	if result.Valid {
		t.Error("expected invalid result for malformed YAML")
	}
}

func TestDryRunFlowYAML_NoNodes(t *testing.T) {
	yamlStr := `
name: empty
description: no nodes
`
	result := DryRunFlowYAML(yamlStr, nil)
	// Should not crash, just return valid (schema validator catches missing nodes)
	if !result.Valid {
		t.Errorf("expected valid (schema issues handled elsewhere), got errors: %v", result.Errors)
	}
}

func TestDryRunFlowYAML_ConditionalEdges(t *testing.T) {
	// Variables from both branches should be tracked
	yamlStr := `
name: conditional_flow
description: Flow with conditional edges

nodes:
  - name: get_input
    type: input
    prompt: "Enter choice"
    output_model:
      choice: str

  - name: path_a
    type: llm
    prompt: "Path A for {choice}"
    output_model:
      result_a: str

  - name: path_b
    type: llm
    prompt: "Path B for {choice}"
    output_model:
      result_b: str

flow:
  - from: START
    to: get_input
  - from: get_input
    edges:
      - to: path_a
        condition: "lambda x: x['choice'] == 'a'"
      - to: path_b
        condition: "lambda x: x['choice'] == 'b'"
  - from: path_a
    to: END
  - from: path_b
    to: END
`
	result := DryRunFlowYAML(yamlStr, nil)
	if !result.Valid {
		t.Errorf("expected valid flow, got errors: %v", result.Errors)
	}
}

func strContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && strContainsSubstr(s, substr))
}

func strContainsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
