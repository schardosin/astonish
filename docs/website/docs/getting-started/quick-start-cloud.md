# Quick Start: Cloud

The cloud deployment uses PostgreSQL with pgvector, enabling multi-tenant features: three-tier memory, envelope encryption for credentials, database-per-org isolation, and team collaboration.

## Prerequisites

- Astonish binary [installed](./installation.md)
- PostgreSQL 15+ with the `pgvector` extension enabled
- A database user with CREATE DATABASE permissions

## 1. Set the Database Connection

Export the PostgreSQL connection string:

```bash
export ASTONISH_PLATFORM_DSN="postgres://user:password@host:5432/astonish?sslmode=require"
```

Or add it to your shell profile for persistence. When `ASTONISH_PLATFORM_DSN` is set (or `platform_dsn` is configured in the YAML config), Astonish automatically uses PostgreSQL for all storage and multi-tenant features become available.

## 2. Initialize the Platform

Run the platform initialization command:

```bash
astonish platform init
```

This creates the platform database, sets up the encryption key hierarchy, and walks you through creating the first organization and admin account.

## 3. Create Your Organization

If you did not create an org during init, or want additional organizations:

```bash
astonish platform org create --name "Engineering" --slug engineering
```

## 4. Invite Team Members

Invite users by email. Specify the org, assign roles, and optionally assign to a team:

```bash
astonish platform org invite --org engineering --email alice@company.com --role admin
astonish platform org invite --org engineering --email bob@company.com --team backend
astonish platform org invite --org engineering --email carol@company.com
```

Roles: `owner`, `admin`, `member` (default: `member`). Users are added to the `general` team by default unless `--team` is specified.

## 5. Team Members Connect

Invited users install the Astonish binary and log in to your platform:

```bash
astonish login https://your-astonish-server.com
```

This authenticates via password or OIDC/SSO (if configured), selects the org and team, and stores credentials locally. After login:

```bash
astonish chat          # Chat through the platform
astonish flows list    # Browse team flows
astonish status        # Show connection info
```

Studio is available at `http://localhost:9393` when the daemon is running, or through your server's URL for remote access.

## Configure OIDC/SSO (Optional)

For enterprise environments, connect your identity provider by configuring OIDC settings in the platform configuration file:

```yaml
# In your Astonish config (e.g., ~/.config/astonish/config.yaml)
auth:
  oidc:
    issuer: https://accounts.google.com
    client_id: YOUR_CLIENT_ID
    client_secret: YOUR_CLIENT_SECRET
```

OIDC group claims auto-map to Astonish team memberships. Users authenticate through your existing SSO flow.

## What Cloud Deployment Adds

| Capability | Description |
|-----------|-------------|
| Three-tier memory | Personal + team + org knowledge with hybrid search |
| Cascading config | Provider defaults, MCP servers, skills flow from org to team to user |
| Envelope encryption | Per-org data encryption keys (AES-256-GCM) |
| Audit logging | Immutable, team-scoped audit trail |
| Sandboxes | Per-org network-isolated execution (Incus or Kubernetes) |
| Channels | Telegram, Email, Slack with team-scoped routing |
| Remote CLI | Team members connect from anywhere |

## Next Steps

- [Choose Your Interface](./choose-your-interface.md) — Studio, CLI, Remote CLI, channels
- [Architecture](./architecture.md) — Understand the layer model
