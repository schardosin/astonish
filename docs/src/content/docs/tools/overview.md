---
title: Tools Overview
description: How Astonish uses tools to interact with the world
---

Astonish ships with **74+ built-in tools** across 11 categories, and supports unlimited extension through MCP servers. Tools are how the AI agent takes action — reading files, running commands, browsing the web, sending emails, and more.

## How tools work

During a chat, the AI agent decides which tools to call based on your task. It sends a tool call with parameters, receives structured results, and uses those results to formulate its response. A single task may involve multiple tool calls chained together.

## Tool categories

| Category | Tools | Examples |
|----------|-------|---------|
| File & Search | 9 | Read, write, edit files; search code; browse directories |
| Shell & Process | 6 | Run commands, manage long-running processes, delegate to OpenCode |
| Web & HTTP | 2 | Fetch web pages, make API calls with credential injection |
| Browser | 32 | Full browser automation — navigate, click, type, screenshot, stealth |
| Email | 8 | Read, send, reply, search, wait for emails |
| Memory | 3 | Save, search, and retrieve persistent knowledge |
| Credential | 5 | Manage encrypted credentials with automatic auth injection |
| Scheduler | 4 | Create and manage cron-based scheduled tasks |
| Agent | 3 | Delegate tasks, look up skills, distill flows |
| Fleet | 2 | Save and validate fleet plans |

## Tool approval

By default, the agent asks for your confirmation before executing any tool. This gives you control over what actions are taken on your behalf.

To skip confirmation prompts:

- Set `chat.auto_approve: true` in your config file
- Pass the `--auto-approve` flag when starting a chat

## MCP tools

Extend Astonish with external tool servers via the [Model Context Protocol](https://modelcontextprotocol.io/). MCP servers add new capabilities without modifying the core application.

Configure MCP servers in `mcp_config.json`, or browse and install them from the built-in MCP Store.

## CLI commands

**List all available tools** (built-in + MCP):

```bash
astonish tools list
astonish tools list --json   # Machine-readable output
```

**Manage MCP servers:**

```bash
astonish tools servers              # List configured MCP servers
astonish tools enable <name>        # Enable an MCP server
astonish tools disable <name>       # Disable an MCP server
astonish tools refresh              # Refresh MCP tool cache
```
