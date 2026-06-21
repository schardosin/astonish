# Quick Start: Local

The local deployment uses SQLite as the platform backend. No external database or server setup needed — all platform features (authentication, encryption, sessions, memory) run locally out of the box.

## 1. Run the Setup Wizard

After [installing](./installation.md) the binary, run the setup wizard:

```bash
astonish setup
```

The wizard walks you through:

1. **Deployment mode** — Choose SQLite (local) or PostgreSQL (cloud). Select SQLite for local use.
2. **Organization** — Create your organization name and slug.
3. **Admin account** — Set your admin email and password.
4. **AI provider** — Select a provider (OpenAI, Anthropic, Google Gemini, etc.) and enter your API key.

Configuration is stored in `~/.config/astonish/`.

## 2. Start the Daemon

The daemon runs the platform in the background — it serves Studio, manages sessions, handles scheduled flows, and channel integrations:

```bash
astonish daemon install
astonish daemon start
```

This registers Astonish as a system service (launchd on macOS, systemd on Linux). The platform is now running.

## 3. Open Studio

Open your browser and navigate to:

```
http://localhost:9393
```

Log in with the admin email and password you created during setup. Studio gives you the full visual experience: chat interface, flow designer, apps tab, settings management, and real-time execution display.

## 4. Connect the CLI (Optional)

If you prefer working in the terminal, authenticate the CLI against your local platform:

```bash
astonish login http://localhost:9393
```

Enter your admin email and password when prompted. After login:

```bash
astonish chat                                      # New chat session
astonish chat -p anthropic -m claude-sonnet-4-20250514  # Specific provider/model
astonish chat --resume                             # Resume last session
astonish flows list                                # List distilled flows
astonish flows run <name>                          # Run a saved flow
```

## What You Get

- Full agent engine with 90+ tools
- Personal memory with semantic search
- Flow distillation (chat → reusable YAML workflows)
- Generative UI (describe apps, get live React dashboards)
- MCP server support
- All 12+ AI providers
- Studio web interface at `http://localhost:9393`
- Envelope encryption for credentials
- Session persistence across restarts

## Next Steps

- [Choose Your Interface](./choose-your-interface.md) — Studio, CLI, Telegram, and more
- [Quick Start: Cloud](./quick-start-cloud.md) — Scale to your team with PostgreSQL
