# Slack

The Slack channel integrates Astonish into your Slack workspace, enabling AI agent interactions in channels, DMs, and threads.

## Setup

### 1. Create a Slack App

1. Go to [api.slack.com/apps](https://api.slack.com/apps) and click **Create New App**
2. Choose **From scratch**, name it, and select your workspace
3. Under **OAuth & Permissions**, add these bot token scopes:
   - `chat:write`
   - `app_mentions:read`
   - `im:history`
   - `im:read`
   - `im:write`
4. Install the app to your workspace and copy the **Bot User OAuth Token** (`xoxb-...`)
5. For Socket Mode: Under **App-Level Tokens**, create a token with `connections:write` scope (`xapp-...`)
6. Under **Event Subscriptions**, subscribe to:
   - `message.im`
   - `app_mention`

### 2. Configure via CLI

Run the interactive setup wizard:

```bash
astonish channels setup slack
```

The wizard validates your bot token, collects the app-level token (for Socket Mode) or signing secret (for Events API), and stores credentials securely.

Alternatively, configure manually:

```yaml
channels:
  slack:
    enabled: true
    mode: "socket"              # "socket" (WebSocket) or "events" (HTTP webhook)
    bot_token: "xoxb-..."       # Stored in credential store
    app_token: "xapp-..."       # For Socket Mode (stored in credential store)
    allow_from:
      - "U0KRQLJ9H"            # Allowed Slack user IDs
```

### 3. Start the Daemon

```bash
astonish daemon start
```

## Connection Modes

| Mode | Transport | Use Case |
|------|-----------|----------|
| `socket` | WebSocket (Socket Mode) | Recommended for most setups. No public URL needed. |
| `events` | HTTP webhook (Events API) | For environments requiring HTTP endpoints. Needs `signing_secret`. |

## Interaction Patterns

- **Direct Message** — Send a DM to the bot for a private conversation
- **Mention** — `@Astonish <message>` in any channel the bot is invited to
- **Thread** — Replies within a thread maintain session context

## Available Commands

Send these as messages to the bot:

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/status` | Show this session's provider, model (including pin), and session info |
| `/new` | Start a new session |
| `/distill` | Distill the last task into a reusable flow |
| `/jobs` | Show scheduled jobs |
| `/org <slug>` | Switch active organization |
| `/team <slug>` | Switch active team |
| `/context` | Show current routing context |
| `/fleet` | Start a fleet session |

## Multi-Tenant Routing (PostgreSQL)

In PostgreSQL deployments, Slack gains multi-tenant capabilities:

- **User linking** — Slack users link their account to their platform identity via `/link <code>`
- **Context switching** — `/org` and `/team` commands change the active context
- **Platform-managed access** — Access is governed by platform org membership rather than a static allowlist

### Linking Slack to Platform Account

```
User: /link ABC123
Bot:  ✓ Account linked. You're now connected as alice@acme.corp (org: acme, team: backend)
```

## Managing the Channel

```bash
astonish channels status           # Check channel status
astonish channels disable slack    # Disable the channel
```
