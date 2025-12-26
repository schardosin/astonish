---
title: Overview
description: Introduction to Astonish Studio, the visual flow editor
sidebar:
  order: 1
---

# Astonish Studio

Astonish Studio is the visual interface for designing, testing, and managing AI flows. It transforms complex agent logic into intuitive drag-and-drop workflows.

![Astonish Studio Interface](/src/assets/placeholder.png)
*The Astonish Studio workspace*

## Why Use Studio?

| Feature | Benefit |
|---------|---------|
| **Visual Design** | See your entire flow at a glance |
| **Real-time Testing** | Run and debug without leaving the editor |
| **Node Configuration** | Forms instead of YAML syntax |
| **MCP Store** | Browse and install tools visually |
| **Flow Store** | Discover and install community flows |

## Launching Studio

```bash
astonish studio
```

This starts the web server at **http://localhost:9393**.

### Custom Port

```bash
astonish studio -port 8080
```

## Workspace Overview

The Studio interface has four main areas:

### 1. Sidebar (Left)

- **Flows List** — All your local and installed flows
- **+ New Flow** — Create a new flow
- **Settings** — Access configuration

### 2. Canvas (Center)

- **Nodes** — Drag and position processing steps
- **Edges** — Connect nodes to define flow order
- **Zoom/Pan** — Navigate large flows

### 3. Node Editor (Right)

When you select a node:
- Configure properties
- Set prompts and parameters
- Choose tools and outputs

### 4. Top Bar

- **Run** — Execute the current flow
- **Save** — Save changes (Cmd/Ctrl+S)
- **YAML** — View/edit raw YAML

## What Studio Creates

Everything you build in Studio is saved as a YAML file:

```
~/.astonish/agents/<flow-name>.yaml
```

These files can be:
- Edited with any text editor
- Run from the command line
- Version controlled with Git
- Shared with your team

## Studio vs CLI

| I want to... | Use |
|--------------|-----|
| Design a new flow | Studio |
| Debug visually | Studio |
| Run in automation | CLI |
| Edit YAML directly | CLI or any editor |
| Quick one-off run | CLI |

Both work with the same YAML format—use whichever fits your workflow.

## Next Steps

- **[Creating Flows](/studio/creating-flows/)** — Build your first flow
- **[Working with Nodes](/studio/working-with-nodes/)** — Learn all node types
- **[Connecting Edges](/studio/connecting-edges/)** — Add logic and branching
- **[Running & Debugging](/studio/running-debugging/)** — Test your flows
