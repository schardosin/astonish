---
title: CLI Quickstart
description: Build your first AI agent from the command line in under 5 minutes
sidebar:
  order: 2
---

# CLI Quickstart

This guide walks you through creating and running your first AI agent using the command line and YAML.

**Time:** ~5 minutes

## Prerequisites

- [Astonish installed](/getting-started/installation/)
- An API key from any supported provider

## 1. Run Interactive Setup

First, configure an AI provider:

```bash
astonish setup
```

This launches an interactive wizard:

1. Select your provider (e.g., **OpenRouter**)
2. Enter your API key
3. Choose a default model
4. Done!

To find where your configuration is stored, run:

```bash
astonish config directory
```

## 2. Create Your Flow YAML

Create a file called `hello_world.yaml`:

```yaml
description: hello_world
nodes:
  - name: greet
    type: llm
    system: You are a helpful assistant.
    prompt: Greet the user warmly and ask how you can help them today.
    output_model:
      response: str
    user_message:
      - response

flow:
  - from: START
    to: greet
  - from: greet
    to: END
```

Import it into Astonish:

```bash
astonish flows import hello_world.yaml
```

## 3. Run Your Flow

```bash
astonish flows run hello_world
```

You'll see the AI response stream to your terminal:

```
AI: Hello! 👋 I'm happy to help you today. What can I assist you with?
```

## 4. List Available Flows

See all your flows:

```bash
astonish flows list
```

Output:
```
AVAILABLE FLOWS

📁 LOCAL (1)
  hello_world
    My first Astonish agent
```

## 5. Add Parameters

Parameters come from **input nodes**. Without `-p`, Astonish prompts interactively. With `-p`, you provide values upfront.

Create a new flow called `personalized_greeting.yaml`:

```yaml
description: personalized_greeting
nodes:
  - name: get_name
    type: input
    prompt: What's your name?
    output_model:
      user_name: str
  - name: greet_user
    type: llm
    system: You are a helpful assistant.
    prompt: Greet the user warmly by their name '{user_name}' and ask how you can help them today.
    output_model:
      response: str
    user_message:
      - response

flow:
  - from: START
    to: get_name
  - from: get_name
    to: greet_user
  - from: greet_user
    to: END
```

Import and run with a parameter:

```bash
astonish flows import personalized_greeting.yaml
astonish flows run personalized_greeting -p get_name=Rafael
```

Output:
```
✓ Using provided value for 'get_name': Rafael
✓ Processing greet_user...

Agent:
Hello Rafael! It's wonderful to connect with you today. How can I assist you?
✓ Processing END...
```

Without `-p`, Astonish would prompt: *"What's your name?"*

## 6. Use a Different Model

Override the default model at runtime:

```bash
# Use a specific model
astonish flows run hello_world -model gpt-4

# Use a different provider
astonish flows run hello_world -provider anthropic -model claude-3-opus
```

## 7. Enable Debug Mode

See what's happening under the hood:

```bash
astonish flows run hello_world -debug
```

This shows:
- Node execution order
- Tool calls (if any)
- State changes

## Common Commands

| Command | Description |
|---------|-------------|
| `astonish flows list` | List all flows |
| `astonish flows run <name>` | Run a flow |
| `astonish flows run <name> -p key=value` | Run with parameters |
| `astonish flows show <name>` | Visualize flow structure |
| `astonish flows edit <name>` | Open YAML in editor |
| `astonish config show` | Show current config |

## YAML Structure Reference

Here's the basic structure:

```yaml
name: flow-name           # Required: unique identifier
description: "..."        # Optional: what this flow does

nodes:                    # Required: list of processing steps
  - name: node_name       # Unique name for this node
    type: llm             # Node type: llm, input, tool, output
    prompt: "..."         # What the AI should do

flow:                     # Required: how nodes connect
  - from: START           # Every flow starts at START
    to: first_node
  - from: first_node
    to: END               # Every flow ends at END
```

## Next Steps

Now that you're running flows from CLI:

- **[Parameters & Variables](/cli/parameters/)** — Advanced parameter handling
- **[Automation](/cli/automation/)** — Cron jobs, CI/CD integration
- **[YAML Reference](/concepts/yaml/)** — Complete schema documentation
- **[Add MCP Servers](/using-the-app/add-mcp-servers/)** — Connect tools like GitHub
