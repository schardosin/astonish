# Shell & Process Tools

Five tools for executing commands and managing background processes.

## Tools

| Tool | Description | Confirmation |
|------|-------------|-------------|
| `shell_command` | Execute a command in a PTY-backed shell | always-confirm |
| `process_read` | Read stdout from a background process | auto-approve |
| `process_write` | Send input to a background process | always-confirm |
| `process_list` | List active background processes | auto-approve |
| `process_kill` | Terminate a background process | always-confirm |

## shell_command

Executes commands in a full PTY (pseudo-terminal), supporting interactive programs, color output, and signal handling:

```
shell_command:
  command: "npm run build"
  timeout: 120
  working_dir: "/home/user/project"
```

Features:
- Full PTY emulation (supports curses, colors, prompts)
- Configurable timeout (default 120 seconds, max 3600)
- Working directory override
- Exit code reported in results
- Background mode via `background: true` parameter

### Background Mode

Long-running services (dev servers, watchers) can be started in the background by setting `background: true`:

```
shell_command:
  command: "npm run dev"
  background: true
```

This returns a `session_id` immediately. Use the process tools to interact with it.

## Process Management

Once a background process is running:

```
# Read output
process_read:
  session_id: "abc123"

# Send input (e.g., respond to a prompt)
process_write:
  session_id: "abc123"
  input: "yes\n"

# List all running processes
process_list: {}

# Stop a process
process_kill:
  session_id: "abc123"
```

## Working Directory

The agent tracks a working directory that persists across tool calls within a session. File paths can be relative to this directory. Set it via the `working_dir` parameter on `shell_command`.

See [File & Search Tools](./file-search.md) for filesystem operations and [Chat](../chat.md) for the confirmation system.
