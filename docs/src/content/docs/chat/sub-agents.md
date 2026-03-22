---
title: "Sub-agents & Delegation"
description: "How the agent spawns child agents for parallel work"
---

For complex tasks with multiple independent parts, the Astonish agent can delegate work to sub-agents that run in parallel. This speeds up multi-step workflows significantly.

## How delegation works

The `delegate_tasks` tool spawns up to 10 sub-agents simultaneously. Each sub-agent receives:

- **Its own isolated session** — linked to the parent session for traceability
- **Read-only access to memory** — sub-agents can reference stored knowledge but do not write to it
- **Filtered tool access** — only the tools needed for the specific sub-task
- **A timeout** — 5 minutes by default, configurable

The agent decides autonomously when to delegate. Typically this happens when a task has 3 or more independent parts that each require multiple tool calls.

Sub-agent results are collected and synthesized by the parent agent into a unified response.

## The OpenCode sub-agent

The `opencode` tool is a specialized coding sub-agent designed for complex software engineering tasks. The parent agent can delegate implementation, refactoring, or debugging work to it.

## Viewing sub-agent work

To see sub-agent traces inlined with the parent session:

```bash
astonish sessions show <id> --recursive
```

Each sub-agent session is also accessible individually through the standard `astonish sessions show` command.

## Configuration

These settings can be adjusted through **Studio Settings > Sub-agents**, or in `config.yaml`:

```yaml
sub_agents:
  enabled: true              # enable/disable delegation (default: true)
  max_depth: 2               # maximum nesting depth (default: 2)
  max_concurrent: 5          # max parallel sub-agents (default: 5)
  task_timeout_sec: 300      # per-task timeout in seconds (default: 300)
```
