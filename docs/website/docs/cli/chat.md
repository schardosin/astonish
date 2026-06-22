# Chat Commands

The `astonish chat` command starts an interactive agent session from the terminal.

## Usage

```bash
astonish chat [flags]
```

Running `astonish chat` with no arguments starts a new interactive session using your default provider and model.

## Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--provider` | `-p` | AI provider to use |
| `--model` | `-m` | Model name |
| `--resume` | `-r` | Resume a session by ID (or latest if no ID given) |
| `--workspace` | `-w` | Working directory (default: current directory) |
| `--auto-approve` | | Auto-approve all tool executions |
| `--debug` | | Enable debug mode |

## Examples

```bash
# Start a new chat with defaults
astonish chat

# Chat with a specific provider and model
astonish chat -p openai -m gpt-4o

# Resume the most recent session
astonish chat -r

# Resume a specific session
astonish chat -r abc123

# Auto-approve all tool calls
astonish chat --auto-approve
```

## In-Session Commands

While in an active chat session, type `/` to access these commands:

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/status` | Show provider, model, context, tools, and session info |
| `/new` | Start a fresh conversation |
| `/compact` | Show context window usage and compaction status |
| `/distill` | Distill the current session into a reusable flow |
| `/fleet` | Show available fleets and fleet commands |
| `/fleet-plan` | Create a reusable fleet plan (redirects to Studio) |
| `/drill` | Create a drill suite with guided wizard |
| `/drill-add` | Add new drills to an existing suite |
| `/authorize <code>` | Authorize a device to access Studio |

Type `exit` or `quit` to end the session.
