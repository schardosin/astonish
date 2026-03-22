---
title: "YAML Reference"
description: "Complete YAML schema for Astonish flow files"
---

Flow files are stored as YAML in `~/.config/astonish/flows/`.

## Basic Structure

```yaml
description: "What this flow does"
nodes:
  - name: "step1"
    type: "input"
    prompt: "Enter your name"
    output_model:
      user_name: "string"
  - name: "step2"
    type: "llm"
    prompt: "Greet {{user_name}}"
    system: "You are a friendly assistant"
    tools: true
  - name: "step3"
    type: "output"
    value: "{{step2_output}}"
flow:
  - from: "START"
    to: "step1"
  - from: "step1"
    to: "step2"
  - from: "step2"
    to: "step3"
  - from: "step3"
    to: "END"
```

## Node Types

| Type | Purpose | Key Fields |
|------|---------|------------|
| `input` | Collect user input | `prompt`, `output_model`, `options` |
| `llm` | Send prompt to AI model | `prompt`, `system`, `tools`, `tools_selection`, `tools_auto_approval` |
| `tool` | Execute a specific tool | `action`, `args`, `raw_tool_output` |
| `output` | Display results | `value` |
| `update_state` | Modify state variables | `updates` |

## Node Properties

| Property | Type | Description |
|----------|------|-------------|
| `name` | string | Unique node identifier |
| `type` | string | Node type (see above) |
| `prompt` | string | Prompt text (supports `{{variable}}` interpolation) |
| `system` | string | System prompt for LLM nodes |
| `tools` | bool | Enable tool use on LLM nodes |
| `tools_selection` | string[] | Restrict to specific tools |
| `output_model` | object | Define output variables (key: type pairs) |
| `args` | object | Tool arguments for tool nodes |
| `continue_on_error` | bool | Don't stop on failure |
| `max_retries` | int | Retry count (default: 3) |
| `retry_strategy` | string | `intelligent` or `simple` |
| `silent` | bool | Suppress output |
| `parallel` | object | Parallel execution: `forEach`, `as`, `index_as`, `maxConcurrency` |
| `output_action` | string | How to aggregate parallel output (`append`) |

## Edge Properties

Edges are defined in the `flow` section of the YAML.

| Property | Type | Description |
|----------|------|-------------|
| `from` | string | Source node name (or `"START"`) |
| `to` | string | Target node name (or `"END"`) |
| `edges` | array | Conditional edges: `{to, condition}` |

Conditional edges use the `condition` field with expressions that reference state variables. See [Nodes, Edges & State](/flows/nodes-edges-state/) for examples.

## MCP Dependencies

Flows can declare MCP server dependencies that get auto-installed when the flow is run:

```yaml
mcp_dependencies:
  - server: "github"
    tools: ["create_issue", "list_repos"]
    source: "store"
    store_id: "github-mcp"
```
