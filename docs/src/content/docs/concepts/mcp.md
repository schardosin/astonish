---
title: MCP Integration
description: Connecting AI agents to external tools via Model Context Protocol
---

# MCP Integration

Astonish integrates with the **Model Context Protocol (MCP)** to give your agents access to powerful tools—GitHub, databases, file systems, web search, and more.

## What is MCP?

MCP (Model Context Protocol) is a standard for connecting AI models to external capabilities. Think of MCP servers as "plugins" that add tools your agents can use.

## Built-in Tools

Astonish includes some built-in tools:

| Tool | Description |
|------|-------------|
| `shell_command` | Execute shell commands |
| `read_file` | Read file contents |
| `write_file` | Write to files |

## Adding MCP Servers

### Via Astonish Studio

1. Open Settings → MCP Servers
2. Click "Add Server"
3. Configure the server command & args
4. Save

### Via CLI

Edit your MCP configuration:

```bash
astonish tools edit
```

This opens your MCP config file. Add servers in JSON format:

```json
{
  "mcpServers": {
    "tavily-mcp": {
      "command": "npx",
      "args": ["-y", "tavily-mcp@0.1.2"],
      "env": {
        "TAVILY_API_KEY": "<your-api-key>"
      },
      "transport": "stdio"
    }
  }
}
```

## Using MCP Tools in Flows

Once an MCP server is configured, its tools become available:

```yaml
nodes:
  - name: search_web
    type: llm
    prompt: "Search the web for: {query}"
    tools: true
    tools_selection:
      - search       # Tool from tavily-mcp
```

## MCP Store

Install popular MCP servers with one click:

```bash
# Browse available servers
astonish tools store list

# Install a server
astonish tools store install
```

Or use Astonish Studio's **MCP Store** interface.

## Popular MCP Servers

| Server | Description |
|--------|-------------|
| `tavily-mcp` | Web search |
| `github-mcp` | GitHub API access |
| `sqlite-mcp` | SQLite database |
| `puppeteer-mcp` | Browser automation |

## Environment Variables

MCP servers often require API keys. Configure them:

1. In the MCP config's `env` section
2. Or as system environment variables

Sensitive values are never logged or displayed in the UI.

## Dependency Resolution

When you save a flow, Astonish automatically detects which MCP servers are required based on your `tools_selection`:

```yaml
mcp_dependencies:
  - server: tavily-mcp
    tools:
      - search
    source: store
    store_id: official/tavily-mcp
```

This metadata helps with:
- Showing which dependencies are missing
- One-click installation from the Canvas
- Sharing flows that work out of the box
