---
title: Using MCP Tools
description: Learn how to integrate MCP tools into your agents
---

# Using MCP Tools

In this tutorial, you'll learn how to extend your agents with MCP (Model Context Protocol) tools.

## What is MCP?

MCP servers provide tools that your agents can use. Examples:
- **Web search** (Tavily)
- **GitHub API** (GitHub MCP)
- **Browser automation** (Puppeteer)
- **Databases** (SQLite, PostgreSQL)

## Installing an MCP Server

### Via CLI

```bash
astonish tools store install
```

This opens an interactive installer. Select a server (e.g., `tavily-mcp`).

### Via Studio

1. Open **Settings → MCP Servers**
2. Click **Add Server** or browse the **MCP Store**
3. Configure and save

### Manual Configuration

Edit MCP config:

```bash
astonish tools edit
```

Add the server:

```json
{
  "mcpServers": {
    "tavily-mcp": {
      "command": "npx",
      "args": ["-y", "tavily-mcp@0.1.2"],
      "env": {
        "TAVILY_API_KEY": "tvly-your-api-key"
      },
      "transport": "stdio"
    }
  }
}
```

## Using Tools in Your Flow

Once installed, reference tools in your nodes:

```yaml
nodes:
  - name: search_web
    type: llm
    prompt: "Search the web for information about {topic}"
    tools: true
    tools_selection:
      - search    # Tool from tavily-mcp
    output_model:
      results: str
```

### How It Works

1. The LLM receives the available tools
2. It decides when to use them
3. Astonish executes the tool call
4. Results are passed back to the LLM

## Example: Research Agent

An agent that searches the web and summarizes findings:

```yaml
name: research_agent
description: Search the web and summarize findings

nodes:
  - name: search
    type: llm
    prompt: |
      Search the web for: {query}
      Find 3-5 relevant sources.
    tools: true
    tools_selection:
      - search
    output_model:
      search_results: str

  - name: summarize
    type: llm
    system: You are a research assistant.
    prompt: |
      Summarize these search results into a clear report:
      {search_results}
    output_model:
      summary: str
    user_message:
      - summary

flow:
  - from: START
    to: search
  - from: search
    to: summarize
  - from: summarize
    to: END
```

## Tool Selection

### All Tools

Enable all available tools:

```yaml
tools: true
# No tools_selection = all tools available
```

### Specific Tools

Limit to specific tools:

```yaml
tools: true
tools_selection:
  - search
  - read_file
```

This is recommended for:
- Security (limit what the agent can do)
- Focus (prevent distraction from irrelevant tools)
- Performance (fewer tools = faster decisions)

## Troubleshooting

### "Tool not found"

1. Verify the MCP server is configured: `astonish tools list`
2. Check the server is running correctly
3. Ensure environment variables are set

### Server Fails to Start

Check your MCP config:
- Is the command correct?
- Are all required env vars set?
- Try running the command manually

### Tool Timeouts

Some tools take time. The LLM will retry or report the issue.

## Next Steps

- [MCP Integration](/concepts/mcp/) — Deep dive into MCP
- [Flow Store](/concepts/taps/) — Find MCP servers from the community
