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

## Managing Repositories (Studio)

### Adding a Repository

1. Open Studio: `astonish studio`
2. Go to **Settings** → **Repositories**
3. Enter the repository URL or `owner/repo` format
4. Optionally set an alias
5. Click **Add Repository**

![Repositories Settings](/astonish/images/studio-repositories.webp)
*Managing tap repositories in Studio*

### Enterprise GitHub

Works with GitHub Enterprise — just enter the full URL:
```
github.enterprise.com/team/flows
```

For private repositories, set your token:
```bash
export GITHUB_ENTERPRISE_TOKEN="ghp_xxx"
```

## Flow Store

Once repositories are added, browse and install flows from the Flow Store:

1. Go to **Settings** → **Flow Store**
2. Filter by repository or search
3. Click **Install** on any flow

![Flow Store](/astonish/images/studio-flow_store.webp)
*Browse and install flows from all configured repositories*

Installed flows appear in your flow list and can be run immediately.

## MCP Dependency Detection

When you open an installed flow, Astonish **automatically detects missing MCP servers** and offers one-click installation:

![MCP Dependency Detection](/astonish/images/studio-mcp_dependency.webp)
*Astonish detects missing MCP servers and offers installation*

### MCP Server Sources

MCP servers can come from three different sources:

| Source | Description |
|--------|-------------|
| **Official MCP Store** | The built-in MCP server store |
| **Tap Store** | Defined in a tap's `manifest.yaml` |
| **Inline** | Custom configuration embedded in flows |

When you use an MCP server in your flow, Astonish tracks its source and embeds this information in the flow YAML. This ensures that when you share a flow, recipients know exactly where to get the required MCP servers.

---

## Managing Repositories (CLI)

### Adding Taps

```bash
astonish tap add <repo>
```

Examples:

```bash
# Add the official tap
astonish tap add schardosin/astonish-flows

# Add a custom tap
astonish tap add mycompany/astonish-flows

# Add with an alias
astonish tap add mycompany/astonish-flows --as company

# Enterprise GitHub
astonish tap add github.enterprise.com/team/flows --as internal
```

### Naming Rules

| You Type | Repo Used | Tap Name |
|----------|-----------|----------|
| `owner` | `owner/astonish-flows` | `owner` |
| `owner/repo` | `owner/repo` | `owner-repo` |
| `owner/repo --as name` | `owner/repo` | `name` |

### Listing Taps

```bash
astonish tap list
```

### Updating Taps

Refresh manifests from all taps:

```bash
astonish tap update
```

### Removing Taps

```bash
astonish tap remove <name>
```

## Browsing Content (CLI)

### List Flows

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

## Next Steps

- **[Share Your Flows](/using-the-app/share-flows/)** — Create your own tap
- **[Troubleshooting](/using-the-app/troubleshooting/)** — Debug common issues
