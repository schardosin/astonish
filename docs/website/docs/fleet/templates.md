# Fleet Templates

A fleet template defines the composition of an agent team — which agents participate, what roles they fill, and how they interact. Templates are reusable YAML files that serve as blueprints for creating [plans](./plans.md).

## Structure

```yaml
name: "full-stack-team"
description: "A team for full-stack application development"

hub:
  name: "coordinator"
  system_prompt: |
    You are a project coordinator. Break down objectives into tasks,
    assign them to the appropriate specialist, and synthesize results.
  model: "claude-sonnet"

spokes:
  - name: "backend"
    role: "Backend Engineer"
    system_prompt: |
      You are a backend engineer specializing in Go and PostgreSQL.
      Implement APIs, data models, and business logic.
    model: "claude-sonnet"
    tools:
      - file_write
      - file_read
      - grep
      - bash

  - name: "frontend"
    role: "Frontend Engineer"
    system_prompt: |
      You are a frontend engineer specializing in React and Tailwind CSS.
      Build UI components, pages, and client-side logic.
    model: "claude-sonnet"
    tools:
      - file_write
      - file_read
      - grep
      - bash

  - name: "devops"
    role: "DevOps Engineer"
    system_prompt: |
      You are a DevOps engineer. Handle deployments, CI/CD,
      infrastructure, and containerization.
    model: "claude-sonnet"
    tools:
      - file_write
      - bash
```

## Fields

### Hub

| Field | Description | Required |
|-------|-------------|----------|
| `name` | Identifier for the hub agent | Yes |
| `system_prompt` | Instructions defining coordination behavior | Yes |
| `model` | Model to use for the hub agent | No (uses default) |

### Spokes

| Field | Description | Required |
|-------|-------------|----------|
| `name` | Unique identifier for this spoke | Yes |
| `role` | Human-readable role description | Yes |
| `system_prompt` | Instructions defining the spoke's expertise | Yes |
| `model` | Model to use for this spoke | No (uses default) |
| `tools` | List of tools available to this spoke | No (uses all) |

## Communication Patterns

The template defines implicit communication patterns:

- The hub can message any spoke
- Spokes respond only to the hub
- Each hub–spoke pair gets its own isolated [session](./sessions-threads.md)

## Managing Templates

```bash
# List available templates
astonish fleet templates

# Create a plan from a template
astonish fleet plan create --template full-stack-team --objective "Build user auth"
```

Templates are stored in your Astonish config directory (`~/.config/astonish/fleet/`) or in the project's `.astonish/fleet/` directory.
