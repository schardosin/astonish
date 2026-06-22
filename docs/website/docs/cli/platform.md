# Platform Commands

The `astonish platform` command manages multi-tenant platform operations — database initialization, organization management, and user administration.

## Usage

```bash
astonish platform <subcommand> [flags]
```

## Subcommands

### Initialize Platform Database

```bash
astonish platform init \
  --host <postgres-host> \
  --password <postgres-admin-password>
```

This creates the platform database and runs migrations. It prints the connection DSN for use in Helm values or config.

| Flag | Default | Env Fallback | Description |
|------|---------|--------------|-------------|
| `--host` | (required) | `PGHOST` | PostgreSQL hostname |
| `--port` | `5432` | `PGPORT` | PostgreSQL port |
| `--user` | `postgres` | `PGUSER` | PostgreSQL admin user |
| `--password` | (required) | `PGPASSWORD` | PostgreSQL admin password |
| `--sslmode` | `prefer` | `PGSSLMODE` | SSL mode |
| `--suffix` | auto-generated | — | Fixed instance suffix |

### Generate Secret

```bash
astonish platform gen-secret
```

Generates a cryptographically secure random secret (64-character hex string) suitable for use as `masterKey` or `jwtSecret`.

### Platform Status

```bash
astonish platform status
```

Displays organization count, user count, lists organizations with team counts, and shows the PostgreSQL version.

### Organization Management

```bash
# Create a new organization
astonish platform org create --name "Acme Corp" --slug acme-corp

# Create with an existing user as owner
astonish platform org create --name "Acme Corp" --slug acme-corp --owner-email admin@acme.com

# List all organizations
astonish platform org list

# Invite a user to an organization
astonish platform org invite \
  --org acme-corp \
  --email user@acme.com \
  --role member

# Invite with password prompt (instead of generated password)
astonish platform org invite \
  --org acme-corp \
  --email admin@acme.com \
  --role owner \
  --password
```

#### `org create` Flags

| Flag | Description |
|------|-------------|
| `--name` | Organization display name (required) |
| `--slug` | URL-safe identifier (required) |
| `--owner-email` | Set an existing user as org owner |

#### `org invite` Flags

| Flag | Description |
|------|-------------|
| `--org` | Organization slug (required) |
| `--email` | User's email address (required) |
| `--role` | Role: `owner`, `admin`, `member` (default: member) |
| `--name` | Display name for new users |
| `--team` | Also add to this team (default: general) |
| `--password` | Prompt for password instead of generating one |

### User Management

```bash
# List all users (optionally filter by org)
astonish platform user list [--org <slug>]

# Show user details
astonish platform user show <email>

# Delete a user
astonish platform user delete <email>

# Set a user's password interactively
astonish platform user set-password <email>

# Disable/enable a user account
astonish platform user disable <email>
astonish platform user enable <email>

# Promote/demote platform superadmin
astonish platform user promote <email>
astonish platform user demote <email>
```

### Issue Access Token

```bash
# Interactive browser-based login
astonish platform issue-token --server https://astonish.example.com --sso

# Direct password authentication
astonish platform issue-token \
  --server https://astonish.example.com \
  --email admin@example.com \
  --password

# Output as JSON
astonish platform issue-token --server https://astonish.example.com --json
```

### Sandbox Audit

```bash
astonish platform sandbox-audit
```

Audits sandbox PVCs for orphaned data.

## Roles

| Role | Permissions |
|------|-------------|
| `owner` | Full org control, manage all members and teams |
| `admin` | Manage members, teams, settings |
| `member` | Use agents, view team resources |
