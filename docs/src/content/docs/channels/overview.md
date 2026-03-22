---
title: Channels Overview
description: Connect Astonish to external messaging platforms
---

Channels let Astonish communicate through external messaging platforms. Instead of using the CLI or Studio, you can interact with your agent through Telegram, Email, and other services.

## Architecture

Channels use a plugin-based architecture. Each channel is an adapter that translates between the platform's protocol and Astonish's internal chat system. All channels share the same session system, memory, and tools available in CLI and Studio chat.

## How Channels Work

1. The daemon starts channel listeners based on your configuration.
2. Listeners poll for new messages (Telegram uses long polling, Email uses IMAP polling).
3. Incoming messages are routed to the AI agent.
4. Agent responses are formatted and delivered back through the channel.

Channels require the daemon to be running:

```bash
astonish daemon start
```

## Configuration

The easiest way to set up a channel is the interactive CLI:

```bash
astonish channels setup telegram
astonish channels setup email
```

You can also configure channels through **Studio Settings > Channels**.

The top-level `channels.enabled` flag acts as a master switch. Individual channels have their own `enabled` flag. For the full configuration schema, see the [Config File Reference](/astonish/configuration/config-reference/).

## Channel Management CLI

| Command | Description |
|---------|-------------|
| `astonish channels status` | Check status of all channels |
| `astonish channels setup <channel>` | Interactive setup for a channel |
| `astonish channels disable <channel>` | Disable a channel |

## Scheduled Task Delivery

Scheduled task results can be broadcast to channels. For example, a daily report can be configured to send its output to Telegram or Email automatically.

## Available Channels

| Channel | Status |
|---------|--------|
| [Telegram](/channels/telegram) | Available |
| [Email](/channels/email-channel) | Available |
| Slack | Planned |
| Discord | Planned |
| WhatsApp | Planned |
| Microsoft Teams | Planned |
