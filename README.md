<div align="center">
<img src="https://raw.githubusercontent.com/schardosin/astonish/main/images/astonish-logo-only.svg" width="200" height="200" alt="Astonish Logo">

# Astonish

### Build Production AI Agents in Minutes, Not Months

*Design visually. Run anywhere. No servers required.*

[![Documentation](https://img.shields.io/badge/Documentation-Astonish-purple.svg)](https://schardosin.github.io/astonish/)
[![Lint](https://github.com/schardosin/astonish/actions/workflows/lint.yml/badge.svg)](https://github.com/schardosin/astonish/actions/workflows/lint.yml)
[![Build Status](https://github.com/schardosin/astonish/actions/workflows/build.yml/badge.svg)](https://github.com/schardosin/astonish/actions/workflows/build.yml)
[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/schardosin/astonish)](https://goreportcard.com/report/github.com/schardosin/astonish)

</div>

---

## 💡 Our Vision

**AI SOPs should be designed, not coded.**

We believe the future of AI automation is declarative. You should focus on *what* your agent does, the business logic, the steps, and the outcomes, not *how* to wire up providers, handle errors, or manage retries. Astonish is built for **SOP-driven AI agents** that are structured, reliable, and repeatable.

| You Focus On | Astonish Handles | 
| ----- | ----- | 
| Designing the business flow | Provider connections & authentication | 
| Choosing which tools to use | Error detection & intelligent retries | 
| Defining success criteria | State management (Blackboard pattern) | 
| Business logic | Parallel execution & performance | 

**MCP servers extend capabilities.** Need GitHub integration? Database access? Search the Internet? Add an MCP server. Your flow stays clean, and capabilities plug in via the Model Context Protocol.

**AI assists your design.** Not sure how to structure your flow? Describe what you want in plain English. The AI Assistant generates the flow, refines nodes, and optimizes sequences.

## What Makes Astonish Different

### 🎯 Single Binary, Zero Infrastructure

No web servers. No Docker-compose hell. No cloud subscriptions. Astonish is a single **Go-compiled executable** that runs anywhere, including your laptop, a Raspberry Pi, or a CI/CD pipeline.

```bash
# Add it to your cron
0 9 * * * /usr/local/bin/astonish flows run daily_report >> /var/log/report.log

# Run in any script
./astonish flows run code_reviewer -p repo="./my-project"
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

Use **Astonish Studio** to design flows visually, then run the exact same YAML from the command line. There is no "export" step and no format conversion.

---

## ✨ Astonish Studio

<div align="center">
<img src="https://github.com/user-attachments/assets/fafeb202-b864-4ff8-ba37-7ae9f1a5dd0c" width="1000" alt="Astonish Studio">

<p>Design your agent flows visually with the built-in <b>Astonish Studio</b></p>
</div>

• 🤖 **AI Assistant**. Describe what you want and let AI generate or refine your entire DAG.  
• 🎨 **Visual Designer**. Drag-and-drop nodes with real-time streaming execution output.  
• 🔧 **MCP Native**. First-class support for any MCP server like GitHub, Slack, or Postgres.  
• 🏪 **Flow Store**. Install community agent flows with Homebrew-style taps.  
• 💾 **GitOps Ready**. Save directly to YAML for instant version control.  

---

## 🚀 Quick Start

### 1. Install

```bash
# macOS/Linux (Homebrew)
brew install schardosin/astonish/astonish

# Or via curl
curl -fsSL [https://raw.githubusercontent.com/schardosin/astonish/refs/heads/main/install.sh](https://raw.githubusercontent.com/schardosin/astonish/refs/heads/main/install.sh) | sh
```

### 2. Launch Studio

```bash
astonish studio
```

Opens a local UI at `http://localhost:9393` to configure providers (Gemini, Claude, GPT, Ollama) and design your first flow.

### 3. Run from CLI

Once configured, run your agents anywhere:

```bash
# Interactive mode
astonish flows run my_agent

# With injected variables
astonish flows run summarizer -p file_path="./notes.txt"

# Perfect for cron jobs and automation
0 9 * * * /usr/local/bin/astonish flows run daily_report >> /var/log/report.log
```

---

## 🔍 Why Astonish?

| Feature | n8n / Flowise | CrewAI / AutoGen | Astonish | 
| ----- | ----- | ----- | ----- | 
| **Setup** | Server-based (Docker) | Python Library | **Single Binary (Go)** | 
| **Logic** | Webhooks/Triggers | LLM "Roleplay" | **Deterministic SOPs** | 
| **Storage** | Database | Python Scripts | **YAML Files** | 
| **Speed** | Moderate | Slow (Python overhead) | **Fast (Goroutines)** | 

## 🏗️ Architecture

Built on **Google's Agent Development Kit (ADK)**, Astonish handles the heavy lifting of provider configuration and tool orchestration. It uses a **State Blackboard** architecture to ensure clean data flow, where Astonish manages the entire connection lifecycle to AI providers.

```mermaid
flowchart TB
    subgraph Astonish["🚀 ASTONISH"]
        YAML["📄 YAML Flow"]
        Studio["🎨 Studio"]
        CLI["⌨️ CLI Runner"]
        
        YAML --> Engine
        Studio --> Engine
        CLI --> Engine
        
        Engine["⚙️ Engine (Go)
        • State Blackboard
        • Parallel Execution
        • Intelligent Retries
        • Error Recovery"]
        
        subgraph Orchestration["🔧 Google ADK"]
            ADK_Core["ADK Core"]
            MCP["🔌 MCP Servers"]
            Tools["🛠️ Built-in Tools"]
            
            ADK_Core --- MCP
            ADK_Core --- Tools
        end

        ProviderLayer["🔐 Astonish Provider Layer
        • Auth & Credential Management
        • Connection Lifecycle
        • API Communication"]
        
        Engine --> ADK_Core
        ADK_Core --> ProviderLayer
    end
    
    ProviderLayer --> Providers["☁️ AI Providers (Gemini, Claude, GPT, Ollama...)"]
```

## 📋 Example: Web Search Assistant

A versatile agent that interacts with users, performs live web searches via MCP tools, and loops until the user is satisfied.

```yaml
name: web_search_summarizer
description: Search the internet and provide a summary of findings

nodes:
  - name: get_query
    type: input
    prompt: "What would you like me to search for?"
    output_model:
      user_query: str

  - name: search_and_summarize
    type: llm
    system: "You are a helpful research assistant. Search the web for the user's query and provide a comprehensive, well-organized summary of the findings. Include key facts, relevant details, and cite sources when possible."
    prompt: "Search for: {user_query}"
    tools: true
    tools_selection:
      - tavily-search
    output_model:
      summary: str
    user_message:
      - summary

  - name: ask_continue
    type: input
    prompt: "Would you like to search for something else?"
    output_model:
      choice: str
    options:
      - "yes"
      - "no"

flow:
  - from: START
    to: get_query
  
  - from: get_query
    to: search_and_summarize
  
  - from: search_and_summarize
    to: ask_continue
  
  - from: ask_continue
    edges:
      - to: get_query
        condition: "lambda x: x['choice'] == 'yes'"
      - to: END
        condition: "lambda x: x['choice'] == 'no'"
```

Run it:

```bash
astonish flows run web_search_assistant
```

---

## 🏪 Flow Store & Taps

Astonish uses a Homebrew-inspired **Tap system**. Anyone can share agent flows or MCP configurations by simply creating a GitHub repository.

```bash
# Add a community repo
astonish tap add schardosin/astonish-flows

# Install a flow
astonish flows store install technical_article_generator

# Run it
astonish flows run technical_article_generator
```

---

## 🎯 Use Cases

• **Infrastructure Observability**. Automate routine CLI tasks, analyze Kubernetes logs, or troubleshoot cluster issues using local tools and AI reasoning.  
• **Personal Knowledge Base (RAG)**. Embed local documentation and source code to allow instant, private semantic search across your entire filesystem.  
• **CI/CD & GitOps Automation**. Run agents directly in GitHub Actions to perform deep code reviews, manage project boards, or automate repo maintenance.  
• **Engineering SOPs**. Transform manual troubleshooting guides into executable agents that anyone on the team can run via a single portable binary.  
• **Workflow Orchestration**. Bridge the gap between local scripts, MCP servers, and enterprise APIs using a single, versionable YAML definition.  

---

## 🤝 Contributing & Support

Built with ❤️ by an engineer who just wanted his agents to work without the boilerplate.

• [Full Documentation](https://schardosin.github.io/astonish/)
• [Submit a Pull Request](https://github.com/schardosin/astonish/pulls)
• **License**. AGPL-3.0

<div align="center">
<b>[⭐ Star us on GitHub](https://github.com/schardosin/astonish)</b>
</div>
