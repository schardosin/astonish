# Introduction

Astonish is an AI agent platform that makes your whole team smarter. When one person solves a problem, the solution flows to everyone who needs it. Knowledge compounds across every conversation, every team member, every day.

Built in Go on Google's Agent Development Kit, Astonish combines autonomous tool-use agents with three-tier memory, flow distillation, generative UI, and enterprise-grade multi-tenancy. It runs as a single binary with all platform capabilities regardless of deployment size:

- **Local (SQLite)** — Zero-config, runs entirely on your machine. SQLite handles storage and vector search out of the box. Install and start chatting in under a minute.
- **Cloud (PostgreSQL)** — Multi-tenant with pgvector. Organizations, teams, shared memory, cascading configuration, and enterprise security for your whole team.

Same binary, same 58+ tools, same platform. Your choice of database backend.

## Core Capabilities

**Autonomous Agent Engine.** LLM-driven tool-use loops that solve problems dynamically. 58+ built-in tools spanning shell execution, file operations, web fetching, browser automation, memory, and more. Sub-agent delegation for complex multi-step tasks.

**15+ AI Providers.** OpenAI, Anthropic, Google Gemini, Groq, OpenRouter, xAI, Ollama, LM Studio, SAP AI Core, LiteLLM, and others. Switch providers per conversation or set team-wide defaults.

**MCP Native.** Any MCP-compatible server works out of the box. Admins can configure MCP servers at the team level and everyone gets instant access.

**Three-Tier Memory.** Personal, team, and organization-level knowledge stores searched together with intelligent weighting. Powered by hybrid search (vector similarity + keyword matching via FTS5 or pgvector). Solutions persist and surface automatically when relevant.

**Flow Distillation.** After solving a multi-step problem, distill the execution trace into a reusable YAML workflow. Parameterized, validated, shareable. Schedule flows, version-control them, or edit visually in Studio.

**Generative UI.** Describe a dashboard, tool, or interactive app in plain English. Astonish builds it live in the chat using React 19 and Tailwind CSS. Save and share with your team.

**Enterprise Security.** Envelope encryption (AES-256-GCM), OIDC/SSO federation, per-organization sandboxes (Incus or Kubernetes), immutable audit logs, and database-per-org isolation.

**Multi-Channel Access.** Studio (web UI), CLI, Remote CLI, Telegram, Email, and Slack. All channels connect to the same platform with consistent context.

**Fleet.** Multi-agent collaboration for complex missions. Delegate work across parallel sub-agents with isolated sessions and filtered tool access.

## The Platform Differentiator

Most AI tools are isolated to individual users. Astonish is built for teams from the ground up.

When someone on your team spends an hour debugging a tricky Kubernetes networking issue, the solution goes into team memory. Next week, when another teammate hits the same problem, the agent already knows the answer. No repeated work, no tribal knowledge lost in chat logs.

Resources cascade downward — provider configs, MCP servers, skills, and sandbox templates flow from platform to org to team to individual. Data ownership flows upward only when explicitly published. You control what stays private and what benefits the team.

## How It Works

```bash
# Local — start immediately
astonish setup         # Configure your AI provider
astonish daemon        # Start the daemon (Studio available at http://localhost:9393)
astonish chat          # Start solving problems

# Cloud — scale to your team
export ASTONISH_DSN="postgres://..."
astonish platform init                         # Set up the platform
astonish platform org invite alice@company.com # Invite your team
```

The agent solves problems using autonomous tool-use loops. It selects tools, chains them together, and works through multi-step tasks without manual intervention. After a successful interaction, distill it into a reusable flow that anyone on the team can run.

```
You:    "Deploy the staging environment and run the smoke tests"
Agent:  [executes 12 tool calls across shell, git, and kubectl]
        Deployment complete. All 47 smoke tests passing.

You:    /distill
Agent:  Saved: deploy_staging.yaml
        Run again: astonish flows run deploy_staging -p env="staging"
```

## What's Next

- [Architecture](./getting-started/architecture.md) — Understand the layer model
- [Installation](./getting-started/installation.md) — Get Astonish on your machine
- [Quick Start: Local](./getting-started/quick-start-local.md) — Start chatting in under a minute
- [Quick Start: Cloud](./getting-started/quick-start-cloud.md) — Deploy for your team
- [Choose Your Interface](./getting-started/choose-your-interface.md) — Studio, CLI, Telegram, and more

## At a Glance

| Dimension | Local (SQLite) | Cloud (PostgreSQL) |
|-----------|---------------|-------------------|
| Database | SQLite (with built-in vector search) | PostgreSQL 15+ with pgvector |
| Users | Single user | Multi-tenant (orgs, teams, members) |
| Memory | Personal tier | Personal + Team + Organization tiers |
| Security | Local credentials | Envelope encryption, OIDC/SSO, audit logs |
| Channels | Studio, CLI | Studio, CLI, Remote CLI, Telegram, Email, Slack |
| Sandboxes | Local | Per-org network-isolated (Incus/Kubernetes) |
| Config | Local files | Cascading (platform → org → team → personal) |

Both deployments ship in the same binary and share the same agent engine, tools, and capabilities.
