---
title: "Scheduler & Agent Tools"
description: "Schedule tasks, delegate work, and distill conversations into flows"
---

These tools handle scheduled execution, parallel task delegation, conversation-to-flow distillation, and skill loading.

## Scheduler Tools

### schedule_job

Create a scheduled job that runs on a cron schedule.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | Yes | Descriptive job name |
| `mode` | string | Yes | `routine` or `adaptive` |
| `schedule` | string | Yes | 5-field cron expression (e.g., `0 9 * * 1-5`) |
| `timezone` | string | No | IANA timezone (e.g., `America/New_York`) |
| `flow` | string | No | Flow name (required for `routine` mode) |
| `params` | object | No | Flow parameters |
| `instructions` | string | No | AI instructions (required for `adaptive` mode) |
| `channel` | string | No | Delivery channel label |
| `target` | string | No | Target ID for delivery |
| `test_first` | bool | No | Run immediately to test before confirming the schedule |

#### Routine mode

Runs a saved flow with fixed parameters on schedule. Every execution is deterministic and repeatable. Use this when the steps are known and stable.

```
schedule_job(
  name: "daily-report",
  mode: "routine",
  schedule: "0 9 * * 1-5",
  timezone: "America/New_York",
  flow: "generate-sales-report",
  params: { period: "daily", format: "pdf" }
)
```

#### Adaptive mode

Gives the AI agent a set of instructions and lets it figure out how to accomplish the task. Each execution can differ based on current state, new data, or changed conditions.

```
schedule_job(
  name: "competitor-monitor",
  mode: "adaptive",
  schedule: "0 8 * * *",
  instructions: "Check competitor pricing pages and summarize any changes since yesterday.",
  channel: "slack",
  target: "#pricing-alerts"
)
```

### list_scheduled_jobs

List all scheduled jobs with their status, next run time, and configuration. No parameters.

### remove_scheduled_job

Remove a scheduled job by its ID or name.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `id` | string | Yes | Job ID or name |

### update_scheduled_job

Update an existing scheduled job. Enable, disable, reschedule, or rename.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `id` | string | Yes | Job ID or name |
| `enabled` | bool | No | Enable or disable the job |
| `schedule` | string | No | New cron expression |
| `timezone` | string | No | New IANA timezone |
| `name` | string | No | New job name |

## Agent Tools

### delegate_tasks

Spawn parallel sub-agents to handle independent tasks concurrently.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `tasks` | array | Yes | Array of task objects |

Each task object:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Task identifier |
| `task` | string | Yes | Task description / prompt |
| `instructions` | string | No | Additional instructions for the sub-agent |
| `tools` | string[] | No | Restrict which tools the sub-agent can use |

Each sub-agent runs in an isolated session with read-only access to the parent's memory and a filtered set of tools. Tasks time out after 5 minutes. A maximum of 10 tasks can be dispatched per call.

### distill_flow

Convert the current conversation into a reusable flow YAML file. Analyzes the tool calls from the session trace and produces a deterministic workflow that can be saved and replayed with `schedule_job` in routine mode.

No parameters. Distills from the current session.

### skill_lookup

Load a skill guide by name. The agent uses this to learn how to operate a CLI tool or service before executing commands.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | Yes | Skill name (e.g., `github`, `docker`, `git`, `aws`) |
