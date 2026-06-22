# Flow Commands

The `astonish flows` command manages agent flow pipelines from the command line.

## Usage

```bash
astonish flows <subcommand> [flags]
```

## Subcommands

### List Flows

```bash
astonish flows list
```

### Run a Flow

```bash
# Run a flow by name
astonish flows run <flow-name>

# Run with parameters
astonish flows run my-flow -p file=data.csv -p format=json

# Run with a specific model
astonish flows run my-flow --provider openai --model gpt-4o

# Auto-approve all tool calls
astonish flows run my-flow --auto-approve
```

### Show Flow Structure

```bash
# Visualize a flow's node structure
astonish flows show <flow-name>
```

### Edit a Flow

```bash
# Open flow YAML file for editing
astonish flows edit <flow-name>
```

### Import a Flow

```bash
# Import a flow from a local YAML file
astonish flows import <path-to-flow.yaml>
```

### Remove a Flow

```bash
astonish flows remove <flow-name>
```

### Flow Store

Browse and install flows from configured taps:

```bash
# List available flows from all taps
astonish flows store list

# Search for flows
astonish flows store search <query>

# Install a flow from a tap
astonish flows store install <tap-name>/<flow-name>

# Uninstall a tap flow
astonish flows store uninstall <tap-name>/<flow-name>

# Update all tap manifests
astonish flows store update
```

## Flags for `run`

| Flag | Short | Description |
|------|-------|-------------|
| `--provider` | | AI provider to use |
| `--model` | | Model name |
| `-p` | | Parameter in `key=value` format (repeatable) |
| `--auto-approve` | | Auto-approve all tool executions |
| `--browser` | | Launch with embedded web browser UI |
| `--port` | | Port for web server (with --browser, default: 8080) |
| `--debug` | | Enable debug mode |

## Scheduling

Flows can be scheduled for recurring execution. Ask the agent to schedule a flow, or manage existing schedules with the [scheduler](./daemon-scheduler.md).
