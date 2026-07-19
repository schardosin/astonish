# Utility Commands

Miscellaneous commands for setup, status, and system management.

## `astonish setup`

Interactive first-run wizard that configures your Astonish installation:

```bash
astonish setup
```

Walks you through:
- Selecting a storage backend (SQLite or PostgreSQL)
- Selecting an AI provider and entering API keys
- Choosing a default model
- Configuring web search tools
- Setting up browser automation
- Initializing container sandboxes (optional)

## `astonish status`

Display the current state of all Astonish subsystems:

```bash
astonish status
```

Shows provider configuration, daemon status, MCP servers, memory state, and more.

## `astonish --version`

Print version and build information:

```bash
astonish --version
# or
astonish -v
```

::: info
This is a flag, not a subcommand. Use `--version` or `-v`.
:::

## `astonish login`

Authenticate with a remote platform instance:

```bash
astonish login
```

Used when connecting the CLI to a cloud-deployed Astonish platform. After login, commands like `chat`, `flows`, and `scheduler` operate against the remote server.

## `astonish logout`

Disconnect from the remote platform:

```bash
astonish logout
```

## `astonish daemon`

Manage the background daemon service and Studio web UI (local-only):

```bash
# Install as a system service (auto-starts on login)
astonish daemon install

# Start / stop / restart the service
astonish daemon start
astonish daemon stop
astonish daemon restart

# Check service status
astonish daemon status

# Run in the foreground (starts the Studio HTTP server on :9393)
astonish daemon run

# View logs
astonish daemon logs

# Uninstall the service
astonish daemon uninstall
```

`astonish daemon run` starts the Studio web interface at `http://localhost:9393`. The installed service runs this automatically in the background on login.

## `astonish config`

Manage configuration:

```bash
astonish config
```

## `astonish tools`

Manage MCP servers and tools:

```bash
# List all available tools
astonish tools list

# List MCP servers
astonish tools servers

# Refresh tool cache
astonish tools refresh

# Enable/disable an MCP server
astonish tools enable <name>
astonish tools disable <name>

# Edit MCP configuration
astonish tools edit

# Browse MCP server store
astonish tools store

# Search tools
astonish tools search <query>
```

## `astonish sessions`

Manage chat sessions:

```bash
astonish sessions
```

## `astonish credentials`

Manage the encrypted credential store (local-only):

```bash
astonish credentials
```

## `astonish sandbox`

Manage container sandboxes (local-only):

```bash
astonish sandbox
```

## Deprecated Commands

### `astonish memory`

::: warning Deprecated
The `memory` CLI command is no longer available. Memory is managed through the agent's built-in memory tools during chat sessions, or via Studio Settings.
:::

See [Studio Overview](../studio/) for details on the web interface.
