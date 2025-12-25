---
title: astonish agents
description: Manage and run AI agents
---

# astonish agents

Manage and execute AI agents.

## Commands

### agents list

List all available agents:

```bash
astonish agents list
```

### agents run

Run an agent:

```bash
astonish agents run <agent_name> [flags]
```

#### Flags

| Flag | Description |
|------|-------------|
| `-p, --param` | Inject parameters (key=value) |
| `--no-stream` | Disable streaming output |

#### Examples

```bash
# Run an agent interactively
astonish agents run my_agent

# Inject parameters
astonish agents run summarizer -p file_path="/path/to/doc.txt"

# Multiple parameters
astonish agents run report -p date="2024-01-01" -p format="pdf"
```

### agents create

Create a new agent:

```bash
astonish agents create <agent_name>
```

This creates a new YAML file in your agents directory.

### agents edit

Open an agent's YAML file in your editor:

```bash
astonish agents edit <agent_name>
```

### agents delete

Delete an agent:

```bash
astonish agents delete <agent_name>
```

## Use Cases

### Cron Jobs

Perfect for scheduled automation:

```bash
# Add to crontab
0 9 * * * /usr/local/bin/astonish agents run daily_report >> /var/log/report.log
```

### CI/CD Pipelines

Run agents in GitHub Actions:

```yaml
- name: Run Code Reviewer
  run: astonish agents run code_reviewer -p repo="${{ github.workspace }}"
```

### Shell Scripts

Chain with other commands:

```bash
#!/bin/bash
astonish agents run summarizer -p file="$1" > summary.txt
```
