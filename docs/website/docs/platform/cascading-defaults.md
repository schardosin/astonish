# Cascading Defaults

Configuration in Astonish cascades through **platform → org → team → personal**, with optional **per-session** and **per-app** model pins on top. Each level can override settings from the level above, and the most specific value wins.

## Resolution Order

When the platform resolves a configuration value, it checks from most specific to least specific:

```
Session / App pin  →  Personal  →  Team  →  Org  →  Platform
      (wins)                                          (fallback)
```

For provider/model selection specifically:

1. **Session pin** (chat) or **App pin** (generative UI) — set from the Studio Model control or CLI `-p`/`-m`
2. **Personal / user default** — when configured for the user
3. **Team** → **Org** → **Platform** — admin cascade

If a pin or personal default is empty, the next layer applies. If a pinned provider has no credential, inference falls back to the cascade default and the UI shows a soft warning; the pin is not auto-cleared.

## What Cascades

| Category | Example Settings |
|----------|-----------------|
| **Providers** | Default model, API keys, temperature, token limits |
| **MCP Servers** | Available servers, connection URLs, auth tokens |
| **Skills** | Enabled skills, skill parameters, custom skill definitions |
| **Sandboxes** | Container images, resource limits, network policies |
| **Memory** | Embedding model, search limits, tier weights |
| **Agent defaults** | System prompts, tool allowlists, max turns |

Session and app **model pins** are not admin cascade settings — they are per-resource overrides that only affect that chat session or that app.

## Example: Provider Configuration

A platform admin sets the default model for everyone. An org overrides it with their preferred provider. A team pins a specific model for consistency. A user chooses their own default. A chat session can still override for one conversation.

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

The resolved config for this user (no session pin): Anthropic Claude Sonnet, 8192 max tokens (from team), temperature 0.2 (personal override). With a session pin of `openai/gpt-4o`, that conversation uses OpenAI instead while other sessions keep the cascade.

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

Per-conversation and per-app model pins are managed in the UI, not Settings:

- **Chat** — toolbar **Model** control (see [Studio Chat](../studio/chat.md))
- **Apps** — detail header **Model** control (see [Building Apps](../generative-ui/building-apps.md))
- **CLI** — `astonish chat -p` / `-m` (see [Chat Commands](../cli/chat.md))

The local config file (`~/.config/astonish/config.yaml`) can also be edited directly:

```bash
astonish config edit    # Opens config.yaml in your editor
astonish config show    # Prints current config file contents
```

## How Resolution Works Internally

When the agent runs a turn, the platform merges configuration from the admin cascade, then applies the user default and any session/app pin. The merge strategy depends on the setting type:

- **Scalar values** (model name, temperature): most specific wins
- **Lists** (MCP servers, skills): merged additively from all levels
- **Maps** (provider settings): deep-merged with most specific keys winning

Admin cascade values are stable for the deployment; session and app pins can change without affecting other users.

## Next Steps

- [Administration](./administration) — managing platform and org configuration
- [Organizations & Teams](./organizations-and-teams) — the hierarchy that drives cascading
- [AI Providers](../configuration/providers.md) — provider setup and Studio model controls
