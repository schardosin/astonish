# Sessions

A session is a single conversation between you and the agent. Sessions persist across restarts, allowing you to resume work exactly where you left off.

## Session Lifecycle

1. **Created** — A new session starts when you begin a chat
2. **Active** — Messages, tool calls, and results are appended
3. **Paused** — You disconnect; the session waits on disk
4. **Resumed** — You reconnect and continue from the last message
5. **Archived** — Old sessions move to cold storage after inactivity

## Storage Backends

### Local (SQLite)

Sessions are stored in the SQLite database at `~/.local/share/astonish/`. Each session is a series of events (messages, tool calls, tool results, metadata) stored in structured tables with full indexing.

### Cloud (PostgreSQL)

Sessions are stored in PostgreSQL with full indexing:

- Full-text search across session content
- Team-scoped visibility (team members can view shared sessions)
- Retention policies managed by org admins
- Concurrent access for collaborative sessions

## Resuming Sessions

```bash
# List recent sessions
astonish session list

# Resume by ID
astonish chat --session abc123

# Resume the most recent session
astonish chat --resume
```

In Studio, the session sidebar shows all sessions with search and filtering.

## Team-Scoped Sessions (Cloud Deployment)

Sessions can be shared within a team:

```bash
# Share a session with your team
astonish session share <session-id>

# List team sessions
astonish session list --scope team
```

Shared sessions are read-only for other team members unless explicitly granted write access.

## Session Metadata

Each session tracks:

- Creation time and last activity
- Model used
- Token usage (input/output)
- Tool calls executed
- Working directory context

## Cleanup

```bash
# Delete a session
astonish session delete <session-id>

# Prune sessions older than 30 days
astonish session prune --older-than 30d
```

See [Memory](./memory.md) for how to extract durable knowledge from sessions, and [Chat](./chat.md) for the interaction model.
