# Flows Overview

Flows are deterministic YAML workflows that capture proven agent behavior as repeatable, schedulable automation. Where chat is exploratory and open-ended, a flow is a fixed node graph executed by Astonish's agent state machine — producing the same result every time given the same inputs.

## What Is a Flow?

A flow is a directed graph of **nodes** (actions) connected by **edges** (transitions). Each node performs a single operation — calling an LLM, invoking a tool, evaluating a condition, or collecting input — and passes state to the next node in the graph.

```yaml
name: summarize-pr
description: Summarize a GitHub pull request and post to Slack
params:
  - name: pr_url
    type: string
    required: true

nodes:
  - id: fetch_pr
    type: tool
    tool: github_get_pr
    args:
      url: "{{pr_url}}"

  - id: summarize
    type: llm
    prompt: "Summarize this PR for a team standup: {{fetch_pr.output}}"

  - id: post
    type: tool
    tool: slack_post
    args:
      channel: "#engineering"
      text: "{{summarize.output}}"

edges:
  - from: fetch_pr
    to: summarize
  - from: summarize
    to: post
```

## How Flows Differ from Chat

| Aspect | Chat | Flow |
|--------|------|------|
| Execution | Non-deterministic, exploratory | Deterministic, repeatable |
| Versioning | Session transcript | YAML in git or database |
| Scheduling | Manual invocation | Cron, webhook, or event trigger |
| Sharing | Share a transcript | Share executable automation |
| Parameters | Freeform prompts | Declared typed inputs |

## The Flow Lifecycle

Flows follow a natural progression from discovery to production:

1. **Chat** — Solve a problem interactively with the agent.
2. **Distill** — Run `/distill` to extract a parameterized flow from the session. See [Flow Distillation](./distillation.md).
3. **Edit** — Refine the YAML directly or through the Studio visual editor.
4. **Schedule** — Attach a cron expression, webhook trigger, or event subscription.
5. **Share** — Publish to your team via Studio or distribute via [Taps](./taps.md).

<!-- IMAGE: diagram showing the lifecycle stages as a pipeline -->

## Storage & Multi-Tenancy

Flows are stored in the platform database with full multi-tenancy:

- Each flow belongs to a user or team.
- Flows can be private, team-shared, or published to the organization.
- Version history is maintained — roll back to any previous revision.
- Execution logs are retained for audit and debugging.

## Running a Flow

```bash
# Run from CLI
astonish flows run summarize-pr -p pr_url="https://github.com/org/repo/pull/42"
```

In Studio, navigate to the Flows tab, select a flow, click Run, and fill in the parameters visually.

Flows can also be triggered programmatically via the [REST API](../reference/), making them composable building blocks for larger automation pipelines.

## Next Steps

- [Flow Distillation](./distillation.md) — Create flows from chat sessions
- [YAML Reference](./yaml-reference.md) — Full schema documentation
- [Nodes, Edges & State](./nodes-edges-state.md) — Deep dive into flow mechanics
- [Taps & Flow Store](./taps.md) — Community flow repositories
