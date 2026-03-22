---
title: "Email Tools"
description: "Read, send, and manage emails programmatically"
---

Astonish provides 8 email tools for full inbox management. These tools require IMAP/SMTP configuration.

## Configuration

Set up email with the interactive CLI:

```bash
astonish channels setup email
```

Or configure through **Studio Settings > Channels**. For manual configuration, add the following to `config.yaml`:

```yaml
channels:
  email:
    enabled: true
    imap_server: "imap.gmail.com:993"
    smtp_server: "smtp.gmail.com:587"
    address: "agent@example.com"
    username: "agent@example.com"
    password: "app-password"  # Stored in credential store after setup
```

For Gmail, use an [App Password](https://support.google.com/accounts/answer/185833) rather than your account password. After initial setup, the password is moved to the encrypted [credential store](/tools/credentials/) and removed from the config file.

## Tools

### email_list

List emails with optional filtering.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `folder` | string | No | IMAP folder (default: INBOX) |
| `unread` | bool | No | Only unread messages |
| `from` | string | No | Filter by sender (substring match) |
| `subject` | string | No | Filter by subject (substring match) |
| `since` | string | No | Messages after this date (ISO 8601) |
| `limit` | int | No | Max results (default: 20) |

### email_read

Read the full content of an email by its ID. Returns the body, headers, links, and any extracted verification links.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `id` | string | Yes | Message ID from `email_list` |

### email_search

Search emails with advanced filters.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `query` | string | No | Free-text search |
| `from` | string | No | Filter by sender |
| `to` | string | No | Filter by recipient |
| `subject` | string | No | Filter by subject |
| `since` | string | No | Date range start (ISO 8601) |
| `before` | string | No | Date range end (ISO 8601) |
| `has_attachment` | bool | No | Only messages with attachments |
| `folder` | string | No | IMAP folder (default: INBOX) |
| `limit` | int | No | Max results (default: 20) |

### email_send

Compose and send a new email.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `to` | string[] | Yes | Recipient addresses |
| `cc` | string[] | No | CC addresses |
| `subject` | string | Yes | Subject line |
| `body` | string | Yes | Plain text body |
| `html` | string | No | Optional HTML body |
| `reply_to` | string | No | Reply-To address |

### email_reply

Reply to an existing email, preserving the thread.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `id` | string | Yes | Message ID to reply to |
| `body` | string | Yes | Reply body |
| `html` | string | No | Optional HTML reply body |
| `reply_all` | bool | No | Reply to all recipients (default: false) |

### email_mark_read

Mark one or more emails as read or unread.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `ids` | string[] | Yes | Message IDs |
| `unread` | bool | No | Mark as unread instead (default: false) |

### email_delete

Delete one or more emails.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `ids` | string[] | Yes | Message IDs |
| `permanent` | bool | No | Skip trash, delete permanently (default: false) |

### email_wait

Wait for a matching email to arrive. Polls the inbox until a match is found or the timeout is reached. Useful for registration and verification flows.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `from` | string | No | Sender substring match |
| `subject` | string | No | Subject substring match |
| `timeout_seconds` | int | No | How long to wait (default: 120, max: 300) |
| `poll_interval_seconds` | int | No | Check interval in seconds (default: 5, min: 3) |

## Common Workflow: Automated Signup Verification

Email tools pair well with [browser automation](/tools/browser/) for end-to-end signup flows:

1. **Submit the registration form** using browser tools (`browser_navigate`, `browser_type`, `browser_click`).
2. **Wait for the verification email** with `email_wait`, matching on the expected sender or subject.
3. **Read the email** with `email_read` to extract the verification link.
4. **Complete verification** by navigating to the link with `browser_navigate`.
