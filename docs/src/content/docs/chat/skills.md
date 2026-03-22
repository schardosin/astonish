---
title: "Skills"
description: "Pre-built expertise guides for CLI tools and workflows"
---

Skills are instruction guides that teach the agent how to use specific CLI tools and workflows. They provide structured knowledge so the agent can work effectively with tools it encounters.

## How skills work

Astonish uses a two-tier hybrid retrieval system — keyword matching combined with vector similarity search. When the agent encounters a tool or workflow it needs guidance on, it automatically looks up relevant skills.

The agent can also use the `skill_lookup` tool during conversations to load skill instructions on demand.

## Bundled skills

Astonish ships with 10 bundled skills covering common tools like git, docker, and other widely-used CLI utilities.

## Installing skills from ClawHub

ClawHub is the community skill registry. Install a skill by its slug:

```bash
astonish skills install <slug>
```

## Creating custom skills

Generate a skill template in `~/.config/astonish/skills/`:

```bash
astonish skills create <name>
```

The skills directory is monitored by a live filesystem watcher. New or updated skill files are indexed automatically — no restart required.

## CLI commands

List all available skills:

```bash
astonish skills list
```

Display full skill content:

```bash
astonish skills show <name>
```

Check which skills are eligible for the current environment:

```bash
astonish skills check
```

Install a skill from ClawHub:

```bash
astonish skills install <slug>
```

Create a new skill from template:

```bash
astonish skills create <name>
```

## Configuration

These settings can be adjusted through **Studio Settings > Skills**, or in `config.yaml`:

```yaml
skills:
  enabled: true
  user_dir: ~/.config/astonish/skills
  extra_dirs:
    - /path/to/additional/skills
  allowlist:
    - skill-name
```
