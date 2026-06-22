# Envelope Encryption

Astonish uses a two-tier envelope encryption scheme to protect stored credentials. This architecture ensures that a database compromise alone cannot expose secrets, and that key rotation does not require re-encrypting every credential.

## Architecture

```
┌─────────────┐      wraps      ┌─────────────┐      encrypts      ┌──────────────────┐
│  Master KEK │ ────────────────▶│ Per-Org DEK │ ──────────────────▶│ Credential Data  │
└─────────────┘                 └─────────────┘                    └──────────────────┘
   (one per platform)             (one per org)                      (AES-256-GCM)
```

| Layer | Key | Scope | Storage |
|-------|-----|-------|---------|
| Master Key Encryption Key (KEK) | Platform-wide | One | External secret (env var, K8s Secret) |
| Data Encryption Key (DEK) | Per organization | One per org | Encrypted in database (wrapped by KEK) |
| Credential ciphertext | Per credential | Many per org | Database, encrypted by org DEK |

## Why Envelope Encryption?

- **Key rotation without mass re-encryption.** Rotating the KEK only requires re-wrapping the DEKs (a handful of rows), not every credential.
- **Org isolation at the cryptographic layer.** Even if two orgs share infrastructure, their DEKs are distinct — a leaked DEK from one org cannot decrypt another's credentials.
- **Defense in depth.** An attacker needs both the KEK (external to the database) and database access to decrypt anything.

## Key Management

### Master KEK

The master KEK is provided at startup via environment variable:

```bash
export ASTONISH_MASTER_KEY="<64-hex-chars>"
```

Generate a key:

```bash
openssl rand -hex 32
```

This produces a 256-bit (32-byte) key encoded as 64 hexadecimal characters.

In Kubernetes, store this in a Secret and reference it in the Helm values. For non-containerized deployments, Astonish can also auto-generate and store the key in a local file (`.store_key`) with `0600` permissions.

### Per-Org DEK

When an organization first stores a credential, Astonish generates a random 256-bit DEK and stores it wrapped (encrypted) by the master KEK in the `org_encryption_keys` table. The plaintext DEK exists only in memory during request processing.

### Encryption Algorithm

All encryption uses **AES-256-GCM** with random 96-bit nonces. GCM provides both confidentiality and integrity — tampering with ciphertext is detected on decryption.

## Credential Resolution

When an agent needs a credential at runtime, Astonish resolves it using a **personal-first with team fallback** strategy:

1. Check for a personal credential owned by the requesting user.
2. If not found, check team-level credentials for teams the user belongs to.
3. Decrypt the matching credential's ciphertext using the org DEK.
4. Inject the plaintext into the tool call environment (never into LLM context).

This allows individuals to override team defaults — for example, using a personal API key for development while the team shares a production key.

## Decryption Fallback Chain

For backward compatibility with credentials stored before envelope encryption was introduced:

1. Attempt decryption with the per-org DEK.
2. If that fails, attempt decryption with the master key directly.
3. If that fails, treat as plaintext (legacy data).

This ensures zero-downtime migration when envelope encryption is enabled on an existing deployment.

## See Also

- [Credential Security](./credential-security.md) — redaction layers and credential lifecycle
- [Authentication](./authentication.md) — how user identity determines credential access
