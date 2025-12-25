---
title: astonish tap
description: Manage extension repositories
---

# astonish tap

Manage Homebrew-style extension repositories (taps).

## Commands

### tap add

Add a new repository:

```bash
astonish tap add <repository> [flags]
```

#### Flags

| Flag | Description |
|------|-------------|
| `--as` | Custom alias for the tap |

#### Examples

```bash
# Add a public tap
astonish tap add schardosin/astonish-flows

# Add with alias
astonish tap add mycompany/internal-flows --as internal

# Add from GitHub Enterprise
astonish tap add github.mycompany.com/team/flows
```

### tap list

List all configured taps:

```bash
astonish tap list
```

### tap remove

Remove a tap:

```bash
astonish tap remove <name>
```

### tap update

Update all tap manifests:

```bash
astonish tap update
```

## Environment Variables

For private repositories:

| Variable | Description |
|----------|-------------|
| `GITHUB_TOKEN` | GitHub.com authentication |
| `GITHUB_ENTERPRISE_TOKEN` | GitHub Enterprise authentication |

## Examples

### Setting Up Enterprise Taps

```bash
# Set token
export GITHUB_ENTERPRISE_TOKEN=ghp_xxxxx

# Add enterprise tap
astonish tap add github.mycompany.com/ai-team/agents

# Install from it
astonish flows store install ai-team/daily_reporter
```
