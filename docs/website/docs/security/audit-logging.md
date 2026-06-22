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
| `team_id` | Team context (if applicable) |
| `action` | HTTP method (GET, POST, DELETE, etc.) |
| `resource` | Route pattern and path of the request |
| `detail` | Additional structured data (JSON — includes path, status code) |
| `ip_address` | Client IP address (from X-Forwarded-For or RemoteAddr) |
| `session_id` | Session that generated the event (if applicable) |

Audit logging is implemented as HTTP middleware — every API request is automatically recorded without requiring explicit instrumentation in handlers. Writes are asynchronous to avoid adding latency to requests.

## Event Categories

- **Authentication** — login, logout, token refresh, failed attempts
- **Credentials** — created, rotated, deleted, accessed
- **Sessions** — started, ended, tool calls executed
- **Administration** — user invited, team modified, org settings changed
- **Sandboxes** — created, destroyed, resource limit changes

## Viewing Audit Logs

Audit logs are scoped by role:

- **Org admins** see all events within their organization.
- **Team admins** see events within their team.
- **Members** see only their own events.

### In Studio

Studio provides an audit log viewer under **Settings → Audit Log** with filters for action type, user, resource, and time range.

### Via API

Query audit logs programmatically:

```
GET /api/audit?action=POST&resource=/api/credentials&since=2024-01-01&limit=100
```

Supported query parameters:

| Parameter | Description |
|-----------|-------------|
| `action` | Filter by HTTP method |
| `resource` | Filter by resource path pattern |
| `since` | Start time (ISO 8601) |
| `until` | End time (ISO 8601) |
| `limit` | Maximum records to return |
| `offset` | Pagination offset |

## Retention

Audit logs are retained indefinitely by default. Configure retention policies in platform settings if your compliance requirements specify a maximum retention period.

## See Also

- [Authentication](./authentication.md) — events that generate audit records
- [Credential Security](./credential-security.md) — credential access is always audited
