# Telegram

The Telegram channel connects Astonish to a Telegram bot, enabling real-time AI conversations directly in Telegram chats.

## Setup

### 1. Create a Bot with BotFather

1. Open Telegram and message [@BotFather](https://t.me/BotFather)
2. Send `/newbot` and follow the prompts
3. Copy the bot token (format: `123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11`)

### 2. Configure Astonish

Add the Telegram section to your config file:

```yaml
channels:
  telegram:
    token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
    allowlist:
      - "987654321"   # Your Telegram user ID
      - "123456789"   # Another allowed user
```

To find your Telegram user ID, message [@userinfobot](https://t.me/userinfobot).

### 3. Start the Channel

```bash
astonish daemon start
```

The daemon manages the Telegram listener alongside other background services.

## Configuration Options

| Option | Description | Default |
|--------|-------------|---------|
| `token` | Bot API token from BotFather | Required |
| `allowlist` | List of allowed Telegram user IDs | `[]` |
| `parse_mode` | Message formatting (`Markdown`, `HTML`) | `Markdown` |

## Supported Commands

These commands are available within the Telegram chat:

| Command | Description |
|---------|-------------|
| `/new` | Start a new session |
| `/model <name>` | Switch model |
| `/status` | Show agent status |

## Cloud Deployment

In cloud deployments, the Telegram channel gains multi-tenant capabilities.

### Database Allowlist

Instead of a static config list, allowed users are managed through the platform API:

```bash
astonish platform org invite --channel telegram --user-id 987654321
```

The allowlist is checked against the database on every incoming message, so changes take effect immediately without restarting the daemon.

### Organization Routing

When a user belongs to multiple organizations, Astonish routes messages to their default org. Users can switch context with in-chat commands:

| Command | Description |
|---------|-------------|
| `/org <name>` | Switch active organization |
| `/team <name>` | Switch active team within the current org |
| `/org` | Show current organization |
| `/team` | Show current team |

### Example Interaction

```
User: /org acme-corp
Bot:  ✓ Switched to organization: acme-corp

User: /team backend
Bot:  ✓ Switched to team: backend

User: What's the status of the migration task?
Bot:  Based on the team's recent activity...
```

Context switches persist for the duration of the conversation. Starting a new session (`/new`) resets to the user's default org and team.
