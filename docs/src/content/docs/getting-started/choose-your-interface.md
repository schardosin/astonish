---
title: Choose Your Interface
description: "CLI, Studio, Telegram, or daemon — pick what works for you"
---

Astonish exposes multiple interfaces, all powered by the daemon. They share the same sessions, memory, credentials, and configuration — pick whichever fits the moment.

## Daemon (Required)

The daemon is the foundation of Astonish. It must be running for all interfaces to work. If you haven't installed it yet:

```bash
astonish daemon install && astonish daemon start
```

When running as a service, the daemon:

- Serves the Studio web UI
- Listens on Telegram and Email channels
- Executes scheduled tasks
- Manages fleet multi-agent sessions

See [Running as a Service](/astonish/getting-started/running-as-a-service/) for details on management, logs, and configuration.

## Studio (Web UI)

Best for visual work, managing settings, designing flows, and fleet management. Accessed at [http://localhost:9393](http://localhost:9393) when the daemon is running.

Studio provides a chat interface with streaming responses, slash commands, a model selector, session history, visual flow designer, and settings management — all in the browser.

## CLI Chat

```bash
astonish chat
```

Best for quick terminal tasks, scripting, and SSH sessions. Provides full access to all 74+ tools with session persistence.

Useful flags:

| Flag | Purpose |
|---|---|
| `--provider` | Override the default AI provider |
| `--model` | Override the default model |
| `--resume` | Resume a previous session |
| `--workspace` | Set the working directory for the session |
| `--auto-approve` | Skip confirmation prompts for tool execution |

Example:

```bash
astonish chat --provider openai --model gpt-4o --workspace ~/projects/myapp
```

## Telegram

Best for mobile access and async tasks. Configure a Telegram bot through `astonish channels`, then chat with your AI assistant from anywhere — your phone, tablet, or desktop Telegram client.

Telegram also receives results from scheduled tasks, so you can get notified when background work completes.

## Email Channel

The agent can monitor an inbox and respond to incoming emails. Configure an email channel to let Astonish process messages, draft replies, or trigger automations based on what arrives.
