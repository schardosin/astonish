---
title: "Studio Overview"
description: "The Astonish web IDE — chat, flows, fleet, and settings in one place"
---

Astonish Studio is the primary visual interface for Astonish. It runs as a full web IDE at `http://localhost:9393` and provides access to every major feature in one place.

## What Studio includes

- **Chat** — Full AI chat with streaming, slash commands, model selection, and approval workflows.
- **Flow Editor** — Visual drag-and-drop flow designer with AI Assist for building automation workflows.
- **Fleet Management** — Create and manage multi-agent team templates, plans, and sessions.
- **Settings** — Configure providers, MCP servers, credentials, channels, browser, scheduler, memory, and more.

The top navigation bar has three main sections: **Chat**, **Canvas** (flow editor), and **Fleet**.

## Launching Studio

**Via daemon (recommended):**

Studio is always available at `http://localhost:9393` while the daemon is running. This is the recommended way to access Studio.

```bash
astonish daemon install && astonish daemon start
```

If the daemon is already installed, just ensure it is running:

```bash
astonish daemon status
```

**Standalone (without daemon):**

If you don't want to run the daemon, you can launch Studio directly:

```bash
astonish studio
astonish studio --port 8080
```

This starts Studio as a foreground process attached to your terminal. It will stop when you close the terminal.

**Dev mode (for contributors):**

```bash
make studio-dev
```

This starts the Vite dev server with live UI reload at `http://localhost:5173`.

## Authentication

Authentication is enabled by default when running via the daemon. On first access, Studio prompts you to create a password. Authentication behavior is configurable under `daemon.auth` in the config file.
