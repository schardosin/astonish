---
title: astonish tools
description: Manage MCP tools and servers
---

# astonish tools

Manage MCP servers and their tools.

## Commands

### tools list

List all available tools:

```bash
astonish tools list
```

Shows tools from all configured MCP servers.

### tools edit

Edit MCP server configuration:

```bash
astonish tools edit
```

Opens your MCP config file in the default editor.

### tools store list

List available MCP servers from the store:

```bash
astonish tools store list
```

### tools store install

Install an MCP server:

```bash
astonish tools store install
```

Launches an interactive installer.

## Configuration Format

```json
{
  "mcpServers": {
    "server-name": {
      "command": "npx",
      "args": ["-y", "package-name@version"],
      "env": {
        "API_KEY": "your-key"
      },
      "transport": "stdio"
    }
  }
}
```

## Examples

### Adding a Web Search Tool

```json
{
  "mcpServers": {
    "tavily-mcp": {
      "command": "npx",
      "args": ["-y", "tavily-mcp@0.1.2"],
      "env": {
        "TAVILY_API_KEY": "tvly-..."
      },
      "transport": "stdio"
    }
  }
}
```

### Adding GitHub Tools

```json
{
  "mcpServers": {
    "github-mcp": {
      "command": "npx",
      "args": ["-y", "github-mcp-server"],
      "env": {
        "GITHUB_TOKEN": "ghp_..."
      },
      "transport": "stdio"
    }
  }
}
```
