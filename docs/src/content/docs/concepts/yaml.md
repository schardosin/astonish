---
title: YAML Configuration
description: Complete reference for Astonish YAML flow configuration
---

# YAML Configuration

Every Astonish agent is defined in a YAML file. This page covers the complete configuration reference.

## File Structure

```yaml
name: my_agent                    # Agent name (required)
description: What this agent does # Description (optional)

nodes:                            # List of processing nodes
  - name: node_name
    type: llm
    # ... node configuration

flow:                             # How nodes connect
  - from: START
    to: node_name
  - from: node_name
    to: END

mcp_dependencies:                 # Required MCP servers (auto-generated)
  - server: tavily-mcp
    tools: [search_web]
```

## Node Types

### LLM Node

Calls an AI language model:

```yaml
- name: analyze
  type: llm
  system: "You are a helpful assistant."
  prompt: "Analyze this data: {input}"
  tools: true                     # Enable tool use
  tools_selection:                # Specific tools to allow
    - search_web
    - read_file
  output_model:                   # Structured output
    analysis: str
    sentiment: str
```

### Input Node

Request user input:

```yaml
- name: get_confirmation
  type: input
  prompt: "Please confirm (yes/no):"
  output_model:
    confirmation: str
```

### Tool Node

Execute a specific tool:

```yaml
- name: execute_search
  type: tool
  tool_name: search_web
  output_model:
    results: str
```

## Output Models

Output models define the structure of data a node produces. They're written to the state blackboard.

```yaml
output_model:
  title: str           # String
  count: int           # Integer
  is_valid: bool       # Boolean
  items: list          # List
  metadata: dict       # Dictionary
```

## Flow Edges

Edges define how nodes connect.

### Simple Edge

```yaml
flow:
  - from: START
    to: first_node
  - from: first_node
    to: second_node
  - from: second_node
    to: END
```

### Conditional Edge

```yaml
flow:
  - from: decision_node
    to: path_a
    condition: choice == "a"
  - from: decision_node
    to: path_b
    condition: choice == "b"
```

## Variable Substitution

Reference state variables using `{variable_name}`:

```yaml
prompt: "Summarize this article: {article_content}"
```

Variables are read from the state blackboard, populated by previous nodes.

## MCP Dependencies

The `mcp_dependencies` section is automatically generated when you save a flow. It lists which MCP servers are required:

```yaml
mcp_dependencies:
  - server: github-mcp
    tools:
      - list_pull_requests
      - get_pr_diff
    source: store
    store_id: official/github-mcp
```

## Full Example

```yaml
name: pr_description_generator
description: Generate PR descriptions from code changes

nodes:
  - name: get_prs
    type: llm
    prompt: List open PRs using the gh CLI
    tools: true
    tools_selection: [shell_command]
    output_model:
      prs: str

  - name: select_pr
    type: input
    prompt: "Select a PR number:\n{prs}"
    output_model:
      pr_number: int

  - name: get_diff
    type: llm
    prompt: Get the diff for PR #{pr_number}
    tools: true
    tools_selection: [shell_command]
    output_model:
      diff: str

  - name: generate_description
    type: llm
    system: You are a technical writer.
    prompt: |
      Generate a clear PR description for this diff:
      {diff}
    output_model:
      description: str

flow:
  - from: START
    to: get_prs
  - from: get_prs
    to: select_pr
  - from: select_pr
    to: get_diff
  - from: get_diff
    to: generate_description
  - from: generate_description
    to: END
```
