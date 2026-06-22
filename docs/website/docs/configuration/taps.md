# Taps & Flow Store

Taps are git-based extension repositories that distribute community flows, skills, and MCP server configurations for Astonish.

## What Are Taps?

A tap is a git repository containing reusable flows and tools. When you add a tap, Astonish fetches its manifest and makes its contents available for browsing and installation.

## Adding a Tap

```bash
# Add by full URL
astonish tap add https://github.com/example/astonish-flows.git

# Add by GitHub shorthand (owner/repo)
astonish tap add example/astonish-flows

# Add with a custom alias
astonish tap add example/astonish-flows --as my-flows
```

## Managing Taps

```bash
# List installed taps
astonish tap list

# Update all tap manifests to latest
astonish tap update

# Remove a tap
astonish tap remove <name>
```

## Browsing and Installing Flows

After adding taps, browse and install available flows:

```bash
# List all flows from all taps
astonish flows store list

# Install a flow from a tap
astonish flows store install <tap-name>/<flow-name>

# Update all tap manifests
astonish flows store update
```

You can also browse and install flows from the **Flow Store** in Studio Settings.

## Tap Repository Structure

A tap repository follows this layout:

```
my-tap/
├── tap.yaml            # Tap metadata
├── flows/
│   ├── web-scraper/
│   │   └── flow.yaml
│   └── data-pipeline/
│       └── flow.yaml
└── skills/
    └── docker-expert.md
```

## Creating Your Own Tap

Create a git repository with a `tap.yaml` at the root:

```yaml
name: "my-team-flows"
description: "Internal flows for the engineering team"
version: "1.0.0"
```

Add flow directories under `flows/` and push to any git host. Team members can then add your tap by URL.

See [Skills](../agent/skills.md) for how tap-distributed skills integrate with the agent.
