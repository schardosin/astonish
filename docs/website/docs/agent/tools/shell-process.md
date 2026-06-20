# Shell & Process Tools

Six tools for executing commands, managing processes, and interacting with the system shell.

## Tools

| Tool | Description | Confirmation |
|------|-------------|-------------|
| `shell_command` | Execute a command in a PTY-backed shell | always-confirm |
| `shell_command_background` | Run a command in the background | always-confirm |
| `process_list` | List running background processes | auto-approve |
| `process_kill` | Terminate a background process | always-confirm |
| `process_output` | Read stdout/stderr from a background process | auto-approve |
| `working_directory` | Get or set the current working directory | auto-approve |

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
- Configurable timeout (default 60 seconds)
- Working directory override
- Exit code reported in results

## Background Processes

Long-running services (dev servers, watchers) can be started in the background:

```
shell_command_background:
  command: "npm run dev"
  label: "dev-server"
```

Then monitored:

```
process_output:
  label: "dev-server"
  lines: 50
```

And stopped when no longer needed:

```
process_kill:
  label: "dev-server"
```

## Working Directory

The agent tracks a working directory that persists across tool calls within a session. File paths can be relative to this directory:

```
working_directory:
  path: "/home/user/project/src"
```

See [File & Search Tools](./file-search.md) for filesystem operations and [Chat](../chat.md) for the confirmation system.
