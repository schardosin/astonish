# Organizations & Teams

Astonish organizes users into a three-level hierarchy: **organizations**, **teams**, and **users**. Each level provides isolation boundaries and role-based access control.

## Hierarchy

```
Organization (Acme Corp)
├── Team: Backend
│   ├── Alice (admin)
│   ├── Bob (member)
│   └── Carol (member)
├── Team: Frontend
│   ├── Dave (admin)
│   └── Eve (member)
└── Team: Platform
    └── Frank (admin, org owner)
```

A user belongs to exactly one organization and can be a member of multiple teams within it. Every user also has a private personal schema regardless of team membership.

## Roles

### Organization Roles

| Role | Permissions |
|------|-------------|
| **Owner** | Full control. Manage org, promote admins, configure platform settings. |
| **Admin** | Manage teams, invite/remove users, promote knowledge to org level, configure org defaults. |
| **Member** | Use platform features within assigned teams, publish personal resources to team. |

### Team Roles

| Role | Permissions |
|------|-------------|
| **Admin** | Manage team members, publish/unpublish resources, configure team defaults. |
| **Member** | Read/write team resources, publish personal resources to team. |

## Creating an Organization

Organizations are created during the `astonish setup` wizard or via the platform CLI:

```bash
# Platform admin creates an org
astonish platform org create --name "Acme Corp" --slug acme
```

Optionally assign an existing user as owner:

```bash
astonish platform org create --name "Acme Corp" --slug acme --owner-email alice@acme.corp
```

This provisions a new database (PostgreSQL) or directory (SQLite), creates the org-wide schema, and assigns the owner.

## Managing Teams

Teams are managed through **Studio** (Settings → Teams). The CLI provides read-only access:

```bash
# List teams in current org (requires login)
astonish team list
```

Team creation, member management, and configuration are handled through the Studio web interface or during the initial `astonish setup` wizard.

## Inviting Members

```bash
# Invite by email (platform admin)
astonish platform org invite --org acme --email alice@acme.corp --role admin
astonish platform org invite --org acme --email bob@acme.corp --team backend
astonish platform org invite --org acme --email carol@acme.corp
```

Flags:
- `--org` — Organization slug (required)
- `--email` — User's email address (required)
- `--role` — Role: `owner`, `admin`, `member` (default: `member`)
- `--team` — Also add user to this team (default: `general`)
- `--name` — Display name (defaults to email prefix)
- `--password` — Prompt for password instead of generating one

When OIDC federation is configured, users authenticate through your identity provider and are auto-provisioned into the org on first login. See [Administration](./administration) for OIDC setup.

## Switching Context

To switch your active org or team, log out and log back in with the desired context:

```bash
astonish logout
astonish login https://astonish.acme.corp --org acme --team frontend
```

Or omit the flags to be prompted interactively during login.

To check your current context:

```bash
astonish status
```

## Data Isolation Guarantees

- **Cross-org**: impossible. Separate databases (PostgreSQL) or separate directories (SQLite) with separate credentials.
- **Cross-team**: enforced via PostgreSQL schema grants or separate SQLite files. A user can only access their personal schema plus the schemas of teams they belong to.
- **Personal**: only the owning user has access. Not even org admins can read personal schemas without explicit consent.

## Next Steps

- [Three-Tier Memory](./three-tier-memory) — how memory spans the hierarchy
- [Cascading Defaults](./cascading-defaults) — configuration inheritance across levels
- [Publish & Fork](./publish-and-fork) — sharing resources between tiers
