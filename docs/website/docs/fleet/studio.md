# Fleet in Studio

Studio provides a visual interface for managing fleet operations. The Fleet tab gives you real-time visibility into plans, agents, and their communication.

<!-- IMAGE: Fleet dashboard showing active plans with progress indicators -->

## Plan Dashboard

The Fleet tab opens to the plan dashboard, displaying:

- **Active plans** with progress bars and task counts
- **Recent activity** showing the latest agent messages
- **Quick actions** to create, pause, or resume plans

Click any plan to drill into its details.

## Agent Status

Within a plan, each agent is shown with its current state:

| Indicator | Meaning |
|-----------|---------|
| 🟢 Active | Agent is processing a task |
| 🟡 Waiting | Agent is idle, awaiting instructions |
| 🔴 Error | Agent encountered a failure |
| ⚪ Paused | Agent is paused with the plan |

The hub agent's task breakdown is displayed as an interactive checklist, updating in real time as spokes complete their work.

## Message Threads

Select any hub–spoke pair to view their conversation threads. Messages are displayed in chronological order with:

- Sender identification (hub or spoke name)
- Timestamps
- Tool calls and their results (expandable)
- File artifacts produced

## Real-Time Progress

Fleet operations stream updates via SSE (Server-Sent Events). The Studio UI updates live as:

- New tasks are created by the hub
- Spokes begin and complete work
- Messages are exchanged between agents
- Plan status transitions occur

No manual refresh is needed — the dashboard reflects the current state of all fleet operations.

## Creating Plans from Studio

1. Navigate to the **Fleet** tab
2. Click **New Plan**
3. Select a [template](./templates.md) from the dropdown
4. Enter the mission objective
5. Click **Launch**

The plan immediately begins its [lifecycle](./plans.md) and appears on the dashboard.
