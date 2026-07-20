# Scheduler & Agent Tools

Tools for scheduling future tasks, delegating to parallel sub-agents, distilling sessions into flows, and managing plans.

## Scheduling Tools

| Tool | Description | Confirmation |
|------|-------------|-------------|
| `schedule_job` | Create a scheduled job on a cron schedule | always-confirm |
| `list_scheduled_jobs` | List all scheduled jobs with status | auto-approve |
| `update_scheduled_job` | Update a job's schedule or enable/disable it | always-confirm |
| `remove_scheduled_job` | Remove a scheduled job | always-confirm |

### schedule_job

Creates a recurring task that runs on a cron schedule:

```
schedule_job:
  name: "daily-backup"
  mode: "adaptive"
  schedule: "0 2 * * *"
  scope: "personal"   # default — uses your personal credentials
  instructions: "Run database backup and upload to S3"
```

Modes:
- **routine** — Runs a saved flow with fixed parameters (deterministic)
- **adaptive** — An AI agent executes free-form instructions each run (flexible)

Scope (platform mode):
- **personal** (default) — Private to you; runs with your personal credentials (plus team fallback). Delivery is limited to you (`owner`). Prefer this when the job needs a personal OAuth token or API key.
- **team** — Shared team job; runs with team credentials only. Requires team admin. Use for service accounts and shared automation.

Do not publish a personal credential to the team just to schedule a job — use `scope: personal` instead.

The daemon must be running for scheduled jobs to execute.

## Delegation Tools

| Tool | Description | Confirmation |
|------|-------------|-------------|
| `delegate_tasks` | Spawn parallel child agents for concurrent work | always-confirm |
| `announce_plan` | Show a structured plan checklist to the user | auto-approve |

### delegate_tasks

Spawns up to 10 parallel child agents. See [Sub-agents](../sub-agents.md) for full details.

### announce_plan

Displays a structured plan to the user before starting multi-step work. Plan steps are automatically tracked as sub-tasks complete.

## Flow & Discovery Tools

| Tool | Description | Confirmation |
|------|-------------|-------------|
| `distill_flow` | Convert current session into a reusable flow | always-confirm |
| `run_flow` | Execute a saved flow | always-confirm |
| `search_flows` | Search for saved flows/workflows | auto-approve |
| `search_tools` | Discover available tools by description | auto-approve |
| `skill_lookup` | Load instructions for a CLI tool or workflow | auto-approve |
| `list_team_members` | List team members for delivery targeting | auto-approve |

### distill_flow

Converts the current conversation into a reusable flow file. Captures the sequence of tool calls, decision points, and parameters. Equivalent to the `/distill` slash command.

### search_tools

Searches for available tools by describing what you want to do. Found tools become available for the current session. Useful when the agent needs capabilities not in its default tool set.

### skill_lookup

Loads detailed instructions for a specific CLI tool or workflow (e.g., `git`, `docker`, `kubernetes`). The instructions are injected into the agent's context for the current task.

See [Skills](../skills.md) for how skills work, [Sub-agents](../sub-agents.md) for delegation patterns, and [Flows](../../flows/index.md) for flow distillation.
