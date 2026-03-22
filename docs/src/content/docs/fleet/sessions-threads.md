---
title: "Sessions & Threads"
description: "How fleet agents communicate and track progress"
---

When a fleet plan processes a task, it creates a session — a running instance with all configured agents active.

## Agent Communication

- Agents communicate through **pairwise threads**. Each pair of agents that needs to talk gets a dedicated conversation.
- The hub-and-spoke router uses **LLM-based message routing** to determine which agent should receive each message.
- Agents can **address specific team members** or **broadcast** to the group.

## Threads

Each thread is a conversation between exactly two agents. Threads maintain full context, so agents can reference earlier messages in the same thread.

The thread viewer in Studio shows all conversations organized by agent pair, making it straightforward to follow how work flows between team members.

## Progress Tracking

Fleet sessions track milestones as the team progresses. Session status reflects overall completion, giving you a high-level view of how far the mission has advanced.

## Session Recovery

Sessions persist as JSONL files. If the daemon restarts, active sessions can be recovered from their persisted state — no work is lost.

## Viewing Sessions

**Studio Fleet UI** — Session trace viewer with per-agent and per-thread filtering. See [Fleet in Studio](/fleet/studio-fleet/) for details.

**CLI:**

```bash
astonish sessions show <id> --recursive
```

This outputs the full trace, including all agent messages and tool calls.

## Retrying Failed Sessions

Failed sessions can be retried from the Studio UI. This re-runs the session from its last checkpoint, preserving any progress made before the failure.
