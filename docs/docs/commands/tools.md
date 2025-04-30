---
sidebar_position: 3
---

# Tools Command

The `tools` command is used to manage and configure tools in Astonish, including both built-in tools and MCP (Model Context Protocol) tools.

## Usage

```bash
astonish tools [OPTIONS] COMMAND [ARGS]
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
| `list` | List available tools |
| `edit` | Edit MCP configuration |

## Examples

### Listing Available Tools

```bash
astonish tools list
```

This command lists all available tools, including both built-in tools and MCP tools. For each tool, it displays the name and description.

### Editing MCP Configuration

```bash
astonish tools edit
```

This command opens the MCP configuration file in your default editor, allowing you to modify the MCP server settings.

## Built-in Tools

Astonish comes with several built-in tools:

- `read_file`: Read the contents of a file
- `write_file`: Write content to a file
- `shell_command`: Execute a shell command
- `validate_yaml_with_schema`: Validate YAML content against a schema

These tools are always available and don't require any additional configuration.

## MCP Tools

MCP (Model Context Protocol) tools are external tools that can be integrated with Astonish. These tools are provided by MCP servers and can extend the capabilities of your agents.

To use MCP tools, you need to:

1. Configure the MCP server in the MCP configuration file
2. Enable the tools you want to use in your agent's YAML configuration

## Built-in tools

The built-in tools include:

- `read_file`: Read the contents of a file
- `write_file`: Write content to a file
- `shell_command`: Execute a shell command
- `validate_yaml_with_schema`: Validate YAML content against a schema

For more details on using tools in agents, see the [Tools](/docs/concepts/tools) documentation.
