---
title: "Fleet Overview"
description: "Autonomous multi-agent teams that collaborate on complex missions"
---

Fleet is Astonish's multi-agent system. Instead of relying on a single agent, Fleet lets you define teams of specialized AI agents that collaborate on complex tasks — each with its own role, tools, and workspace.

## Architecture

Fleet uses an event-driven, hub-and-spoke message routing architecture:

- A **central router** receives messages from all agents and routes them to the appropriate recipient.
- Agents communicate through **pairwise conversation threads** — dedicated channels between each pair of agents that needs to interact.
- Each agent has its own **session**, **tools**, and optional **workspace**, keeping concerns separated.

## When to Use Fleet

**Single agent** is the right choice for most tasks — one conversation, one agent, full tool access. Use it whenever the work fits naturally into a single workflow.

**Fleet** is the right choice when:

- A project requires multiple specialized roles (e.g., developer + QA + PM).
- You need parallel workstreams running simultaneously.
- The mission is long-running and benefits from structured coordination.

## Key Concepts

| Concept | Description |
|---|---|
| **Template** | Defines the team composition — agent roles, models, and capabilities. |
| **Plan** | A configured instance of a template for a specific mission, with credentials, channels, and settings. |
| **Session** | A running execution of a plan, with real-time agent communication. |
| **Thread** | A pairwise conversation between two agents within a session. |

## Triggering Fleet Plans

Fleet plans can be started from multiple entry points:

- **Studio chat** — `/fleet` or `/fleet-plan` slash commands.
- **Telegram** — `/fleet` or `/fleet_plan` commands.
- **CLI** — `astonish fleet activate`.
- **External sources** — automated polling (e.g., GitHub Issues).
