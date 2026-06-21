# Sessions

A session is a single conversation between you and the agent. Sessions persist across restarts, allowing you to resume work exactly where you left off.

## Session Lifecycle

1. **Created** — A new session starts when you begin a chat
2. **Active** — Messages, tool calls, and results are appended
3. **Paused** — You disconnect; the session persists in the database
4. **Resumed** — You reconnect and continue from the last message

## Managing Sessions in Studio

Studio provides the primary session management experience:

- **Session sidebar** — Browse all sessions with search and filtering
- **Resume** — Click any session to continue the conversation
- **Delete** — Remove sessions you no longer need
- **Session details** — View metadata (model, token usage, tool calls, timestamps)

## CLI Session Commands

```bash
# List your sessions
astonish sessions list

# Show session details/trace
astonish sessions show <session-id>

# Resume a session in chat
astonish chat --resume <session-id>
astonish chat -r <session-id>

# Delete a session
astonish sessions delete <session-id>

# Delete all sessions
astonish sessions clear
```

::: tip Plural Command
The CLI command is `astonish sessions` (plural), not `astonish session`.
:::

## Session Storage

Sessions are stored in the platform database:

- **SQLite** — Stored in the personal SQLite database file
- **PostgreSQL** — Stored in your personal schema with full indexing and full-text search

In team deployments, sessions can be published to your team via Studio (see [Publish & Fork](../platform/publish-and-fork.md)).

## Session Metadata

Each session tracks:

- Creation time and last activity
- Model used
- Token usage (input/output)
- Tool calls executed
- Working directory context

## Context Compaction

For long-running sessions approaching the model's context window limit, the agent automatically compacts older messages while preserving key context. Use the `/compact` slash command to check compaction status.

See [Memory](./memory.md) for how to extract durable knowledge from sessions, and [Chat](./chat.md) for the interaction model.
