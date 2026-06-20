# MCP Servers

The Model Context Protocol (MCP) allows Astonish to connect to external tool servers, extending the agent's capabilities beyond its 58+ built-in tools.

## What Is MCP?

MCP is an open protocol for AI tool integration. An MCP server exposes tools (functions with schemas) that the agent can discover and invoke at runtime. This enables:

- Third-party tool integrations without modifying the agent
- Team-specific internal tools
- Dynamic tool discovery and versioning

## Configuration

MCP servers are defined in `~/.config/astonish/mcp_config.json`. In cloud deployments, MCP servers can also be managed per-team through the platform admin API.

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/home/user/projects"],
      "transport": "stdio"
    },
    "github": {
      "command": "gh-mcp-server",
      "args": [],
      "env": {
        "GITHUB_TOKEN": "${GITHUB_TOKEN}"
      },
      "transport": "stdio"
    },
    "remote-tools": {
      "url": "https://tools.example.com/mcp",
      "transport": "sse",
      "headers": {
        "Authorization": "Bearer ${MCP_TOKEN}"
      }
    }
  }
}
```

## Transport Types

### stdio

The server runs as a child process. Astonish communicates via stdin/stdout. Best for local tools.

```json
{
  "command": "path/to/server",
  "args": ["--flag"],
  "env": {"KEY": "value"},
  "transport": "stdio"
}
```

### SSE (Server-Sent Events)

The server is a remote HTTP endpoint. Astonish connects over the network. Best for shared team servers.

```json
{
  "url": "https://mcp.internal.company.com/sse",
  "transport": "sse",
  "headers": {
    "Authorization": "Bearer ${TOKEN}"
  }
}
```

## Cloud Deployment

In cloud deployments, MCP servers can be scoped at multiple levels:

| Level | Managed By | Visibility |
|-------|-----------|------------|
| Platform | Platform admin | All users |
| Org | Org admin | Org members |
| Team | Team admin | Team members |
| Personal | Individual user | Self only |

Admin-managed servers cannot be removed by users. They appear alongside any personal servers the user has configured.

```json
{
  "mcpServers": {
    "internal-api": {
      "url": "https://tools.corp.com/mcp",
      "transport": "sse",
      "managed": true,
      "scope": "org"
    }
  }
}
```

## Tool Caching

Astonish caches MCP tool schemas locally to avoid re-discovery on every session start. The cache is invalidated when the server configuration changes or on explicit refresh:

```bash
astonish mcp refresh
astonish mcp list
```

## Best Practices

- Use `stdio` transport for development and local tools
- Use `sse` transport for production shared servers
- Keep sensitive tokens in environment variables, not inline
- In cloud deployments, prefer team-scoped servers over personal for shared tooling

See [Tools Overview](../agent/tools/index.md) for how MCP tools integrate with the built-in tool system.
