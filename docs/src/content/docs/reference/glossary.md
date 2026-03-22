---
title: Glossary
description: Key terms and concepts in Astonish
sidebar:
  order: 2
---

Key terms and concepts used throughout Astonish, listed alphabetically.

---

### Agent

The AI entity you interact with in chat. Uses tools and memory to accomplish tasks.

### Channel

A communication adapter connecting Astonish to external messaging platforms (Telegram, Email).

### Compaction

Automatic summarization of older conversation messages when the context window gets full.

### Credential

An encrypted secret stored in the credential store. Used for API authentication, OAuth, and service access.

### Daemon

The Astonish background service that runs Studio, channels, scheduler, and fleet sessions.

### Delegation

When the agent spawns sub-agents to handle parts of a task in parallel.

### Distillation

Converting an interactive chat session into a reusable YAML flow.

### Edge

A connection between nodes in a flow. Can be unconditional or conditional.

### Embedding

A vector representation of text used for semantic search in the memory system.

### Fleet

Multi-agent team system. A group of specialized AI agents that collaborate on complex tasks.

### Flow

A structured automation workflow defined in YAML as a directed graph of nodes and edges.

### Knowledge

Information stored in the memory system. Retrieved automatically or via `memory_search`.

### Master Key

An optional password protecting credential store values from being viewed.

### MCP

Model Context Protocol -- an open standard for connecting AI agents to external tool servers.

### Memory

Persistent vector store where Astonish saves and retrieves knowledge across sessions.

### Node

A unit of work in a flow (`input`, `llm`, `tool`, `output`, or `update_state`).

### Plan

A configured fleet template instance bound to a specific project, channel, and credentials.

### Provider

An AI model service (OpenAI, Anthropic, Ollama, etc.) that Astonish connects to for inference.

### Redaction

The 5-layer security system that prevents secrets from appearing in logs or LLM context.

### Scheduler

The cron-based job execution system that runs tasks on a schedule.

### Session

A persisted conversation with its full history, stored as a JSONL file.

### Skill

A pre-built instruction guide that teaches the agent how to use a specific CLI tool or workflow.

### State

The shared key-value store that passes data between nodes in a flow.

### Sub-agent

A child agent spawned by delegation, with its own session and filtered tool access.

### Sub-session

A session created by a sub-agent, linked to its parent session.

### Tap

A Git repository that provides flows and MCP server configurations as extensions.

### Template

A fleet team definition specifying agent roles, models, and capabilities.

### Thread

A pairwise conversation between two agents in a fleet session.

### Tool

A capability the agent can invoke (file operations, shell commands, web requests, etc.).

### Toolset

A group of related tools (e.g., browser tools, email tools).

### Vector Store

The chromem-go database that stores embeddings for semantic memory search.
