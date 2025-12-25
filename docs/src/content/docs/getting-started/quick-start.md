---
title: Quick Start
description: Build and run your first AI agent in under 5 minutes
---

# Quick Start

This guide will have you running your first AI agent in under 5 minutes.

## 1. Launch Astonish Studio

```bash
astonish studio
```

This opens the visual designer at `http://localhost:9393`, where you can:

- **Configure providers** — A built-in setup wizard guides you through connecting AI providers (Gemini, Claude, GPT-4, Ollama, etc.)
- **Design flows visually** — Drag-and-drop nodes, connect edges, test in real-time
- **Manage MCP servers** — Add GitHub, Slack, databases, or custom tools

:::tip
Prefer the command line? Run `astonish setup` for CLI-based configuration instead.
:::

## 2. Configure Your Provider

On first launch, the setup wizard will guide you through configuring an AI provider. You'll need an API key from one of the supported providers:

| Provider | Type | 
|----------|------|
| Google Gemini | Cloud |
| Anthropic Claude | Cloud |
| OpenAI GPT-4 | Cloud |
| Groq | Cloud |
| OpenRouter | Cloud |
| Ollama | Local |
| LM Studio | Local |

## 3. Create Your First Agent

In the Studio:

1. Click **New Agent** 
2. Choose a name (e.g., `my_first_agent`)
3. Add an LLM node by clicking the canvas
4. Connect `START → your node → END`
5. Configure the node's prompt
6. Click **Run** to test

Or create it via YAML:

```yaml
name: my_first_agent
description: My first Astonish agent

nodes:
  - name: greet
    type: llm
    prompt: "Say hello to the user and ask how you can help them today."

flow:
  - from: START
    to: greet
  - from: greet
    to: END
```

## 4. Run from CLI

Once you've built your agent, run it anywhere:

```bash
# Interactive mode
astonish agents run my_first_agent

# With injected variables
astonish agents run summarizer -p file_path="/path/to/document.txt"

# Perfect for cron jobs
0 9 * * * astonish agents run daily_report >> /var/log/report.log
```

## Next Steps

- [Agent Flows](/concepts/flows/) — Understand how flows work
- [YAML Configuration](/concepts/yaml/) — Deep dive into YAML structure
- [MCP Integration](/concepts/mcp/) — Connect tools and capabilities
- [Tutorials](/tutorials/first-agent/) — Step-by-step walkthroughs
