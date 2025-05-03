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
| `run <task> [options]` | Run a specific agentic workflow |
| `flow <task>` | Print the flow of a specific agentic workflow |
| `list` | List all available agents |
| `edit <agent>` | Edit a specific agent |

### Run Command Options

| Option | Description |
|--------|-------------|
| `-p KEY=VALUE`, `--param KEY=VALUE` | Parameter to pass to the agent in key=value format. Can be used multiple times. |

## Examples

### Running an Agent

```bash
astonish agents run agents_creator
```

This command runs the built-in agent creator, which helps you design new agentic workflows.

### Running an Agent with Parameters

You can pass parameters to an agent to pre-populate input fields:

```bash
astonish agents run simple_question_answer_loop -p get_question="Who was Albert Einstein" -p continue_loop=no
```

This runs the agent with pre-defined values for the `get_question` and `continue_loop` nodes, bypassing the interactive prompts.

You can also use file content or environment variables:

```bash
# Using file content
astonish agents run simple_question_answer_loop -p get_question="$(<question.txt)" -p continue_loop=no

# Using environment variables
astonish agents run simple_question_answer_loop -p get_question="$QUESTION" -p continue_loop=no
```

For more details on parameter passing, see the [Parameter Passing](/docs/concepts/parameter-passing) documentation.

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

## Agent Structure

Agents are defined using YAML files with the following structure:

- `description`: A description of what the agent does
- `nodes`: A list of nodes that define the steps in the workflow
- `flow`: The connections between nodes that define the execution path

For more details on agent structure, see the [YAML Configuration](/docs/concepts/yaml-configuration) documentation.
