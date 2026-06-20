# Email

The Email channel allows users to interact with Astonish agents by sending and receiving standard email. It supports asynchronous workflows where responses may take longer to generate.

## Setup

### 1. Configure IMAP/SMTP

Add the email channel to your config file:

```yaml
channels:
  email:
    imap:
      host: "imap.example.com"
      port: 993
      username: "bot@example.com"
      password: "app-password"
      tls: true
    smtp:
      host: "smtp.example.com"
      port: 587
      username: "bot@example.com"
      password: "app-password"
      tls: true
    allowlist:
      - "user@company.com"
      - "*@company.com"   # Wildcard: all users at domain
    poll_interval: "30s"
```

### 2. Start the Channel

```bash
astonish daemon start
```

The daemon polls the IMAP inbox at the configured interval and processes new messages.

## Configuration Options

| Option | Description | Default |
|--------|-------------|---------|
| `imap.host` | IMAP server hostname | Required |
| `imap.port` | IMAP server port | `993` |
| `imap.tls` | Enable TLS | `true` |
| `smtp.host` | SMTP server hostname | Required |
| `smtp.port` | SMTP server port | `587` |
| `smtp.tls` | Enable TLS | `true` |
| `allowlist` | Allowed sender addresses (supports wildcards) | `[]` |
| `poll_interval` | How often to check for new mail | `"30s"` |

## Email Processing Pipeline

1. **Poll** — Check IMAP inbox for unread messages
2. **Filter** — Verify sender against allowlist
3. **Parse** — Extract text content (plain text preferred, HTML stripped as fallback)
4. **Route** — Determine org/team context (cloud deployment) or use default
5. **Execute** — Send content to agent engine
6. **Reply** — Format agent response and send via SMTP as a reply to the original thread

Subject lines are used as session identifiers. Replies within the same email thread continue the same agent session.

## Cloud Deployment

### Plus-Addressing Routing

In cloud deployments, the email channel supports plus-addressing to route messages to specific organizations:

```
bot+acme-corp@example.com   → Routes to org "acme-corp"
bot+startup-x@example.com   → Routes to org "startup-x"
bot@example.com             → Routes to sender's default org
```

This allows users to control routing per-message without any in-band commands. Configure the base address:

```yaml
channels:
  email:
    address: "bot@example.com"
    plus_addressing: true
```

### Database Allowlist

Like other channels in cloud deployments, the allowlist is managed through the platform API rather than static config:

```bash
astonish platform org invite --channel email --address "user@company.com"
```

Wildcard patterns (`*@company.com`) are supported in the database allowlist as well.
