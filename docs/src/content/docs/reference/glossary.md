---
title: Glossary
description: Key terms and definitions for Astonish
sidebar:
  order: 2
---

# Glossary

Key terms and definitions used throughout Astonish.

---

## A

### Agent
See [Flow](#flow).

---

## E

### Edge
A connection between two nodes in a flow. Edges can be **conditional** (with a `condition` property) or **unconditional** (direct connection).

---

## F

### Flow
A directed graph of nodes and edges that defines an AI workflow. Flows are stored as YAML files and can be run via CLI or Studio.

---

## L

### LLM
**Large Language Model**. An AI model that generates text, such as GPT-4, Claude, or Gemini. LLM nodes call these models.

---

## M

### Manifest
A `manifest.yaml` file in a tap repository that indexes available flows and MCP server configurations.

### MCP
**Model Context Protocol**. An open standard for connecting AI models to external tools and data sources. See [MCP Concepts](/concepts/mcp/).

### MCP Server
A process that provides tools to Astonish via the MCP protocol. Configured in `mcp_config.json`.

---

## N

### Node
A processing step in a flow. Node types include: `input`, `llm`, `tool`, `output`, `update_state`.

---

## O

### Output Model
A structured definition of variables that a node produces. Defined in YAML as `output_model`.

---

## P

### Provider
An AI model provider such as OpenAI, Anthropic, Google, or OpenRouter. Configured in `config.yaml`.

---

## S

### State
A shared object that carries data between nodes during flow execution. Variables are added by nodes and read using `{variable}` syntax.

### Studio
The visual, browser-based editor for creating and editing flows. Launch with `astonish studio`.

---

## T

### Tap
A GitHub repository that provides flows and MCP configurations. Managed with `astonish tap` commands.

### Tool
A capability provided by an MCP server that flows can use. Examples: `web_search`, `create_issue`, `read_file`.

---

## Y

### YAML
The file format used for flow definitions. All flows are stored as `.yaml` files in `~/.astonish/agents/`.
