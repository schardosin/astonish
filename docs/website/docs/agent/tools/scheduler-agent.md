# Scheduler & Agent Tools

Four tools for scheduling future tasks, delegating to parallel sub-agents, distilling sessions into flows, and looking up skills.

## Tools

| Tool | Description | Confirmation |
|------|-------------|-------------|
| `schedule_task` | Schedule a task for future execution | always-confirm |
| `delegate_tasks` | Spawn parallel child agents | always-confirm |
| `distill_flow` | Convert current session into a reusable flow | always-confirm |
| `skill_lookup` | Search available skills by description | auto-approve |

## schedule_task

Schedules a task for later execution via the daemon:

```
schedule_task:
  description: "Run database backup and upload to S3"
  cron: "0 2 * * *"
  tools: ["shell_command", "http_request"]
```

Options:
- `cron` — Cron expression for recurring tasks
- `run_at` — ISO timestamp for one-time tasks
- `tools` — Tool allowlist for the scheduled execution
- `flow` — Optional flow file to execute

Requires the daemon to be running (`daemon.enabled: true` in config).

## delegate_tasks

Spawns up to 10 parallel child agents for concurrent work:

```
delegate_tasks:
  tasks:
    - description: "Audit authentication module for vulnerabilities"
      tools: ["read_file", "grep_search", "file_tree"]
    - description: "Check dependency versions for known CVEs"
      tools: ["shell_command", "read_file", "web_fetch"]
    - description: "Review error handling patterns"
      tools: ["read_file", "grep_search"]
```

Each child agent runs independently with its own session. Results are collected and returned to the primary agent. See [Sub-agents](../sub-agents.md) for full details.

## distill_flow

Converts the current conversation into a reusable flow file:

```
distill_flow:
  name: "deploy-staging"
  description: "Build, test, and deploy to staging environment"
  output_path: "~/.config/astonish/flows/deploy-staging.yaml"
```

The distilled flow captures the sequence of tool calls, decision points, and parameters from the session. Equivalent to the `/distill` slash command.

## skill_lookup

Searches available skills by keyword or description:

```
skill_lookup:
  query: "kubernetes pod debugging"
```

Returns matching skills with their descriptions and source (bundled, community, or custom). The agent uses this internally to decide which skills to load.

See [Skills](../skills.md) for how skills work, [Sub-agents](../sub-agents.md) for delegation patterns, and [Sessions](../sessions.md) for flow distillation context.
