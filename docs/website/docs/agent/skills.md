# Skills

Skills are markdown-based instruction files that teach the agent specific tools, APIs, workflows, or domain knowledge. They are loaded on demand when a task matches a skill's description.

## How Skills Work

When you ask the agent to perform a task, it checks available skills for relevance. If a match is found, the skill's content is injected into the agent's context, giving it detailed instructions for the task.

Skills differ from memory:
- **Memory** = facts the agent has learned (declarative knowledge)
- **Skills** = instructions for how to do things (procedural knowledge)

## Skill Structure

A skill is a markdown file with a specific structure:

```markdown
# Skill Name

## Description
When this skill applies — used for matching against user requests.

## Instructions
Detailed steps, commands, code examples, and best practices.

## References
Links to documentation, API references, etc.
```

The `Description` section is critical — the agent uses it to determine when to load the skill.

## Managing Skills

Skills are managed through the platform at three scopes:

| Scope | Managed By | Available To |
|-------|------------|-------------|
| Platform | Platform admin | All users |
| Org | Org admin | All org members |
| Team | Team admin | Team members |

### In Studio

Studio provides the primary interface for skill management:

- Browse available skills
- Create and edit skills
- Install skills from ClawHub (community repository)
- Configure skill scoping (team/org)

### CLI

The CLI provides read access to skills (requires platform connection):

```bash
astonish skills list              # List available skills
astonish skills show <name>       # Show skill content
astonish skills install <source>  # Install from ClawHub
astonish skills create <name>     # Create a new skill template
```

::: tip Plural Command
The CLI command is `astonish skills` (plural), not `astonish skill`.
:::

## ClawHub Community Skills

The [ClawHub](https://github.com/astonish-clawhub) organization hosts community-contributed skills covering common tools and workflows. Install them via Studio or CLI.

## Agent Tool: skill_lookup

During chat, the agent can search for relevant skills using the `skill_lookup` tool:

```
skill_lookup:
  name: "docker"
```

This loads the full skill instructions into the agent's context for the current task.

## Cascading Access

Skills cascade through the platform hierarchy (see [Cascading Defaults](../platform/cascading-defaults.md)):

- Platform skills are available to everyone
- Org skills are available to all org members
- Team skills are available to team members
- Skills from higher scopes cannot be removed at lower scopes, only supplemented

See [Sub-agents](./sub-agents.md) for how delegated tasks inherit skill access, and [Configuration](../configuration/mcp-servers.md) for MCP servers (another way to extend agent capabilities).
