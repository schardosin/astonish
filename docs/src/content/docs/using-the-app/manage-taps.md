---
title: Manage Tap Repositories
description: Add, browse, and manage tap repositories for flows and MCP servers
sidebar:
  order: 3
---

# Manage Tap Repositories

**Taps** are extension repositories that provide flows and MCP server configurations. Think of them like package registries for Astonish.

## What Are Taps?

Taps are GitHub repositories containing:
- **Flows** — Ready-to-use AI workflows
- **MCP Configs** — Pre-configured tool integrations

The official tap is `schardosin/astonish-flows`.

## Adding Taps

### Basic Syntax

```bash
astonish tap add <repo>
```

### Examples

```bash
# Add the official tap
astonish tap add schardosin/astonish-flows

# Add a custom tap
astonish tap add mycompany/astonish-flows

# Add with an alias
astonish tap add mycompany/astonish-flows --as company
```

### Naming Rules

| You Type | Repo Used | Tap Name |
|----------|-----------|----------|
| `owner` | `owner/astonish-flows` | `owner` |
| `owner/repo` | `owner/repo` | `owner-repo` |
| `owner/repo --as name` | `owner/repo` | `name` |

### Enterprise GitHub

Taps work with GitHub Enterprise:

```bash
astonish tap add github.enterprise.com/team/flows --as internal
```

Set your token:
```bash
export GITHUB_ENTERPRISE_TOKEN="ghp_xxx"
```

## Listing Taps

```bash
astonish tap list
```

Output:
```
CONFIGURED TAPS

  schardosin (official)
    https://github.com/schardosin/astonish-flows
    Branch: main

  company
    https://github.enterprise.com/team/flows
    Branch: main
```

## Updating Taps

Refresh manifests from all taps:

```bash
astonish tap update
```

This fetches the latest list of available flows.

## Removing Taps

```bash
astonish tap remove <name>
```

Example:
```bash
astonish tap remove company
```

## Browsing Tap Content

### List Flows from Taps

```bash
astonish flows store list
```

### Search Flows

```bash
astonish flows store search "github"
```

### Install a Flow

```bash
astonish flows store install schardosin/text_to_speech
```

### Browse MCP Servers

```bash
astonish tools store list
```

## The Official Tap

The `schardosin/astonish-flows` tap is maintained by the Astonish team:

```bash
astonish tap add schardosin
```

Contains:
- Curated example flows
- Common MCP server configurations
- Community contributions

## Creating Your Own Tap

Share your flows with others by creating a tap:

1. Create a GitHub repository
2. Add your flow YAML files
3. Create a `manifest.yaml`:

```yaml
name: my-flows
description: My collection of useful flows

flows:
  - name: summarizer
    file: summarizer.yaml
    description: Summarizes long text
  
  - name: translator
    file: translator.yaml
    description: Translates between languages

mcp_servers:
  - name: custom-tool
    file: mcp/custom-tool.json
    description: My custom MCP server
```

4. Others can add your tap:

```bash
astonish tap add yourname/your-repo
```

See **[Share Your Flows](/using-the-app/share-flows/)** for detailed instructions.

## Tap Storage

Taps are tracked in:
```
~/.astonish/store.json
```

Installed flows go to:
```
~/.astonish/store/<tap-name>/
```

## Next Steps

- **[Share Your Flows](/using-the-app/share-flows/)** — Create your own tap
- **[Troubleshooting](/using-the-app/troubleshooting/)** — Debug common issues
