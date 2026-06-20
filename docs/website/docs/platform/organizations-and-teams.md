# Organizations & Teams

Platform mode organizes users into a three-level hierarchy: **organizations**, **teams**, and **users**. Each level provides isolation boundaries and role-based access control.

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
| **Owner** | Full control. Manage billing, delete org, promote admins. |
| **Admin** | Manage teams, invite/remove users, promote knowledge to org level, configure org defaults. |
| **Member** | Join teams (by invite or self-service if enabled), use platform features within assigned teams. |

### Team Roles

| Role | Permissions |
|------|-------------|
| **Admin** | Manage team members, publish/unpublish resources, configure team defaults. |
| **Member** | Read/write team resources, publish personal resources to team. |
| **Viewer** | Read-only access to team memory and published resources. |

## Creating an Organization

```bash
# Platform admin creates an org
astonish platform org create \
  --name "Acme Corp" \
  --slug acme \
  --owner alice@acme.corp
```

This provisions a new PostgreSQL database (`org_acme`), creates the `public` schema with org-wide tables, and assigns the owner.

## Managing Teams

```bash
# Org admin creates a team
astonish team create --name "Backend" --slug backend

# List teams in current org
astonish team list

# Show team details
astonish team show backend
```

Team creation provisions a `team_backend` schema in the org database with the standard table set (memory, sessions, artifacts, config).

## Inviting Members

```bash
# Invite by email (sends invitation link)
astonish org invite alice@acme.corp --role member

# Add existing user to a team
astonish team add-member backend --user alice --role admin

# Remove from team
astonish team remove-member backend --user bob

# Bulk invite from file
astonish org invite --from members.csv
```

When OIDC federation is configured, users authenticate through your identity provider and are auto-provisioned into the org on first login. See [Administration](./administration) for OIDC setup.

## Switching Context

Users who belong to multiple teams can switch their active context:

```bash
# Show current context
astonish status

# Switch active team
astonish use team frontend

# All subsequent commands operate in the frontend team context
astonish memory search "deployment patterns"
```

The active org/team context determines which schemas are searched during memory queries and where new resources are created.

## Self-Service vs. Managed

Organizations can choose between two membership models:

- **Managed** (default) — admins explicitly invite users and assign teams
- **Self-service** — any authenticated user with a matching email domain can join the org and request team access

```yaml
# org config
membership:
  mode: managed           # or "self-service"
  allowed_domains:
    - acme.corp
  auto_join_teams:
    - general
```

## Data Isolation Guarantees

- **Cross-org**: impossible. Separate databases with separate connection credentials.
- **Cross-team**: enforced via PostgreSQL schema grants. A user's database role only has `USAGE` on their personal schema and their teams' schemas.
- **Personal**: only the owning user has access. Not even org admins can read personal schemas without explicit consent.

## Next Steps

- [Three-Tier Memory](./three-tier-memory) — how memory spans the hierarchy
- [Cascading Defaults](./cascading-defaults) — configuration inheritance across levels
- [Publish & Fork](./publish-and-fork) — sharing resources between tiers
