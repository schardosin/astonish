<div align="center">
  <img src="[https://raw.githubusercontent.com/schardosin/astonish/main/images/astonish-logo-only.svg](https://raw.githubusercontent.com/schardosin/astonish/main/images/astonish-logo-only.svg)" width="300" height="300" alt="Astonish Logo">
  
  # Astonish: The Declarative AI Orchestration Engine
  
  *High-performance, state-aware agent workflows built in Go.*
  
  [![Go Report Card](https://goreportcard.com/badge/github.com/schardosin/astonish)](https://goreportcard.com/report/github.com/schardosin/astonish)
  [![Build Status](https://github.com/schardosin/astonish/actions/workflows/build.yml/badge.svg)](https://github.com/schardosin/astonish/actions/workflows/build.yml)
  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

</div>

---

**Astonish** is a high-performance, low-code engine for orchestrating sophisticated AI agent workflows. 

Written purely in **Go**, Astonish bridges the gap between raw LLM capabilities and production-grade automation. It allows you to define complex, state-aware, and parallelized agentic flows using simple declarative YAML, turning the probabilistic nature of AI into deterministic business processes.

## Why Astonish? (The Orchestration Layer)

Astonish is designed to work in harmony with powerful execution frameworks like the **Google Gen AI SDK (ADK)**. 

While the ADK provides the incredible "Engine" (handling model connections, tool calling, and multimodal generation), Astonish provides the **"Assembly Line."**

| Feature | Standard Agent SDKs (Google ADK, LangChain) | Astonish Orchestration Engine |
| :--- | :--- | :--- |
| **Philosophy** | **ReAct Loops:** "Here are tools, figure out what to do." | **Deterministic DAGs:** "Follow this specific Standard Operating Procedure (SOP), but use AI to solve the steps." |
| **Memory** | **History-Based:** Appends every step to a growing chat log. Agents often forget details as context grows. | **State Blackboard:** Stores data in variables (`{repo_name}`, `{pr_id}`). Agents access exact data via O(1) lookup. |
| **Concurrency** | **Sequential:** Steps usually run one after another. | **Parallel Map-Reduce:** Uses **Go Routines** to spin up hundreds of concurrent workers (e.g., review 50 files simultaneously). |
| **Quality Control**| **Self-Correction:** The agent hopes to catch its own errors. | **Validator Nodes:** Explicit "Critic" nodes configured in YAML to strictly filter or reject outputs from previous nodes. |

## Key Features

-   **Go-Native Performance**: Ported from Python to Go to leverage lightweight Goroutines. Execute massive parallel workloads (like reviewing every file in a large repo) with negligible overhead.
-   **Declarative YAML Workflows**: Define your agents as Infrastructure-as-Code. Version control your agent logic just like your software.
-   **State Blackboard Architecture**: Avoid "Context Pollution." Pass exact, structured data between nodes without relying on the LLM's short-term memory.
-   **Parallel Execution**: Native support for `forEach` loops in YAML, allowing you to parallelize LLM tasks over lists of data.
-   **Model Context Protocol (MCP)**: Seamlessly integrate with the MCP ecosystem to give your agents access to GitHub, local files, databases, and more.

## Installation

### Install with Go (Recommended)

```bash
go install github.com/schardosin/astonish@latest
```

### Build from Source

```bash
git clone https://github.com/schardosin/astonish.git
cd astonish
go build -o astonish main.go
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
# Example: Injecting a PR number into a reviewer agent
astonish agents run github_pr_reviewer -p pr_number="123" -p repo="astonish"
```

## The Power of Declarative Flows

Astonish flows are defined in YAML. They separate the **Flow Logic** (Edges) from the **Step Logic** (Nodes).

### Example: A Parallel Code Reviewer
*This snippet demonstrates the power of Astonish: It fetches a PR, splits the files, and uses **Go Routines** to review 5 files at once using a "Validator" pattern.*

```yaml
nodes:
  - name: get_pr_files
    type: tool
    args:
      owner: { owner } # Accessing State Blackboard
      repo: { repo }
      pullNumber: { selected_pr }
    output_model:
      files_to_review: list

  # PARALLEL EXECUTION BLOCK
  - name: review_code
    type: llm
    parallel:
      forEach: "{files_to_review}"
      as: "file"
      maxConcurrency: 5 # Spawns 5 Go Routines
    system: |
      You are a Senior Engineer. Review this code for bugs.
    output_model:
      review_comments: list

  # QUALITY GATE / VALIDATOR BLOCK
  - name: validate_reviews
    type: llm
    parallel:
      forEach: "{review_comments}"
      as: "comment"
    system: |
      You are a QA Lead. Discard this comment if it is about formatting only.
    output_model:
      validated_comments: list

flow:
  - from: get_pr_files
    to: review_code
  - from: review_code
    to: validate_reviews
  - from: validate_reviews
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