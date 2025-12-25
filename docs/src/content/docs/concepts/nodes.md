---
title: Nodes & Edges
description: Understanding the building blocks of Astonish flows
---

# Nodes & Edges

Nodes are the building blocks of your agent flows. Each node performs a specific task, and edges define how data flows between them.

## Node Types

### LLM Node

The most common node type. Calls an AI language model to process data.

```yaml
- name: analyze
  type: llm
  system: "You are an expert analyst."
  prompt: "Analyze: {input}"
  tools: true
  tools_selection:
    - search_web
  output_model:
    analysis: str
    confidence: int
```

**Properties:**

| Property | Description |
|----------|-------------|
| `system` | System prompt for the LLM |
| `prompt` | User prompt (supports `{variables}`) |
| `tools` | Enable tool calling (true/false) |
| `tools_selection` | List of allowed tools |
| `output_model` | Structured output schema |
| `user_message` | Fields to display to user |

### Input Node

Pauses execution to get user input.

```yaml
- name: ask_user
  type: input
  prompt: "What would you like to analyze?"
  output_model:
    user_response: str
```

### Tool Node

Directly executes a specific tool without LLM involvement.

```yaml
- name: run_shell
  type: tool
  tool_name: shell_command
  tool_input:
    command: "ls -la"
  output_model:
    output: str
```

## Special Nodes

### START

Every flow begins with START. It's implicit—you don't define it, just reference it in edges.

### END

Every flow must end at END. Multiple paths can lead to END.

## Edges

Edges connect nodes and define the flow of execution.

### Simple Edges

```yaml
flow:
  - from: START
    to: first_node
  - from: first_node
    to: second_node
```

### Conditional Edges

Branch based on state variables:

```yaml
flow:
  - from: decision
    to: approve
    condition: status == "good"
  - from: decision
    to: reject
    condition: status == "bad"
```

### Multiple Targets

A node can have multiple outgoing edges:

```yaml
flow:
  - from: router
    to: path_a
    condition: route == "a"
  - from: router
    to: path_b
    condition: route == "b"
  - from: router
    to: path_c
    condition: route == "c"
```

## Visual Representation

In Astonish Studio, nodes are represented as colored cards:

| Node Type | Color |
|-----------|-------|
| START | Green |
| LLM | Purple |
| Tool | Purple (darker) |
| Input | Blue |
| END | Red |

Edges are drawn as connecting lines with animated dots showing data flow direction.
