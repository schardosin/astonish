---
title: Shell & Process Tools
description: Execute commands and manage long-running processes
---

The Shell & Process category includes 6 tools for running commands, managing background processes, and delegating complex coding tasks.

## shell_command

Execute a shell command with PTY support. Returns stdout and exit code. If the command waits for input, returns `waiting_for_input=true` with a `session_id` for follow-up interaction.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `command` | string | Yes | Shell command to execute |
| `timeout` | int | No | Timeout in seconds (default: 120, max: 3600) |
| `working_dir` | string | No | Working directory |
| `background` | bool | No | Start in background, return `session_id` immediately |

## process_read

Read output from a running or completed process. Use `offset` for incremental reads to avoid re-reading previous output.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `session_id` | string | Yes | Session ID from `shell_command` |
| `offset` | int | No | Byte offset to start reading from |

## process_write

Send input to a running process. Always include a trailing `\n` to simulate pressing Enter.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `session_id` | string | Yes | Session ID of the process |
| `input` | string | Yes | Text to send (include trailing `\n`) |

## process_list

List all active and recent process sessions.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `filter` | string | No | Filter by command or session ID |

## process_kill

Kill a running process. Sends SIGTERM first, followed by SIGKILL after 5 seconds if the process does not exit.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `session_id` | string | Yes | Session ID to kill |

## opencode

Delegate a task to OpenCode, a specialized AI coding agent. OpenCode can read and write files, run commands, search code, and perform complex software engineering tasks autonomously.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `task` | string | Yes | Task description with full context |
| `dir` | string | Yes | Project root directory |
| `session_id` | string | No | Continue an existing OpenCode session |
| `model` | string | No | Override model (`provider/model` format) |
| `agent` | string | No | Agent type: `build` (default, full access) or `explore` (read-only) |

## Process management workflow

Use the process tools together to manage long-running commands like dev servers, watchers, or test suites:

1. **Start a background process** — call `shell_command` with `background=true`. You get back a `session_id` immediately without waiting for the command to finish.

2. **Check output periodically** — call `process_read` with the `session_id`. Use `offset` to read only new output since your last check, avoiding duplicate data.

3. **Send input when needed** — call `process_write` to respond to prompts or send commands to interactive processes. Always include a trailing `\n`.

4. **Clean up** — call `process_kill` when the process is no longer needed. This frees up the session and terminates the underlying command.

```text
shell_command (background=true)
        |
        v
   session_id
        |
        +---> process_read (offset=0)    --> read initial output
        +---> process_read (offset=1024) --> read new output
        +---> process_write ("yes\n")    --> respond to prompt
        +---> process_kill               --> terminate
```
