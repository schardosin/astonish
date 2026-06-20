# Nodes, Edges & State

This page covers the execution semantics of the flow state machine — how nodes run, how edges route control flow, and how state variables carry data between steps.

## Node Execution Model

The flow executor processes one node at a time in a single-threaded loop:

1. **Resolve inputs** — Template expressions (`{{...}}`) in the node's configuration are evaluated against current state.
2. **Execute** — The node performs its action (LLM call, tool invocation, condition check, etc.).
3. **Map outputs** — Results are written to state variables via the node's `output` mapping.
4. **Select next edge** — The executor evaluates outgoing edges to determine the next node.

If a node has no outgoing edges and is not an output node, execution proceeds to the next node in document order.

## Node Types in Depth

### LLM Nodes

LLM nodes send a rendered prompt to the language model and capture the response.

```yaml
- id: classify
  type: llm
  prompt: |
    Classify this support ticket into one category:
    [bug, feature, question, billing]

    Ticket: {{state.ticket_text}}

    Respond with only the category name.
  temperature: 0
  output:
    state.category: "{{output}}"
```

The `temperature` field controls randomness. For deterministic flows, use `0`. The `model` field can override the default provider for specific nodes.

### Tool Nodes

Tool nodes invoke any available tool — built-in, MCP server, or custom-registered.

```yaml
- id: search
  type: tool
  tool: grep
  args:
    pattern: "TODO|FIXME"
    path: "{{state.repo_path}}"
    include: "*.go"
  output:
    state.todos: "{{output.matches}}"
  on_error: skip
```

Error handling options:
- `fail` (default) — Stop the flow and report the error.
- `skip` — Log the error and proceed to the next edge.
- `retry` — Retry with exponential backoff per the `retry` configuration.

### Conditional Nodes

Conditional nodes evaluate a boolean expression and branch accordingly.

```yaml
- id: route
  type: conditional
  condition: "{{state.category}} == 'bug' && {{state.severity}} >= 3"
  then: escalate
  else: queue_normal
```

Supported operators: `==`, `!=`, `>`, `<`, `>=`, `<=`, `&&`, `||`, `!`. String comparisons are case-sensitive. Use `len({{var}})` for collection length checks.

### Input Nodes

Input nodes pause execution and prompt the user for a decision. In scheduled (non-interactive) mode, input nodes use their `default` value or fail.

```yaml
- id: ask_approval
  type: input
  prompt: "Deploy to production?"
  default: "no"
  options: ["yes", "no"]
  timeout: 300              # Seconds before using default (scheduled mode)
  output:
    state.approved: "{{output}}"
```

### Output Nodes

Output nodes emit the flow's final result. Multiple output nodes are allowed for flows with branching endpoints.

```yaml
- id: result
  type: output
  value:
    status: "completed"
    report: "{{state.summary}}"
```

## Edge Routing

Edges define the graph topology. They are evaluated in declaration order — the first matching edge wins.

### Simple Edges

```yaml
edges:
  - from: step_a
    to: step_b
```

### Conditional Edges

Attach conditions directly to edges for multi-way branching without a dedicated conditional node:

```yaml
edges:
  - from: classify
    to: handle_bug
    condition: "{{state.category}} == 'bug'"

  - from: classify
    to: handle_feature
    condition: "{{state.category}} == 'feature'"

  - from: classify
    to: handle_other
    condition: default
```

The `default` condition acts as a catch-all. If no edge matches and no default exists, the flow fails with a routing error.

## State Variable Interpolation

State is a flat key-value map available to all nodes. Variables are referenced with double-brace syntax:

```
{{param_name}}           — Flow parameter
{{state.key}}            — State variable
{{node_id.output}}       — Previous node's raw output
{{node_id.output.field}} — Nested field access
```

### Writing to State

Use the `output` mapping on any node to write results into state:

```yaml
- id: count
  type: tool
  tool: wc
  args:
    path: "{{state.file}}"
  output:
    state.line_count: "{{output.lines}}"
    state.word_count: "{{output.words}}"
```

### State Scoping

- State is scoped to a single execution — concurrent runs of the same flow have independent state.
- State variables persist for the entire execution. Once set, they remain available to all subsequent nodes.
- Overwriting a state variable replaces its value entirely (no deep merge).

## Debugging Flows

```bash
# Dry-run with verbose output showing state at each step
astonish flow run my-flow --dry-run --verbose

# Show the resolved state after a specific node
astonish flow run my-flow --break-at classify
```

In Studio, the flow debugger visualizes state changes at each node with a step-through interface.

## Next Steps

- [YAML Reference](./yaml-reference.md) — Full schema documentation
- [Taps & Flow Store](./taps.md) — Share and discover community flows
