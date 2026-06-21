# Platform Overview

Astonish is always a platform — the same agent engine, tools, and capabilities run in every deployment. The database backend determines the scale: SQLite for local/small team setups, PostgreSQL for multi-tenant team deployments. Both backends run in full platform mode with authentication, encryption, sessions, and memory.

## Why Multi-Tenancy Matters

A single developer using Astonish accumulates memory, refines flows, and builds a library of skills. When backed by PostgreSQL, that knowledge benefits the entire team at scale. When one engineer solves a tricky deployment issue, the resolution enters team memory. When a team discovers a pattern, it can be promoted to org-level knowledge available to every team.

The core value proposition: **knowledge compounds**. Every interaction makes the platform smarter for everyone — without sacrificing privacy or control over personal data.

## Database Backends

The same `astonish` binary powers both deployment options. The backend is selected during `astonish setup`:

| Aspect | Local (SQLite) | Cloud (PostgreSQL) |
|--------|---------------|-------------------|
| Database | SQLite with built-in vector search | PostgreSQL 15+ with pgvector |
| Memory | Three-tier (personal/team/org) | Three-tier (personal/team/org) |
| Auth | JWT + builtin auth | JWT + OIDC/SSO |
| Encryption | Envelope encryption | Envelope encryption |
| Scale | Single user / small team | Multi-tenant (many orgs, teams) |
| Isolation | Separate SQLite files per scope | Database-per-org, schema-per-team |
| CLI | Via `astonish login` | Via `astonish login` |

Both backends provide full platform features. PostgreSQL adds database-level cross-org isolation and scales to large multi-tenant deployments.

## Database Layout (PostgreSQL)

PostgreSQL deployments use **database-per-org** isolation. Each organization gets its own PostgreSQL database, making cross-org data leakage architecturally impossible.

```
PostgreSQL Cluster
│
├── astonish_platform              ← Platform-level metadata
│   ├── public.organizations
│   ├── public.users
│   └── public.platform_config
│
├── astonish_org_acme              ← Acme Corp's database
│   ├── public.*                   ← Org-wide tables (shared memory, config)
│   ├── team_backend/              ← Backend team schema
│   │   ├── memory
│   │   ├── sessions
│   │   └── artifacts
│   ├── team_frontend/             ← Frontend team schema
│   │   ├── memory
│   │   ├── sessions
│   │   └── artifacts
│   ├── personal_<user_uuid>/      ← Alice's private schema
│   └── personal_<user_uuid>/      ← Bob's private schema
│
└── astonish_org_globex            ← Globex Inc's database (completely isolated)
    ├── public.*
    ├── team_engineering/
    └── personal_<user_uuid>/
```

Each tier within an org database is a PostgreSQL schema:

- **`public`** — org-wide configuration, promoted knowledge, shared resources
- **`team_<slug>`** — team memory, sessions, artifacts, and published resources
- **`personal_<user_uuid>`** — private sessions, memory, drafts, and personal config

Schema-level `GRANT`/`REVOKE` enforces team boundaries. A user can only access their personal schema plus the schemas of teams they belong to.

## Database Layout (SQLite)

SQLite deployments use separate database files to maintain the same isolation model:

```
~/.local/share/astonish/
└── orgs/<slug>/
    ├── org.db                     ← Org-wide data
    ├── teams/<team_slug>.db       ← Per-team data
    └── personal/<user_id>.db      ← Per-user private data
```

## Key Concepts

**Three-Tier Memory** — Searches span personal, team, and org memory in parallel, merged with Reciprocal Rank Fusion and tier-specific weights. See [Three-Tier Memory](./three-tier-memory).

**Cascading Defaults** — Configuration flows from platform → org → team → personal. A platform admin sets global provider keys; an org overrides models; a team pins specific MCP servers; a user adds personal preferences. See [Cascading Defaults](./cascading-defaults).

**Publish & Fork** — Resources are private by default. You explicitly publish sessions, flows, or apps to your team. Teammates can fork published resources into their own workspace. Admins can promote team knowledge to org level. See [Publish & Fork](./publish-and-fork).

**Authentication** — Built-in JWT + bcrypt authentication works out of the box. For enterprise environments, federate with any OIDC provider (SAP IAS, Azure AD, Okta, etc.). See [Administration](./administration).

## Getting Started

```bash
# 1. Run the setup wizard (choose SQLite or PostgreSQL)
astonish setup

# 2. Start the platform
astonish daemon install
astonish daemon start

# 3. Log in via Studio (http://localhost:9393) or CLI
astonish login http://localhost:9393
astonish chat
```

For PostgreSQL deployments, invite team members after setup:

```bash
astonish platform org invite --org acme --email alice@acme.corp --role admin
```

Users connect with the CLI:

```bash
astonish login https://astonish.acme.corp
astonish status
```

## Next Steps

- [Organizations & Teams](./organizations-and-teams) — set up your org hierarchy
- [Three-Tier Memory](./three-tier-memory) — understand how knowledge flows
- [Remote CLI](./remote-cli) — connect your team to the platform
- [Administration](./administration) — provision and manage the platform
