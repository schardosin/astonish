---
title: YAML Reference
description: Complete YAML schema for Astonish flows
sidebar:
  order: 6
---

# YAML Reference

Complete reference for Astonish flow YAML files.

## Basic Structure

```yaml
name: flow-name                  # Required
description: What this flow does # Optional

nodes:                          # Required
  - name: node_name
    type: node_type
    # ... node properties

flow:                           # Required
  - from: START
    to: first_node
  - from: first_node
    to: END

layout:                         # Optional (Studio only)
  positions:
    node_name: [x, y]

mcp_dependencies:               # Optional
  - server_name
```

## Top-Level Properties

| Property | Required | Type | Description |
|----------|----------|------|-------------|
| `name` | Yes | string | Unique flow identifier |
| `description` | No | string | Human-readable description |
| `nodes` | Yes | array | List of node definitions |
| `flow` | Yes | array | Edge connections |
| `layout` | No | object | Node positions (Studio) |
| `mcp_dependencies` | No | array | Required MCP servers |

---

## Node Schema

All nodes share these properties:

```yaml
- name: unique_name    # Required
  type: node_type      # Required
  # ... type-specific properties
```

### Input Node

```yaml
- name: get_input
  type: input
  prompt: "What would you like?"
  options:             # Optional (static or dynamic)
    - Option 1
    - Option 2
  output_model:        # Required
    variable_name: str
```

Dynamic options from state:

```yaml
- name: select_branch
  type: input
  prompt: "Select a branch:"
  options:
    - branches         # References state.branches list
  output_model:
    selected_branch: str
```

| Property | Required | Description |
|----------|----------|-------------|
| `prompt` | Yes | Text shown to user |
| `options` | No | List of choices |
| `output_model` | Yes | Variables to store response |

### LLM Node

```yaml
- name: process
  type: llm
  prompt: "Process {input}"
  system: "You are helpful."    # Optional
  tools: true                   # Optional
  tools_selection:              # Optional
    - tool_name
  output_model:                 # Optional
    summary: str
    score: float
  user_message:                 # Optional
    - "Processing complete"
```

| Property | Required | Description |
|----------|----------|-------------|
| `prompt` | Yes | User message to AI |
| `system` | No | System prompt |
| `tools` | No | Enable tool calling |
| `tools_selection` | No | Whitelist tools |
| `output_model` | No | Parse structured output |
| `user_message` | No | Display after execution |

### Tool Node

```yaml
- name: run_command
  type: tool
  tools_selection:
    - shell_command
  args:
    command: "echo 'Hello {name}'"
  output_model:
    result: str
```

| Property | Required | Description |
|----------|----------|-------------|
| `tools_selection` | Yes | Tools to call |
| `args` | No | Arguments to pass to tool |
| `output_model` | No | Variables to store result |

### Output Node

```yaml
- name: display
  type: output
  user_message:
    - "Result:"
    - result_variable
```

| Property | Required | Description |
|----------|----------|-------------|
| `user_message` | Yes | Array of strings/variables |

#### Simple Updates (Legacy)

```yaml
- name: set_defaults
  type: update_state
  updates:
    counter: "0"
    status: "ready"
```

#### With Action

```yaml
- name: append_item
  type: update_state
  source_variable: new_item
  action: append
  output_model:
    items: list
```

```yaml
- name: overwrite_status
  type: update_state
  value: "completed"
  action: overwrite
  output_model:
    status: str
```

```yaml
- name: increment_counter
  type: update_state
  action: increment
  value: 1
  output_model:
    counter: int
```

| Property | Required | Description |
|----------|----------|-------------|
| `updates` | No | Key-value pairs (legacy mode) |
| `action` | No | `append`, `overwrite`, or `increment` |
| `source_variable` | No | Variable to read value from |
| `value` | No | Literal value (or increment amount) |
| `output_model` | No | Target variable (exactly 1 key) |

---

## Flow Schema

### Sequential Edge

```yaml
flow:
  - from: node_a
    to: node_b
```

### Conditional Edges

```yaml
flow:
  - from: decision
    edges:
      - to: path_a
        condition: "lambda x: x['choice'] == 'a'"
      - to: path_b
        condition: "lambda x: x['choice'] == 'b'"
```

### Edge Properties

| Property | Required | Description |
|----------|----------|-------------|
| `from` | Yes | Source node name |
| `to` | Yes* | Target node (simple edge) |
| `edges` | Yes* | Array of conditional edges |
| `condition` | No | Python lambda expression |

*Use `to` for simple edges, `edges` for conditional.

---

## Output Model Types

```yaml
output_model:
  text: str       # String
  count: int      # Integer
  score: float    # Decimal
  valid: bool     # Boolean
  items: list     # Array
  data: dict      # Object
```

---

## Variable Syntax

Reference variables with curly braces:

```yaml
prompt: "Hello {name}, analyze {topic}"
```

In user_message arrays:

```yaml
user_message:
  - "Static text"
  - variable_name    # Variable reference (no braces)
  - "More text"
```

---

## Condition Syntax

Python lambda expressions:

```python
# String comparison
"lambda x: x['status'] == 'approved'"

# Numeric comparison
"lambda x: x['score'] > 0.5"

# Boolean check
"lambda x: x['is_valid']"

# Not check
"lambda x: not x['error']"

# Contains
"lambda x: 'error' in x['response']"
```

---

## Layout Schema

For Studio positioning:

```yaml
layout:
  nodes:
    START:
      x: 488
      'y': 12
    get_code:
      x: 486
      'y': 162
    analyze:
      x: 450
      'y': 312
  edges: {}
```

---

## Complete Example

```yaml
name: code_reviewer
description: Reviews code and provides feedback

nodes:
  - name: get_code
    type: input
    prompt: "Paste your code:"
    output_model:
      code: str

  - name: analyze
    type: llm
    system: "You are an expert code reviewer."
    prompt: |
      Review this code for:
      - Bugs
      - Performance issues
      - Best practices
      
      Code:
      {code}
    output_model:
      review: str
      has_issues: bool

  - name: show_review
    type: output
    user_message:
      - "## Code Review"
      - review

flow:
  - from: START
    to: get_code
  - from: get_code
    to: analyze
  - from: analyze
    to: show_review
  - from: show_review
    to: END

layout:
  positions:
    START: [0, 0]
    get_code: [200, 0]
    analyze: [400, 0]
    show_review: [600, 0]
    END: [800, 0]
```

---

## Validation

Validate your YAML with:

```bash
# Check syntax
python -c "import yaml; yaml.safe_load(open('flow.yaml'))"

# Or use Studio's YAML view
astonish studio
```

## Next Steps

- **[Flows](/concepts/flows/)** — Flow concepts
- **[Nodes](/concepts/nodes/)** — Node types
- **[State](/concepts/state/)** — Data flow
