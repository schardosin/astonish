---
title: "Fleet in Studio"
description: "Managing fleet teams through the Studio web UI"
---

The Fleet section in Studio (accessible from the top navigation) provides visual management for all fleet operations.

## Template View

- Browse available fleet templates.
- View agent cards showing each team member's role, model, and capabilities.
- Expand agent details for full configuration.

## Plan Management

- **Create plans** with the AI-guided wizard, which walks through template selection, credentials, and channel configuration.
- **View plan details** with agent communication flow diagrams.
- **Activate and deactivate** plans directly from the UI.
- **Edit plan YAML** with the split-pane CodeMirror editor for direct configuration access.
- **Duplicate or delete** plans.

## Session Trace

- **Real-time view** of active sessions via SSE streaming and polling.
- **Per-agent message filtering** — isolate what a specific agent is doing.
- **Per-thread filtering** — view a specific agent-to-agent conversation.
- **Expandable tool call details** — inspect what tools were invoked and their results.
- **Failed session retry** — re-run a failed session from the UI.

## Creating a Plan from Chat

Type `/fleet-plan` in the Studio chat to start the creation wizard. The AI guides you through:

1. Selecting a template.
2. Configuring credentials for external services.
3. Setting up the communication channel.
4. Validating connections.
5. Saving the plan.

Once a plan exists, type `/fleet` in the Studio chat to launch a fleet session from it.
