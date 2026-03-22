---
title: "Studio Chat"
description: "AI chat interface in the Studio web UI"
---

The Studio chat is the web-based equivalent of `astonish chat` — a full conversational AI interface with streaming, tool use, and session management.

## Features

- **SSE streaming** — Responses stream in real-time as the agent generates them.
- **Thinking indicators** — See when the agent is reasoning versus executing tools.
- **Tool call display** — Expandable blocks showing what the agent did and the results.
- **Model selector** — Switch AI providers and models on the fly.
- **Session management** — Start new sessions, browse history, and resume past conversations.
- **Image support** — Paste or upload images for the agent to analyze.
- **Markdown rendering** — Agent responses render with full GitHub-flavored markdown.
- **Approval workflows** — When the agent wants to execute a tool, you can approve or reject the action.

## Slash commands

Type `/` in the chat input to see available commands:

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/status` | Current session info |
| `/new` | Start a fresh session |
| `/compact` | Compress conversation history to free context window space |
| `/distill` | Convert the current session into a reusable flow |
| `/fleet` | Launch a fleet session |
| `/fleet-plan` | Start the fleet plan creation wizard |

## Session sidebar

The sidebar lists recent sessions with previews. Click any session to resume it.

Sessions are shared between CLI and Studio. A session started with `astonish chat` on the command line appears in Studio, and vice versa.
