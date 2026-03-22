---
title: "Sessions"
description: "How Astonish persists and manages conversation history"
---

Every conversation in Astonish is a session. Each session has a unique ID and is persisted as a JSONL file so it survives restarts. You can resume any past conversation at any time.

## Managing sessions

### List sessions

```bash
astonish sessions list
```

Alias: `astonish sessions ls`

Shows all sessions with timestamps and a message preview.

### Show a session

```bash
astonish sessions show <id>
```

Display the full session trace. Session IDs can be abbreviated — Astonish matches by prefix.

| Flag | Description |
|---|---|
| `--json` | JSON output for scripting |
| `--tools-only` | Only show tool calls |
| `--verbose` | Full tool args/results without truncation |
| `--recursive` | Include sub-agent sessions inline |
| `--last 10` | Show only the last N events |

### Delete sessions

Delete a single session:

```bash
astonish sessions delete <id>
```

Delete all sessions (prompts for confirmation):

```bash
astonish sessions clear
```

## Session compaction

When a conversation grows too large for the model's context window, Astonish automatically compacts older messages into a summary. This keeps the session usable without losing important context.

These settings can also be adjusted through **Studio Settings > Sessions**. In `config.yaml`:

```yaml
sessions:
  compaction:
    enabled: true
    threshold: 0.8          # fraction of context window before compaction triggers
    preserve_recent: 4      # number of recent messages to keep intact
```

You can also trigger compaction manually with the `/compact` slash command.

## Sub-sessions

When the agent delegates tasks to sub-agents, each sub-agent gets its own session linked to the parent. Use `--recursive` when viewing the parent session to see sub-agent traces inlined.

## Storage

Sessions are stored in `~/.config/astonish/sessions/` by default. The location is configurable in your config file.
