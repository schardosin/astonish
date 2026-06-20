# Chat Commands

The `astonish chat` command starts and manages interactive agent sessions from the terminal.

## Usage

```bash
astonish chat [flags]
```

Running `astonish chat` with no arguments starts a new interactive session using your default provider and model.

## Subcommands

### Start a New Session

```bash
# New session with defaults
astonish chat

# New session with specific provider/model
astonish chat --provider anthropic --model claude-sonnet

# New session with an initial message
astonish chat "Explain the authentication flow in this codebase"
```

### Resume a Session

```bash
# Resume the most recent session
astonish chat --resume

# Resume a specific session by ID
astonish chat --resume <session-id>
```

### List Sessions

```bash
# List recent sessions
astonish chat list

# List with details (model, token count, timestamp)
astonish chat list --verbose
```

## Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--provider` | `-p` | AI provider to use |
| `--model` | `-m` | Model name |
| `--resume` | `-r` | Resume a session (latest or by ID) |
| `--flow` | `-f` | Start chat in the context of a flow |
| `--no-tools` | | Disable tool use for this session |
| `--system` | `-s` | Override system prompt |

## Examples

```bash
# Quick question (non-interactive, single response)
astonish chat --once "What port does the API server use?"

# Chat with a specific model
astonish chat -p openai -m gpt-4o

# Resume last conversation
astonish chat -r
```

## In-Session Commands

While in an active chat session, these commands are available:

| Command | Description |
|---------|-------------|
| `/new` | Start a new session |
| `/model <name>` | Switch model |
| `/quit` | Exit the session |
| `/clear` | Clear terminal display |
