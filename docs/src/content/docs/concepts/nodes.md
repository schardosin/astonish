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
| **tool** | Execute MCP tool | `tools_selection` |
| **output** | Display message | `user_message` |
| **update_state** | Modify variables | `updates` |

---

## START & END

Every flow has these automatically. You don't define them in YAML.

- **START** — First node, receives initial parameters
- **END** — Final node, flow completes here

---

## Input Node

Pauses execution to collect user input.

```yaml
- name: get_topic
  type: input
  prompt: "What would you like to learn about?"
  output_model:
    topic: str
```

### With Options

```yaml
- name: choose_action
  type: input
  prompt: "Select an action:"
  options:
    - Summarize
    - Translate
    - Analyze
  output_model:
    action: str
```

### Properties

| Property | Description |
|----------|-------------|
| `prompt` | Text shown to user |
| `options` | Optional list of choices |
| `output_model` | Variables to store response |

---

## LLM Node

Calls an AI model with a prompt.

```yaml
- name: analyze
  type: llm
  prompt: "Analyze this text: {input}"
  system: "You are a helpful analyst."
```

### With Tools

```yaml
- name: research
  type: llm
  prompt: "Find information about {topic}"
  tools: true
  tools_selection:
    - web_search
```

### Structured Output

```yaml
- name: extract
  type: llm
  prompt: "Extract key information from {text}"
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
| `tools` | Enable tool calling |
| `tools_selection` | Whitelist specific tools |
| `output_model` | Parse structured output |
| `user_message` | Display message after execution |

---

## Tool Node

Calls an MCP tool directly without AI.

```yaml
- name: search
  type: tool
  tools_selection:
    - web_search
```

Use when:
- Action is deterministic
- No AI reasoning needed
- Performance is critical

### Properties

| Property | Description |
|----------|-------------|
| `tools_selection` | Which tool(s) to call |

---

## Output Node

Displays a message to the user without AI processing.

```yaml
- name: show_result
  type: output
  user_message:
    - "Analysis complete!"
    - "Result:"
    - result_variable
```

### User Message Format

An array of strings and variable names:

```yaml
user_message:
  - "Static text"
  - variable_name      # Replaced with variable value
  - "More static text"
```

### Properties

| Property | Description |
|----------|-------------|
| `user_message` | Array of strings/variables to display |

---

## Update State Node

Modifies variables directly without AI.

```yaml
- name: initialize
  type: update_state
  updates:
    counter: 0
    status: "pending"
```

Use for:
- Setting default values
- Resetting counters
- Transforming data

### Properties

| Property | Description |
|----------|-------------|
| `updates` | Key-value pairs to set |

---

## Common Properties

All nodes share:

| Property | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Unique identifier |
| `type` | Yes | Node type |

## Variable References

Use `{variable}` syntax in prompts:

```yaml
prompt: "Hello {name}, analyze {topic}"
```

Variables come from:
- Parameters passed at runtime
- Previous node outputs
- State updates

## Next Steps

- **[State](/concepts/state/)** — How data flows between nodes
- **[Flows](/concepts/flows/)** — Full flow structure
- **[YAML Reference](/concepts/yaml/)** — Complete syntax
