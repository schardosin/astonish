# Configuration

## Overview

Astonish uses a YAML-based configuration system with two main config types: the application config (`config.yaml`) for global settings, and agent configs (flow YAML files) for workflow definitions. Configuration is loaded from `~/.config/astonish/` and supports provider credentials, channel settings, sandbox options, scheduler jobs, MCP servers, and more.

## Key Design Decisions

### Why YAML

YAML was chosen over JSON, TOML, or environment variables because:

- **Readability**: Complex nested structures (providers, channels, MCP servers) are easy to read and edit.
- **Comments**: YAML supports comments, which are important for documenting configuration choices.
- **Ecosystem familiarity**: Developers are accustomed to YAML from Kubernetes, Docker Compose, and CI/CD tools.
- **LLM-friendly**: The agent can read and suggest configuration changes in YAML format.

### Why a Centralized Config Directory

All configuration lives under `~/.config/astonish/`:

```
~/.config/astonish/
  config.yaml           # Main application config
  credentials.enc       # Encrypted credential store
  .store_key           # Credential encryption key
  agents/              # Flow YAML files
  flow_registry.json   # Flow index
  sessions/            # Session data
  memory/              # Knowledge files
  skills/              # Skill documents
  logs/                # Daemon logs
```

This provides a single location for all Astonish state, making backup and migration straightforward.

### Daemon Log Destination

The daemon supports two application log destinations: `file` and `stdout`. Local/default mode writes to `~/.config/astonish/logs/daemon.log` so `astonish daemon logs` and service-managed installs keep their existing behavior. Container roles selected with `ASTONISH_MODE=api` or `ASTONISH_MODE=worker` write to stdout by default, which is the cloud invariant for Kubernetes: `kubectl logs` and cluster log collectors must see application logs without reading files inside the container filesystem.

Operators can override the default with `ASTONISH_LOG_DEST=file` or `ASTONISH_LOG_DEST=stdout`. `daemon.log_dir` only affects the `file` destination.

### Why Config-Driven Provider Setup

Provider API keys can come from three sources (in priority order):

1. **Credential store**: The encrypted store is the recommended source.
2. **Config file**: `config.yaml` providers section (legacy, migrated automatically).
3. **Environment variables**: Standard env vars (ANTHROPIC_API_KEY, OPENAI_API_KEY, etc.).

The `provider_env.go` module resolves credentials from the store and sets them as environment variables before any provider adapter is initialized. This means provider adapters don't need to know about the credential store -- they just read their standard env vars.

### First-Run Setup Bootstrap Authority

Before initializing a database through Studio, the operator must set a secret
`ASTONISH_BOOTSTRAP_TOKEN` in the Astonish server process environment and supply
that value in the setup wizard's **Bootstrap Token** password field. Both
`POST /api/platform/init` (PostgreSQL) and `POST /api/platform/init/sqlite`
require the matching `X-Astonish-Bootstrap-Token` request header. This applies
on loopback and to SQLite single-user (`auth_mode: none`) setup too; missing
server configuration or a missing/incorrect header is rejected. The server does
not generate or disclose the token over HTTP.

Studio keeps this token only in setup-component memory, never in browser
storage, URL/query parameters, or the JSON setup body. Initialization is disabled
until a nonblank token is entered. Errors preserve the value for correction and
retry; successful initialization clears it. The API wrappers in
`web/src/api/platform.ts` accept the token as a separate required argument, not
as a database-configuration field. This bootstrap authority does not replace
normal post-setup authentication.

## Architecture

### Application Config Structure

```yaml
# Provider configuration
provider: anthropic
model: claude-sonnet-4-20250514

# Alternative providers
providers:
  anthropic:
    api_key: ""          # Migrated to credential store
  openai:
    api_key: ""
  google:
    api_key: ""

# Sandbox settings
sandbox:
  enabled: true
  limits:
    memory: "2GB"
    cpu: 2
    processes: 500
  network: bridged
  prune:
    orphan_check_hours: 6
    idle_timeout_minutes: 10

# Channel configuration
channels:
  telegram:
    bot_token: ""
    allow_from: [123456789]
  email:
    imap_host: "imap.gmail.com"
    smtp_host: "smtp.gmail.com"
    username: ""
    password: ""
    allow_from: ["user@example.com"]

# MCP servers
mcp_servers:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "{{CREDENTIAL:github:token}}"

# Scheduler
scheduler:
  jobs:
    daily_report:
      cron: "0 9 * * *"
      mode: adaptive
      instruction: "Generate the daily status report"

# Memory
memory:
  embedding:
    provider: local            # local, openai, ollama, openai-compat
    timeout_seconds: 30        # Remote embedding HTTP deadline; nonzero by default

# Chat runtime deadlines
chat:
  pre_provider_retrieval_timeout_seconds: 10

# Session management
sessions:
  expiry_hours: 720            # 30 days

# Agent behavior
custom_prompt: ""              # Additional system prompt text
auto_approve: false            # Skip tool approval in chat mode
debug_mode: false

# Agent identity (for web portal interactions)
identity:
  name: "Astonish Agent"
  username: "astonish"
  email: "agent@example.com"
```

