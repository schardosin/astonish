# Credential Security

Astonish provides a secure credential store that keeps API keys, tokens, and other secrets encrypted at rest and invisible to the LLM at runtime. A five-layer redaction pipeline ensures that secrets never leak into model context, logs, or responses.

## Credential Store

Credentials are stored encrypted using [envelope encryption](./envelope-encryption.md) and scoped to either a user (personal) or a team:

| Scope | Visibility | Use Case |
|-------|-----------|----------|
| Personal | Only the owning user | Dev/testing API keys |
| Team | All team members | Shared production credentials |

### Credential Types

- **API Key** — static bearer tokens for external services
- **OAuth Client** — client ID + secret pairs for OAuth2 flows
- **Token** — short-lived tokens with optional refresh configuration
- **Custom** — arbitrary key-value secrets (e.g., database connection strings)

## 5-Layer Output Redaction

Astonish applies five independent redaction layers to guarantee secrets never reach the LLM context:

| Layer | Stage | Mechanism |
|-------|-------|-----------|
| 1 | Injection | Secrets are injected into tool call environments, never into the prompt |
| 2 | Tool output filtering | Tool stdout/stderr is scanned; matching values are replaced with `[REDACTED]` |
| 3 | Context assembly | The context builder verifies no known secret values appear in assembled messages |
| 4 | Response streaming | Outbound LLM responses are scanned against the active credential set |
| 5 | Audit and logs | All logging pipelines strip secret values before writing |

Each layer operates independently. If one layer fails, the remaining layers still prevent exposure.

## How Secrets Reach Tools

When an agent invokes a tool that requires credentials:

1. The credential is resolved (personal-first, team fallback).
2. The encrypted ciphertext is decrypted in memory using the org's DEK.
3. The plaintext is injected as an **environment variable** into the sandbox container.
4. The tool reads the credential from its environment.
5. After execution, the environment is destroyed with the container.

At no point does the LLM see the credential value. The model only knows that a credential named (e.g.) `GITHUB_TOKEN` is available — never its contents.

## CLI Management

```bash
# Store a personal credential
astonish credential set my-api-key --type api_key --value "sk-..."

# Store a team credential
astonish credential set prod-github --team backend --type api_key --value "ghp_..."

# List credentials (values are never displayed)
astonish credential list

# Rotate a credential
astonish credential set prod-github --team backend --type api_key --value "ghp_new..."

# Delete a credential
astonish credential delete my-api-key
```

Credential values are never echoed back in any command output. The `list` command shows names, types, scopes, and last-rotated timestamps only.

## See Also

- [Envelope Encryption](./envelope-encryption.md) — how credentials are encrypted at rest
- [Sandboxes](./sandboxes.md) — the isolated execution environment where credentials are consumed
- [Audit Logging](./audit-logging.md) — all credential access is recorded
