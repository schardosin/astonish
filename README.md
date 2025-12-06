<div align="center">
  <img src="https://raw.githubusercontent.com/schardosin/astonish/main/images/astonish-logo-only.svg" width="300" height="300" alt="Astonish Logo">
  
  # Astonish: The Declarative AI Orchestration Engine
  
  *High-performance, state-aware agent workflows built in Go.*
  
  [![Go Report Card](https://goreportcard.com/badge/github.com/schardosin/astonish)](https://goreportcard.com/report/github.com/schardosin/astonish)
  [![Build Status](https://github.com/schardosin/astonish/actions/workflows/build.yml/badge.svg)](https://github.com/schardosin/astonish/actions/workflows/build.yml)
  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

</div>

---

**Astonish** is a high-performance, low-code engine for orchestrating sophisticated AI agent workflows. 

Written purely in **Go**, Astonish bridges the gap between raw LLM capabilities and production-grade automation. It allows you to define complex, state-aware agentic flows using simple declarative YAML, turning the probabilistic nature of AI into deterministic business processes.

## Why Astonish? (The Orchestration Layer)

Astonish is designed to work in harmony with powerful execution frameworks like the **Google Gen AI SDK (ADK)**. 

While the ADK provides the incredible "Engine" (handling model connections, tool calling, and multimodal generation), Astonish provides the **"Assembly Line."**

| Feature | Standard Agent SDKs (Google ADK, LangChain) | Astonish Orchestration Engine |
| :--- | :--- | :--- |
| **Philosophy** | **ReAct Loops:** "Here are tools, figure out what to do." | **Deterministic DAGs:** "Follow this specific Standard Operating Procedure (SOP), but use AI to solve the steps." |
| **Memory** | **History-Based:** Appends every step to a growing chat log. Agents often forget details as context grows. | **State Blackboard:** Stores data in variables (`{file_path}`, `{summary}`). Agents access exact data via O(1) lookup. |
| **Concurrency** | **Sequential:** Steps usually run one after another. | **Parallel Map-Reduce:** Uses **Go Routines** to spin up hundreds of concurrent workers (e.g., process 50 items simultaneously). |
| **Quality Control**| **Self-Correction:** The agent hopes to catch its own errors. | **Validator Nodes:** Explicit "Critic" nodes configured in YAML to strictly filter or reject outputs from previous nodes. |

## Key Features

-   **Go-Native Performance**: Ported from Python to Go to leverage lightweight Goroutines. Execute massive parallel workloads with negligible overhead.
-   **Declarative YAML Workflows**: Define your agents as Infrastructure-as-Code. Version control your agent logic just like your software.
-   **State Blackboard Architecture**: Avoid "Context Pollution." Pass exact, structured data between nodes without relying on the LLM's short-term memory.
-   **Parallel Execution**: Native support for `forEach` loops in YAML, allowing you to parallelize LLM tasks over lists of data.
-   **Model Context Protocol (MCP)**: Seamlessly integrate with the MCP ecosystem to give your agents access to GitHub, local files, databases, and more.

## Installation

### Install with Homebrew (Recommended)

```bash
brew tap schardosin/astonish
brew install astonish
```

### Alternative: Install with Go

```bash
go install github.com/schardosin/astonish@latest
```

### Alternative: Build from Source

```bash
git clone https://github.com/schardosin/astonish.git
cd astonish
go build -o astonish .
```

## Quick Start

### 1. Setup

Configure your AI providers (Google Gemini, Anthropic, OpenAI, etc.) and MCP servers.

```bash
astonish setup
```

### 2. Create an Agent (The AI Architect)

Don't want to write YAML from scratch? Let Astonish write it for you.

```bash
astonish agents run agents_creator
```
*Describe your goal, and the system will generate a valid YAML workflow for you.*

### 3. Run a Workflow

```bash
astonish agents run <agent_name>
```

You can also inject runtime variables directly into the State Blackboard:

```bash
# Example: Injecting a question into a generic agent
astonish agents run simple_question_answer -p question="Who was Steve Jobs?"
```

## The Power of Declarative Flows

Astonish flows are defined in YAML. They separate the **Flow Logic** (Edges) from the **Step Logic** (Nodes).

### Example: A File Summarizer
*This snippet demonstrates the core logic of Astonish: Gathering input, using a specific tool (reading a file), and passing that specific data to an LLM for summarization using the State Blackboard (`{file_content}`).*

```yaml
description: An agent that reads a file and summarizes it.
nodes:
  - name: get_file_path
    type: input
    prompt: |
      Please enter the path to the file you want to analyze:
    output_model:
      file_path: str

  - name: read_file_content
    type: llm
    system: |
      You are a file reading assistant.
    prompt: |
      Read the contents of the file at path: {file_path}
    output_model:
      file_content: str
    tools: true
    tools_selection:
      - read_file

  - name: extract_summary
    type: llm
    system: |
      You are an AI assistant specialized in summarizing text.
    prompt: |
      Analyze the following file content and extract the core information:

      {file_content}

      Provide a concise summary.
    output_model:
      summary: str

  - name: present_summary
    type: output
    user_message:
      - summary

flow:
  - from: START
    to: get_file_path
  - from: get_file_path
    to: read_file_content
  - from: read_file_content
    to: extract_summary
  - from: extract_summary
    to: present_summary
  - from: present_summary
    to: END
```

## Supported AI Providers

Astonish acts as a neutral orchestrator, supporting major providers via standard APIs.

| Provider | Status | 
|----------|--------|
| **Google AI (Gemini)** | **First-Class Support** |
| Anthropic | Supported |
| OpenAI | Supported |
| Groq | Supported |
| Ollama (Local) | Supported |
| LM Studio (Local) | Supported |
| OpenRouter | Supported |
| X AI | Supported |

## Project Structure

- `core/`: The Go-based orchestration engine.
- `engine/runner.go`: Handles the DAG execution and State Blackboard management.
- `mcp/`: Client implementation for Model Context Protocol.
- `agents/`: Directory where your declarative YAML agents live.

## Contributing

We are building the standard for deterministic agent orchestration.

1. Fork the repository
2. Create a feature branch
3. Submit a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.