---
title: Overview
description: Introduction to using Astonish from the command line
sidebar:
  order: 1
---

# Using the CLI

The Astonish CLI lets you run AI flows anywhere—from your terminal, in scripts, CI/CD pipelines, or cron jobs.

## Why CLI?

| Feature | Benefit |
|---------|---------|
| **Automation** | Run flows in cron jobs, scripts, CI/CD |
| **Headless** | No GUI required—works on servers |
| **Scripting** | Chain flows together, pass parameters |
| **Version Control** | YAML files work great with Git |

## Core Commands

```bash
astonish [command] [subcommand] [flags]
```

### Main Commands

| Command | Purpose |
|---------|---------|
| `flows` | Run, list, and manage flows |
| `tools` | Manage MCP tools |
| `tap` | Manage extension repositories |
| `config` | View and edit configuration |
| `setup` | Interactive provider setup |
| `studio` | Launch the visual editor |

### Quick Examples

```bash
# Run a flow
astonish flows run my_agent

# Run with parameters
astonish flows run analyzer -p input="analyze this text"

# List available tools
astonish tools list

# Check your config
astonish config show
```

## Getting Help

Every command has built-in help:

```bash
# Main help
astonish --help

# Command help
astonish flows --help

# Subcommand help
astonish flows run --help
```

## Configuration

The CLI uses the same configuration as Studio:

```bash
# View config location
astonish config directory

# View current settings
astonish config show

# Edit in your default editor
astonish config edit
```

## Common Flags

These flags are available on most commands:

| Flag | Description |
|------|-------------|
| `-h, --help` | Show help message |
| `-v, --version` | Show version information |

### `flows run` Specific Flags

| Flag | Description |
|------|-------------|
| `-p key=value` | Pass parameter to flow |
| `-model <name>` | Override default model |
| `-provider <name>` | Override default provider |
| `-debug` | Show verbose output |
| `-browser` | Run with web UI |

## Next Steps

- **[Running Flows](/cli/running-agents/)** — Execute your AI flows
- **[Parameters & Variables](/cli/parameters/)** — Dynamic flow inputs
- **[Automation](/cli/automation/)** — Scripts, cron, CI/CD
