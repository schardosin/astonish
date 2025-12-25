---
title: Your First Agent
description: Step-by-step guide to building your first AI agent
---

# Your First Agent

In this tutorial, you'll build a simple AI agent that greets users and answers questions.

## Prerequisites

- Astonish installed ([Installation Guide](/getting-started/installation/))
- An AI provider configured (we'll use the setup wizard)

## Step 1: Launch Studio

Open your terminal and run:

```bash
astonish studio
```

This opens Astonish Studio at `http://localhost:9393`.

## Step 2: Configure a Provider

If this is your first time, the Setup Wizard appears:

1. Select a provider (e.g., **Google Gemini**)
2. Enter your API key
3. Choose a default model
4. Click **Complete Setup**

## Step 3: Create New Agent

1. Click **+ New Agent** in the sidebar
2. Name it `greeter`
3. Click **Create**

You'll see an empty canvas with START and END nodes.

## Step 4: Add an LLM Node

1. Click anywhere on the canvas to add a node
2. Select **LLM Node**
3. Configure it:
   - **Name**: `greet`
   - **Prompt**: `Greet the user warmly and ask how you can help them today.`

## Step 5: Connect the Nodes

1. Hover over the START node's bottom handle
2. Drag to the `greet` node's top handle
3. Repeat: drag from `greet` to END

Your flow should look like:

```
START → greet → END
```

## Step 6: Run It!

1. Click the **Run** button (▶️)
2. Watch the output stream in the chat panel
3. The agent will greet you!

## Step 7: Save

Press `Cmd+S` (or `Ctrl+S`) to save. Your agent YAML is stored in `~/.astonish/flows/greeter.yaml`.

## The Generated YAML

Here's what was created:

```yaml
name: greeter
description: ""

nodes:
  - name: greet
    type: llm
    prompt: Greet the user warmly and ask how you can help them today.

flow:
  - from: START
    to: greet
  - from: greet
    to: END
```

## Running from CLI

Now run it from anywhere:

```bash
astonish agents run greeter
```

## Next Steps

- [PR Description Generator](/tutorials/pr-generator/) — A real-world example
- [Using MCP Tools](/tutorials/mcp-tools/) — Add external capabilities
- [YAML Configuration](/concepts/yaml/) — Deep dive into the config format
