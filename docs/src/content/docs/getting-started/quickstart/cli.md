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

Your configuration is saved to `~/.config/astonish/config.yaml`.

:::tip[Quick Setup for OpenRouter]
```bash
# Set API key directly
export OPENROUTER_API_KEY="your-key-here"
```
:::

## 2. Create Your Flow YAML

Create a file called `hello_world.yaml`:

```bash
# Create the flows directory if needed
mkdir -p ~/.astonish/agents

# Create your flow
cat > ~/.astonish/agents/hello_world.yaml << 'EOF'
name: hello_world
description: My first Astonish agent

nodes:
  - name: greet
    type: llm
    prompt: Greet the user warmly and ask how you can help them today.

flow:
  - from: START
    to: greet
  - from: greet
    to: END
EOF
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

Make your flow accept input:

```yaml
name: greeter
description: Greets a user by name

nodes:
  - name: greet
    type: llm
    prompt: "Greet {name} warmly and wish them a great day."

flow:
  - from: START
    to: greet
  - from: greet
    to: END
```

Run with parameters:

```bash
astonish flows run greeter -p name="Alice"
```

Output:
```
AI: Hello Alice! 🌟 I hope you're having a wonderful day...
```

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
