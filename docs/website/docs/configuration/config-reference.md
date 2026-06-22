# Config Reference

Astonish configuration lives in two places depending on deployment mode:

- **Personal mode** (local/SQLite): Everything is in `~/.config/astonish/config.yaml` and the local credential store
- **Platform mode** (cloud/PostgreSQL): Infrastructure settings are in `config.yaml` (deployed via Helm ConfigMap), while tenant-specific settings (providers, models, tools) are stored in the database and managed through Studio Settings

## Configuration Tiers (Platform Mode)

In platform mode, tenant-specific settings cascade through three database-managed tiers:

```
Platform → Org → Team
(lowest priority)      (highest priority)
```

Each tier can add or override settings from the tier above:

| Tier | Managed By | Scope |
|------|-----------|-------|
| **Platform** | Platform superadmin | All orgs and teams |
| **Org** | Org admin/owner | All teams in the organization |
| **Team** | Team admin/owner | Single team |

### What Lives in the Database (Platform Mode)

| Setting | Cascade Tiers | Description |
|---------|--------------|-------------|
| Providers (API keys, endpoints) | Platform → Org → Team | AI provider configurations |
| Default provider/model | Platform → Org → Team | Which provider and model to use by default |
| Web search/extract tools | Team only | MCP servers for web search |
| Context length | Team only | Max context window size |
| Web servers (Tavily, etc.) | Team only | Web tool API keys |
| Memory provider/model | Team only | Embedding configuration |
| Sandbox template | Team only | Container template name |
| Disabled tools | Team only | Tools blocked for this team |
| MCP servers | Platform → Org → Team | External tool servers |
| Secrets (API keys, tokens) | All tiers (encrypted) | Encrypted with master KEK |

### What Lives in `config.yaml` (Shared Infrastructure)

These settings are shared across all tenants and set at deploy time:

| Setting | Description |
|---------|-------------|
| `storage.backend` | Database backend (`postgres` in platform mode) |
| `storage.postgres.*` | Connection pool settings |
| `storage.auth.*` | Authentication mode, token TTLs, OIDC config |
| `sandbox.*` | Backend type, resource limits, K8s/OpenShell settings |
| `daemon.*` | Port, log directory, Studio auth |
| `chat.*` | System prompt, max tool calls, auto-approve |
| `sessions.*` | Compaction, cleanup settings |
| `memory.*` | RAG embedding, chunking, search defaults |
| `browser.*` | Headless mode, viewport, proxy |
| `sub_agents.*` | Delegation depth, concurrency, timeout |
| `skills.*` | Skills system configuration |
| `scheduler.*` | Job scheduler enabled/disabled |
| `security.*` | Secret scanner settings |

## File Location

```
~/.config/astonish/config.yaml
```

In Kubernetes deployments, this file is rendered from the Helm chart ConfigMap and mounted into the pod.

## Personal Mode: Full YAML Structure

In personal/local mode, **all** configuration lives in `config.yaml`:

