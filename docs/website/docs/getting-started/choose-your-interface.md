# Choose Your Interface

Astonish provides multiple interfaces for different workflows and environments. All interfaces connect to the same agent engine and share sessions, memory, and flows.

## Interfaces

### Studio (Web UI)

The full visual interface running at `http://localhost:9393`. Includes the chat interface, visual flow designer, apps tab for generative UI, settings management, and real-time agent execution display with token tracking. Studio is served automatically by the daemon — just open `http://localhost:9393` in your browser.

Best for: flow design, generative UI, managing apps and settings, visual execution monitoring.

<!-- IMAGE: Studio interface showing chat panel, flow designer, and apps tab -->

### CLI

A rich terminal chat interface with colors, markdown rendering, and interactive elements. Supports all agent capabilities including tool use, memory, and flow execution.

```bash
astonish chat                           # New session
astonish chat -p openai -m gpt-4o      # Specific provider/model
astonish chat --resume                  # Resume last session
```

Best for: quick interactions, scripting, developers who prefer the terminal.

### Remote CLI

A rich terminal client for connecting to a cloud deployment from anywhere. Authenticates via password or SSO, then provides the full CLI experience against the remote platform.

```bash
astonish login https://platform.yourcompany.com
astonish chat
astonish flows list
astonish status
```

Best for: team members accessing the shared platform remotely, CI/CD integration.

### Telegram

Bot integration for mobile and desktop access. Supports database-backed allowlists and dynamic per-message routing in cloud deployments. Switch org and team context with in-channel commands.

Best for: quick questions on mobile, notifications, async interactions.

### Email

Send messages to the agent via email. Supports plus-addressing for per-org routing (`bot+orgname@domain.com`). Responses are delivered back to the sender.

Best for: async workflows, forwarding content for processing, users who prefer email.

### Slack

Workspace integration with team-scoped routing. Messages route to the correct org and team context based on the Slack workspace and channel configuration.

Best for: team collaboration, integrating agent responses into existing Slack workflows.

## Comparison

| Interface | Deployment | Real-time | Visual | Mobile |
|-----------|-----------|-----------|--------|--------|
| Studio | Local / Cloud | Yes | Yes | No |
| CLI | Local / Cloud | Yes | No | No |
| Remote CLI | Cloud only | Yes | No | No |
| Telegram | Local / Cloud | Yes | No | Yes |
| Email | Local / Cloud | No (async) | No | Yes |
| Slack | Cloud only | Yes | No | Yes |

## Running Multiple Interfaces

Interfaces are not mutually exclusive. You can run Studio for visual work while using the CLI for quick tasks, and have Telegram configured for mobile access. All interfaces share the same sessions, memory, and platform context.

The daemon (`astonish daemon start`) must be running for channel integrations (Telegram, Email, Slack) and Studio to function.