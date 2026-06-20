# Audit Logging

Astonish maintains an append-only, immutable audit trail of all security-relevant actions. Audit records cannot be modified or deleted — not by users, not by administrators, not by the application itself.

## Database-Level Immutability

The audit table is protected at the PostgreSQL grant level:

```sql
GRANT INSERT ON audit_log TO astonish_app;
-- No UPDATE, DELETE, or TRUNCATE granted
```

Even if the application is compromised, existing audit records cannot be altered. This guarantee is enforced by the database, not application code.

## What Is Logged

Every audit entry captures:

| Field | Description |
|-------|-------------|
| `timestamp` | UTC time of the event |
| `user_id` | Authenticated user who performed the action |
| `org_id` | Organization context |
| `team_id` | Team context (if applicable) |
| `action` | Event type (e.g., `credential.created`, `session.started`) |
| `resource` | Target resource identifier |
| `ip_address` | Client IP address |
| `session_id` | Session that generated the event |
| `metadata` | Additional structured data (JSON) |

## Event Categories

- **Authentication** — login, logout, token refresh, failed attempts
- **Credentials** — created, rotated, deleted, accessed
- **Sessions** — started, ended, tool calls executed
- **Administration** — user invited, team modified, org settings changed
- **Sandboxes** — created, destroyed, resource limit changes

## Access and Filtering

Audit logs are scoped by role:

- **Org admins** see all events within their organization.
- **Team admins** see events within their team.
- **Members** see only their own events.

Filter logs via the CLI:

```bash
# All events for a team in the last 24h
astonish audit list --team backend --since 24h

# Credential access events for a specific user
astonish audit list --user jane@example.com --action "credential.*"
```

## Retention

Audit logs are retained indefinitely by default. Configure retention policies in platform settings if your compliance requirements specify a maximum retention period.

## See Also

- [Authentication](./authentication.md) — events that generate audit records
- [Credential Security](./credential-security.md) — credential access is always audited
