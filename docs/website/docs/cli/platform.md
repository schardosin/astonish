# Platform Commands

The `astonish platform` command manages multi-tenant platform operations — initialization, organization management, and migrations.

## Usage

```bash
astonish platform <subcommand> [flags]
```

## Subcommands

### Initialize Platform

```bash
# Initialize the platform with PostgreSQL
astonish platform init --dsn "postgres://user:pass@localhost:5432/astonish"

# Initialize with all options
astonish platform init \
  --dsn "postgres://user:pass@localhost:5432/astonish" \
  --admin-email "admin@company.com" \
  --org-name "my-company"
```

This creates the database schema, sets up encryption keys, and creates the initial organization and admin user.

### Platform Status

```bash
# Show platform status
astonish platform status
```

Displays: database connection, schema version, active organizations, user count, and service health.

### Organization Management

```bash
# Create a new organization
astonish platform org create --name "acme-corp" --display-name "Acme Corporation"

# List all organizations
astonish platform org list

# Invite a user to an organization
astonish platform org invite --org acme-corp --email "user@acme.com" --role member

# Remove a user from an organization
astonish platform org remove --org acme-corp --email "user@acme.com"

# List org members
astonish platform org members --org acme-corp
```

### Team Management

```bash
# Create a team within an org
astonish platform team create --org acme-corp --name backend

# Add a user to a team
astonish platform team add --org acme-corp --team backend --email "dev@acme.com"
```

### Database Migrations

```bash
# Run pending migrations
astonish platform migrate

# Show migration status
astonish platform migrate status

# Rollback last migration
astonish platform migrate rollback
```

## Roles

| Role | Permissions |
|------|-------------|
| `owner` | Full org control, billing, delete org |
| `admin` | Manage members, teams, settings |
| `member` | Use agents, view team resources |

## Flags

| Flag | Description |
|------|-------------|
| `--dsn` | PostgreSQL connection string |
| `--org` | Target organization name |
| `--email` | User email address |
| `--role` | Role to assign (`owner`, `admin`, `member`) |
| `--force` | Skip confirmation prompts |
