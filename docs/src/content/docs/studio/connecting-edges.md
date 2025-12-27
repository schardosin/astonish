---
title: Connecting Edges
description: Learn how to connect nodes and add conditional branching in Astonish Studio
sidebar:
  order: 4
---

# Connecting Edges

Edges define how your flow moves from one node to another. They can be simple connections or include conditions for branching logic.

## Creating Edges

### Basic Connection

1. Hover over a node's **output handle** (bottom edge)
2. Click and drag toward another node
3. Release on the target node's **input handle** (top edge)

![Creating an Edge](/src/assets/placeholder.png)
*Dragging to create a connection*

### Connection Rules

- **Every node** must have at least one incoming edge (except START)
- **Every node** must have at least one outgoing edge (except END)
- **Multiple outgoing edges** require conditions
- **No circular loops** to the same node without conditions

## Edge Types

### Sequential Edge

The simplest type—flow moves unconditionally to the next node:

```
START → process → END
```

In YAML:
```yaml
flow:
  - from: START
    to: process
  - from: process
    to: END
```

### Conditional Edges

Create branches based on data:

```
         ┌─── yes ───→ approve
check ───┤
         └─── no ────→ reject
```

In YAML:
```yaml
flow:
  - from: check
    edges:
      - to: approve
        condition: "lambda x: x['decision'] == 'yes'"
      - to: reject
        condition: "lambda x: x['decision'] == 'no'"
```

## Adding Conditions

### In Studio

1. Select an edge by clicking on it
2. The Edge Editor panel opens
3. Enter a **Condition** expression

![Edge Editor](/src/assets/placeholder.png)
*The edge editor panel with condition input*

### Condition Syntax

Conditions are Python lambda expressions:

```python
lambda x: x['variable_name'] == 'value'
```

| Expression | Meaning |
|------------|---------|
| `x['choice'] == 'yes'` | Choice equals "yes" |
| `x['score'] > 50` | Score greater than 50 |
| `x['items']` | Items is truthy (not empty) |
| `not x['error']` | No error present |

### Common Patterns

**Boolean check:**
```python
lambda x: x['is_valid']
```

**String comparison:**
```python
lambda x: x['status'] == 'approved'
```

**Numeric comparison:**
```python
lambda x: x['confidence'] >= 0.8
```

**Contains check:**
```python
lambda x: 'error' in x['response']
```

## Branching Patterns

### If/Else Branch

Two paths based on a single condition:

```yaml
flow:
  - from: decision_node
    edges:
      - to: path_a
        condition: "lambda x: x['choice'] == 'a'"
      - to: path_b
        condition: "lambda x: x['choice'] != 'a'"
```

### Multiple Branches

More than two paths:

```yaml
flow:
  - from: router
    edges:
      - to: handle_text
        condition: "lambda x: x['type'] == 'text'"
      - to: handle_code
        condition: "lambda x: x['type'] == 'code'"
      - to: handle_image
        condition: "lambda x: x['type'] == 'image'"
      - to: handle_unknown
        condition: "lambda x: True"  # Default fallback
```

### Merge Pattern

Multiple paths converging:

```
path_a ───┐
          ├───→ next_step
path_b ───┘
```

```yaml
flow:
  - from: path_a
    to: next_step
  - from: path_b
    to: next_step
```

### Loop Pattern

Repeat until condition is met:

```yaml
flow:
  - from: attempt
    to: verify
  - from: verify
    edges:
      - to: END
        condition: "lambda x: x['success']"
      - to: attempt
        condition: "lambda x: not x['success']"
```

## Deleting Edges

1. Click the edge to select it
2. Press **Delete** or **Backspace**
3. The connection is removed

## Edge Validation

Studio validates your flow:

- ⚠️ **Warning**: Node has no outgoing edge
- ⚠️ **Warning**: Conditions may not cover all cases
- ❌ **Error**: Invalid condition syntax

## Next Steps

- **[Running & Debugging](/studio/running-debugging/)** — Test your branching logic
- **[Key Concepts: Flows](/concepts/flows/)** — Deep dive into flow theory
