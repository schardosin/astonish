---
title: Flows
description: Understanding AI flows in Astonish
sidebar:
  order: 1
---

# Flows

A **flow** is a sequence of connected steps that process data and produce output. Flows are the core building block of Astonish.

## What is a Flow?

A flow is a directed graph where:
- **Nodes** perform actions (AI calls, user input, tool execution)
- **Edges** connect nodes and control execution order
- **State** carries data between nodes

```
START → input → process → output → END
```

## Flow Lifecycle

1. **START** — Execution begins
2. **Nodes execute** — Each performs its action
3. **State updates** — Output stored for later nodes
4. **Edges followed** — Next node is determined
5. **END** — Execution completes

## Flow as Code (YAML)

Every flow is a YAML file:

```yaml
name: my-flow
description: What this flow does

nodes:
  - name: greet
    type: llm
    prompt: "Say hello to the user."

flow:
  - from: START
    to: greet
  - from: greet
    to: END
```

This file is the **source of truth**—both Studio and CLI read the same format.

## Flow Components

### Nodes

Processing steps in your flow:

| Type | Purpose |
|------|---------|
| `llm` | Call an AI model |
| `input` | Get user input |
| `tool` | Call an MCP tool |
| `output` | Display a message |
| `update_state` | Modify variables |

See **[Nodes](/concepts/nodes/)** for details.

### Edges

Connections between nodes:

```yaml
flow:
  - from: node_a
    to: node_b
```

Edges can have conditions for branching:

```yaml
flow:
  - from: check
    edges:
      - to: approve
        condition: "lambda x: x['ok']"
      - to: reject
        condition: "lambda x: not x['ok']"
```

### State

Variables that carry data between nodes:

```yaml
nodes:
  - name: ask
    type: input
    prompt: "What's your name?"
    output_model:
      name: str  # Stored in state

  - name: greet
    type: llm
    prompt: "Say hello to {name}"  # Read from state
```

See **[State](/concepts/state/)** for details.

## Flow Patterns

### Linear

```
START → A → B → C → END
```

### Branching

```
        ┌→ B
START → A ┤
        └→ C
```

### Merge

```
B ┐
  ├→ D → END
C ┘
```

### Loop

```
START → process ←─┐
           │      │
           ▼      │
        check ────┘ (if retry)
           │
           ▼
          END (if done)
```

## Flow Storage

| Location | Purpose |
|----------|---------|
| `~/.astonish/agents/` | Your local flows |
| `~/.astonish/store/<tap>/` | Installed tap flows |

## Creating Flows

- **Studio:** Visual drag-and-drop
- **CLI:** Write YAML directly

Both produce the same YAML format.

## Best Practices

1. **Name descriptively** — `github_pr_reviewer` not `flow1`
2. **Keep flows focused** — One task per flow
3. **Document** — Add a description field
4. **Test incrementally** — Build step by step
5. **Version control** — Track changes with Git

## Next Steps

- **[Nodes](/concepts/nodes/)** — Learn all node types
- **[State](/concepts/state/)** — Understand data flow
- **[YAML Reference](/concepts/yaml/)** — Complete schema
