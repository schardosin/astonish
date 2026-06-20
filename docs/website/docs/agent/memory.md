# Memory

Astonish provides persistent memory that survives across sessions. The agent can store, search, and retrieve knowledge automatically, building a growing context base over time.

## How Memory Works

Before each response, the agent automatically retrieves relevant memories based on the current conversation context. You can also explicitly save and search memories using tools.

Memory is distinct from session history—sessions are conversation logs, while memory is curated knowledge the agent accumulates.

## Local (SQLite)

With the SQLite backend, memory uses hybrid search combining vector similarity and full-text keyword matching:

- **Vector search** — Embeddings stored as BLOBs in SQLite, with cosine similarity computed in Go (pure Go, no C extensions required)
- **Keyword search** — FTS5 virtual tables provide BM25-ranked full-text search
- **Hybrid fusion** — Results from both methods are merged using Reciprocal Rank Fusion (RRF)

This works out of the box with zero configuration. No external vector database or extensions are needed.

```yaml
storage:
  backend: "sqlite"            # Default — can be omitted
memory:
  auto_retrieve: true
  max_results: 10
```

Performance is excellent at local scale:

| Corpus Size | Vector Search | FTS5 Search | Total Hybrid |
|-------------|---------------|-------------|--------------|
| 1,000 entries | < 1 ms | < 2 ms | < 4 ms |
| 10,000 entries | ~ 5 ms | < 5 ms | ~ 12 ms |
| 100,000 entries | ~ 50 ms | < 10 ms | ~ 65 ms |

## Cloud (PostgreSQL)

With PostgreSQL, memory uses **pgvector** for vector search and **tsvector** for keyword matching, enabling the full three-tier memory system:

| Tier | Scope | Use Case |
|------|-------|----------|
| Personal | Individual user | Personal preferences, learned patterns |
| Team | Team members | Shared project knowledge, conventions |
| Org | Organization | Company policies, architecture decisions |

The agent searches all accessible tiers and merges results by relevance with tier-specific weighting (personal 1.2x, team 1.0x, org 0.8x).

```yaml
storage:
  backend: "postgres"
  postgres:
    dsn: "${ASTONISH_DSN}"
memory:
  auto_retrieve: true
  max_results: 15
  tiers:
    - personal
    - team
    - org
```

PostgreSQL deployments use IVFFlat indexes for sublinear search performance at scale (millions of memories).

## Memory Tools

| Tool | Description |
|------|-------------|
| `memory_save` | Store a new memory with content and tags |
| `memory_search` | Semantic search across stored memories |
| `memory_get` | Retrieve a specific memory by ID |

### Saving Memory

The agent saves memories when it learns something worth retaining:

```
User: "Our API uses camelCase for JSON fields and snake_case for database columns"
Agent: [saves to memory with tags: "conventions", "api", "database"]
```

### Searching Memory

```
Agent: [memory_search query="API naming conventions"]
→ Returns: "API uses camelCase for JSON, snake_case for DB columns" (score: 0.92)
```

## Auto-Retrieval

When `auto_retrieve: true`, the agent queries memory before every response. This happens transparently—relevant context is injected into the system prompt without explicit tool calls.

Disable auto-retrieval if you want full manual control:

```yaml
memory:
  auto_retrieve: false
```

## CLI Commands

```bash
# Search memories from the command line
astonish memory search "deployment process"

# List recent memories
astonish memory list --limit 20

# Export all memories
astonish memory export --format json > memories.json

# Import memories
astonish memory import memories.json
```

## Best Practices

- Let the agent save memories organically during conversations
- Use tags for categorization (the agent does this automatically)
- In cloud deployments, save team knowledge at the team tier so colleagues benefit
- Periodically review saved memories with `astonish memory list`
- For deployments exceeding 100,000 memory entries, consider switching to PostgreSQL for sublinear search performance

See [Sessions](./sessions.md) for how session history differs from memory, and [Config Reference](../configuration/config-reference.md) for all memory options.
