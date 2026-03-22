---
title: "Taps & Flow Store"
description: "Extension repositories for flows and MCP server configs"
---

Taps are Git repositories that provide flows and MCP server configurations for Astonish. They are the primary mechanism for sharing and distributing automation workflows.

## The Official Tap

The official tap is `schardosin/astonish-flows`.

## Adding Taps

```bash
# Add the official tap
astonish tap add schardosin/astonish-flows

# Add a tap from a custom host with a local alias
astonish tap add github.enterprise.com/org/repo --as my-tap
```

In Studio, go to **Settings > Taps** to manage tap repositories.

## Managing Taps

| Command | Description |
|---------|-------------|
| `astonish tap list` | List all configured taps |
| `astonish tap remove <name>` | Remove a tap |
| `astonish tap update` | Update all tap manifests |

## Installing Flows from Taps

| Command | Description |
|---------|-------------|
| `astonish flows store list` | Browse available flows |
| `astonish flows store install <name>` | Install a flow |
| `astonish flows store search <query>` | Search for flows |

## Creating a Tap Repository

To publish your own flows, create a Git repository with the following structure:

```
my-tap/
  manifest.yaml
  flows/
    my-flow.yaml
    another-flow.yaml
```

The `manifest.yaml` file lists the available flows and MCP configurations provided by the tap. The `flows/` directory contains the YAML flow definitions.
