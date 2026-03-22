---
title: Quick Setup
description: Get from install to first conversation in 3 minutes
---

Three steps to a working AI assistant.

## Step 1: Run the Setup Wizard

```bash
astonish setup
```

This interactive wizard walks you through selecting an AI provider and entering your API key. Supported providers include:

- OpenAI
- Anthropic
- Google Gemini
- Groq
- Ollama (local, no API key needed)
- OpenRouter
- xAI
- DeepSeek
- And more

Configuration is stored in `~/.config/astonish/`. You can re-run `astonish setup` at any time to change providers or update keys.

## Step 2: Install and Start the Daemon

```bash
astonish daemon install && astonish daemon start
```

This installs Astonish as a background service (**launchd** on macOS, **systemd** on Linux) and starts it immediately. The daemon is the foundation of Astonish — it powers everything:

- Serves the **Studio web UI** at [http://localhost:9393](http://localhost:9393) — a full visual interface for chat, settings, flow design, and fleet management
- Listens on **communication channels** (Telegram, Email) once configured
- Runs **scheduled tasks** on a cron schedule
- Manages **fleet sessions** for multi-agent teams

Open [http://localhost:9393](http://localhost:9393) in your browser to access Studio. On first visit, you will be prompted to create a password.

Check status at any time:

```bash
astonish daemon status
```

## Step 3: Start Chatting

Now that the daemon is running, you can chat through Studio at [http://localhost:9393](http://localhost:9393), or use the CLI:

```bash
astonish chat
```

Try a few things to see what it can do:

- "Read the README in this directory and summarize it."
- "Search the web for the latest Go release."
- "List all files larger than 10MB in my home directory."
- "Store my GitHub token as a credential."

The agent has access to 74+ tools out of the box — file operations, web search, browser automation, email, credential management, and more. It will ask for confirmation before running anything destructive.

## Next Steps

- [Choose your interface](/astonish/getting-started/choose-your-interface/) — CLI, Studio, Telegram, or all of them
- [Connect Telegram](/astonish/channels/telegram/) for mobile access
- [Explore built-in tools](/astonish/tools/overview/) — 74+ tools the agent can use
