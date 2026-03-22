---
title: "Chat Overview"
description: "The core Astonish experience — conversational AI with tools"
---

Chat is the primary way to use Astonish. You have a conversation with an AI agent that can use 74+ built-in tools to accomplish tasks on your behalf.

## How it works

1. You ask a question or describe a task.
2. The agent thinks about the best approach.
3. It uses tools — file operations, shell commands, web requests, browser automation, email, and more — to carry out the work.
4. It responds with results.

The agent streams responses in real-time with thinking indicators so you always know what it is doing.

## Interfaces

The daemon must be running for chat to work. Astonish chat is available through two interfaces:

- **Studio** — the web-based chat UI at `http://localhost:9393` (served by the daemon)
- **CLI** — `astonish chat` in your terminal

Both interfaces support the same capabilities, slash commands, and tool set. Sessions are shared — a conversation started in CLI appears in Studio and vice versa.

## Starting a chat

Start a new session:

```bash
astonish chat
```

Resume a previous session:

```bash
astonish chat --resume <id>
```

### Key flags

| Flag | Short | Description |
|---|---|---|
| `--provider` | `-p` | AI provider to use |
| `--model` | `-m` | Model name |
| `--workspace` | `-w` | Working directory for the session |
| `--auto-approve` | | Skip tool approval prompts |
| `--debug` | | Enable debug logging |

## Slash commands

These commands are available in both CLI and Studio:

| Command | Description |
|---|---|
| `/help` | Show available commands |
| `/status` | Show session info |
| `/new` | Start a new session |
| `/compact` | Compress conversation history (manages context window) |
| `/distill` | Convert current session into a reusable flow |
| `/fleet` | Launch a fleet session |
| `/fleet-plan` | Create a fleet plan |

## Tool approval

By default, the agent asks for your confirmation before executing tools. This gives you control over what actions are taken.

To skip approval prompts, either pass the `--auto-approve` flag or configure the default in your settings file.
