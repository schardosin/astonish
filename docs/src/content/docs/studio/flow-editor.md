---
title: "Flow Editor"
description: "Visual drag-and-drop flow designer with AI Assist"
---

The Flow Editor (Canvas view) lets you design AI automation workflows visually. Create a new flow by clicking **+ New Flow** in the sidebar and giving it a name.

## Canvas capabilities

- **Drag and drop** — Add nodes from the palette and arrange them on the canvas.
- **7 node types** — Start, End, Input, LLM, Tool, Output, UpdateState.
- **Conditional edges** — Connect nodes with edges that support conditions for branching logic.
- **Auto-layout** — ELKjs-based automatic arrangement of nodes.
- **Undo/redo** — Up to 100 levels of history.
- **Copy/paste and multi-select** — Standard canvas operations.
- **Context menu** — Right-click any node or edge for available operations.

## Node types

| Node | Purpose |
|------|---------|
| **Input** | Collects user input and defines output variables |
| **LLM** | Sends a prompt to the AI model, optionally with tools |
| **Tool** | Executes a specific tool with parameters |
| **Output** | Displays results to the user |
| **UpdateState** | Modifies the flow's state variables |
| **Start / End** | Mark the entry and exit points of the flow |

## Connecting nodes

Drag from a node's output handle to another node's input handle. When a node has multiple outgoing edges, each edge requires a condition to determine which path to follow.

## AI Assist

Describe what you want in natural language and AI Assist builds the flow structure for you. It can also help find and install MCP tools from the store.

## YAML source view

Toggle the source drawer to see and edit the underlying YAML directly using the built-in CodeMirror editor. Changes in the source view are reflected on the canvas and vice versa.

## Auto-save

Flows save automatically with a 1-second debounce after any change.

## Running flows

Click the **Run** button to execute a flow. The execution panel shows real-time node progression and results as each step completes.
