# Skills System

## Overview

Skills are markdown-based guides that teach the AI agent how to use specific CLI tools and platforms. Each skill document describes capabilities, commands, patterns, and best practices for a technology (git, Docker, Kubernetes, Terraform, etc.). Skills are indexed in the vector store and retrieved automatically when the user's request matches a skill's domain.

## Key Design Decisions

### Why Markdown with YAML Frontmatter

Skills use plain markdown files with YAML frontmatter for metadata:

```markdown
---
name: docker
description: Container management with Docker
requires:
  binaries: [docker]
  os: [linux, darwin]
---

# Docker

## Building Images
...
```

This format was chosen because:

- **Human-readable and editable**: Anyone can write or modify a skill.
- **LLM-friendly**: Markdown is the format LLMs are most comfortable consuming and producing.
- **Structured metadata**: YAML frontmatter provides machine-parseable eligibility criteria without polluting the content.
- **Version-controllable**: Plain text files work naturally with git.

### Why Eligibility Checking

Not all skills are relevant to every environment. A Kubernetes skill is useless if `kubectl` isn't installed. Eligibility checking validates:

- **OS**: The skill applies to the current operating system.
- **Binaries**: Required CLI tools are installed (checked via `exec.LookPath`).
- **Environment variables**: Required env vars are set.

Ineligible skills are excluded from indexing, preventing the agent from attempting operations that will fail due to missing prerequisites.

### Why Bundled Skills Plus Marketplace

Astonish ships with 10 bundled skills covering common tools. But organizations and communities may need custom skills for their specific toolchains. The **ClawHub marketplace** provides:

- A registry of community-contributed skills.
- Install/uninstall via the CLI or Studio.
- Skills are downloaded to the local skills directory and indexed alongside bundled ones.

### Why Directory Watching

Skills can be added, modified, or removed at any time. An `fsnotify` file watcher detects changes and triggers re-indexing automatically. This means:

- Installing a new skill from the marketplace is immediately available.
- Editing a skill file updates the vector index without restart.
- Removing a skill removes it from retrieval.

## Architecture

### Skill Discovery and Retrieval

```
User message: "Deploy the app to our Kubernetes cluster"
    |
    v
Auto-knowledge retrieval (vector + BM25 search)
    |
    v
Matches: kubernetes skill (high relevance)
    |
    v
Skill content injected into system prompt (Tier 3 dynamic knowledge)
    |
    v
Agent uses Kubernetes commands and patterns from the skill
```

The `skill_lookup` tool also allows the agent to explicitly search for skills mid-turn.

### Skill Index in System Prompt

The system prompt (Tier 1 static) includes a lightweight skill listing -- just names and one-line descriptions:

```
Available Skills: git (version control), docker (containers), kubernetes (orchestration), ...
```

This tells the agent what skills exist without consuming tokens for full content. When a skill is relevant, the full content is retrieved from the vector store.

### Bundled Skills

| Skill | Description |
|---|---|
| git | Version control operations, branching, merging |
| github | GitHub CLI, issues, PRs, releases |
| docker | Container management, Compose, images |
| kubernetes | kubectl, deployments, services, pods |
| terraform | Infrastructure as code, state management |
| aws | AWS CLI, common services |
| gcloud | Google Cloud CLI, projects, services |
| npm | Node.js package management, scripts |
| python | Python development, pip, venv, uv |
| web-registration | Web portal account creation patterns |

### Memory Integration

Skills are indexed in the same vector store as other memory documents, under the "skill" category. This means:

- Skills participate in the same hybrid search (vector + BM25) as general knowledge.
- The `CategoryFromPath` function assigns the "skill" category based on file location.
- Skills can be filtered by category for targeted retrieval.

## Key Files

| File | Purpose |
|---|---|
| `pkg/skills/skills.go` | Skill loading, eligibility checking, directory management |
| `pkg/skills/bundled.go` | Embedded bundled skill files |
| `pkg/skills/marketplace.go` | ClawHub marketplace integration |
| `pkg/skills/watcher.go` | fsnotify-based directory watching for live updates |
| `pkg/tools/skill_lookup.go` | skill_lookup tool for explicit skill search |

## Interactions

- **Memory**: Skills are indexed in the vector store for automatic retrieval. Category-based filtering allows partitioned searches.
- **Agent Engine**: Skill content is injected into the system prompt via Tier 3 dynamic knowledge. The skill index (Tier 1) lists available skills.
- **Configuration**: Skill directory is configured in app config. Custom skill paths can be added.
- **Daemon**: Skill indexer initializes at daemon startup with fsnotify watcher.
