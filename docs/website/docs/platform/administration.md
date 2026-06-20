# Administration

This page covers platform initialization, org provisioning, user management, authentication setup, and migration from local to cloud deployments.

## Platform Initialization

```bash
# Initialize the platform database and schema
astonish platform init --dsn "postgres://admin:secret@db.acme.corp:5432/astonish_platform"
```

This creates the `astonish_platform` database with tables for organizations, users, and platform-level configuration. The DSN can also be set via environment variable:

```bash
export ASTONISH_PLATFORM_DSN="postgres://admin:secret@db.acme.corp:5432/astonish_platform"
astonish platform init
```

## Starting the Server

```bash
# Start platform server
astonish platform serve --port 8080

# With TLS
astonish platform serve --port 8443 --tls-cert /etc/ssl/cert.pem --tls-key /etc/ssl/key.pem

# With config file
astonish platform serve --config /etc/astonish/platform.yaml
```

Platform server configuration:

```yaml
# /etc/astonish/platform.yaml
server:
  port: 8080
  host: 0.0.0.0
database:
  dsn: postgres://admin:secret@db.acme.corp:5432/astonish_platform
  max_connections: 50
auth:
  method: oidc          # "builtin" or "oidc"
  jwt_secret: ${JWT_SECRET}
  session_ttl: 24h
```

## Organization Provisioning

```bash
# Create an organization
astonish platform org create --name "Acme Corp" --slug acme --owner alice@acme.corp

# List all organizations
astonish platform org list

# Show org details
astonish platform org show acme

# Delete an org (requires confirmation, destroys database)
astonish platform org delete acme --confirm
```

Org creation provisions a dedicated PostgreSQL database (`org_acme`) with the standard schema set. The designated owner receives the `owner` role automatically.

## User Management

```bash
# Create a user (built-in auth)
astonish platform user create --email bob@acme.corp --org acme --role member

# List users
astonish platform user list --org acme

# Change role
astonish platform user set-role bob@acme.corp --org acme --role admin

# Deactivate user (preserves data, revokes access)
astonish platform user deactivate bob@acme.corp

# Reactivate
astonish platform user activate bob@acme.corp
```

## Authentication Setup

### Built-in Auth

The default mode. Users authenticate with email/password. Passwords are hashed with bcrypt. Sessions use JWTs.

```yaml
auth:
  method: builtin
  jwt_secret: ${JWT_SECRET}
  password_policy:
    min_length: 12
    require_uppercase: true
    require_number: true
```

### OIDC Federation

For enterprise environments, federate with any OIDC-compliant provider:

```yaml
auth:
  method: oidc
  oidc:
    issuer: https://accounts.sap.com
    client_id: ${OIDC_CLIENT_ID}
    client_secret: ${OIDC_CLIENT_SECRET}
    scopes: [openid, email, profile]
    # Map OIDC claims to Astonish roles
    claims_mapping:
      email: email
      name: name
      org: custom:organization
```

Tested providers: SAP IAS, Azure AD, Okta, Google Workspace, Keycloak.

Users are auto-provisioned on first login when their email domain matches an org's allowed domains (see [Organizations & Teams](./organizations-and-teams)).

## Migration from Local to Cloud

Users running Astonish locally with SQLite can migrate their data to a cloud PostgreSQL deployment:

```bash
# Export local data
astonish export --format archive --output ~/astonish-backup.tar.gz

# Import into cloud platform (run as the user, after login)
astonish platform migrate --from ~/astonish-backup.tar.gz
```

The migration imports:
- Sessions and messages → personal schema
- Memory entries → personal memory tier
- Flows and apps → personal workspace
- Configuration → personal config overrides

After migration, the local SQLite database remains untouched as a backup.

## Platform CLI Commands

| Command | Description |
|---------|-------------|
| `astonish platform init` | Initialize platform database |
| `astonish platform serve` | Start platform server |
| `astonish platform org create` | Provision new organization |
| `astonish platform org list` | List all organizations |
| `astonish platform org delete` | Remove organization |
| `astonish platform user create` | Create user account |
| `astonish platform user list` | List users in org |
| `astonish platform user set-role` | Change user role |
| `astonish platform user deactivate` | Suspend user access |
| `astonish platform migrate` | Import local SQLite data |
| `astonish platform backup` | Backup org database |
| `astonish platform restore` | Restore from backup |

## Backups

```bash
# Backup a specific org
astonish platform backup --org acme --output /backups/acme-2025-03-15.sql.gz

# Backup all orgs
astonish platform backup --all --output-dir /backups/

# Restore
astonish platform restore --org acme --from /backups/acme-2025-03-15.sql.gz
```

## Next Steps

- [Platform Overview](./index) — architecture and design decisions
- [Cascading Defaults](./cascading-defaults) — configuring defaults at each level
- [Remote CLI](./remote-cli) — how users connect to the platform
