# Email

The Email channel allows users to interact with Astonish agents by sending and receiving standard email. It supports asynchronous workflows where responses may take longer to generate.

## Setup

### 1. Configure via CLI

Run the interactive setup wizard:

```bash
astonish channels setup email
```

The wizard collects IMAP/SMTP details, tests the IMAP connection, and stores credentials securely in the encrypted credential store.

Alternatively, configure manually in your config file:

```yaml
channels:
  email:
    enabled: true
    provider: "imap"
    imap_server: "imap.example.com:993"
    smtp_server: "smtp.example.com:587"
    address: "bot@example.com"
    username: "bot@example.com"
    poll_interval: 30           # Seconds between inbox checks
    allow_from:
      - "user@company.com"
      - "*@company.com"         # Wildcard: all users at domain
```

Passwords are stored in the encrypted credential store, not in the config file.

### 2. Start the Daemon

```bash
astonish daemon start
```

The daemon polls the IMAP inbox at the configured interval and processes new messages.

## Configuration Options

| Option | Description | Default |
|--------|-------------|---------|
| `provider` | Email provider type | `"imap"` |
| `imap_server` | IMAP server (host:port) | Required |
| `smtp_server` | SMTP server (host:port) | Required |
| `address` | Bot's email address | Required |
| `username` | IMAP/SMTP username | Required |
| `poll_interval` | Seconds between inbox checks | `30` |
| `allow_from` | Allowed sender addresses (supports `*` wildcards) | `[]` |
| `folder` | IMAP folder to monitor | `"INBOX"` |
| `mark_read` | Mark processed messages as read | `true` |
| `max_body_chars` | Maximum email body length to process | `50000` |

## Email Processing Pipeline

1. **Poll** — Check IMAP inbox for unread messages
2. **Filter** — Verify sender against allowlist
3. **Parse** — Extract text content (plain text preferred, HTML stripped as fallback)
4. **Route** — Determine org/team context from plus-addressing or user identity
5. **Execute** — Send content to agent engine
6. **Reply** — Format agent response and send via SMTP as a reply to the original thread

Email threads (based on `In-Reply-To`/`References` headers) maintain the same agent session.

## Plus-Addressing Routing (PostgreSQL)

In PostgreSQL deployments, the email channel supports plus-addressing to route messages to specific organizations:

```
bot+acme-corp@example.com          → Routes to org "acme-corp"
bot+acme-corp+backend@example.com  → Routes to org "acme-corp", team "backend"
bot@example.com                    → Routes to sender's default org
```

This allows users to control routing per-message without any in-band commands.

## Managing the Channel

```bash
astonish channels status           # Check channel status
astonish channels disable email    # Disable the channel
```
