---
title: Flow Store & Taps
description: Share and discover agent flows with Homebrew-style repositories
---

# Flow Store & Taps

Astonish includes a **Flow Store** and **Tap system**—a Homebrew-inspired approach for sharing AI agent flows and MCP server configurations.

## What are Taps?

Taps are GitHub repositories that provide:
- **Flows**: Ready-to-use agent configurations
- **MCP Servers**: Tool configurations and dependencies

Think of them like Homebrew taps, but for AI agents.

## Using the Flow Store

### Browse Available Flows

```bash
# List flows from all repositories
astonish flows store list
```

In Astonish Studio, browse the **Flow Store** in the sidebar.

### Install a Flow

```bash
# Install a flow
astonish flows store install github_pr_description_generator

# Run the installed flow
astonish agents run github_pr_description_generator
```

## Managing Taps

### Add a Repository

```bash
# Add a community tap
astonish tap add schardosin/astonish-flows

# Add with a custom alias
astonish tap add mycompany/ai-tools --as company

# Add from GitHub Enterprise
astonish tap add github.mycompany.com/team/extensions
```

### List Taps

```bash
astonish tap list
```

### Remove a Tap

```bash
astonish tap remove company
```

### Update All Taps

```bash
astonish tap update
```

## Creating Your Own Tap

Share your flows and MCP servers with the community!

### 1. Create a Repository

Create a GitHub repository with a `manifest.yaml`:

```yaml
name: My Awesome Extensions
author: your-username
description: A collection of flows and MCP servers

flows:
  code_reviewer:
    description: Reviews code and suggests improvements
    tags: [development, code-quality]
  
  daily_standup:
    description: Generates daily standup summaries
    tags: [productivity, team]

mcps:
  my-database-server:
    description: Custom database integration
    command: npx
    args: ["-y", "my-database-mcp@latest"]
    env:
      DB_HOST: ""
      DB_PASSWORD: ""
    tags: [database, integration]
```

### 2. Add Flow Files

Add your flow YAML files to the repository:

```
your-repo/
├── manifest.yaml
├── code_reviewer.yaml
├── daily_standup.yaml
└── README.md
```

### 3. Share It

Others can now tap your repository:

```bash
astonish tap add your-username/your-repo
astonish flows store install your-username/code_reviewer
```

## Enterprise GitHub

For private repositories on GitHub Enterprise:

```bash
# Set your enterprise token
export GITHUB_ENTERPRISE_TOKEN=ghp_xxxxx

# Add the tap
astonish tap add github.mycompany.com/team/extensions
```

### Environment Variables

| Variable | Purpose |
|----------|---------|
| `GITHUB_TOKEN` | Public GitHub authentication |
| `GITHUB_ENTERPRISE_TOKEN` | Enterprise GitHub authentication |

## Studio Integration

Taps are also manageable in **Astonish Studio**:

- **Settings → Repositories**: Add, remove, and manage taps
- **Flow Store**: Browse and install flows from all taps
- **MCP Servers**: Install MCPs from official store + taps
