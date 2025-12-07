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

### 1. LLM Node (PREFERRED for tool usage)
AI processing with optional tool use. Use output_model to store results in state.
IMPORTANT: To use tools, set tools: true and tools_selection with the tool names.
` + "```yaml" + `
# LLM without tools
- name: analyze_request
  type: llm
  system_prompt: "You are a helpful assistant..."
  prompt: "Analyze this: {user_input}"
  output_model:
    analysis_result: str

# LLM with tools - THIS IS THE PREFERRED WAY TO USE TOOLS
- name: search_and_summarize
  type: llm
  system_prompt: "You are a helpful assistant. Use the search tool to find information."
  prompt: "Search for information about: {query}"
  tools: true
  tools_selection:
    - tavily-search  # Use exact tool name from Available Tools
  output_model:
    search_result: str
` + "```" + `

### 2. Input Node
Collect user input. output_model is REQUIRED to store input in state.
` + "```yaml" + `
# Free text input
- name: get_user_text
  type: input
  prompt: "Enter your text:"
  output_model:
    user_text: str  # REQUIRED - stores input in state

# Multiple choice input
- name: get_user_choice
  type: input
  prompt: |
    Please select an option:
    1. Option one
    2. Option two
  output_model:
    user_choice: str  # REQUIRED
  options:
    - "1"
    - "2"
` + "```" + `

### 3. Tool Node (RARELY USED)
Execute a tool directly WITHOUT LLM intelligence. Only use when you need deterministic tool execution.
In most cases, prefer LLM node with tools: true instead.
` + "```yaml" + `
- name: run_fixed_command
  type: tool
  tools_selection:
    - shell_command
  args:
    command: "ls -la"
  output_model:
    shell_result: str
` + "```" + `

### 4. Output Node
Display messages to user. Use user_message array with strings and state variable names.
` + "```yaml" + `
- name: show_result
  type: output
  user_message:
    - "Here is the result:"
    - result_variable  # Reference state variable without braces
` + "```" + `

### 5. Update State Node
Modify state variables.
` + "```yaml" + `
- name: set_defaults
  type: update_state
  updates:
    processed: "true"
    counter: "1"
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
- Use {variable_name} to reference state (single braces, no spaces)
- Data flows through nodes via output_model which saves to state
- Access previous node outputs from state keys defined in output_model

## Rules
1. Flow must start from START node
2. Flow must end at END node
3. All node names must be unique
4. Edge targets must reference valid node names or START/END
5. Use output_model on LLM/tool nodes to pass data to later nodes
`

// GetFlowSchema returns the schema as a string for AI context
func GetFlowSchema() string {
	return FlowSchema
}

