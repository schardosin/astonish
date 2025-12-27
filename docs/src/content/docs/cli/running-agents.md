---
title: Running Flows
description: Execute AI flows from the command line
sidebar:
  order: 2
---

# Running Flows

The `astonish flows run` command executes flows from your terminal.

## Basic Usage

```bash
astonish flows run <flow-name>
```

### Example

```bash
# Run a flow called "summarizer"
astonish flows run summarizer
```

Output streams to your terminal in real-time.

## Run Command Flags

| Flag | Description | Default |
|------|-------------|---------|
| `-p key=value` | Pass parameter(s) | — |
| `-model <name>` | Override model | Config default |
| `-provider <name>` | Override provider | Config default |
| `--debug` | Verbose output | false |

## Passing Parameters

Use `-p` to inject values into your flow:

```bash
astonish flows run analyzer -p input="Hello world"
```

Multiple parameters:

```bash
astonish flows run reporter -p topic="AI news" -p format="markdown"
```

These become available as `{input}`, `{topic}`, `{format}` in your prompts.

## Override Model

Run with a different model:

```bash
astonish flows run summarizer -model gpt-4
```

## Override Provider

Switch providers at runtime:

```bash
astonish flows run summarizer -provider anthropic -model claude-3-sonnet
```

## Debug Mode

See detailed execution info:

```bash
astonish flows run --debug my_agent
```

Shows:
- Node execution order
- State changes
- Tool call details
- Condition evaluations

## Listing Flows

See all available flows:

```bash
astonish flows list
```

Output:
```
AVAILABLE FLOWS

📁 LOCAL (2)
  summarizer
    Summarizes text input
  code_reviewer
    Reviews code for best practices

📦 community-flows (1)
  translator
    Translates text between languages
```

## Flow Locations

To find where flows are stored, run:

```bash
astonish config directory
```

Flows are in the `flows/` subdirectory. Installed taps are in `taps/`.

## Showing Flow Structure

View a flow's structure as text:

```bash
astonish flows show summarizer
```

## Editing Flows

Open a flow in your default editor:

```bash
astonish flows edit summarizer
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Error (check output for details) |

Use in scripts:

```bash
if astonish flows run validator; then
  echo "Validation passed"
else
  echo "Validation failed"
fi
```

## Next Steps

- **[Parameters & Variables](/cli/parameters/)** — Advanced parameter handling
- **[Automation](/cli/automation/)** — Cron jobs and CI/CD
