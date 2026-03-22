---
title: "Utility Commands"
description: "credential, skills, memory, tools, config, setup, tap, and demo"
---

## astonish credential

Manage encrypted credentials. Also available as `astonish credentials`.

| Subcommand | Description |
|------------|-------------|
| `list` (`ls`) | List credentials (values hidden) |
| `show <name>` | Show decrypted values |
| `add <name>` | Add credential interactively |
| `remove <name>` (`rm`) | Remove a credential |
| `test <name>` | Test a credential |
| `master-key` | Set/change/remove master key |

## astonish skills

Manage skill guides.

| Subcommand | Description |
|------------|-------------|
| `list` | List available skills |
| `show <name>` | Show full skill content |
| `check` | Check eligible skills |
| `create <name>` | Create from template |
| `install <slug>` | Install from ClawHub |

## astonish memory

Manage semantic memory.

| Subcommand | Description |
|------------|-------------|
| `search <query>` | Semantic search (flags: `--max-results`, `--min-score`, `--verbose`) |
| `list` | List memory files and chunks |
| `status` | Memory system status |
| `reindex` | Force re-index all files |

## astonish tools

Manage MCP tools.

| Subcommand | Description |
|------------|-------------|
| `list` | List all tools (built-in + MCP). `--json` for JSON output |
| `edit` | Edit MCP configuration |
| `store` | Browse/install MCP servers |
| `servers` | List MCP servers with status. `--json` for JSON output |
| `enable <name>` | Enable an MCP server |
| `disable <name>` | Disable an MCP server |
| `refresh` | Refresh tools cache. `-v` for verbose |

## astonish config

Manage configuration.

| Subcommand | Description |
|------------|-------------|
| `edit` | Open config.yaml in editor |
| `show` | Print config.yaml contents |
| `directory` | Print config directory path |
| `browser` | Configure browser engine |

## astonish setup

Interactive setup wizard. Walks through provider selection, API key configuration, model selection, and optional web search/browser setup.

## astonish tap

Manage extension repositories.

| Subcommand | Description |
|------------|-------------|
| `add <repo>` | Add a tap (supports `--as` alias) |
| `list` | List all taps |
| `remove <name>` | Remove a tap |
| `update` | Update all manifests |

## astonish demo

Generate animated HTML terminal demos from YAML scripts.

Key flags: `--script`, `--output`, `--width`, `--height`, `--title`, `--template`
