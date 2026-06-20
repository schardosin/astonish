# Flow Commands

The `astonish flows` command manages agent flow pipelines from the command line.

## Usage

```bash
astonish flows <subcommand> [flags]
```

## Subcommands

### List Flows

```bash
# List all available flows
astonish flows list

# List with execution history
astonish flows list --history
```

### Run a Flow

```bash
# Run a flow by name
astonish flows run my-flow

# Run with input parameters
astonish flows run my-flow --input '{"file": "data.csv", "format": "json"}'

# Run with a parameter file
astonish flows run my-flow --input-file params.json

# Dry run (validate without executing)
astonish flows run my-flow --dry-run
```

### Edit a Flow

```bash
# Open flow in Studio's Flow Editor
astonish flows edit my-flow
```

This launches Studio and navigates directly to the flow in the visual editor.

### Delete a Flow

```bash
# Delete a flow
astonish flows delete my-flow

# Delete without confirmation prompt
astonish flows delete my-flow --force
```

## Flags for `run`

| Flag | Short | Description |
|------|-------|-------------|
| `--input` | `-i` | JSON string of input parameters |
| `--input-file` | | Path to JSON file with parameters |
| `--dry-run` | | Validate flow without executing |
| `--verbose` | `-v` | Show detailed execution output |
| `--timeout` | `-t` | Maximum execution time (e.g., `5m`) |

## Scheduling

Flows can be scheduled for recurring execution using the [scheduler](./daemon-scheduler.md):

```bash
# Run a flow every day at 9am
astonish scheduler add --flow my-flow --cron "0 9 * * *"
```

## Output

Flow execution outputs are printed to stdout by default. Use `--output` to write results to a file:

```bash
astonish flows run report-gen --output report.md
```
