# Publish & Fork

Resources in cloud deployments are **private by default**. You own your sessions, flows, apps, and memory entries. Sharing is always an explicit action — publish to your team, fork from your team.

## The Model

```
┌──────────┐   publish   ┌──────────┐   promote   ┌──────────┐
│ Personal │ ──────────▶ │   Team   │ ──────────▶ │   Org    │
│  (yours) │             │ (shared) │             │  (all)   │
└──────────┘             └──────────┘             └──────────┘
                              │
                         fork │
                              ▼
                        ┌──────────┐
                        │ Personal │
                        │  (copy)  │
                        └──────────┘
```

- **Publish** — share your resource with your team (read access for team members)
- **Fork** — copy a team resource into your personal workspace for modification
- **Promote** — elevate team knowledge to org level (admin action)

## Publishing

Any team member can publish their own resources to the team:

```bash
# Publish a session
astonish session publish sess_4a2c --to backend

# Publish a flow
astonish flow publish my-deploy-flow --to backend

# Publish an app
astonish app publish my-dashboard --to backend
```

Published resources appear in the team's shared space. The original remains in your personal schema — publishing creates a linked copy in the team schema.

### What Gets Published

| Resource | What's Shared | What Stays Private |
|----------|--------------|-------------------|
| Session | Messages, artifacts, memory extracts | Draft messages, personal notes |
| Flow | Flow definition, steps, tool config | Execution history |
| App | Source code, manifest | Local environment variables |
| Memory | Content, embeddings, tags | Source session reference |

## Forking

Team members can fork any published resource into their personal workspace:

```bash
# Fork a team flow
astonish flow fork team-deploy-flow

# Fork with a new name
astonish flow fork team-deploy-flow --as my-deploy-variant
```

Forking creates an independent copy. Changes to your fork do not affect the team resource, and vice versa. Provenance metadata tracks the fork origin.

## Promotion

Org admins and team admins can promote resources from team level to org level, making them available to every team:

```bash
# Promote a team flow to org level
astonish flow promote team-deploy-flow --from backend --to org

# Promote memory entries
astonish memory promote mem_9c4d1e --from team backend --to org
```

Promotion is useful for:
- Standards and best practices that apply org-wide
- Flows that every team should have access to
- Memory entries containing institutional knowledge

## Unpublishing and Deletion

```bash
# Remove from team (your personal copy remains)
astonish session unpublish sess_4a2c --from backend

# Delete entirely (personal + published copies)
astonish session delete sess_4a2c --everywhere
```

Unpublishing removes the team copy but does not affect forks that other users have already created.

## Permissions Summary

| Action | Who Can Do It |
|--------|--------------|
| Publish to team | Any team member (own resources only) |
| Fork from team | Any team member or viewer |
| Unpublish from team | Author or team admin |
| Promote to org | Team admin or org admin |
| Demote from org | Org admin only |

## Next Steps

- [Three-Tier Memory](./three-tier-memory) — how published memory is searched
- [Organizations & Teams](./organizations-and-teams) — roles that govern publish/promote
