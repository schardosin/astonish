---
title: "MCP Servers"
description: "Extend Astonish with Model Context Protocol tool servers"
---

MCP (Model Context Protocol) is an open standard for connecting AI agents to external tools. Astonish supports MCP servers that provide additional capabilities beyond the built-in tool set.

## Configuration

MCP servers are configured in `~/.config/astonish/mcp_config.json`. Two transport modes are supported: `stdio` (local process) and `sse` (remote HTTP).

```json
{
  "mcpServers": {
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": { "GITHUB_TOKEN": "ghp_..." },
      "enabled": true
    },
    "remote-server": {
      "transport": "sse",
      "url": "https://example.com/mcp",
      "enabled": true
    }
  }
}
```

## Installing from the MCP Store

Studio includes a built-in MCP store browser. From the CLI, use:

```bash
astonish tools store
```

## Managing Servers

| Command | Description |
|---------|-------------|
| `astonish tools servers` | List all MCP servers with status |
| `astonish tools enable <name>` | Enable a server |
| `astonish tools disable <name>` | Disable a server |
| `astonish tools refresh` | Reconnect and refresh tool cache |
| `astonish tools edit` | Edit MCP config directly |

## How Tools Appear

Tools from MCP servers appear alongside built-in tools. The agent uses them seamlessly without any distinction — you simply describe what you need, and the agent selects the appropriate tool whether it is built-in or provided by an MCP server.

## Flow Dependencies

Flows can declare MCP dependencies that get auto-installed when the flow runs. See the [YAML Reference](/flows/yaml-reference/) for the `mcp_dependencies` syntax.
