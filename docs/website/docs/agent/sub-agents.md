# Sub-agents

The `delegate_tasks` tool spawns parallel child agents that work independently on subtasks. This enables the primary agent to break complex work into concurrent streams, each with its own isolated session and filtered tool access.

## How Delegation Works

1. The primary agent identifies subtasks suitable for parallel execution
2. It calls `delegate_tasks` with a list of task descriptions and tool groups
3. Up to 10 child agents spawn, each with its own isolated session
4. Children execute independently, using their assigned tools
5. Results return to the primary agent as concise summaries for synthesis

```
Primary Agent
├── Child 1: "Research competitor pricing"     [browser, web]
├── Child 2: "Analyze our usage metrics"       [core]
└── Child 3: "Draft pricing recommendations"   [core, web]
```

## Planning with `announce_plan`

For complex multi-step tasks, the agent first calls `announce_plan` to show you a structured checklist of its approach. Plan steps are automatically tracked as sub-tasks complete, giving you real-time visibility into progress.

## Isolated Sessions

Each child agent operates in its own session context:

- Separate conversation history
- Independent tool execution
- No cross-talk between children
- Only concise result summaries enter the parent's context (not raw output)
- 10-minute timeout per child (automatically retried once if making progress)

## Tool Groups

Child agents receive tools via named groups. Available groups:

| Group | Tools Included |
|-------|---------------|
| `core` | File operations, shell, search, memory, git diff, file tree |
| `browser` | Full browser automation (34 tools) |
| `web` | web_fetch, read_pdf, http_request |
| `credentials` | Save, list, remove, test, resolve credentials |
| `process` | Process read, write, list, kill |
| `scheduler` | Schedule and manage recurring jobs |
| `skill` | Skill lookup |

MCP server tools can also be assigned: `mcp:<server_name>`

## When to Use Delegation

Good use cases:
- Researching multiple topics simultaneously
- Running independent analysis tasks
- Processing multiple files in parallel
- Gathering information from different sources
- Any task producing large raw output (keeps parent context clean)

Poor use cases:
- Tasks with sequential dependencies (use separate delegation calls instead)
- Work requiring shared state between subtasks
- Simple tasks faster done inline

## Limits

- Maximum 10 concurrent child agents per delegation call
- Each child has its own token budget
- 10-minute timeout per child (retried once if making progress)
- Children have read-only memory access

See [Chat](./chat.md) for the primary agent loop and [Tools Overview](./tools/index.md) for available tools.
