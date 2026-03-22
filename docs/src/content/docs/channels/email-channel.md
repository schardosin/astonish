---
title: Email Channel
description: Monitor an inbox and respond to emails automatically
---

The email channel lets Astonish monitor an email inbox and respond to incoming messages automatically. This is different from the email tools (which send and read emails on demand) — the email channel is a continuous listener that processes incoming emails as conversations.

## Setup

### 1. Configure IMAP and SMTP

The easiest way is the interactive setup:

```bash
astonish channels setup email
```

This walks you through entering server addresses, credentials, and allowed senders.

You can also configure email through **Studio Settings > Channels**, or manually in `config.yaml`:

```yaml
channels:
  enabled: true
  email:
    enabled: true
    provider: "imap"              # "imap" or "gmail"
    imap_server: "imap.gmail.com:993"
    smtp_server: "smtp.gmail.com:587"
    address: "agent@example.com"
    username: "agent@example.com"
    password: "app-password"      # Stored in credential store
    poll_interval: 30             # Seconds between inbox checks
    allow_from:
      - "user@example.com"       # Allowed senders (["*"] for anyone)
    folder: "INBOX"
    mark_read: true
    max_body_chars: 50000
```

### 2. Gmail Users

If you are using Gmail, you must use an App Password instead of your regular password. Generate one at **Google Account > Security > App Passwords**.

### 3. Start the Daemon

Start or restart the daemon to activate the channel:

```bash
astonish daemon restart
```

## How It Works

1. The channel polls the inbox at the configured interval.
2. New messages from allowed senders are processed by the AI agent.
3. Responses are sent as reply emails, preserving threading (`In-Reply-To` and `References` headers).
4. Processed emails are marked as read (configurable).

## Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `provider` | `imap` | `imap` or `gmail` |
| `poll_interval` | `30` | Seconds between inbox checks |
| `allow_from` | | List of allowed sender emails. `["*"]` for anyone |
| `folder` | `INBOX` | IMAP folder to monitor |
| `mark_read` | `true` | Mark processed emails as read |
| `max_body_chars` | `50000` | Truncate long email bodies |

## Security

The `allow_from` list controls who can interact with the agent via email. Use specific email addresses for security. Setting `["*"]` allows anyone to trigger the agent and is not recommended for public-facing inboxes.