```yaml
# General settings
general:
  default_provider: "anthropic"
  default_model: "claude-sonnet-4-20250514"
  web_search_tool: ""          # MCP server name for web search
  web_extract_tool: ""         # MCP server name for content extraction
  context_length: 200000       # Max context window size
  timezone: ""                 # e.g., "America/New_York"

# AI Providers (map of instance names to config)
providers:
  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"
  openai:
    api_key: "${OPENAI_API_KEY}"
  ollama:
    base_url: "http://localhost:11434"
  # For custom instance names, specify type explicitly:
  my-custom-provider:
    type: "openai"
    api_key: "${CUSTOM_KEY}"
    base_url: "https://custom-endpoint.example.com/v1"

# Chat behavior
chat:
  system_prompt: ""            # Custom system prompt text
  max_tool_calls: 0            # Max tool calls per turn (0 = unlimited)
  max_tools: 0                 # Max tools exposed to model (0 = all)
  auto_approve: false          # Auto-approve tool executions
  workspace_dir: ""            # Default working directory
  flow_save_dir: ""            # Where distilled flows are saved

# Session management
sessions:
  storage: "file"              # file | sqlite
  base_dir: ""                 # Session storage directory
  compaction:
    enabled: true
    threshold: 0.8             # Context usage threshold to trigger compaction
    preserve_recent: 4         # Number of recent messages to preserve
  cleanup:
    max_age_days: 5            # Auto-delete sessions older than this

# Semantic memory (RAG)
memory:
  enabled: true
  memory_dir: ""               # Directory for memory markdown files
  vector_dir: ""               # Directory for vector index
  embedding:
    provider: "auto"           # auto | openai | ollama | openai-compat
    model: ""
    base_url: ""
    api_key: ""
  chunking:
    max_chars: 1600
    overlap: 320
  search:
    max_results: 6
    min_score: 0.35
  sync:
    watch: true                # Watch for file changes
    debounce_ms: 1500

# Storage backend
storage:
  backend: "sqlite"            # file | sqlite | postgres
  sqlite:
    data_dir: ""               # Default: ~/.local/share/astonish
  postgres:
    platform_dsn: ""           # PostgreSQL connection string
    instance_suffix: ""        # Database name suffix
    max_open_conns: 25
    max_idle_conns: 5
    conn_max_lifetime_minutes: 30
  auth:
    mode: "builtin"            # builtin | oidc
    jwt_secret: ""
    access_token_ttl_minutes: 15
    refresh_token_ttl_days: 90
    allow_registration: true
    require_email_verification: true
    default_org_name: "Default Organization"
    default_org_slug: "default"
    oidc:
      issuer_url: ""
      client_id: ""
      client_secret: ""
      redirect_url: ""
      scopes: ["openid", "profile", "email"]

# Daemon (Studio web server)
daemon:
  port: 9393
  log_dir: ""                  # Default: ~/.config/astonish/logs/
  auth:
    disabled: false
    session_ttl_days: 90

# Communication channels
channels:
  enabled: false
  telegram:
    enabled: false
    bot_token: ""
    allow_from: []             # Allowed user IDs
  email:
    enabled: false
    provider: "imap"
    imap_server: ""
    smtp_server: ""
    address: ""
    username: ""
    password: ""
    poll_interval: 30
    allow_from: []
  slack:
    enabled: false
    mode: "socket"
    bot_token: ""
    app_token: ""

# Job scheduler
scheduler:
  enabled: true

# Browser automation
browser:
  headless: false
  viewport_width: 1280
  viewport_height: 720
  no_sandbox: null
  chrome_path: ""              # Custom Chromium binary
  user_data_dir: ""
  navigation_timeout: 30       # Seconds
  proxy: ""
  remote_cdp_url: ""           # Connect to existing Chrome instance
  fingerprint_seed: ""
  fingerprint_platform: ""
  handoff_bind_address: "127.0.0.1"
  handoff_port: 9222

# Sub-agent delegation
sub_agents:
  enabled: true
  max_depth: 2                 # Maximum delegation depth
  max_concurrent: 5            # Max parallel sub-agents
  task_timeout_sec: 300        # Per-task timeout

# Skills system
skills:
  enabled: true
  user_dir: ""                 # Custom skills directory
  extra_dirs: []               # Additional skill directories
  allowlist: []                # Restrict to specific skills

# OpenCode agent
opencode:
  model: ""                    # Model override for OpenCode delegate

# Agent identity (for web registrations)
agent_identity:
  name: ""
  username: ""
  email: ""
  bio: ""

# Container sandbox
sandbox:
  enabled: false
  privileged: false
  backend: "incus"             # incus | k8s | openshell
  network: ""
  limits:
    memory: "4GB"
    cpu: 2
    processes: 500
    requests:
      cpu_millis: 100
      memory_mib: 256
  prune:
    orphan_check_hours: 0
    idle_timeout_minutes: 10
  kubernetes:                  # Only for backend: k8s
    namespace: "astonish-sandboxes"
    sandbox_image: "schardosin/astonish-sandbox-base:latest"
    overlay_mode: "fuse"       # fuse | kernel | auto
    layers_pvc_name: "astonish-layers"
    uppers_pvc_name: "astonish-uppers"
  openshell:                   # Only for backend: openshell
    gateway_addr: ""
    gateway_tls: true
    sandbox_image: "schardosin/astonish-sandbox-openshell:latest"
    network_policy:
      presets: ["default"]
      extra_endpoints: []

# Security
security:
  secret_scanner:
    enabled: true
    entropy_threshold: 4.0
    min_token_length: 16
```

## Platform Mode: Helm ConfigMap

In Kubernetes deployments, only infrastructure settings go into the ConfigMap. Provider and tenant settings are managed via Studio Settings (stored in the database):

```yaml
# What the Helm chart renders into config.yaml
storage:
  backend: "postgres"
  postgres:
    instance_suffix: ""
    max_open_conns: 25
    max_idle_conns: 5
    conn_max_lifetime_minutes: 30
  auth:
    mode: "builtin"            # or "oidc"
    access_token_ttl_minutes: 15
    refresh_token_ttl_days: 90
    allow_registration: true

sandbox:
  backend: "k8s"               # or "openshell"
  limits:
    memory: "2GB"
    cpu: 2
    processes: 500
  kubernetes:
    namespace: "astonish-sandbox"
    sandbox_image: "schardosin/astonish-sandbox-base:latest"
    overlay_mode: "fuse"
```

Secrets (master key, JWT secret, platform DSN) are injected as environment variables from Kubernetes Secrets — never in the ConfigMap.

## Environment Variable Substitution

Any value in `config.yaml` can reference environment variables using `${VAR_NAME}` syntax:

```yaml
providers:
  openai:
    api_key: "${OPENAI_API_KEY}"
```

## Minimal Local Config

A working local setup needs only a provider. The `astonish setup` wizard creates this automatically:

```yaml
general:
  default_provider: "anthropic"
  default_model: "claude-sonnet-4-20250514"

providers:
  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"
```

All other settings use sensible defaults (SQLite storage, memory enabled, daemon on port 9393).

## Managing Settings

| Mode | How to Configure |
|------|-----------------|
| **Personal** | Edit `~/.config/astonish/config.yaml` directly, or use `astonish setup` |
| **Platform (infrastructure)** | Helm values → ConfigMap (deploy-time) |
| **Platform (providers/tools)** | Studio Settings UI → stored in database |
| **Platform (secrets)** | Studio Settings UI → encrypted in database |

See [Providers](./providers.md) for supported AI backends and [MCP Servers](./mcp-servers.md) for tool extension.
