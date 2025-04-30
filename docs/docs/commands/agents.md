---
sidebar_position: 2
---

# Agents Command

The `agents` command is used to manage and run agentic workflows in Astonish.

## Usage

```bash
astonish agents [OPTIONS] COMMAND [ARGS]
```

## Options

| Option | Description |
|--------|-------------|
| `-h`, `--help` | Show help message and exit |
| `-v`, `--verbose` | Enable verbose output |
| `--version` | Show version information and exit |

## Commands

| Command | Description |
|---------|-------------|
| `run <task>` | Run a specific agentic workflow |
| `flow <task>` | Print the flow of a specific agentic workflow |
| `list` | List all available agents |
| `edit <agent>` | Edit a specific agent |

## Examples

### Running an Agent

```bash
astonish agents run agents_creator
```

This command runs the built-in agent creator, which helps you design new agentic workflows.

### Viewing an Agent's Flow

```bash
astonish agents flow essay
```

This command prints the flow diagram of the "essay" agent, showing the nodes and connections in the workflow.

### Listing Available Agents

```bash
astonish agents list
```

This command lists all available agents, including both built-in agents and custom agents you've created.

### Editing an Agent

```bash
astonish agents edit my_custom_agent
```

This command opens the YAML configuration file for the specified agent in your default editor, allowing you to modify its behavior.

## Agent Locations

Astonish looks for agents in two locations:

1. Built-in agents in the `astonish.agents` package
2. Custom agents in the user's config directory (`~/.config/astonish/agents` on Linux, `~/Library/Application\ Support/astonish/agents` on macOS, `%APPDATA%\astonish\agents` on Windows)

## Implementation Details

The agents command is implemented in the `main.py` file and uses several components:

- `run_agent()` in `agent_runner.py` for executing agents
- `print_flow()` in `graph_builder.py` for visualizing agent flows
- `list_agents()` in `utils.py` for listing available agents
- `edit_agent()` in `utils.py` for opening agent files in an editor

## Agent Structure

Agents are defined using YAML files with the following structure:

- `description`: A description of what the agent does
- `nodes`: A list of nodes that define the steps in the workflow
- `flow`: The connections between nodes that define the execution path

For more details on agent structure, see the [YAML Configuration](/docs/concepts/yaml-configuration) documentation.
