---
title: "Flows Overview"
description: "Reusable AI automation workflows defined in YAML"
---

Flows are structured automation workflows — directed graphs of nodes connected by edges, carrying shared state between steps.

While chat is the primary Astonish experience (dynamic, adaptive, conversational), flows provide **deterministic, repeatable automation**. Use flows when you need a process to run the same way every time.

## Use Cases

- CI/CD pipelines
- Data processing and transformation
- Scheduled reports
- Multi-step workflows that require consistent execution

## Creating Flows

A flow is defined in YAML and can be created in three ways:

- **Visually** in the Studio Flow Editor
- **By hand** writing YAML directly
- **Via flow distillation** — converting a chat session into a reusable flow

## Flow Lifecycle

1. A **START** node begins execution.
2. Nodes execute in order, reading from and writing to shared state.
3. Edges connect nodes, optionally with conditions for branching.
4. An **END** node completes the flow.

## Running Flows

From the CLI:

```bash
astonish flows run <name>
```

Or use the **Run** button in Studio.

### Parameters

Flows support parameters passed at runtime:

```bash
astonish flows run my-flow -p key="value"
```

## Sharing Flows

Share flows through tap repositories:

```bash
astonish tap add <repo>
```

See [Taps & Flow Store](/configuration/taps/) for details on publishing and installing flows.
