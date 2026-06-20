# Config File Reference

Astonish uses a YAML configuration file that controls agent behavior, provider selection, storage, authentication, and runtime options.

## File Location

Configuration is stored at `~/.config/astonish/config.yaml`. In cloud deployments with PostgreSQL, settings cascade from platform → org → team → personal, with lower levels overriding higher ones.

## Full Structure

```yaml
# General
name: "my-agent"
model: "claude-sonnet-4-20250514"
temperature: 0.7
max_tokens: 8192
system_prompt_file: "~/.config/astonish/system.md"

# Providers
providers:
  - name: anthropic
    api_key: "${ANTHROPIC_API_KEY}"
    default: true
  - name: openai
    api_key: "${OPENAI_API_KEY}"
  - name: ollama
    base_url: "http://localhost:11434"

# Memory
memory:
  auto_retrieve: true
  max_results: 10
  embedding_model: "text-embedding-3-small"

# Storage
storage:
  backend: "sqlite"              # sqlite | postgres
  sqlite:
    data_dir: "~/.local/share/astonish"
  postgres:
    dsn: "${ASTONISH_DSN}"
    max_connections: 20

# Auth (cloud deployments)
auth:
  provider: "oidc"
  issuer_url: "https://auth.example.com"
  client_id: "astonish"
  scopes: ["openid", "profile"]

# Daemon
daemon:
  enabled: false
  port: 9393
  host: "127.0.0.1"
  auto_start: true

# Browser
browser:
  headless: true
  stealth: true
  timeout: 30
  viewport:
    width: 1280
    height: 720
  user_data_dir: ""

# Sandbox
sandbox:
  enabled: false
  runtime: "docker"            # docker | firecracker
  image: "astonish-sandbox:latest"
  network: false
  timeout: 300
```

## Cloud Deployment Config

In cloud deployments, administrators define base configuration at the platform and org levels. Users see a merged view:

```yaml
# Platform-level (set by admin)
platform:
  enforce_providers: true
  allowed_models:
    - "claude-sonnet-4-20250514"
    - "gpt-4o"
  require_confirmation:
    - "shell_command"
    - "write_file"

# Org-level override
org:
  providers:
    - name: sap-ai-core
      base_url: "https://api.ai.sap.com"
      default: true

# Team-level override
team:
  sandbox:
    enabled: true
    runtime: "docker"
```

## Environment Variable Substitution

Any value can reference environment variables using `${VAR_NAME}` syntax. This keeps secrets out of the config file:

```yaml
providers:
  - name: openai
    api_key: "${OPENAI_API_KEY}"
```

## Minimal Local Config

A working local setup needs only a provider:

```yaml
providers:
  - name: anthropic
    api_key: "${ANTHROPIC_API_KEY}"
    default: true
```

All other settings use sensible defaults (SQLite storage, auto memory retrieval, daemon on port 9393). See [Providers](./providers.md) for supported backends and [MCP Servers](./mcp-servers.md) for tool extension.
