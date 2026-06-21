# Quick Start: Local

The local deployment uses SQLite for all storage and vector search. No external database, no server setup, no accounts to create. All platform features run locally out of the box.

## 1. Run the Setup Wizard

After [installing](./installation.md) the binary, configure your AI provider:

```bash
astonish setup
```

The wizard prompts you to select a provider (OpenAI, Anthropic, Google Gemini, etc.) and enter your API key. Configuration is stored locally in `~/.config/astonish/`.

## 2. Start the Daemon

The daemon runs in the background and serves Studio, handles scheduled flows, channel integrations, and persistent sessions:

```bash
astonish daemon install
astonish daemon start
```

This registers Astonish as a system service (launchd on macOS, systemd on Linux). Studio is immediately available at `http://localhost:9393`.

## 3. Start Chatting

Open a conversation in the terminal:

```bash
astonish chat
```

Or open Studio in your browser at `http://localhost:9393` for the full web interface with the visual flow designer, apps tab, and chat interface.

## Common Commands

```bash
astonish chat                           # New chat session
astonish chat -p anthropic -m claude-sonnet-4-20250514  # Use a specific provider and model
astonish chat --resume                  # Resume the last session
astonish flows list                     # List distilled flows
astonish flows run <name>               # Run a saved flow
```

## What You Get

- Full agent engine with 90+ tools
- Personal memory with semantic search (built into SQLite)
- Flow distillation (chat to reusable YAML)
- Generative UI (describe apps, get live React dashboards)
- MCP server support
- All 12+ AI providers
- Studio web interface at `http://localhost:9393`

## Next Steps

- [Choose Your Interface](./choose-your-interface.md) — Studio, CLI, Telegram, and more
- [Quick Start: Cloud](./quick-start-cloud.md) — Scale to your team with PostgreSQL
