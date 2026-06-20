# Utility Commands

Miscellaneous commands for setup, status, and system management.

## `astonish setup`

Interactive first-run wizard that configures your Astonish installation:

```bash
astonish setup
```

Walks you through:
- Selecting an AI provider
- Entering API keys
- Choosing a default model
- Configuring optional features (daemon, channels)

## `astonish status`

Display the current state of all Astonish subsystems:

```bash
astonish status
```

Output:
```
Astonish v0.12.0
Database: sqlite
Config: ~/.config/astonish/config.yaml
Providers:
  anthropic: configured (claude-sonnet)
  openai: configured (gpt-4o)
Daemon: running (http://localhost:9393)
MCP Servers: 2 active
Memory: 47 entries
```

## `astonish version`

Print version and build information:

```bash
astonish version
```

## `astonish memory`

Manage the agent memory system:

```bash
# List stored memories
astonish memory list

# Search memories
astonish memory search "database schema"

# Clear all memories
astonish memory clear

# Clear a specific tier (cloud deployments)
astonish memory clear --tier team
```

## `astonish login`

Authenticate with a cloud platform instance (cloud deployments only):

```bash
# Interactive login
astonish login

# Login with specific server
astonish login --server https://astonish.company.com
```

## `astonish studio`

::: warning Deprecated
The `astonish studio` command may be removed in a future release. Studio is now served automatically by the daemon at `http://localhost:9393`. Use `astonish daemon start` instead.
:::

Opens the Studio web UI in your default browser. This is a convenience shortcut that ensures the daemon is running and opens the browser:

```bash
astonish studio
```

See [Studio Overview](../studio/) for details on the web interface.
