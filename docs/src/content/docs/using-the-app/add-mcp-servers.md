---
title: Add MCP Servers
description: Connect external tools to your Astonish flows via MCP
sidebar:
  order: 2
---

# Add MCP Servers

**MCP (Model Context Protocol)** is a standard for connecting AI models to external tools. Add MCP servers to give your flows access to GitHub, Slack, databases, and more.

## What is MCP?

MCP servers provide **tools** that your flows can use:

| Server | Tools |
|--------|-------|
| **GitHub** | Read PRs, create issues, review code |
| **Slack** | Send messages, read channels |
| **Filesystem** | Read/write files |
| **Database** | Query SQL databases |
| **Web** | Search, scrape, extract |

Learn more at [modelcontextprotocol.io](https://modelcontextprotocol.io/).

## Method 1: MCP Store (Studio)

Browse and install from the built-in store:

1. Open Studio: `astonish studio`
2. Go to **Settings** → **MCP Servers**
3. Click **Browse Store**
4. Select a server and click **Install**

![MCP Store](/src/assets/placeholder.png)
*The MCP Server Store in Studio*

## Method 2: CLI Store

```bash
# List available servers
astonish tools store list

# Install a server
astonish tools store install github-mcp-server
```

## Method 3: Manual Configuration

Edit the MCP config file:

```bash
astonish tools edit
```

Or edit directly:

```bash
# macOS
code ~/Library/Application\ Support/astonish/mcp_config.json

# Linux
code ~/.config/astonish/mcp_config.json
```

### Configuration Format

```json
{
  "mcpServers": {
    "server-name": {
      "command": "npx",
      "args": ["-y", "package-name"],
      "env": {
        "API_KEY": "your-key"
      },
      "transport": "stdio"
    }
  }
}
```

### Properties

| Property | Description |
|----------|-------------|
| `command` | Executable to run (`npx`, `uvx`, `docker`) |
| `args` | Command arguments |
| `env` | Environment variables |
| `transport` | `stdio` (standard) or `sse` (streaming) |

## Common MCP Servers

### GitHub

```json
{
  "mcpServers": {
    "github": {
      "command": "docker",
      "args": [
        "run", "-i", "--rm",
        "-e", "GITHUB_PERSONAL_ACCESS_TOKEN",
        "ghcr.io/github/github-mcp-server"
      ],
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_xxxx"
      },
      "transport": "stdio"
    }
  }
}
```

**Tools:** `create_issue`, `read_pull_request`, `create_comment`, etc.

### Tavily (Web Search)

```json
{
  "mcpServers": {
    "tavily": {
      "command": "npx",
      "args": ["-y", "tavily-mcp@latest"],
      "env": {
        "TAVILY_API_KEY": "tvly-xxxx"
      },
      "transport": "stdio"
    }
  }
}
```

**Tools:** `web_search`, `web_extract`

### Filesystem

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@anthropic/mcp-fs", "/path/to/directory"],
      "env": {},
      "transport": "stdio"
    }
  }
}
```

**Tools:** `read_file`, `write_file`, `list_directory`

### Python Execution

```json
{
  "mcpServers": {
    "python": {
      "command": "uvx",
      "args": ["mcp-run-python", "stdio"],
      "env": {},
      "transport": "stdio"
    }
  }
}
```

**Tools:** `run_python`

## Verifying Installation

List installed tools:

```bash
astonish tools list
```

Output:
```
MCP Servers:
  github (4 tools)
    - create_issue
    - read_pull_request
    - create_comment
    - list_repositories
  
  tavily (2 tools)
    - web_search
    - web_extract
```

## Using Tools in Flows

Enable tools in an LLM node:

```yaml
nodes:
  - name: research
    type: llm
    prompt: "Search for recent news about {topic}"
    tools: true
    tools_selection:
      - web_search
```

The AI can now use `web_search` to answer the prompt.

### Whitelist Specific Tools

Limit which tools the AI can use:

```yaml
tools_selection:
  - web_search
  - web_extract
```

### Direct Tool Nodes

Call tools without AI:

```yaml
- name: search
  type: tool
  tools_selection:
    - web_search
```

## Troubleshooting

### Server Not Starting

Check the command works directly:

```bash
npx -y tavily-mcp@latest
```

### Missing API Key

Verify environment variables:

```json
"env": {
  "TAVILY_API_KEY": "tvly-xxxx"
}
```

### Tool Not Found

Run `astonish tools list` to verify the server is loaded and see available tools.

## Next Steps

- **[Manage Taps](/using-the-app/manage-taps/)** — Find community MCP configs
- **[Key Concepts: MCP](/concepts/mcp/)** — Deep dive into MCP
