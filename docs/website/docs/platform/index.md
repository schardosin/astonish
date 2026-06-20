# Platform Overview

Astonish is always a platform — the same agent engine, tools, and capabilities run in every deployment. The database backend determines the scale: SQLite for local single-user setups, PostgreSQL for multi-tenant team deployments. When backed by PostgreSQL, Astonish provides organizations, teams, shared memory, cascading configuration, and enterprise security.

## Why Multi-Tenancy Matters

A single developer using Astonish accumulates memory, refines flows, and builds a library of skills. With a PostgreSQL deployment, that knowledge benefits the entire team. When one engineer solves a tricky deployment issue, the resolution enters team memory. When a team discovers a pattern, it can be promoted to org-level knowledge available to every team.

The core value proposition: **knowledge compounds**. Every interaction makes the platform smarter for everyone — without sacrificing privacy or control over personal data.

## Database Backends

The same `astonish` binary powers both deployment options. The presence of a PostgreSQL DSN determines which backend is active:

| Aspect | Local (SQLite) | Cloud (PostgreSQL) |
|--------|---------------|-------------------|
| Database | SQLite with built-in vector search | PostgreSQL 15+ with pgvector |
| Memory | Personal tier only | Three-tier (personal/team/org) |
| Auth | Local credentials | JWT + OIDC |
| Resources | Local filesystem | Publish/Fork model |
| Config | `~/.config/astonish/` | Cascading defaults |
| CLI | Direct execution | Remote via `astonish login` |

You lose nothing by deploying with PostgreSQL — your personal data remains private in your own schema, invisible to teammates unless you explicitly publish it.

## Database Layout

Cloud deployments use **database-per-org** isolation. Each organization gets its own PostgreSQL database, making cross-org data leakage architecturally impossible.

```
PostgreSQL Cluster
│
├── astonish_platform          ← Platform-level metadata
│   ├── public.organizations
│   ├── public.users
│   └── public.platform_config
│
├── org_acme                   ← Acme Corp's database
│   ├── public.*               ← Org-wide tables (shared memory, config)
│   ├── team_backend/          ← Backend team schema
│   │   ├── memory
│   │   ├── sessions
│   │   └── artifacts
│   ├── team_frontend/         ← Frontend team schema
│   │   ├── memory
│   │   ├── sessions
│   │   └── artifacts
│   ├── personal_u_7f3a/      ← Alice's private schema
│   └── personal_u_9b2c/      ← Bob's private schema
│
└── org_globex                 ← Globex Inc's database (completely isolated)
    ├── public.*
    ├── team_engineering/
    └── personal_u_4d1e/
```

Each tier within an org database is a PostgreSQL schema:

- **`public`** — org-wide configuration, promoted knowledge, shared resources
- **`team_{name}`** — team memory, sessions, artifacts, and published resources
- **`personal_{user_id}`** — private sessions, memory, drafts, and personal config

Schema-level `GRANT`/`REVOKE` enforces team boundaries. A user can only access their personal schema plus the schemas of teams they belong to.

## Key Concepts

**Three-Tier Memory** — Searches span personal, team, and org memory in parallel, merged with Reciprocal Rank Fusion and tier-specific weights. See [Three-Tier Memory](./three-tier-memory).

**Cascading Defaults** — Configuration flows from platform → org → team → personal. A platform admin sets global provider keys; an org overrides models; a team pins specific MCP servers; a user adds personal preferences. See [Cascading Defaults](./cascading-defaults).

**Publish & Fork** — Resources are private by default. You explicitly publish sessions, flows, or apps to your team. Teammates can fork published resources into their own workspace. Admins can promote team knowledge to org level. See [Publish & Fork](./publish-and-fork).

**Authentication** — Built-in JWT + bcrypt authentication works out of the box. For enterprise environments, federate with any OIDC provider (SAP IAS, Azure AD, Okta, etc.). See [Administration](./administration).

## Getting Started with Cloud Deployment

```bash
# Initialize the platform database
astonish platform init --dsn "postgres://user:pass@localhost:5432/astonish_platform"

# Create your first organization
astonish platform org create --name "Acme Corp" --slug acme

# Start the daemon (Studio available at http://localhost:9393)
astonish daemon
```

Users connect with the [Remote CLI](./remote-cli):

```bash
astonish login https://astonish.acme.corp
astonish status
```

## Next Steps

- [Organizations & Teams](./organizations-and-teams) — set up your org hierarchy
- [Three-Tier Memory](./three-tier-memory) — understand how knowledge flows
- [Remote CLI](./remote-cli) — connect your team to the platform
- [Administration](./administration) — provision and manage the platform
