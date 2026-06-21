---
# Email Tools

Eight tools for managing email: reading, sending, searching, and organizing messages.

## Tools

| Tool | Description | Confirmation |
|------|-------------|-------------|
| `email_list` | List messages in the inbox | auto-approve |
| `email_read` | Read a specific message | auto-approve |
| `email_search` | Search messages by criteria | auto-approve |
| `email_send` | Compose and send an email | always-confirm |
| `email_reply` | Reply to a message | always-confirm |
| `email_mark_read` | Mark messages as read or unread | always-confirm |
| `email_delete` | Delete messages | always-confirm |
| `email_wait` | Wait for an email matching criteria | auto-approve |

## Configuration

Email tools require IMAP/SMTP configuration. Set up email access in your config:

```yaml
email:
  imap:
    host: "imap.example.com"
    port: 993
    tls: true
  smtp:
    host: "smtp.example.com"
    port: 587
    tls: true
```

Email credentials are stored securely via the [credential system](./credentials.md).

## Usage Examples

### List recent messages

```
email_list:
  folder: "INBOX"
  limit: 20
  unread_only: true
```

### Search messages

```
email_search:
  query: "from:boss@company.com subject:quarterly"
  since: "2025-06-01"
```

### Send email

```
email_send:
  to: "team@company.com"
  subject: "Deployment Complete"
  body: "The v2.1 release has been deployed to production."
  cc: "manager@company.com"
```

### Wait for email

```
email_wait:
  from: "ci@company.com"
  subject: "Build Complete"
  timeout: 300
```

Useful for automation workflows that need to wait for confirmation emails.

## Security

- Credentials are stored encrypted (AES-256-GCM)
- Email content is never persisted to memory unless explicitly requested
- Send and reply operations always require confirmation
- Delete operations always require confirmation

See [Credentials](./credentials.md) for how email secrets are managed and [Tools Overview](./index.md) for the full tool catalog.
---
