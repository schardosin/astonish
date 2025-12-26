---
title: Creating Flows
description: Step-by-step guide to creating new flows in Astonish Studio
sidebar:
  order: 2
---

# Creating Flows

This guide covers everything about creating and organizing flows in Astonish Studio.

## Creating a New Flow

### Method 1: Sidebar Button

1. Click **+ New Flow** in the sidebar
2. Enter a name (e.g., `my_analyzer`)
3. Click **Create**

![New Flow Dialog](/src/assets/placeholder.png)
*The new flow dialog*

### Method 2: Keyboard Shortcut

Press **N** to open the new flow dialog.

## Naming Conventions

Flow names should be:
- **Lowercase** with underscores: `my_flow_name`
- **Descriptive**: `github_pr_reviewer` not `flow1`
- **Unique**: No duplicate names

Good Examples:
```
daily_report_generator
code_reviewer
slack_summarizer
```

## The Empty Canvas

Every new flow starts with two nodes:

- **START** — Where execution begins
- **END** — Where execution completes

These are system nodes that cannot be deleted.

```
START ─────────────────────────── END
         (your nodes go here)
```

## Adding Your First Node

1. Click anywhere on the canvas between START and END
2. A node type menu appears
3. Select a node type (e.g., **LLM**)

![Node Type Menu](/src/assets/placeholder.png)
*Selecting a node type*

### Available Node Types

| Type | Icon | Purpose |
|------|------|---------|
| **LLM** | 🧠 | Call an AI model |
| **Input** | 📥 | Get user input |
| **Tool** | 🔧 | Call an MCP tool directly |
| **Output** | 📤 | Display a message |
| **Update State** | ⚙️ | Modify variables |

## Connecting Nodes

After adding a node, connect it to the flow:

1. Hover over **START** node's output handle (bottom edge)
2. Click and drag to your new node's input handle (top edge)
3. Release to create the connection
4. Repeat to connect your node to **END**

![Creating Connections](/src/assets/placeholder.png)
*Dragging to create an edge*

## Building a Complete Flow

Here's a typical flow structure:

```
START
  │
  ▼
┌─────────┐
│  Input  │  ← Get user question
└─────────┘
  │
  ▼
┌─────────┐
│   LLM   │  ← Process with AI
└─────────┘
  │
  ▼
 END
```

## Saving Your Flow

### Auto-Save

Studio does **not** auto-save. Always save manually:

- **Cmd+S** (Mac) or **Ctrl+S** (Windows/Linux)
- Click the **Save** button in the top bar

### Save Location

Flows are saved to:
```
~/.astonish/agents/<flow-name>.yaml
```

## Opening Existing Flows

Click any flow name in the sidebar to open it.

The canvas will load with all nodes and connections.

## Duplicating Flows

Currently, duplicate a flow by:

1. Open the flow in Studio
2. Click **YAML** to view the raw file
3. Copy the content
4. Create a new flow
5. Paste into the YAML editor

## Deleting Flows

1. Right-click the flow in the sidebar
2. Select **Delete**
3. Confirm the deletion

:::caution
Deleted flows cannot be recovered unless you have a backup.
:::

## Flow Organization

For large projects, consider:

- **Naming prefixes**: `api_*`, `report_*`, `slack_*`
- **Git version control**: Track changes to your YAML files
- **Taps**: Share flows across machines

## Next Steps

- **[Working with Nodes](/studio/working-with-nodes/)** — Deep dive into each node type
- **[Connecting Edges](/studio/connecting-edges/)** — Add conditions and branching
