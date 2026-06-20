# Credential Tools

Two tools for secure secret storage and retrieval, with AES-256-GCM encryption and multi-layer output redaction.

## Tools

| Tool | Description | Confirmation |
|------|-------------|-------------|
| `credential_store` | Store a secret (API key, password, token) | always-confirm |
| `credential_get` | Retrieve a stored credential for use | auto-approve |

## How It Works

Credentials are encrypted at rest and injected at runtime into tool calls that need them. The agent never sees plaintext secrets in its conversation context.

```
1. User stores credential → encrypted on disk
2. Agent needs credential → calls credential_get
3. Value injected into target tool → never in LLM context
4. Output redaction removes any accidental leakage
```

## Storing Credentials

```
credential_store:
  name: "github-token"
  value: "ghp_xxxxxxxxxxxxx"
  description: "GitHub personal access token"
```

Or from CLI:

```bash
astonish credential store github-token
# Prompts for value (hidden input)

astonish credential store aws-key --from-env AWS_SECRET_ACCESS_KEY
```

## Retrieving Credentials

When the agent needs a secret (e.g., for `http_request` or `browser_type`), it calls:

```
credential_get:
  name: "github-token"
```

The value is injected directly into the consuming tool call. It does not appear in:
- Conversation history
- Session logs
- Studio UI
- Streaming responses

## Encryption

### Local (SQLite)

- AES-256-GCM symmetric encryption
- Key derived from machine-specific entropy
- Stored in the local SQLite database

### Cloud (Envelope Encryption)

- Per-organization Data Encryption Keys (DEKs)
- DEKs wrapped by a platform Key Encryption Key (KEK)
- KEK stored in HSM or cloud KMS (AWS KMS, GCP KMS, Azure Key Vault)
- Zero-knowledge: platform operators cannot decrypt user credentials

```
┌─────────────────────────────────────┐
│ Platform KEK (in KMS)               │
│   └── Org DEK (wrapped)            │
│         └── Credential (encrypted) │
└─────────────────────────────────────┘
```

## 5-Layer Output Redaction

Even if a credential value leaks into tool output, five redaction layers prevent exposure:

1. **Tool-level** — Tools redact known credential patterns from their output
2. **Agent-level** — The agent runtime scans all tool results for stored values
3. **Session-level** — Session persistence strips matched patterns before writing
4. **Stream-level** — SSE streaming to Studio applies real-time redaction
5. **Display-level** — Studio UI masks any residual patterns matching `[REDACTED:name]`

## Managing Credentials

```bash
# List stored credentials (names only, no values)
astonish credential list

# Delete a credential
astonish credential delete github-token

# Rotate a credential
astonish credential store github-token  # Overwrites existing
```

## Cloud Deployment Access Control

In cloud deployments, credentials can be scoped:

| Scope | Access |
|-------|--------|
| Personal | Only the owning user |
| Team | All team members (read-only) |
| Org | All org members (read-only) |

Team and org credentials are managed by admins and cannot be read raw—only injected into tool calls.

See [Web & HTTP Tools](./web-http.md) for credential injection in API calls and [Browser Automation](./browser.md) for form-filling with secrets.
