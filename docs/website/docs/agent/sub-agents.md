# Sub-agents

The `delegate_tasks` tool spawns parallel child agents that work independently on subtasks. This enables the primary agent to break complex work into concurrent streams.

## How Delegation Works

1. The primary agent identifies subtasks suitable for parallel execution
2. It calls `delegate_tasks` with a list of task descriptions
3. Up to 10 child agents spawn, each with its own isolated session
4. Children execute independently, using tools to complete their subtask
5. Results return to the primary agent for synthesis

```
Primary Agent
├── Child 1: "Research competitor pricing"
├── Child 2: "Analyze our usage metrics"
└── Child 3: "Draft pricing recommendations"
```

## Isolated Sessions

Each child agent operates in its own session context:

- Separate conversation history
- Independent tool execution
- No cross-talk between children
- Results collected only when the child completes

## Filtered Tool Access

Child agents receive a filtered subset of available tools. The primary agent can specify which tools each child may use:

```
delegate_tasks:
  - task: "Find all TODO comments in the codebase"
    tools: ["grep_search", "read_file", "file_tree"]
  - task: "Run the test suite and report failures"
    tools: ["shell_command", "read_file"]
```

If no tool filter is specified, children inherit the primary agent's full tool set (minus `delegate_tasks` itself to prevent recursive spawning).

## When to Use Delegation

Good use cases:
- Researching multiple topics simultaneously
- Running independent analysis tasks
- Processing multiple files in parallel
- Gathering information from different sources

Poor use cases:
- Tasks with sequential dependencies
- Work requiring shared state between subtasks
- Simple tasks faster done inline

## Limits

- Maximum 10 concurrent child agents
- Each child has its own token budget
- Children cannot spawn further children (single-level delegation)
- Timeout applies per child (configurable, default 5 minutes)

See [Chat](./chat.md) for the primary agent loop and [Tools Overview](./tools/index.md) for available tools.
