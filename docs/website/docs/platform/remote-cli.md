# Remote CLI

The same `astonish` binary works both locally and against a remote platform server. After logging in, CLI commands execute in your platform context with access to shared memory, team resources, and cascading configuration.

## Logging In

```bash
# Authenticate with a platform server
astonish login https://astonish.acme.corp
```

This prompts for your email and password interactively. For SSO-enabled environments:

```bash
# SSO/OIDC login (uses device-code flow internally)
astonish login https://astonish.acme.corp --sso
```

You can also pre-select your org and team to skip interactive prompts:

```bash
astonish login https://astonish.acme.corp --org acme --team backend
```

Credentials are stored locally in `~/.config/astonish/remote.yaml`.

## Checking Status

```bash
astonish status
```

Shows your current connection info: server URL, user, org, and team.

## Selecting Org and Team

Org and team context is set during login. To switch, log out and log back in:

```bash
astonish logout
astonish login https://astonish.acme.corp --org acme --team frontend
```

If you omit `--org` and `--team`, the login flow will prompt you to select from available options interactively.

## Available Commands in Remote Mode

Once logged in, these commands work against the platform:

```bash
# Chat (session stored on the platform)
astonish chat
astonish chat --resume <session-id>

# Sessions
astonish sessions list
astonish sessions show <session-id>
astonish sessions delete <session-id>

# Flows
astonish flows list
astonish flows run <name>
astonish flows show <name>

# Team and org context
astonish team list
astonish org list
astonish status
```

## Configuration

Remote CLI settings are stored in `~/.config/astonish/remote.yaml`:

```yaml
url: https://astonish.acme.corp
org: acme
team: backend
user_email: alice@acme.corp
```

This file is created automatically by `astonish login` and removed by `astonish logout`.

## Logging Out

```bash
astonish logout
```

This removes the `remote.yaml` file and revokes the stored session.

## Next Steps

- [Platform Overview](./index) — understanding the platform architecture
- [Organizations & Teams](./organizations-and-teams) — the org/team context model
