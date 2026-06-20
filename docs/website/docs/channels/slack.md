# Slack

The Slack channel integrates Astonish into your Slack workspace, enabling AI agent interactions in channels, DMs, and threads.

## Setup

### 1. Create a Slack App

1. Go to [api.slack.com/apps](https://api.slack.com/apps) and click **Create New App**
2. Choose **From scratch**, name it, and select your workspace
3. Under **OAuth & Permissions**, add these bot token scopes:
   - `chat:write`
   - `app_mentions:read`
   - `im:history`
   - `im:read`
   - `im:write`
4. Install the app to your workspace and copy the **Bot User OAuth Token**
5. Under **Event Subscriptions**, enable events and subscribe to:
   - `message.im`
   - `app_mention`

### 2. Configure Astonish

```yaml
channels:
  slack:
    bot_token: "xoxb-..."
    app_token: "xapp-..."  # For Socket Mode
    signing_secret: "abc123..."
```

### 3. Start the Channel

```bash
astonish daemon start
```

## Configuration Options

| Option | Description | Default |
|--------|-------------|---------|
| `bot_token` | Bot User OAuth Token (`xoxb-...`) | Required |
| `app_token` | App-Level Token for Socket Mode (`xapp-...`) | Required |
| `signing_secret` | Request signing secret | Required |

## Interaction Patterns

- **Direct Message** — Send a DM to the bot for a private conversation
- **Mention** — `@Astonish <message>` in any channel the bot is invited to
- **Thread** — Replies within a thread maintain session context

## Cloud Deployment

### Workspace-to-Org Mapping

In cloud deployments, Slack workspaces are mapped to organizations:

```bash
astonish platform org link-slack --org acme-corp --workspace-id T01ABC123
```

All messages from a linked workspace are automatically routed to the corresponding organization. Users in multiple workspaces are routed based on which workspace the message originates from.

### Team Routing

Within an org, Slack channels can be mapped to teams:

```bash
astonish platform team link-slack --team backend --channel-id C01XYZ789
```

Users can also switch context with slash commands:

- `/astonish org <name>` — Switch organization
- `/astonish team <name>` — Switch team

### Access Control

In cloud deployments, access is governed by the platform's org membership rather than a static allowlist. Any user in a linked workspace who is also a member of the mapped organization can interact with the bot.
