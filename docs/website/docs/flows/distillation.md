# Flow Distillation

Distillation is the process of converting an exploratory chat session into a deterministic, parameterized flow. The `/distill` command analyzes the agent's execution trace — every tool call, LLM invocation, and decision point — and produces a reusable YAML workflow.

## How It Works

When you run `/distill`, the agent:

1. **Analyzes the execution trace** — Identifies the sequence of actions taken during the session.
2. **Detects parameters** — Finds values that varied or were user-supplied, converting them into declared parameters.
3. **Identifies decision points** — Extracts conditional logic into branching nodes.
4. **Generates the YAML** — Produces a complete flow definition with proper node types, edges, and state wiring.
5. **Validates** — Runs the flow schema validator to ensure correctness.

## Example: Distilling a PR Summary Flow

```
You:    "Get the latest PR from our repo and summarize it for the team"
Agent:  [tool: github_list_prs] Fetching PRs from org/repo...
        [tool: github_get_pr] Got PR #142: "Add rate limiting to API gateway"
        [llm] Here's a standup summary: PR #142 adds rate limiting using a
        token bucket algorithm. Key changes: new middleware in api/gateway.go,
        config options for burst/rate, integration tests.
You:    "/distill"
Agent:  [distill] Analyzing execution trace (3 steps)...

        I've created a flow from this session:

        Name: summarize-latest-pr
        Parameters:
          - repo (string, required) — extracted from "org/repo"
          - channel (string, default: "#engineering")

        Nodes: fetch_prs → get_latest → summarize → post_summary

        Saved to: ~/.config/astonish/flows/summarize-latest-pr.yaml

        Would you like to edit the flow or schedule it?
```

## The Generated Flow

```yaml
name: summarize-latest-pr
description: Fetch the latest PR and post a summary to the team
params:
  - name: repo
    type: string
    required: true
  - name: channel
    type: string
    default: "#engineering"

nodes:
  - id: fetch_prs
    type: tool
    tool: github_list_prs
    args:
      repo: "{{repo}}"
      state: open
      limit: 1

  - id: summarize
    type: llm
    prompt: |
      Summarize this PR for a team standup. Be concise.
      PR: {{fetch_prs.output}}

  - id: post_summary
    type: tool
    tool: slack_post
    args:
      channel: "{{channel}}"
      text: "{{summarize.output}}"

edges:
  - from: fetch_prs
    to: summarize
  - from: summarize
    to: post_summary
```

## Distillation Options

```bash
# Distill with a custom name
/distill --name deploy-checker

# Distill only the last N steps
/distill --steps 5

# Distill and immediately schedule
/distill --cron "0 9 * * 1-5"
```

## Team Knowledge in Cloud Deployments

In cloud deployments, distilled flows become shared team knowledge:

- Flows are automatically tagged with the originating session for traceability.
- Team members can discover flows created by others in the Flows tab.
- Popular flows surface in search results and recommendations.
- Forking a team flow creates a personal copy for customization.

This creates a flywheel: the more your team chats with Astonish, the more reusable automation accumulates in your flow library.

## Tips for Better Distillation

- **Be explicit about inputs** — The agent detects parameters more accurately when you clearly state variable values.
- **Keep sessions focused** — Distill single-purpose sessions rather than sprawling multi-topic chats.
- **Review the output** — Distillation is a starting point. Edit the YAML to add error handling or tighten prompts.
- **Test with different inputs** — Run the flow with varied parameters to verify it generalizes correctly.

## Next Steps

- [YAML Reference](./yaml-reference.md) — Understand the full flow schema
- [Nodes, Edges & State](./nodes-edges-state.md) — How nodes execute and pass data
