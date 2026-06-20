# Fleet Overview

Fleet is Astonish's multi-agent coordination system. It enables teams of specialized AI agents to collaborate on complex, multi-step projects through structured communication and task management.

## When to Use Fleet

Fleet is designed for tasks that exceed what a single agent can accomplish in one session:

- **Multi-domain projects** — Tasks requiring expertise in backend, frontend, infrastructure, and documentation
- **Long-running missions** — Projects spanning multiple steps with dependencies
- **Parallel workstreams** — Tasks that can be split and worked on concurrently by specialists
- **Supervised execution** — Projects requiring human checkpoints between phases

For simpler tasks, a single agent session (via Chat, CLI, or a Channel) is more appropriate.

## Architecture: Hub and Spoke

Fleet uses a hub-and-spoke message routing topology:

```
                 ┌──────────┐
                 │   Hub    │
                 │  Agent   │
                 └────┬─────┘
           ┌──────────┼──────────┐
           │          │          │
     ┌─────▼──┐ ┌────▼───┐ ┌───▼─────┐
     │ Spoke  │ │ Spoke  │ │  Spoke  │
     │Backend │ │Frontend│ │  DevOps │
     └────────┘ └────────┘ └─────────┘
```

- **Hub Agent** — Coordinates the mission. Breaks down objectives into tasks, assigns work to spokes, synthesizes results, and reports progress.
- **Spoke Agents** — Specialists that execute assigned tasks. Each spoke has a focused role, system prompt, and toolset optimized for its domain.

All inter-agent communication flows through the hub. Spokes do not communicate directly with each other — this keeps coordination predictable and auditable.

## Key Concepts

| Concept | Description |
|---------|-------------|
| [Template](./templates.md) | YAML definition of an agent team's composition and roles |
| [Plan](./plans.md) | A mission instance created from a template with a specific objective |
| [Session](./sessions-threads.md) | A pairwise communication channel between hub and spoke |
| Thread | An isolated conversation within a session |

## Lifecycle

1. **Define** — Create a [template](./templates.md) describing your agent team
2. **Launch** — Create a [plan](./plans.md) from the template with a mission objective
3. **Coordinate** — The hub agent breaks down the mission and delegates to spokes
4. **Execute** — Spoke agents work on their tasks, reporting back to the hub
5. **Complete** — The hub synthesizes results and marks the plan as complete

## Integration with GitHub Issues

Fleet integrates with GitHub Issues for task tracking. The hub agent can create issues for each task, assign them to spokes, and update status as work progresses. See [Plans](./plans.md) for details.

## Fleet in Studio

The [Studio UI](./studio.md) provides a visual dashboard for managing fleet operations — viewing plans, monitoring agent status, and inspecting message threads in real time.
