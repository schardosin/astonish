# Cascading Defaults

Configuration in Astonish cascades through four levels: **platform → org → team → personal**. Each level can override settings from the level above, and the most specific value wins.

## Resolution Order

When the platform resolves a configuration value, it checks from most specific to least specific:

```
Personal  →  Team  →  Org  →  Platform
 (wins)                        (fallback)
```

If a user has set a value, it takes precedence. If not, the team setting applies. If the team has no override, the org default is used. The platform level provides the final fallback.

## What Cascades

| Category | Example Settings |
|----------|-----------------|
| **Providers** | Default model, API keys, temperature, token limits |
| **MCP Servers** | Available servers, connection URLs, auth tokens |
| **Skills** | Enabled skills, skill parameters, custom skill definitions |
| **Sandboxes** | Container images, resource limits, network policies |
| **Memory** | Embedding model, search limits, tier weights |
| **Agent defaults** | System prompts, tool allowlists, max turns |

## Example: Provider Configuration

A platform admin sets the default model for everyone. An org overrides it with their preferred provider. A team pins a specific model for consistency. A user chooses their own.

```yaml
# Platform level (set by platform admin)
providers:
  default: openai
  openai:
    model: gpt-4o
    max_tokens: 4096

# Org level (overrides platform)
providers:
  default: anthropic
  anthropic:
    model: claude-sonnet-4-20250514

# Team level (overrides org)
providers:
  anthropic:
    model: claude-sonnet-4-20250514
    max_tokens: 8192

# Personal level (overrides team)
providers:
  anthropic:
    temperature: 0.2
```

The resolved config for this user: Anthropic Claude Sonnet, 8192 max tokens (from team), temperature 0.2 (personal override).

## Example: MCP Servers

```yaml
# Org level — available to all teams
mcp_servers:
  - name: github
    url: https://mcp.acme.corp/github
  - name: jira
    url: https://mcp.acme.corp/jira

# Team level — adds team-specific servers
mcp_servers:
  - name: database
    url: https://mcp.acme.corp/db-backend
```

MCP server lists are **merged** (additive), not replaced. The team gets `github`, `jira`, and `database`. A user can disable a server at their level but cannot remove it from the team's available set.

## Managing Configuration

Configuration at each level is managed through **Studio** (Settings panel):

- **Platform level** — Platform Admin → Settings
- **Org level** — Org Admin → Organization Settings
- **Team level** — Team Admin → Team Settings
- **Personal level** — User → Settings (personal overrides)

The local config file (`~/.config/astonish/config.yaml`) can also be edited directly:

```bash
astonish config edit    # Opens config.yaml in your editor
astonish config show    # Prints current config file contents
```

## How Resolution Works Internally

When the agent starts a session, the platform merges configuration from all four levels. The merge strategy depends on the setting type:

- **Scalar values** (model name, temperature): most specific wins
- **Lists** (MCP servers, skills): merged additively from all levels
- **Maps** (provider settings): deep-merged with most specific keys winning

The resolved configuration is computed at session start and remains stable for the session's lifetime.

## Next Steps

- [Administration](./administration) — managing platform and org configuration
- [Organizations & Teams](./organizations-and-teams) — the hierarchy that drives cascading
