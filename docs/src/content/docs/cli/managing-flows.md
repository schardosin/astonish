---
title: Managing Flows
description: List, install, and organize flows from the command line
sidebar:
  order: 3
---

# Managing Flows

Use the CLI to discover, install, and organize your flow collection.

## Listing Local Flows

```bash
astonish flows list
```

Shows all flows on your system:

```
AVAILABLE FLOWS

📁 LOCAL (2)
  summarizer
    Summarizes text input
  analyzer
    Analyzes data patterns

📦 community (3)
  translator
    Translates between languages
  code_reviewer
    Reviews code quality
  email_drafter
    Drafts professional emails

Total: 5 flows
```

## Flow Store

Browse community flows from taps:

```bash
# List all available flows from taps
astonish flows store list

# Search for flows
astonish flows store search "github"
```

## Installing Flows

Install a flow from a tap:

```bash
astonish flows store install <tap-name>/<flow-name>
```

Example:
```bash
astonish flows store install community/translator
```

The flow is installed to your local taps directory.

## Uninstalling Flows

Remove an installed flow:

```bash
astonish flows store uninstall community/translator
```

:::caution
Only removes installed tap flows. Local flows must be deleted manually from your flows directory.
:::

## Updating Flows

Update all tap manifests:

```bash
astonish flows store update
```

This refreshes the list of available flows from all your taps.

## Creating a New Flow

Create a YAML file and import it:

```bash
astonish flows import my_flow.yaml
```

The flow is added to your flows directory and appears in `astonish flows list`.

:::tip[Prefer Visual?]
Use `astonish studio` to create flows with a visual editor instead.
:::

## Viewing Flow Details

### Show Structure

```bash
astonish flows show translator
```

Displays the flow as a text diagram.

### View Raw YAML

Use the edit command to view the raw file:

```bash
astonish flows edit translator
```

## Flow File Locations

To find where flows are stored:

```bash
astonish config directory
```

Local flows are in `flows/`, installed taps are in `taps/`.

## Organizing Flows

### Naming Conventions

Use descriptive, consistent names:

```
# Good
daily_report_generator
github_pr_reviewer
slack_summarizer

# Avoid
flow1
test
my_agent
```

### Version Control

Track your local flows with Git:

```bash
cd <flows-directory>/flows  # from: astonish config directory
git init
git add .
git commit -m "Initial flows"
```

### Backup

```bash
# Get your flows directory path
astonish config directory

# Backup all flows
cp -r <flows-directory>/flows ~/flows-backup-$(date +%Y%m%d)
```

## Next Steps

- **[Parameters & Variables](/cli/parameters/)** — Dynamic inputs
- **[Automation](/cli/automation/)** — Scripts and scheduling
