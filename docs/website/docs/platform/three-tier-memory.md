# Three-Tier Memory

Astonish platform memory operates across three tiers — personal, team, and org — searched in parallel and merged with weighted Reciprocal Rank Fusion (RRF). This gives agents access to the full depth of organizational knowledge while prioritizing context that is most relevant to the user.

## Tiers and Weights

| Tier | Schema | Weight | Contains |
|------|--------|--------|----------|
| **Personal** | `personal_{user_id}` | 1.2× | Your sessions, notes, corrections, personal patterns |
| **Team** | `team_{slug}` | 1.0× | Published team knowledge, shared sessions, team patterns |
| **Org** | `public` | 0.8× | Promoted org-wide knowledge, standards, institutional memory |

The weight multiplier is applied during RRF score calculation. Personal memories rank slightly higher because they represent your specific context and preferences. Org memories rank slightly lower because they are more general.

## How Search Works

When an agent searches memory, the platform executes queries against all three tiers simultaneously:

```
User query: "How do we handle database migrations?"

┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   Personal   │     │     Team     │     │     Org      │
│  (1.2× wt)  │     │  (1.0× wt)  │     │  (0.8× wt)  │
└──────┬───────┘     └──────┬───────┘     └──────┬───────┘
       │                    │                    │
       ▼                    ▼                    ▼
  Vector search        Vector search        Vector search
  + Full-text          + Full-text          + Full-text
       │                    │                    │
       └────────────────────┼────────────────────┘
                            ▼
                   Reciprocal Rank Fusion
                   (weighted by tier)
                            │
                            ▼
                    Merged result set
```

### Hybrid Search

Each tier uses hybrid search combining two PostgreSQL capabilities:

- **pgvector** — semantic similarity via embedding cosine distance
- **tsvector** — keyword matching via PostgreSQL full-text search

Results from both methods are fused per-tier, then cross-tier RRF produces the final ranked list.

```sql
-- Simplified: what happens inside each tier
SELECT id, content,
  (1.0 / (60 + vector_rank)) * :tier_weight AS vector_score,
  (1.0 / (60 + fts_rank)) * :tier_weight AS fts_score
FROM memory
WHERE embedding <=> :query_vector < 0.8
   OR tsv @@ plainto_tsquery(:query_text)
ORDER BY (vector_score + fts_score) DESC
LIMIT 20;
```

## Knowledge Promotion Chain

Knowledge flows upward through the tiers via explicit promotion:

```
Personal  ──publish──▶  Team  ──promote──▶  Org
```

1. **Personal → Team**: any team member can publish a memory entry to their team. This makes it searchable by all team members.
2. **Team → Org**: a team admin or org admin promotes team knowledge to org level, making it available to every team in the organization.

```bash
# Publish a personal memory entry to your team
astonish memory publish mem_7f3a2b --to team

# Org admin promotes team knowledge to org level
astonish memory promote mem_9c4d1e --from team backend --to org
```

Promotion copies the entry (with provenance metadata) — the original remains in its source tier.

## The Learning Loop

Here is how knowledge compounds in practice:

1. **Alice** debugs a tricky Kubernetes networking issue. Her session is stored in personal memory.
2. Alice publishes the resolution to the **Backend team**. Now when any backend engineer hits a similar issue, the agent surfaces Alice's solution.
3. The team admin notices this resolution is relevant org-wide and **promotes it to org level**.
4. **Dave** on the Frontend team later encounters the same networking issue. The agent finds the org-level memory and guides him through the fix — even though Dave never interacted with Alice.

Each step is explicit. Knowledge does not leak upward automatically.

## Memory Entry Structure

```yaml
id: mem_7f3a2b
content: "PostgreSQL connection pooling with PgBouncer requires..."
embedding: [0.023, -0.118, ...]    # 1536-dim vector
tier: team
source_session: sess_4a2c
author: alice
created_at: 2025-03-15T10:30:00Z
promoted_from: personal
tags: [postgresql, pgbouncer, connection-pooling]
```

## Configuring Memory

Memory behavior can be tuned at each level via [Cascading Defaults](./cascading-defaults):

```yaml
memory:
  embedding_model: text-embedding-3-small
  search_limit: 20              # results per tier before fusion
  weights:
    personal: 1.2
    team: 1.0
    org: 0.8
  auto_memorize: true           # extract key facts from sessions
```

## Privacy Guarantees

- Personal memory is never searched by other users, even org admins.
- Publishing is always an explicit user action — never automatic.
- Promotion requires admin privileges.
- Deleted memories are hard-deleted (not soft-deleted) from all tiers.

## Next Steps

- [Publish & Fork](./publish-and-fork) — the full resource sharing model
- [Organizations & Teams](./organizations-and-teams) — how tiers map to the org hierarchy
