---
title: "Running & Debugging"
description: "Execute flows and debug issues in Studio"
---

Studio supports two execution modes, each suited to different tasks.

## Execution modes

1. **Chat** — Conversational AI with dynamic tool use. No predefined steps; the agent decides what to do based on your messages.
2. **Flow** — Structured execution following the visual flow graph. Each node runs in sequence according to the edges and conditions you defined.

## Running a flow

1. Click the **Run** button in the flow editor.
2. If the flow has input nodes, a dialog prompts you for values.
3. Nodes highlight as they execute and edges show data flow in real-time.
4. The chat panel displays agent responses and tool outputs alongside the visual execution.

## Debugging tips

- **Execution panel** — Check the step-by-step progress to see which node is active and what data it produced.
- **Tool call blocks** — Expand them to inspect full input and output for each tool invocation.
- **YAML source drawer** — Verify the flow structure matches your intent.
- **`/status` command** — In chat sessions, use this to inspect current session state.
- **CLI traces** — Run `astonish sessions show <id> --verbose` from the terminal for detailed execution traces.

## Error handling

When a node fails, the error is displayed inline on the canvas and in the execution panel. Two configuration options help manage failures:

- `continue_on_error: true` — Set on individual nodes to let the flow proceed past a failure.
- `max_retries` — Set on a node to automatically retry it a specified number of times before marking it as failed.
