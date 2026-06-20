# Tools Overview

Astonish provides 58+ built-in tools across 8 categories, plus unlimited extension via MCP servers. Tools are the agent's interface to the world—reading files, running commands, browsing the web, managing credentials, and more.

## Tool Categories

| Category | Tools | Description |
|----------|-------|-------------|
| [Shell & Process](./shell-process.md) | 6 | Command execution, background processes |
| [File & Search](./file-search.md) | 9 | Read, write, search, navigate filesystem |
| [Web & HTTP](./web-http.md) | 2 | Fetch pages, make API requests |
| [Browser Automation](./browser.md) | 32 | Full Chrome DevTools automation |
| [Email](./email.md) | 8 | Inbox management, send, search |
| [Credentials](./credentials.md) | 2 | Secure secret storage and retrieval |
| [Scheduler & Agent](./scheduler-agent.md) | 4 | Task scheduling, delegation, flows |
| Memory | 3 | Save, search, retrieve memories |

## Confirmation System

Every tool has a confirmation level that determines whether user approval is needed before execution:

### auto-approve

Safe, read-only tools that execute immediately:

- `read_file`, `file_tree`, `grep_search`
- `memory_search`, `skill_lookup`
- `web_fetch` (GET only)

### always-confirm

Tools that modify state or have side effects:

- `write_file`, `shell_command`
- `http_request` (POST/PUT/DELETE)
- `email_send`, `browser_click`

### never-confirm

Blocked tools that cannot be invoked (configured by admins in cloud deployments):

```yaml
tools:
  never_confirm:
    - shell_command    # Block shell access entirely
```

### Customizing Confirmation

Override defaults in your config:

```yaml
tools:
  auto_approve:
    - write_file       # Trust file writes
    - shell_command    # Trust shell (use with caution)
  always_confirm:
    - email_send       # Always ask before sending email
```

## MCP Integration

Tools from [MCP servers](../configuration/mcp-servers.md) appear alongside built-in tools. The agent sees a unified tool list and selects from both seamlessly.

MCP tools follow the same confirmation system—new MCP tools default to `always-confirm` until explicitly trusted:

```yaml
tools:
  auto_approve:
    - "mcp:github:create_issue"
    - "mcp:filesystem:read_file"
```

## Tool Execution in Studio

In the Studio interface, tool executions render as expandable cards showing:

- Tool name and arguments
- Execution duration
- Output (truncated with expand option)
- Confirmation buttons for `always-confirm` tools

<!-- IMAGE: Tool execution cards in Studio showing shell_command with confirmation prompt -->

See individual tool pages for detailed documentation on each category.
