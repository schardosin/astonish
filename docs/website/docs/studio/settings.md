# Settings

The Settings panel is accessible from the gear icon in Studio's navigation. It provides configuration for all Astonish subsystems.

## Providers

Configure AI model providers:

| Setting | Description |
|---------|-------------|
| Provider name | OpenAI, Anthropic, Google, Azure, etc. |
| API key | Authentication credential |
| Base URL | Custom endpoint (for proxies or self-hosted models) |
| Default model | Model to use when none is specified |

Multiple providers can be active simultaneously. The model selector in Chat lets you switch between them.

## MCP Servers

Manage Model Context Protocol servers that extend agent capabilities:

- **Add server** — Specify command, args, and environment variables
- **Enable/disable** — Toggle servers without removing config
- **Tool list** — View tools provided by each server
- **Refresh** — Reconnect and reload tool definitions

## Credentials

The credential store manages sensitive values (API keys, tokens, passwords) used by tools and integrations:

- Credentials are encrypted at rest using [envelope encryption](../security/)
- Add, edit, or remove credentials from this panel
- Reference credentials in flows and tool configs by name

## Browser

Settings for browser automation tools:

| Setting | Description |
|---------|-------------|
| Headless | Run browser without visible window |
| Timeout | Maximum time for browser operations |
| Viewport | Default viewport dimensions |

## Memory

Configure the [three-tier memory](../agent/) system:

- **Personal memory** — Private to the user
- **Team memory** — Shared within a team (cloud deployments)
- **Org memory** — Shared across the organization (cloud deployments)

Each tier can be enabled or disabled, and you can view/edit stored memories.

## Daemon

Monitor and control the background daemon:

- **Status** — Running, stopped, or errored
- **Start/Stop** — Control daemon from the UI
- **Logs** — View recent daemon activity
- **Channels** — Status of each active channel listener

## Cloud Deployment: Admin Panels

In cloud deployments, additional panels appear for users with admin roles:

### Team Admin

- Invite or remove team members
- Manage team-level settings and defaults
- View team usage metrics

### Org Admin

- Manage organization membership
- Configure org-wide policies (allowed models, tool restrictions)
- View aggregated usage across teams
- Manage billing and quotas
