---
title: Introduction
description: Learn what Astonish is and how it can help you build production AI agents
sidebar:
  order: 1
---

# Welcome to Astonish

**Astonish** helps you build production AI agents in minutes, not months. Design visually, run anywhere—no servers required.

## What is Astonish?

Astonish is a **community-based, open-source AI agentic tool** that lets you create AI-powered automation workflows called **flows**. Each flow is a series of connected steps that can:

- 🤖 Call AI models (GPT, Claude, Gemini, Ollama, and more)
- 🔧 Use tools via the [MCP protocol](https://modelcontextprotocol.io/)
- 📥 Collect user input
- 🔀 Branch based on conditions
- 📤 Output results

### 🌐 Share and Run Agents

Build an agent once, share it with the community. When you share an agent, you share everything needed to run it—including its MCP server dependencies. Others can tap your repository (similar to Homebrew taps) and execute your agents instantly, no additional setup required.

## Why Astonish?

| Traditional Approach | With Astonish |
|---------------------|---------------|
| Write boilerplate code for each provider | Configure once, switch providers instantly |
| Build custom UI for each workflow | Visual editor + CLI, same YAML runs both |
| Deploy servers, manage infrastructure | Single binary, runs anywhere |
| Lock-in to one platform | Your flows are portable YAML files |

## Key Features

### 🎯 Single Binary, Zero Infrastructure

No web servers. No cloud subscriptions. Astonish is a single executable that runs anywhere—your laptop, a Raspberry Pi, in a container, or a CI/CD pipeline.

```bash
# Run in any script or cron job
astonish flows run daily_report >> /var/log/report.log
```

### 📄 YAML as Source of Truth

Your agent logic lives in simple YAML files. Version control them. Review them in PRs. Move them between environments.

```yaml
name: my-agent
nodes:
  - name: analyze
    type: llm
    prompt: "Analyze {input}"
flow:
  - from: START
    to: analyze
  - from: analyze
    to: END
```

### 🖥️ Design Visually, Run Anywhere

Use **Astonish Studio** to design flows visually, then run the exact same YAML from the command line.

![Astonish Studio Interface](/astonish/images/introduction-canvas.webp)
*Design your AI flows with the visual editor*

## Build Visually, Run Anywhere

Astonish is designed for a seamless workflow: **design in the Studio, execute anywhere.**

| Phase | Tool | What You Do |
|-------|------|-------------|
| **Build** | Astonish Studio | Design flows visually, use AI assist to generate nodes, test and iterate quickly |
| **Run** | CLI (Headless Mode) | Execute your flows anywhere—scripts, cron jobs, CI/CD pipelines, containers |

This isn't an either/or choice. Use the Studio to leverage visual editing and AI assistance while building your flows, then run them headlessly via the command line wherever you need.

## What's Next?

Ready to get started? Here's your path:

1. **[Installation](/getting-started/installation/)** — Get Astonish running on your machine
2. **[Choose Your Path](/getting-started/choose-your-path/)** — Visual or CLI? Pick your adventure
3. **[Quickstart](/getting-started/quickstart/studio/)** — Build your first agent in 5 minutes
