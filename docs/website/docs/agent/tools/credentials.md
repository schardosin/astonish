# Credential Tools

Five tools for secure secret storage, retrieval, and management with AES-256-GCM encryption and multi-layer output redaction.

## Tools

| Tool | Description | Confirmation |
|------|-------------|-------------|
| `save_credential` | Store a secret (API key, password, token) | always-confirm |
| `resolve_credential` | Retrieve credential fields for use in tools | auto-approve |
| `list_credentials` | List stored credentials (names only, no values) | auto-approve |
| `test_credential` | Test a credential by performing its auth flow | always-confirm |
| `remove_credential` | Delete a stored credential | always-confirm |

## How It Works

Credentials are encrypted at rest and injected at runtime into tool calls that need them. The agent never sees plaintext secrets in its conversation context.

```
1. User provides credential → agent calls save_credential → encrypted in store
2. Agent needs credential → calls resolve_credential → gets placeholder token
3. Placeholder injected into target tool → system substitutes real value at execution
4. Output redaction removes any accidental leakage from results
```

## Credential Types

| Type | Use Case |
|------|----------|
| `api_key` | API keys sent as header values |
| `bearer` | Bearer tokens for Authorization header |
| `basic` | Username/password for HTTP Basic Auth |
| `password` | Generic passwords (SSH, databases, etc.) |
| `oauth_client_credentials` | OAuth2 client credentials flow |
| `oauth_authorization_code` | OAuth2 authorization code flow |

## Storing Credentials

The agent stores credentials when you provide them during chat:

```
User: "Here's my GitHub token: ghp_xxxxxxxxxxxxx"
Agent: [save_credential name="github-token" type="bearer" token="ghp_xxxxxxxxxxxxx"]
       Saved credential 'github-token' to the encrypted store.
```

## Using Credentials

Credentials integrate with other tools:

### With http_request

```
http_request:
  method: "GET"
  url: "https://api.github.com/repos"
  credential: "github-token"
```

### With resolve_credential (for non-HTTP use)

For non-HTTP scenarios (SSH, database connections, form filling), `resolve_credential` returns placeholder tokens that are substituted at execution time. The real secret value never appears in the agent's context.

Placeholders are passed to `shell_command`, `process_write`, or `browser_type` where the system substitutes the real value at execution time.

## Encryption

- **AES-256-GCM** symmetric encryption for all stored secrets
- **Envelope encryption** in platform mode (per-org Data Encryption Keys wrapped by a Key Encryption Key)
- Credentials are personal by default — only you can access them

## Output Redaction

Multiple redaction layers prevent credential exposure:

1. **Tool-level** — Tools redact known credential patterns from their output
2. **Agent-level** — The runtime scans all tool results for stored values
3. **Session-level** — Session persistence strips matched patterns before writing
4. **Stream-level** — SSE streaming to Studio applies real-time redaction
5. **Display-level** — Studio UI masks any residual patterns

## Managing Credentials in Studio

Studio Settings provides a credential management interface:

- View all stored credentials (names and types, never values)
- Add new credentials via form
- Delete credentials
- Publish credentials to team (team-scoped, inject-only access)

**Scheduled jobs:** Personal credentials work with **personal-scope** scheduled jobs (the default when scheduling from chat). Only publish a credential to the team when you need shared team automation, fleet, or other members to use it — not merely to schedule your own recurring task.

See [Web & HTTP Tools](./web-http.md) for credential injection in API calls and [Browser Automation](./browser.md) for form-filling with secrets.
