---
title: "Credential Management"
description: "Encrypted credential store with automatic auth injection"
---

Astonish includes a secure credential store using AES-256-GCM encryption. Secrets are encrypted at rest and automatically injected into HTTP requests, browser sessions, and email connections.

A 5-layer output redaction system prevents secrets from appearing in:

- Agent logs
- LLM context windows
- Session transcripts
- Tool output
- Error messages

Optionally set a master key to protect credential viewing with `astonish credential master-key`.

## Credential Types

| Type | Use Case |
|------|----------|
| `api_key` | Custom header + value (e.g., `X-API-Key: abc123`) |
| `bearer` | `Authorization: Bearer <token>` |
| `basic` | HTTP Basic Auth (username + password) |
| `password` | Plain username/password for SSH, FTP, SMTP, databases |
| `oauth_client_credentials` | Machine-to-machine OAuth with automatic token refresh |
| `oauth_authorization_code` | User-authorized OAuth with refresh token support |

## Tools

### save_credential

Save a credential to the encrypted store. The agent calls this automatically when the user provides any secret value.

Key parameters: `name`, `type`, plus type-specific fields:

- **api_key**: `header`, `value`
- **bearer**: `token`
- **basic**: `username`, `password`
- **password**: `username`, `password`
- **oauth_client_credentials**: `client_id`, `client_secret`, `token_url`, `scopes`
- **oauth_authorization_code**: `client_id`, `client_secret`, `auth_url`, `token_url`, `redirect_uri`, `scopes`, `refresh_token`

### list_credentials

List all stored credentials. Values are never exposed in the output.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `filter` | string | No | Filter credentials by name substring |

### remove_credential

Remove a credential from the store.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | Yes | Credential name |

### test_credential

Test that a credential is valid. For OAuth types, this performs the full token flow. For other types, it validates the configuration.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | Yes | Credential name |

### resolve_credential

Retrieve raw credential fields for non-HTTP authentication (SSH, databases, FTP). Pipe the resolved values to `process_write` for interactive login flows.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | Yes | Credential name |

## CLI Commands

Manage credentials from the command line:

```bash
astonish credential list              # List all stored credentials
astonish credential show <name>       # Show decrypted values (requires master key if set)
astonish credential add <name>        # Interactive credential creation
astonish credential remove <name>     # Delete a credential
astonish credential test <name>       # Test a credential
astonish credential master-key        # Set, change, or remove the master key
```

## Integration with Other Tools

The credential store integrates across Astonish:

- **`http_request`** -- Set `credential: "my-api"` to automatically inject the correct auth headers based on the credential type.
- **Browser tools** -- The credential store integrates with browser account management for automated logins.
- **[Email tools](/tools/email/)** -- SMTP and IMAP passwords are stored in the credential store after initial configuration.
- **Provider API keys** -- Keys configured during setup are automatically moved from the config file into the credential store.
