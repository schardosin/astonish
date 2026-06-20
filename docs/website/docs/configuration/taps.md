# Taps & Flow Store

Taps are git-based extension repositories that distribute community flows, skills, and agent configurations for Astonish.

## What Are Taps?

A tap is a git repository containing reusable flows (agent instruction sets). When you add a tap, Astonish clones it locally and makes its contents available for use.

## Adding a Tap

```bash
astonish tap add https://github.com/example/astonish-flows.git
```

This clones the repository to `~/.config/astonish/taps/` and indexes its flows.

## Managing Taps

```bash
# List installed taps
astonish tap list

# Update all taps to latest
astonish tap update

# Update a specific tap
astonish tap update example/astonish-flows

# Remove a tap
astonish tap remove example/astonish-flows
```

## Listing Available Flows

After adding taps, browse installable flows:

```bash
# List all flows from all taps
astonish flow list --available

# Install a flow
astonish flow install devops/k8s-debug

# List installed flows
astonish flow list
```

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
