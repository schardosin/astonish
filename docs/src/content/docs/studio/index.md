---
title: Overview
description: Introduction to Astonish Studio, the visual flow editor
sidebar:
  order: 1
---

# Astonish Studio

Astonish Studio is the visual interface for designing, testing, and managing AI flows. It transforms complex agent logic into intuitive drag-and-drop workflows.

![Astonish Studio Interface](/astonish/images/introduction-canvas.webp)
*The Astonish Studio workspace*

## Why Use Studio?

| Feature | Benefit |
|---------|---------|
| **AI Assist** | Create or modify flows and nodes using natural language prompts |
| **Visual Design** | See your entire flow at a glance |
| **Real-time Testing** | Run and debug without leaving the editor |
| **Node Configuration** | Forms instead of YAML syntax |
| **MCP Store** | Browse and install tools visually |
| **Flow Store** | Discover and install community flows |

## AI Assist

The **AI Assist** is one of Studio's most powerful features. Describe what you want in plain language, and it builds or modifies your flow for you.

- **Create entire flows** — "Create a flow that summarizes a webpage and sends it to Slack"
- **Add nodes** — "Add a condition to check if the response contains an error"
- **Modify existing nodes** — "Change this prompt to be more formal"

Click the **✨ AI Assist** button to open the assistant and start prompting.

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

The Studio interface has five main areas:

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
- **View Source** — View/edit raw YAML

### 5. AI Assist Button (Bottom Right)

The floating ✨ button in the bottom right corner opens the AI Assist panel. Use it to create or modify flows using natural language.

## What Studio Creates

Everything you build in Studio is automatically saved as a YAML file. To find your flows directory:

```bash
astonish config directory
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

- **[Creating Flows](/astonish/studio/creating-flows/)** — Build your first flow
- **[Working with Nodes](/astonish/studio/working-with-nodes/)** — Learn all node types
- **[Connecting Edges](/astonish/studio/connecting-edges/)** — Add logic and branching
- **[Running & Debugging](/astonish/studio/running-debugging/)** — Test your flows
