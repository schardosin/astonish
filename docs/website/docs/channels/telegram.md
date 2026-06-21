# Telegram

The Telegram channel connects Astonish to a Telegram bot, enabling real-time AI conversations directly in Telegram chats.

## Setup

### 1. Create a Bot with BotFather

1. Open Telegram and message [@BotFather](https://t.me/BotFather)
2. Send `/newbot` and follow the prompts
3. Copy the bot token (format: `123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11`)

### 2. Configure via CLI

Run the interactive setup wizard:

```bash
astonish channels setup telegram
```

The wizard validates your token via the Telegram API, detects users via polling, and stores the token securely in the encrypted credential store.

Alternatively, configure manually in your config file:

```yaml
channels:
  telegram:
    enabled: true
    bot_token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
    allow_from:
      - "987654321"   # Your Telegram user ID
      - "123456789"   # Another allowed user
```

To find your Telegram user ID, message [@userinfobot](https://t.me/userinfobot).

### 3. Start the Daemon

```bash
astonish daemon start
```

The daemon manages the Telegram listener alongside other background services.

## Available Commands

These commands are available within the Telegram chat:

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/status` | Show provider, model, and session info |
| `/new` | Start a new session |
| `/distill` | Distill the last task into a reusable flow |
| `/jobs` | Show scheduled jobs |
| `/org <slug>` | Switch active organization |
| `/team <slug>` | Switch active team |
| `/context` | Show current routing context |
| `/fleet` | Start a fleet session |

## Multi-Tenant Routing (PostgreSQL)

In PostgreSQL deployments, the Telegram channel gains multi-tenant capabilities:

- **Database-backed allowlist** — Managed through the platform, not static config. Changes take effect immediately without restarting.
- **Dynamic routing** — Each message is routed to the correct organization and team based on the user's linked identity.
- **Context switching** — Users can run `/org` and `/team` commands to change their active context.

### Linking Telegram to Platform Account

Users link their Telegram account to their platform identity using a code:

```
User: /link ABC123
Bot:  ✓ Account linked successfully. You're now connected as alice@acme.corp
```

The link code is generated in Studio under user settings.

### Example Interaction

```
User: /org acme-corp
Bot:  ✓ Switched to organization: acme-corp

User: /team backend
Bot:  ✓ Switched to team: backend

User: What's the status of the migration task?
Bot:  Based on the team's recent activity...
```

Context switches are persistent. Starting a new session (`/new`) resets to the user's default org and team.

## Managing the Channel

```bash
astonish channels status             # Check channel status
astonish channels disable telegram   # Disable the channel
```
