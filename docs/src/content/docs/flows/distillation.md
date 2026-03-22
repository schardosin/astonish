---
title: "Flow Distillation"
description: "Convert chat sessions into reusable automation flows"
---

Flow distillation is the bridge between interactive chat and repeatable automation. After solving a task in chat, you can convert that session into a YAML flow that reproduces the same steps.

## How It Works

The `distill_flow` tool analyzes the current chat session's tool call trace and generates a YAML flow file that captures the sequence of operations performed. The resulting flow can be run independently without further interaction.

## Triggering Distillation

**In chat**, use the `/distill` slash command after completing a task.

The agent can also distill proactively if it recognizes a workflow that should be reusable.

## What Gets Captured

- Tool calls made during the session
- The sequence and dependencies between steps
- Input parameters that should be configurable
- Output handling

## What It Produces

A saved YAML flow file in `~/.config/astonish/flows/`.

## Using Distilled Flows

Once distilled, the flow can be:

- **Run standalone**: `astonish flows run <name>`
- **Scheduled**: via the scheduler for periodic execution
- **Edited**: in the Studio Flow Editor for fine-tuning
- **Shared**: via tap repositories

## The Distillation Workflow

The natural pattern is:

1. Explore and solve a problem interactively in chat.
2. Distill it into a reusable flow.
3. Run the flow whenever the same task comes up again.

This lets you iterate quickly in conversation, then lock in the solution as deterministic automation.
