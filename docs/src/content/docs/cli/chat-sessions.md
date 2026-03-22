---
title: "chat & sessions"
description: "Interactive AI chat and session management commands"
---

## astonish chat

Start an interactive chat session with an AI agent that can use tools.

### Flags

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--provider` | `-p` | string | from config | AI provider |
| `--model` | `-m` | string | from config | Model name |
| `--workspace` | `-w` | string | current dir | Working directory |
| `--resume` | `-r` | string | | Resume session by ID |
| `--auto-approve` | | bool | false | Auto-approve tool executions |
| `--debug` | | bool | false | Enable debug output |

### Examples

```
astonish chat                           # New session with defaults
astonish chat -p openai -m gpt-4o      # Specific provider/model
astonish chat --resume abc123          # Resume a session
astonish chat --auto-approve           # Skip tool approval prompts
```

## astonish sessions

Manage persistent chat sessions.

### Subcommands

| Subcommand | Aliases | Description |
|------------|---------|-------------|
| `list` | `ls` | List all sessions |
| `show <id>` | | Show session trace |
| `delete <id>` | `rm` | Delete a session |
| `clear` | | Delete all sessions (with confirmation) |

### sessions show flags

| Flag | Short | Description |
|------|-------|-------------|
| `--json` | | Output as JSON |
| `--tools-only` | `-t` | Only show tool calls |
| `--verbose` | `-v` | Full args/results (no truncation) |
| `--recursive` | `-r` | Include sub-agent traces |
| `--last` | `-n` | Show last N events |

Session IDs support prefix matching — you don't need the full ID.
