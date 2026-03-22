---
title: "Memory & Knowledge"
description: "How Astonish learns and remembers across sessions"
---

Astonish has a persistent memory system that lets the agent remember facts, preferences, and project details across sessions. It is powered by a local vector store ([chromem-go](https://github.com/philippgille/chromem-go)) with local embeddings — no external API is needed.

## How it works

The agent automatically retrieves relevant knowledge before responding. If you have told it something before, it remembers. No manual searching is needed on your part.

Before each response, Astonish searches memory for content relevant to your current question and injects matching context into the system prompt.

## Memory tools

The agent has three tools for working with memory:

| Tool | Description |
|---|---|
| `memory_save` | Store facts, preferences, and infrastructure details. Content is organized by category headings in markdown files. |
| `memory_search` | Semantic search across all indexed memory. Returns scored snippets with file references. |
| `memory_get` | Read specific lines from a memory file for full context. |

## Memory files

Memory files are stored in `~/.config/astonish/memory/` as markdown files.

- **MEMORY.md** — the default file for core identity and preferences.
- **Topic-specific files** — knowledge about particular subjects goes in separate files (e.g., `infra/portainer.md`, `projects/myapp.md`).
- **SELF.md** — the agent's self-awareness file containing its identity, preferences, and communication style. Auto-indexed.
- **INSTRUCTIONS.md** — per-project instructions. Place this in a project directory and Astonish reads it when working in that directory.

## CLI commands

Search memory from the terminal:

```bash
astonish memory search <query>
```

List memory files and chunk counts:

```bash
astonish memory list
```

Show memory system status:

```bash
astonish memory status
```

Force re-index all memory files:

```bash
astonish memory reindex
```

## Configuration

These settings can be adjusted through **Studio Settings > Memory**, or in `config.yaml`:

```yaml
memory:
  enabled: true
  memory_dir: ~/.config/astonish/memory
  vector_dir: ~/.config/astonish/vectors
  embedding:
    # provider and model settings
  chunking:
    # chunk size and overlap params
  search:
    max_results: 10
    min_score: 0.5
```
