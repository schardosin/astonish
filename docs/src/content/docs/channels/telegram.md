---
title: Telegram
description: Set up a Telegram bot to chat with Astonish from anywhere
---

The Telegram integration turns Astonish into a Telegram bot you can message from your phone. It provides full AI agent capabilities — the same tools available in CLI and Studio chat.

## Setup

### 1. Create a Bot with BotFather

1. Open [@BotFather](https://t.me/BotFather) in Telegram.
2. Send `/newbot` and follow the prompts.
3. Copy the bot token.

### 2. Get Your Telegram User ID

Send a message to [@userinfobot](https://t.me/userinfobot) (or a similar bot) to retrieve your numeric user ID.

### 3. Configure

The easiest way is the interactive setup:

```bash
astonish channels setup telegram
```

This walks you through entering the bot token and allowed user IDs.

You can also configure Telegram through **Studio Settings > Channels**, or manually in `config.yaml`:

```yaml
channels:
  enabled: true
  telegram:
    enabled: true
    bot_token: "your-bot-token"    # Stored in credential store after setup
    allow_from:
      - "123456789"                # Your Telegram user ID
```

### 4. Start the Daemon

Start or restart the daemon to activate the channel:

```bash
astonish daemon restart
```

### 5. Test It

Send a message to your bot in Telegram. It should respond.

## Features

- **Full agent capabilities** — same tools available as CLI/Studio chat.
- **HTML-formatted responses** — bold, code blocks, and links render natively.
- **Typing indicators** — the bot shows a typing status while the agent is working.
- **Incremental delivery** — long responses are delivered in parts as they are generated.
- **Image support** — send images for the agent to analyze.

## Security

The `allow_from` list restricts which Telegram user IDs can interact with the bot. Only users whose numeric ID appears in this list will receive responses. Messages from other users are ignored.

## Fleet Commands

You can manage fleet sessions directly from Telegram:

| Command | Description |
|---------|-------------|
| `/fleet` | Start a fleet session |
| `/fleet_plan` | Create a fleet plan |
| `/fleet_stop` | Stop an active fleet session |

## Scheduled Delivery

Scheduled tasks can send results to Telegram. When creating a schedule, specify the channel and target for delivery.
