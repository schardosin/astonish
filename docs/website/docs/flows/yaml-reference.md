# YAML Reference

This page documents the complete schema for Astonish flow YAML files. Flows are validated against this schema at load time — invalid flows produce clear error messages pointing to the offending field.

## Top-Level Structure

```yaml
name: string              # Unique identifier (kebab-case)
description: string       # Human-readable summary
version: string           # Semver (optional, defaults to "0.1.0")
author: string            # Optional author field

params: []                # Input parameter declarations
state: {}                 # Initial state variables
nodes: []                 # Node definitions
edges: []                 # Edge definitions
```

## Parameters

Parameters declare the typed inputs a flow accepts at runtime.

```yaml
params:
  - name: repo_url
    type: string          # string | number | boolean | array | object
    required: true
    description: "The repository URL to analyze"

  - name: max_files
    type: number
    default: 10
    description: "Maximum files to process"

  - name: notify
    type: boolean
    default: true
```

Supported types: `string`, `number`, `boolean`, `array`, `object`. Parameters are available in node templates as `{{param_name}}`.

## State Variables

State is a mutable key-value store that persists across nodes within a single execution.

```yaml
state:
  results: []
  error_count: 0
  summary: ""
```

Nodes read state with `{{state.key}}` and write to it via their output mappings.

## Nodes

Every node requires an `id` (unique within the flow) and a `type`. The following node types are supported:

### LLM Node

Sends a prompt to the configured language model.

```yaml
- id: analyze
  type: llm
  model: default            # Optional: override model selection
  prompt: |
    Analyze the following code for security issues:
    {{fetch_code.output}}
  temperature: 0.2          # Optional: 0.0-1.0
  output:
    state.analysis: "{{output}}"
```

### Tool Node

Invokes a tool (built-in, MCP, or custom).

```yaml
- id: read_file
  type: tool
  tool: file_read
  args:
    path: "{{state.target_file}}"
  output:
    state.file_content: "{{output}}"
  on_error: skip            # skip | fail | retry
  retry:
    max_attempts: 3
    delay_ms: 1000
```

### Conditional Node

Routes execution based on an expression.

```yaml
- id: check_severity
  type: conditional
  condition: "{{state.severity}} == 'critical'"
  then: alert_team          # Node ID to jump to if true
  else: log_result          # Node ID to jump to if false
```

### Input Node

Pauses execution and requests user input (interactive mode only).

```yaml
- id: confirm
  type: input
  prompt: "Found {{state.issue_count}} issues. Continue with fixes?"
  options:
    - "yes"
    - "no"
    - "review first"
  output:
    state.user_choice: "{{output}}"
```

### Output Node

Emits a result from the flow. A flow may have multiple output nodes.

```yaml
- id: final_report
  type: output
  value:
    summary: "{{state.summary}}"
    issues: "{{state.results}}"
    severity: "{{state.max_severity}}"
```

## Edges

Edges define transitions between nodes. If no edges are specified for a node, execution follows document order.

```yaml
edges:
  - from: fetch_code
    to: analyze

  - from: analyze
    to: check_severity

  - from: check_severity
    to: alert_team
    condition: "{{state.severity}} == 'critical'"

  - from: check_severity
    to: log_result
    condition: default       # Fallback edge
```

Conditional edges on non-conditional nodes allow fan-out routing. The first matching condition wins; `default` acts as a fallback.

## Complete Example

```yaml
name: code-review
description: Automated code review for a pull request
version: "1.0.0"

params:
  - name: pr_url
    type: string
    required: true
  - name: strictness
    type: string
    default: "moderate"

state:
  files: []
  issues: []

nodes:
  - id: fetch_pr
    type: tool
    tool: github_get_pr
    args:
      url: "{{pr_url}}"
    output:
      state.files: "{{output.changed_files}}"

  - id: review
    type: llm
    prompt: |
      Review these code changes with {{strictness}} strictness.
      Files: {{state.files}}
    output:
      state.issues: "{{output}}"

  - id: has_issues
    type: conditional
    condition: "len({{state.issues}}) > 0"
    then: post_review
    else: approve

  - id: post_review
    type: tool
    tool: github_post_review
    args:
      url: "{{pr_url}}"
      body: "{{state.issues}}"
      event: "REQUEST_CHANGES"

  - id: approve
    type: tool
    tool: github_post_review
    args:
      url: "{{pr_url}}"
      body: "LGTM - no issues found."
      event: "APPROVE"

edges:
  - from: fetch_pr
    to: review
  - from: review
    to: has_issues
  - from: has_issues
    to: post_review
    condition: "len({{state.issues}}) > 0"
  - from: has_issues
    to: approve
    condition: default

```

## Validation

Flows are validated automatically when imported or saved. You can also validate via CLI:

```bash
# Import and validate a flow file
astonish flows import ./my-flow.yaml
```

In Studio, the visual flow editor validates in real-time as you build the flow, highlighting errors inline.

## Next Steps

- [Nodes, Edges & State](./nodes-edges-state.md) — Detailed execution semantics
- [Flows Overview](./index.md) — Lifecycle and platform storage
