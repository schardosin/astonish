---
title: "Nodes, Edges & State"
description: "How data flows through nodes and edges in a flow"
---

Flows are directed graphs: nodes do work, edges define the path, and state carries data between them.

## Nodes

Every flow starts with a **START** node and ends with an **END** node (both implicit). Between them, you define nodes of five types:

### Input

Prompts the user for input and stores the response in state via `output_model`.

### LLM

Sends a prompt to the AI model. Can optionally use tools. The model's output is written to state.

### Tool

Directly calls a specific tool with static arguments. Useful when you know exactly which tool and parameters to use.

### Output

Displays a value to the user. Reads from state using variable interpolation.

### UpdateState

Sets or modifies state variables without calling any external service.

## Edges

Edges are defined in the `flow` section of the YAML and connect nodes together.

### Simple Edges

A simple edge always follows the path from one node to another:

```yaml
flow:
  - from: "step1"
    to: "step2"
```

### Conditional Edges

When a node has multiple outgoing paths, each edge can include a `condition` expression that references state variables:

```yaml
flow:
  - from: "decision"
    edges:
      - to: "path_a"
        condition: "approval == 'yes'"
      - to: "path_b"
        condition: "approval == 'no'"
```

Conditions are evaluated against the current state at runtime. The first matching condition determines the path taken.

## State

State is a shared key-value store accessible to all nodes in the flow.

- **Input** nodes write to state via `output_model`.
- **LLM** nodes write their output to state.
- **UpdateState** nodes modify state directly with the `updates` field.
- State variables are accessible in prompts via `{{variable_name}}` interpolation.

All nodes can read from state, and most node types write back to it, creating a chain of data transformations through the flow.

## Parallel Execution

Nodes can process collections in parallel using the `parallel` field:

```yaml
parallel:
  forEach: "items"        # State variable containing the collection
  as: "item"              # Variable name for each element
  maxConcurrency: 3       # Max parallel executions
```

Use `output_action: "append"` to aggregate results from parallel iterations back into a single state variable.
