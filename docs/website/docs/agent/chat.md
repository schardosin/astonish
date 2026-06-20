# Chat

The Astonish agent operates through an LLM-driven tool-use loop. You describe a goal, and the agent plans, executes tools, observes results, and iterates until the task is complete.

## How It Works

1. You send a message (CLI or Studio)
2. The LLM analyzes the request and selects appropriate tools
3. Tools execute, and results feed back to the LLM
4. The LLM decides: respond to you or invoke more tools
5. Loop continues until the task is resolved

There is no fixed pipeline. The agent dynamically adapts based on tool outputs, errors, and intermediate findings.

## Confirmation System

Destructive or sensitive tool calls require confirmation before execution:

- **auto-approve** — Tools like `read_file`, `grep_search` run without asking
- **always-confirm** — Tools like `shell_command`, `write_file` prompt you first
- **never-confirm** — Blocked tools that cannot be invoked

In Studio, confirmations appear as inline approval cards. In CLI, you see a `[y/N]` prompt.

Configure confirmation behavior in your config:

```yaml
tools:
  auto_approve:
    - read_file
    - file_tree
    - grep_search
  always_confirm:
    - shell_command
    - write_file
```

## Slash Commands

| Command | Description |
|---------|-------------|
| `/distill` | Compress current session into a reusable flow |
| `/clear` | Clear conversation history (keeps memory) |
| `/model` | Switch AI model mid-session |
| `/help` | Show available commands |
| `/session` | Session management |

## CLI Usage

```bash
# Start interactive chat
astonish chat

# Single-shot message
astonish chat "explain the auth module"

# Resume a previous session
astonish chat --session <session-id>

# Use a specific model
astonish chat --model gpt-4o
```

## Studio Usage

The Studio provides a web-based chat interface with:

- Real-time streaming responses
- Inline tool execution visualization
- File diffs for write operations
- Artifact previews (reports, generated files)
- Model selector dropdown
- Session history sidebar

<!-- IMAGE: Studio chat interface showing tool execution cards and streaming response -->

## Multi-Turn Context

The agent maintains full context within a session. Reference previous messages, build on prior tool results, and incrementally refine solutions. For long-running sessions, use `/distill` to compress context into a flow.

See [Sessions](./sessions.md) for persistence details and [Tools Overview](./tools/index.md) for the complete tool catalog.
