---
title: "Fleet Plans"
description: "Configure and activate mission-specific fleet instances"
---

A plan is a template configured for a specific mission. It binds a team to a project, channel, and credentials.

## Plan Lifecycle

1. **Create** — Through the Studio UI wizard or the `/fleet-plan` slash command. The AI-guided wizard walks you through team selection, credential mapping, and channel configuration.
2. **Validate** — The `validate_fleet_plan` tool tests external connections (channels, credentials, artifact destinations).
3. **Save** — The `save_fleet_plan` tool persists the plan.
4. **Activate** — Start the plan. This begins polling for work on the configured channel.
5. **Execute** — Sessions are created for each incoming task.
6. **Deactivate** — Stop polling.

## Plan Configuration

| Field | Description |
|---|---|
| `base_fleet_key` | Which template to use. |
| `channel_type` | How work arrives: `chat` (manual) or `github_issues` (automated polling). |
| `channel_config` | Channel-specific settings (e.g., GitHub repo, labels to watch). |
| `channel_schedule` | Cron expression for polling non-chat channels. |
| `credentials` | Mapping of credential names for external services. |
| `artifacts` | Where to store outputs. |
| `behavior_overrides` | Per-agent instruction overrides. |
| `include_agents` | Subset of agents to include from the template. |
| `project_source` | Where the project code lives. |

## GitHub Issues Integration

When `channel_type` is `github_issues`, the plan polls a repository for new issues matching configured labels. Each matching issue becomes a fleet session, automatically assigning work to the team.

## CLI Commands

```bash
# List all plans
astonish fleet list

# View plan details
astonish fleet show <key>

# Start polling
astonish fleet activate <key>

# Stop polling
astonish fleet deactivate <key>

# Check status
astonish fleet status <key>

# Remove a plan
astonish fleet delete <key>
```
