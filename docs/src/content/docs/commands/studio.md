---
title: astonish studio
description: Launch the visual flow designer
---

# astonish studio

Launch the Astonish Studio web interface for visual flow design.

## Usage

```bash
astonish studio [flags]
```

## Description

Opens a local web interface at `http://localhost:9393` where you can:

- **Design flows visually** — Drag-and-drop nodes, connect edges
- **Configure providers** — Setup wizard for AI providers
- **Manage MCP servers** — Add and configure tools
- **Run and test** — Execute flows with real-time output
- **Browse Flow Store** — Install community flows

## Flags

| Flag | Description |
|------|-------------|
| `--port, -p` | Port to run on (default: 9393) |
| `--open, -o` | Open browser automatically |

## Examples

```bash
# Launch on default port
astonish studio

# Launch on custom port
astonish studio --port 8080

# Launch and open browser
astonish studio --open
```

## First Run

On first launch, if no AI provider is configured, the Setup Wizard will guide you through:

1. Selecting a provider (Gemini, Claude, GPT-4, etc.)
2. Entering your API key
3. Choosing a default model

You can also run `astonish setup` for CLI-based configuration.
