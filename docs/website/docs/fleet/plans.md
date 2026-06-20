# Fleet Plans

A plan is a mission instance — a running execution of a [template](./templates.md) with a specific objective. Plans track progress from inception to completion and coordinate agent activity toward a goal.

## Creating a Plan

```bash
astonish fleet plan create \
  --template full-stack-team \
  --objective "Implement user authentication with OAuth2 and session management"
```

Or from Studio: navigate to the Fleet tab and click **New Plan**, then select a template and enter your objective.

## Plan Lifecycle

Plans progress through defined stages:

```
┌──────────┐    ┌────────┐    ┌──────────┐
│ Planning │───▶│ Active │───▶│ Complete │
└──────────┘    └────────┘    └──────────┘
                     │
                     ▼
                ┌─────────┐
                │ Paused  │
                └─────────┘
```

| Stage | Description |
|-------|-------------|
| **Planning** | Hub agent analyzes the objective and creates a task breakdown |
| **Active** | Spoke agents are executing tasks, hub is coordinating |
| **Paused** | Execution halted (manual pause or awaiting human input) |
| **Complete** | All tasks finished, hub has synthesized final output |

## Progress Tracking

Plans maintain a task graph with dependencies and status:

```bash
astonish fleet plan status <plan-id>
```

Output shows each task, its assignee, and current state:

```
Plan: impl-auth-7f3a (Active)
Objective: Implement user authentication with OAuth2

Tasks:
  ✓ Design auth database schema          [backend]
  ✓ Implement OAuth2 provider integration [backend]
  ● Build login/signup UI components      [frontend]   ← in progress
  ○ Add session middleware                [backend]
  ○ Deploy auth service                   [devops]
```

## GitHub Issues Integration

Plans can sync tasks to GitHub Issues for external visibility and tracking:

```yaml
# In template or plan config
github:
  repo: "acme-corp/main-app"
  sync_issues: true
  labels: ["fleet", "ai-generated"]
```

When enabled:
- Each task created by the hub becomes a GitHub Issue
- Status updates are reflected on the issue (comments, label changes)
- Closing an issue in GitHub signals completion to the fleet

## Managing Plans

```bash
# List active plans
astonish fleet plan list

# Pause a plan
astonish fleet plan pause <plan-id>

# Resume a paused plan
astonish fleet plan resume <plan-id>

# View plan details
astonish fleet plan show <plan-id>
```
