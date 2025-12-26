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

The flow is copied to `~/.astonish/store/<tap-name>/`.

## Uninstalling Flows

Remove an installed flow:

```bash
astonish flows store uninstall translator
```

:::caution
Only removes installed tap flows. Local flows in `~/.astonish/agents/` must be deleted manually.
:::

## Updating Flows

Update all tap manifests:

```bash
astonish flows store update
```

This refreshes the list of available flows from all your taps.

## Creating a New Flow

### Method 1: Edit Command

```bash
astonish flows edit new_flow
```

Opens your default editor with an empty template.

### Method 2: Copy Existing

```bash
cp ~/.astonish/agents/existing.yaml ~/.astonish/agents/new_flow.yaml
```

Then edit the name and contents.

### Method 3: Create Manually

```bash
cat > ~/.astonish/agents/hello.yaml << 'EOF'
name: hello
description: A simple greeting flow

nodes:
  - name: greet
    type: llm
    prompt: "Say hello to the user warmly."

flow:
  - from: START
    to: greet
  - from: greet
    to: END
EOF
```

## Viewing Flow Details

### Show Structure

```bash
astonish flows show translator
```

Displays the flow as a text diagram.

### View Raw YAML

```bash
cat ~/.astonish/agents/translator.yaml
```

## Flow File Locations

| Type | Location |
|------|----------|
| Local flows | `~/.astonish/agents/` |
| Installed flows | `~/.astonish/store/<tap-name>/` |

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
cd ~/.astonish/agents
git init
git add .
git commit -m "Initial flows"
```

### Backup

```bash
# Backup all flows
cp -r ~/.astonish/agents ~/flows-backup-$(date +%Y%m%d)

# Or zip them
zip -r flows.zip ~/.astonish/agents
```

## Next Steps

- **[Parameters & Variables](/cli/parameters/)** — Dynamic inputs
- **[Automation](/cli/automation/)** — Scripts and scheduling
