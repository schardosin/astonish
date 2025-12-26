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
  options:             # Optional
    - Option 1
    - Option 2
  output_model:        # Required
    variable_name: str
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
- name: search
  type: tool
  tools_selection:
    - web_search
```

| Property | Required | Description |
|----------|----------|-------------|
| `tools_selection` | Yes | Tools to call |

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

### Update State Node

```yaml
- name: set_defaults
  type: update_state
  updates:
    counter: 0
    status: "ready"
```

| Property | Required | Description |
|----------|----------|-------------|
| `updates` | Yes | Key-value pairs to set |

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
  positions:
    START: [0, 0]
    node_a: [200, 100]
    node_b: [400, 100]
    END: [600, 0]
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
