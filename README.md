<div align="center">
  <img src="https://raw.githubusercontent.com/schardosin/astonish/main/images/astonish-logo-only.svg" width="200" height="200" alt="Astonish Logo">
  
  # Astonish
  
  ### Build Production AI Agents in Minutes, Not Months
  
  *Design visually. Run anywhere. No servers required.*
  
  [![Go Report Card](https://goreportcard.com/badge/github.com/schardosin/astonish)](https://goreportcard.com/report/github.com/schardosin/astonish)
  [![Build Status](https://github.com/schardosin/astonish/actions/workflows/build.yml/badge.svg)](https://github.com/schardosin/astonish/actions/workflows/build.yml)
  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

</div>

---

## 💡 Our Vision

**Agent flows should be designed, not coded.**

We believe the future of AI automation is declarative. You should focus on *what* your agent does—the business logic, the steps, the outcomes—not *how* to wire up providers, handle errors, or manage retries.

Here's how Astonish makes this possible:

| You Focus On | Astonish Handles |
|-------------|------------------|
| Designing the flow | Provider connections & authentication |
| Choosing which tools to use | Error detection & intelligent retries |
| Defining success criteria | State management across steps |
| Business logic | Parallel execution & performance |

**MCP servers extend capabilities** — Need GitHub integration? Database access? File operations? Add an MCP server. Your flow stays clean; capabilities plug in.

**AI assists your design** — Not sure how to structure your flow? Describe what you want in plain English. The AI Assistant generates the flow, refines nodes, and optimizes sequences.

**The framework handles resilience** — When things fail (and they will), Astonish evaluates errors, decides whether to retry or abort, and keeps your flow moving. You don't write a single line of error-handling code.

This is why Astonish exists: to let you think in flows, not in code.

---

## What Makes Astonish Different

### 🎯 Single Binary, Zero Infrastructure

No web servers. No Docker. No cloud subscriptions. Astonish is a single executable that runs anywhere—your laptop, a Raspberry Pi, or a CI/CD pipeline.

```bash
# Add it to your cron
0 9 * * * /usr/local/bin/astonish agents run daily_report >> /var/log/report.log

# Run in any script
./astonish agents run code_reviewer -p repo="./my-project"
```

### 📄 YAML as Source of Truth

Your agent logic lives in simple YAML files. Version control them. Review them in PRs. Move them between environments. No platform lock-in.

```yaml
# This IS your agent. Copy it, share it, version it.
nodes:
  - name: analyze
    type: llm
    prompt: "Analyze {input}"
flow:
  - from: START
    to: analyze
```

### 🖥️ Design Visually, Run Anywhere

Use **Astonish Studio** to design flows visually, then run the exact same YAML from the command line. No "export" step. No format conversion.

---

## The Solution: Astonish

Astonish is a **declarative AI agent engine** that lets you design, test, and run production-grade agents with zero code.

```bash
# Install
brew install schardosin/astonish/astonish

# Design your agent visually
astonish studio

# Or run agents directly from CLI
astonish agents run my_agent
```

**That's it.** No web servers. No Docker. No complex deployments. Just a single binary that runs anywhere—your laptop, a cron job, or a CI/CD pipeline.

---

## ✨ Astonish Studio

Design your agent flows visually with the built-in **Astonish Studio**:

<div align="center">
  <img src="https://raw.githubusercontent.com/schardosin/astonish/main/images/astonish_studio.jpg" width="600" height="400" alt="Astonish Studio">
</div>

- 🤖 **AI Assistant** — Your co-pilot for building agents:
  - **Flow-level**: Describe what you want and let AI generate the entire flow
  - **Node-level**: Select a node and ask AI to refine or improve it
  - **Multi-node**: Select multiple nodes and ask AI to optimize the sequence
- 🎨 **Drag-and-drop** flow designer with visual node connections
- ⚡ **One-click execution** with real-time streaming output
- 🔧 **MCP server integration** for GitHub, databases, and more
- 💾 **Instant save** to YAML—version control your agents like code

---

## Astonish vs. Alternatives

| Feature | Astonish | Traditional No-Code (N8N, Make) | Raw SDKs (ADK, LangChain) |
|---------|----------|--------------------------------|---------------------------|
| **Runs as CLI** | ✅ Native | ❌ Requires server | ❌ You build it |
| **Single binary** | ✅ One file | ❌ Docker/cloud | ❌ Dependencies |
| **Version-controlled flows** | ✅ YAML in Git | ⚠️ Platform exports | ✅ Code in Git |
| **Cron-job friendly** | ✅ Built for it | ⚠️ Needs running server | ✅ If you code it |
| **Visual designer** | ✅ Studio | ✅ Native | ❌ None |
| **Zero boilerplate** | ✅ Declarative | ✅ Visual | ❌ You write it |
| **AI providers** | ✅ 8+ built-in | ✅ Native support | ❌ You integrate |
| **MCP support** | ✅ Native | ✅ Supported | ⚠️ Manual setup |

**Astonish is ideal when you want:**
- Agent flows that run in automation (cron, CI/CD, scripts)
- Simple YAML files you can version control and share
- Visual design with CLI execution
- No server infrastructure to maintain

---

## 🚀 Quick Start

### 1. Install

```bash
# macOS/Linux (Homebrew)
brew install schardosin/astonish/astonish

# Or with Go
go install github.com/schardosin/astonish/cmd/astonish@latest
```

### 2. Setup Providers

```bash
astonish setup
```

Configure your AI providers (Gemini, Claude, GPT-4, Ollama, etc.) and MCP servers.

### 3. Launch Studio (Visual Designer)

```bash
astonish studio
```

Opens a local web UI at `http://localhost:9393` where you can visually design and test your agents.

### 4. Run from CLI

```bash
# Interactive mode
astonish agents run my_agent

# With injected variables
astonish agents run summarizer -p file_path="/path/to/document.txt"

# Perfect for cron jobs and automation
0 9 * * * /usr/local/bin/astonish agents run daily_report >> /var/log/report.log
```

---

## 🏗️ Architecture

Astonish is built on **Google's Agent Development Kit (ADK)** but removes all the boilerplate:

```
┌─────────────────────────────────────────────────────────────┐
│                     ASTONISH                                │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │  YAML Flow  │  │   Studio    │  │    CLI Runner       │  │
│  │  Definition │  │  (Visual)   │  │  (cron/scripts)     │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘  │
│         └────────────────┼────────────────────┘             │
│                    ┌─────▼─────┐                            │
│                    │  Engine   │ ← State Blackboard         │
│                    │  (Go)     │ ← Parallel Execution       │
│                    └─────┬─────┘ ← Error Recovery           │
│                          │                                  │
│         ┌────────────────┼────────────────┐                 │
│   ┌─────▼─────┐    ┌─────▼─────┐    ┌─────▼─────┐           │
│   │  Google   │    │    MCP    │    │  Built-in │           │
│   │    ADK    │    │  Servers  │    │   Tools   │           │
│   └───────────┘    └───────────┘    └───────────┘           │
└─────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
  ┌─────▼─────┐    ┌─────▼─────┐    ┌─────▼─────┐   │
  │  Gemini   │    │  Claude   │    │   GPT-4   │  ...
  │           │    │           │    │           │
  └───────────┘    └───────────┘    └───────────┘
```

---

## 📋 Example: PR Description Generator

A real-world agent that reads GitHub PRs and generates descriptions:

```yaml
description: Generate PR descriptions from code changes

nodes:
  - name: get_prs
    type: llm
    prompt: List open PRs using the gh CLI
    tools: true
    tools_selection: [shell_command]
    output_model:
      prs: str

  - name: select_pr
    type: input
    prompt: "Select a PR number from:\n{prs}"
    output_model:
      pr_number: int

  - name: get_diff
    type: llm
    prompt: Get the diff for PR #{pr_number}
    tools: true
    tools_selection: [shell_command]
    output_model:
      diff: str

  - name: generate_description
    type: llm
    system: You are a technical writer.
    prompt: |
      Generate a clear PR description for this diff:
      {diff}
    output_model:
      description: str

flow:
  - from: START
    to: get_prs
  - from: get_prs
    to: select_pr
  - from: select_pr
    to: get_diff
  - from: get_diff
    to: generate_description
  - from: generate_description
    to: END
```

Run it:
```bash
astonish agents run pr_description_generator
```

---

## 🔌 Supported Providers

| Provider | Type | Status |
|----------|------|--------|
| Google Gemini | Cloud | ✅ |
| Anthropic Claude | Cloud | ✅ |
| OpenAI GPT-4 | Cloud | ✅ |
| SAP AI Core | Enterprise | ✅ |
| Groq | Cloud | ✅ |
| OpenRouter | Cloud | ✅ |
| X.AI (Grok) | Cloud | ✅ |
| Ollama | Local | ✅ |
| LM Studio | Local | ✅ |

---

## 🛠️ MCP Integration

Connect your agents to any MCP-compatible server:

```bash
# Add MCP servers via setup
astonish setup

# Or configure manually in ~/.astonish/mcp_config.json
{
  "github": {
    "command": "npx",
    "args": ["-y", "@modelcontextprotocol/server-github"]
  }
}
```

Your agents can now interact with GitHub, read files, query databases, and more.

---

## 🎯 Use Cases

- **DevOps Automation**: PR reviews, release notes, incident response
- **Data Processing**: ETL pipelines with AI-powered transformation
- **Content Generation**: Blog posts, documentation, translations
- **Code Analysis**: Security audits, refactoring suggestions
- **Research Agents**: Web scraping with intelligent extraction
- **Scheduled Tasks**: Daily summaries, monitoring alerts (perfect for cron)

---

## 📖 Documentation

- [Full Documentation](https://github.com/schardosin/astonish/wiki)
- [Agent Examples](https://github.com/schardosin/astonish/tree/main/agents)
- [MCP Server Setup](https://github.com/schardosin/astonish/wiki/MCP-Setup)

---

## 🤝 Contributing

We're building the future of declarative AI agents. Contributions welcome!

1. Fork the repository
2. Create a feature branch
3. Submit a Pull Request

---

## 📄 License

MIT License - see [LICENSE](LICENSE) for details.

---

<div align="center">
  
  **Built with ❤️ using Google ADK**
  
  [⭐ Star us on GitHub](https://github.com/schardosin/astonish)
  
</div>