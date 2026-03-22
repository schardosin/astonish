---
title: "Fleet Templates"
description: "Define agent team composition and roles"
---

A fleet template defines the team structure — which agents exist, what they do, and how they are configured. Templates are reusable: create one template for your dev team, then make multiple plans from it for different projects.

## What a Template Contains

Templates are YAML definitions that specify:

- Team name and description.
- Agent roles with their purpose and capabilities.
- Default models for each agent.
- Tool access per agent.
- Workspace configuration.

Templates are managed through the Studio Fleet UI.

## Agent Definition

Each agent in a template has the following fields:

| Field | Purpose |
|---|---|
| **Role name** | Identifier used for routing (e.g., `developer`, `reviewer`, `coordinator`). |
| **Description** | What this agent does — used by the router and other agents to understand its purpose. |
| **Model** | Which AI provider and model to use. |
| **Behavior** | Custom instructions for how the agent should approach its work. |
| **Tools** | Which tools this agent has access to. |

## Partial Teams

The `include_agents` field on plans allows you to use a subset of the template's agents for simpler missions. A template might define five roles, but a lightweight task can run with just two of them.

## Listing Templates

From the CLI:

```bash
astonish fleet templates
```

This lists all available templates with their names, descriptions, and agent counts.
