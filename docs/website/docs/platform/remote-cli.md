# Remote CLI

The same `astonish` binary works both locally and against a remote platform server. After logging in, CLI commands execute in your platform context with access to shared memory, team resources, and cascading configuration.

## Logging In

```bash
# Authenticate with a platform server
astonish login https://astonish.acme.corp
```

This opens a browser for authentication (OIDC or built-in), then stores a refresh token locally. For headless environments:

```bash
# Device code flow (no browser needed)
astonish login https://astonish.acme.corp --device-code

# API token (CI/CD pipelines)
astonish login https://astonish.acme.corp --token $ASTONISH_TOKEN
```

## Checking Status

```bash
astonish status
```

```
Server:   https://astonish.acme.corp
User:     alice@acme.corp
Org:      Acme Corp (acme)
Team:     Backend (backend)
Session:  sess_4a2c (active)
Memory:   3 tiers (personal + backend + org)
```

## Selecting Org and Team

If you belong to multiple orgs or teams, switch context:

```bash
# Switch organization
astonish use org globex

# Switch team within current org
astonish use team frontend

# One-shot command in a different team context
astonish --team platform memory search "deployment runbook"
```

## Running Commands Remotely

Once logged in, most commands transparently hit the platform:

```bash
# Search across all three memory tiers
astonish memory search "connection pooling best practices"

# Start a session (stored in your personal schema, uses team/org memory)
astonish chat "Help me optimize this query"

# List team flows
astonish flow list --team

# Run a team flow
astonish flow run team-deploy-flow --env staging
```

## Offline and Local Fallback

If the platform server is unreachable, the CLI falls back to local mode automatically. You can also force local execution:

```bash
# Force local mode (no server communication)
astonish --local chat "Help me with this file"
```

Sessions created offline can be synced when connectivity returns:

```bash
astonish sync
```

## Session Management

```bash
# List your remote sessions
astonish session list

# Resume a session
astonish session resume sess_4a2c

# Switch between local and remote sessions
astonish session list --local
astonish session list --remote
```

## Configuration

Remote CLI settings are stored in `~/.config/astonish/platform.yaml`:

```yaml
server: https://astonish.acme.corp
org: acme
team: backend
auto_sync: true
offline_mode: false
```

## Next Steps

- [Platform Overview](./index) — understanding the platform architecture
- [Organizations & Teams](./organizations-and-teams) — the org/team context model
