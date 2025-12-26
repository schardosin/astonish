---
title: Taps
description: Understanding tap repositories in Astonish
sidebar:
  order: 5
---

# Taps

**Taps** are extension repositories that provide flows and MCP server configurations. They're like package registries for Astonish content.

## What is a Tap?

A tap is a GitHub repository containing:
- **Flows** — Ready-to-use AI workflows
- **MCP Configs** — Pre-configured tool integrations
- **Manifest** — Index of available content

```
my-tap/
├── manifest.yaml
├── flows/
│   ├── summarizer.yaml
│   └── analyzer.yaml
└── mcp/
    └── custom-tool.json
```

## How Taps Work

```
┌──────────────────┐     ┌──────────────────┐
│  GitHub Repo     │ ──► │  Local Storage   │
│  (your-tap)      │     │  ~/.astonish/    │
└──────────────────┘     └──────────────────┘
         │                        │
    manifest.yaml           store/<tap>/
    flows/*.yaml           installed flows
```

1. You add a tap with `astonish tap add`
2. Astonish fetches the manifest
3. You browse and install flows
4. Flows are copied locally

## The Official Tap

Maintained by the Astonish team:

```bash
astonish tap add schardosin
```

Repository: `schardosin/astonish-flows`

Contains curated example flows and MCP configs.

## Tap Commands

| Command | Description |
|---------|-------------|
| `astonish tap add <repo>` | Add a tap |
| `astonish tap list` | List configured taps |
| `astonish tap remove <name>` | Remove a tap |
| `astonish tap update` | Refresh all manifests |

## Adding Taps

### Basic syntax

```bash
astonish tap add owner/repository
```

### Shorthand

```bash
astonish tap add owner
# Expands to: owner/astonish-flows
```

### With Alias

```bash
astonish tap add mycompany/internal-flows --as internal
```

### Enterprise GitHub

```bash
astonish tap add github.enterprise.com/team/flows --as team
```

## Browsing Content

### List All Flows

```bash
astonish flows store list
```

### Search

```bash
astonish flows store search "github"
```

### Install

```bash
astonish flows store install owner/flow-name
```

## Creating Taps

Share your flows by creating a tap:

### 1. Create Repository

Create a GitHub repo (e.g., `your-username/astonish-flows`)

### 2. Add Content

```
your-tap/
├── manifest.yaml
├── flows/
│   └── my-flow.yaml
└── mcp/
    └── my-tool.json
```

### 3. Create Manifest

```yaml
name: your-flows
description: My collection of flows
author: your-username

flows:
  - name: my-flow
    file: flows/my-flow.yaml
    description: Does something useful

mcp_servers:
  - name: my-tool
    file: mcp/my-tool.json
    description: Custom tool integration
```

### 4. Share

```bash
astonish tap add your-username
```

## Tap Storage

| File/Directory | Purpose |
|----------------|---------|
| `~/.astonish/store.json` | Configured tap list |
| `~/.astonish/store/<tap>/` | Installed content |

## Private Taps

For team use:

1. Use a private GitHub repository
2. Ensure team members have access
3. Set token:
```bash
export GITHUB_TOKEN="ghp_xxx"
```

## Best Practices

1. **Version your manifests** — Track changes
2. **Document flows** — Clear descriptions
3. **Test before sharing** — Ensure flows work
4. **Use semantic names** — `github_pr_reviewer` not `flow1`

## Next Steps

- **[Manage Taps](/using-the-app/manage-taps/)** — Detailed tap management
- **[Share Your Flows](/using-the-app/share-flows/)** — Create your own tap