### Config Loading

```
Daemon startup:
  |
  v
LoadAppConfig(configDir):
  1. Read config.yaml
  2. Parse YAML into AppConfig struct
  3. Resolve accessor defaults for unset values
  4. Validate settings (sandbox limits, network mode, etc.)
    |
    v
Provider setup:
  1. Resolve API keys from credential store
  2. Set environment variables (ANTHROPIC_API_KEY, etc.)
    |
    v
Config available to all subsystems
```

### Full Configuration API Secret Boundary

`GET /api/settings/full` masks secrets in response copies, never in the trusted
runtime configuration. Browser `proxy` and `remote_cdp_url` retain the URL
scheme, host, port, and path but replace userinfo, every nonempty query value
(including repeated and vendor-specific parameters), and fragments with `****`.
Malformed or opaque URLs are masked as a whole rather than exposed through a
parser error. The browser handoff VNC password is also masked. URL paths and
query parameter names remain visible; operators must not put credentials there.

`PUT /api/settings/full` requires platform superadmin authority in platform
mode, including on loopback, before decoding the body. Trusted non-platform
local mode retains its existing access. JSON section names and scalar value
formats are unchanged; URL and embedding credential fields distinguish omitted
(or `null`) from an explicit empty string:

- Omitted credentials retain the stored value; `""` explicitly clears it.
- An exact masked browser URL returned by GET restores only that same stored
  field, byte-for-byte, preserving signed URL encodings and repeated parameters.
  Missing or stale placeholders fail with HTTP 400 before config is saved.
- Editing a masked URL's destination or any other component is rejected; submit
  a complete unmasked replacement URL (or clear it) instead. This deliberately
  prevents moving retained credentials to a different scheme, host, port, path,
  or query. Partial replacement of masked URL components is not supported.
- Embedding API key placeholders must match an existing stored secret. Provider
  and base URL omissions retain the existing destination. Changing either while
  retaining a key requires an explicit new key or explicit clear; preserved keys
  are never silently forwarded to a different destination.

These protections cover the API view and updates, not encryption of browser
URLs in `config.yaml`. Channels remain ignored by this PUT endpoint and keep
their dedicated administration path.

### MCP Server Config

MCP servers have their own configuration section with a consistent structure:

```yaml
mcp_servers:
  server_name:
    command: "executable"
    args: ["arg1", "arg2"]
    env:
      KEY: "value"
    sandbox: true              # Run inside container (optional)
```

The `standard_servers.go` file defines well-known MCP servers with their standard configurations, used by the MCP Store for one-click installation.

### Agent Config (Flow YAML)

Flow definitions use a separate schema defined in `yaml_loader.go`:

```yaml
description: "Flow description"
type: ""                       # "", "drill", "drill_suite"
template: ""                   # Sandbox template (optional)
nodes:
  - name: step_name
    type: llm | tool | input
    prompt: "..."
    tools: true
    output_model:
      key: "description"
flow:
  - from: START
    to: step_name
  - from: step_name
    to: END
```

See the [Flow System](flows.md) document for full details.

## Key Files

| File | Purpose |
|---|---|
| `pkg/config/app_config.go` | AppConfig struct, LoadAppConfig, defaults, validation |
| `pkg/config/yaml_loader.go` | AgentConfig struct (flows), Node, FlowItem, Edge definitions |
| `pkg/config/mcp_config.go` | MCP server configuration parsing |
| `pkg/config/standard_servers.go` | Well-known MCP server definitions |
| `pkg/config/provider_env.go` | Provider credential resolution and env var setup |

## Interactions

- **Daemon**: Loads config at startup. Hot-reloads channel changes.
- **Credentials**: Provider keys resolved from credential store. MCP env vars can reference credentials via `{{CREDENTIAL:name:field}}` (resolved at MCP process start).
- **Agent Engine**: Custom prompt, auto_approve, debug_mode, identity all feed into the agent.
- **Sandbox**: Sandbox section configures container limits, networking, pruning.
- **Channels**: Channel section configures Telegram and email adapters.
- **Scheduler**: Jobs section defines scheduled tasks.
- **MCP**: MCP servers section configures external tool servers.
- **Memory**: Embedding provider choice configured here.
