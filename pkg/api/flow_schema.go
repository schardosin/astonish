package api

// FlowSchema contains the schema documentation for AI to understand flow structure
const FlowSchema = `
# Astonish Agent Flow Schema

## Overview
An agent flow is defined in YAML with nodes (processing steps) and flow (edges connecting them).

## Basic Structure
` + "```yaml" + `
name: agent_name
description: What this agent does
model: gemini-2.0-flash  # or gpt-4o, claude-3-5-sonnet, etc.

nodes:
  - name: node_name
    type: llm|input|tool|output|update_state
    # type-specific fields...

flow:
  - from: START
    to: first_node
  - from: first_node
    to: second_node
  - from: last_node
    to: END
` + "```" + `

## Node Types

### 1. LLM Node
AI processing with optional tool use.
` + "```yaml" + `
- name: analyze_request
  type: llm
  system_prompt: "You are a helpful assistant..."
  prompt: "Analyze this: {{ user_input }}"
  tools_selection:
    - tool_name_1
    - tool_name_2
  output_as_state:
    key_name: "{{ output }}"  # Store output in state
` + "```" + `

### 2. Input Node
Collect user input into state.
` + "```yaml" + `
- name: get_user_query
  type: input
  variable_name: user_query  # State key to store input
` + "```" + `

### 3. Tool Node
Execute a specific tool directly (without LLM).
` + "```yaml" + `
- name: run_shell
  type: tool
  tool: shell_command  # Tool name
  args:
    command: "ls -la"
` + "```" + `

### 4. Output Node
Display text to user.
` + "```yaml" + `
- name: show_result
  type: output
  text: "Result: {{ result }}"
` + "```" + `

### 5. Update State Node
Modify state variables.
` + "```yaml" + `
- name: set_defaults
  type: update_state
  updates:
    processed: "true"
    count: "{{ count + 1 }}"
` + "```" + `

## Flow Edges

### Simple Edge
` + "```yaml" + `
- from: node_a
  to: node_b
` + "```" + `

### Conditional Edges
` + "```yaml" + `
- from: decision_node
  edges:
    - to: yes_path
      condition: "lambda x: x['decision'] == 'yes'"
    - to: no_path
      condition: "lambda x: x['decision'] == 'no'"
` + "```" + `

### Loop (Back-edge)
` + "```yaml" + `
- from: process_node
  edges:
    - to: START  # or any earlier node
      condition: "lambda x: x['continue'] == 'yes'"
    - to: END
      condition: "lambda x: x['continue'] == 'no'"
` + "```" + `

## State & Templating
- Use {{ variable }} to reference state
- {{ output }} refers to previous node's output
- State persists across nodes

## Rules
1. Flow must start from START node
2. Flow must end at END node
3. All node names must be unique
4. Edge targets must reference valid node names or START/END
`

// GetFlowSchema returns the schema as a string for AI context
func GetFlowSchema() string {
	return FlowSchema
}
