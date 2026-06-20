# Email Tools

Eight tools for managing email: reading, sending, searching, organizing folders, and handling attachments.

## Tools

| Tool | Description | Confirmation |
|------|-------------|-------------|
| `email_list` | List messages in a folder | auto-approve |
| `email_read` | Read a specific message | auto-approve |
| `email_send` | Compose and send an email | always-confirm |
| `email_reply` | Reply to a message | always-confirm |
| `email_search` | Search messages by criteria | auto-approve |
| `email_folders` | List available folders | auto-approve |
| `email_move` | Move message to folder | always-confirm |
| `email_attachment` | Download or read attachment | auto-approve |

## Configuration

Email tools require IMAP/SMTP credentials stored via the [credential system](./credentials.md):

```bash
# Store email credentials (agent will prompt or you can pre-configure)
astonish credential store email-imap --value "imaps://user:pass@imap.example.com:993"
astonish credential store email-smtp --value "smtp://user:pass@smtp.example.com:587"
```

Or configure in `config.yaml`:

```yaml
email:
  imap:
    host: "imap.example.com"
    port: 993
    tls: true
    credential: "email-imap"
  smtp:
    host: "smtp.example.com"
    port: 587
    tls: true
    credential: "email-smtp"
```

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
  folder: "INBOX"
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

### Handle attachments

```
email_attachment:
  message_id: "abc123"
  attachment_index: 0
  save_to: "/tmp/report.pdf"
```

## Security

- Credentials are stored encrypted (AES-256-GCM)
- Email content is never persisted to memory unless explicitly requested
- Send operations always require confirmation
- Attachment downloads are sandboxed to allowed directories

See [Credentials](./credentials.md) for how email secrets are managed and [Tools Overview](./index.md) for the full tool catalog.
