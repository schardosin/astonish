---
title: "flows"
description: "Design, run, and manage AI automation flows"
---

## astonish flows

Work with AI automation flows.

### Subcommands

| Subcommand | Description |
|------------|-------------|
| `run <name>` | Execute a flow |
| `list` | List available flows |
| `show <name>` | Visualize flow structure |
| `edit <name>` | Edit flow YAML in default editor |
| `import <file>` | Import a flow from YAML file |
| `remove <name>` | Remove a flow |
| `store` | Browse and install flows from stores |

### flows run flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--provider` | string | from config | LLM provider |
| `--model` | string | from config | Model name |
| `-p` | string | | Parameter as key=value (repeatable) |
| `--browser` | bool | false | Launch with embedded browser UI |
| `--port` | int | 8080 | Port for browser UI |
| `--debug` | bool | false | Show tool inputs/responses |
| `--auto-approve` | bool | false | Auto-approve tool executions |

### Examples

```
astonish flows run my-flow                          # Run a flow
astonish flows run my-flow -p name="John"           # With parameters
astonish flows run my-flow --provider openai        # Specific provider
astonish flows list                                 # List local flows
astonish flows import ./workflow.yaml --as my-flow  # Import and rename
```

### flows store subcommands

| Subcommand | Description |
|------------|-------------|
| `list` | List flows from taps (supports `--tag` filter) |
| `install <name>` | Install a flow from a tap |
| `uninstall <name>` | Remove an installed flow |
| `update` | Update all tap manifests |
| `search <query>` | Search for flows |
