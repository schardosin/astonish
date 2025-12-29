---
title: Nodes
description: Understanding node types in Astonish flows
sidebar:
  order: 2
---

# Nodes

Nodes are the building blocks of flows. Each node performs a specific action and passes data to the next.

## Node Types Overview

| Type | Purpose | Key Properties |
|------|---------|----------------|
| **START** | Entry point | Automatic |
| **END** | Exit point | Automatic |
| **input** | Get user input | `prompt`, `options` |
| **llm** | Call AI model | `prompt`, `system`, `tools` |
| **tool** | Execute MCP tool | `tools_selection`, `args` |
| **output** | Display message | `user_message` |
| **update_state** | Modify variables | `source_variable`, `action` |

---

## START & END

Every flow has these automatically. You don't define them in YAML.

- **START** — First node, receives initial parameters
- **END** — Final node, flow completes here

---

## Input Node

Pauses execution to collect user input.

```yaml
- name: get_repo
  type: input
  prompt: 'Enter a GitHub repo (format: owner/repo):'
  output_model:
    repo: str
```

### With Static Options

Provide a list of fixed choices:

```yaml
- name: ask_continue
  type: input
  prompt: What next?
  output_model:
    choice: str
  options:
    - 'yes'
    - search web
    - 'no'
```

### With Dynamic Options

Reference a state variable containing a list:

```yaml
- name: select_branch
  type: input
  prompt: 'Select a branch from the list above:'
  output_model:
    selected_branch: str
  options:
    - branches  # References state.branches list
```

### Properties

| Property | Description |
|----------|-------------|
| `prompt` | Text shown to user |
| `options` | Static list of choices OR reference to state variable |
| `output_model` | Variables to store response |

---

## LLM Node

Calls an AI model with a prompt.

```yaml
- name: analyze
  type: llm
  system: You are a helpful analyst.
  prompt: 'Analyze this text: {input}'
```

### With Tools

Enable tool calling and specify which tools:

```yaml
- name: list_branches_llm
  type: llm
  system: You are a GitHub assistant. Use the list_branches tool to fetch branches.
  prompt: 'Repo: {repo}. List all branches and provide a short summary.'
  tools: true
  tools_selection:
    - list_branches
  output_model:
    branches: list
    branch_summary: str
  user_message:
    - branch_summary
    - branches
```

### Structured Output

Define expected output structure:

```yaml
- name: extract
  type: llm
  prompt: 'Extract key information from {text}'
  output_model:
    summary: str
    sentiment: str
    confidence: float
```

### Properties

| Property | Description |
|----------|-------------|
| `prompt` | User message to AI |
| `system` | System prompt (personality/instructions) |
| `tools` | Enable tool calling (`true`/`false`) |
| `tools_selection` | Whitelist specific tools |
| `output_model` | Parse structured output |
| `user_message` | Display values after execution |

---

## Tool Node

Calls an MCP tool directly without AI.

```yaml
- name: demo_tool
  type: tool
  tools_selection:
    - shell_command
  args:
    command: |-
      echo '=== DEMO TOOL EXECUTED ===
      Repo: {repo}
      Selected branch: {selected_branch}
      =================='
  output_model:
    tool_output: str
```

Use when:
- Action is deterministic
- No AI reasoning needed
- Performance is critical

### Properties

| Property | Description |
|----------|-------------|
| `tools_selection` | Which tool(s) to call |
| `args` | Arguments to pass to the tool |
| `output_model` | Variables to store tool output |

---

## Output Node

Displays a message to the user without AI processing.

```yaml
- name: show_result
  type: output
  user_message:
    - '=== Tool Node Result ==='
    - tool_output
```

### With Variable Interpolation

Mix static text and state variables:

```yaml
- name: final_output
  type: output
  user_message:
    - '=== FINAL SUMMARY ==='
    - 'All accumulated selections: {selections}'
    - Thanks for demoing the flow!
```

### User Message Format

An array of strings and variable names:

```yaml
user_message:
  - 'Static text'
  - variable_name      # Replaced with variable value
  - 'Text with {variable}'  # Interpolation
```

### Properties

| Property | Description |
|----------|-------------|
| `user_message` | Array of strings/variables to display |

---

## Update State Node

Modifies variables directly without AI. Two modes are supported:

### Mode 1: Append to List

Add a value from another variable to a list:

```yaml
- name: append_selection
  type: update_state
  source_variable: selected_branch  # Value to append
  action: append
  output_model:
    selections: list                # Target list variable
```

:::note
`output_model` must have **exactly 1 key** — the target variable name.
:::

### Mode 2: Overwrite

Set a variable to a specific value:

```yaml
- name: set_status
  type: update_state
  value: "completed"
  action: overwrite
  output_model:
    status: str
```

Or copy from another variable:

```yaml
- name: copy_result
  type: update_state
  source_variable: llm_response
  action: overwrite
  output_model:
    final_result: str
```

### Mode 3: Increment

Increment a numeric counter:

```yaml
- name: increment_counter
  type: update_state
  action: increment
  value: 1                # Increment by this amount (default: 1)
  output_model:
    counter: int          # Variable to increment
```

Or increment from one variable into another:

```yaml
- name: step_counter
  type: update_state
  source_variable: step   # Read current value from here
  action: increment
  value: 1
  output_model:
    total_steps: int      # Store result here
```

### Mode 4: Simple Updates (Legacy)

Set multiple key-value pairs directly:

```yaml
- name: initialize
  type: update_state
  updates:
    counter: "0"
    status: "pending"
```

Values can use `{variable}` interpolation.

### Properties

| Property | Description |
|----------|-------------|
| `action` | `append`, `overwrite`, or `increment` |
| `source_variable` | Variable to read from |
| `value` | Literal value (or increment amount for `increment`) |
| `output_model` | Target variable (exactly 1 key) |
| `updates` | Key-value map (legacy mode only) |

---

## Common Properties

All nodes share:

| Property | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Unique identifier |
| `type` | Yes | Node type |
| `silent` | No | Hide execution details (true/false) |

## Variable References

Use `{variable}` syntax in prompts:

```yaml
prompt: 'Hello {name}, analyze {topic}'
```

Variables come from:
- Parameters passed at runtime
- Previous node outputs
- State updates

## Next Steps

- **[State](/concepts/state/)** — How data flows between nodes
- **[Flows](/concepts/flows/)** — Full flow structure
- **[YAML Reference](/concepts/yaml/)** — Complete syntax
