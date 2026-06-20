# Skills

Skills are markdown-based instruction files that teach the agent specific tools, APIs, workflows, or domain knowledge. They are loaded on demand when a task matches a skill's description.

## How Skills Work

When you ask the agent to perform a task, it checks available skills for relevance. If a match is found, the skill's content is injected into the agent's context, giving it detailed instructions for the task.

Skills differ from memory:
- **Memory** = facts the agent has learned (declarative)
- **Skills** = instructions for how to do things (procedural)

## Bundled Skills

Astonish ships with 9 built-in skills:

| Skill | Description |
|-------|-------------|
| `git-workflow` | Branch management, commits, PRs |
| `docker` | Container builds, compose, debugging |
| `kubernetes` | kubectl operations, manifest authoring |
| `terraform` | Infrastructure-as-code workflows |
| `python-project` | Virtual envs, packaging, testing |
| `node-project` | npm/yarn, builds, deployment |
| `api-testing` | REST/GraphQL endpoint validation |
| `database` | SQL queries, migrations, schema design |
| `debugging` | Log analysis, profiling, root cause |

## ClawHub Community Skills

The [ClawHub](https://github.com/astonish-clawhub) organization hosts community-contributed skills. Install them via taps:

```bash
astonish tap add https://github.com/astonish-clawhub/skills.git
astonish skill install clawhub/aws-lambda
```

## Writing Custom Skills

Create a markdown file in `~/.config/astonish/skills/`:

```markdown
# Skill: Deploy to Production

## Description
Handles production deployment for our Next.js app on Vercel.

## Steps
1. Run `npm run build` to verify no build errors
2. Run `npm test` to ensure tests pass
3. Execute `vercel --prod` to deploy
4. Verify deployment with `curl https://app.example.com/health`

## Notes
- Always check the #deployments Slack channel before deploying
- Rollback command: `vercel rollback`
```

### Skill File Structure

```markdown
# Skill: <Name>

## Description
<When this skill applies — used for matching>

## <Sections>
<Detailed instructions, commands, code examples>
```

The `Description` section is critical—the agent uses it to determine when to load the skill.

## Team-Scoped Skills (Cloud Deployment)

In cloud deployments, skills can be shared at the team or org level:

| Scope | Location | Managed By |
|-------|----------|-----------|
| Personal | Local filesystem | Individual |
| Team | Platform storage | Team admin |
| Org | Platform storage | Org admin |

Team skills are automatically available to all team members without individual installation.

## CLI Commands

```bash
# List available skills
astonish skill list

# Show skill content
astonish skill show git-workflow

# Install from tap
astonish skill install <tap>/<skill-name>
```

See [Taps & Flow Store](../configuration/taps.md) for distributing skills via taps, and [Sub-agents](./sub-agents.md) for how delegated tasks inherit skill access.
