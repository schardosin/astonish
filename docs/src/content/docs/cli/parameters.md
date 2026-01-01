---
title: Parameters & Variables
description: Pass dynamic inputs to your flows via command line
sidebar:
  order: 4
---

# Parameters & Variables

Make your flows dynamic by passing parameters at runtime.

## Passing Parameters

Use the `-p` flag with key=value pairs:

```bash
astonish flows run my_flow -p key="value"
```

### Multiple Parameters

```bash
astonish flows run report_generator \
  -p topic="Quarterly Sales" \
  -p format="markdown" \
  -p include_graphs="true"
```

### Special Characters

Quote values with spaces or special characters:

```bash
# Spaces in value
astonish flows run analyzer -p input="Hello, world!"

# JSON-like structures
astonish flows run processor -p config='{"debug": true}'
```

## How Parameters Work

Parameters come from **input nodes** in your flow. The `-p` flag lets you provide values upfront instead of being prompted interactively.

### Example Flow

```yaml
nodes:
  - name: get_input
    type: input
    prompt: "What would you like to analyze?"
    output_model:
      input: str

  - name: analyze
    type: llm
    prompt: "Analyze the following: {input}"

flow:
  - from: START
    to: get_input
  - from: get_input
    to: analyze
  - from: analyze
    to: END
```

### Running Interactively

Without `-p`, the flow prompts you:

```bash
astonish flows run analyzer
# > What would you like to analyze?
# You type your input here
```

### Skipping Prompts with `-p`

Provide the value upfront:

```bash
astonish flows run analyzer -p get_input="Check this data"
```

The input node named `get_input` receives the value directly, skipping the interactive prompt.

## Parameter Types

Parameters are passed as strings. Your flow logic can interpret them:

| You Pass | Flow Receives |
|----------|---------------|
| `-p count="5"` | `"5"` (string) |
| `-p enabled="true"` | `"true"` (string) |
| `-p items="a,b,c"` | `"a,b,c"` (string) |

For type conversion, use the LLM or Update State node.

## Environment Variables

You can also use environment variables in scripts:

```bash
export MY_INPUT="some data"
astonish flows run processor -p input="$MY_INPUT"
```

## Scripting Examples

### From a File

```bash
# Read content from file
content=$(cat document.txt)
astonish flows run summarizer -p text="$content"
```

### Piping

```bash
# Pipe input
echo "Hello world" | xargs -I {} astonish flows run echo -p message="{}"
```

### Loop Processing

```bash
# Process multiple items
for file in *.txt; do
  astonish flows run analyzer -p filename="$file"
done
```

## Output Capture

Capture flow output for further processing:

```bash
# Save to file
astonish flows run reporter > report.md

# Store in variable
result=$(astonish flows run validator -p code="$code")
echo "Result: $result"
```

## Conditional Runs

Use input nodes to control flow behavior based on user choices:

```yaml
nodes:
  - name: get_mode
    type: input
    prompt: "Choose mode"
    options:
      - "detailed"
      - "quick"
    output_model:
      mode: str

  - name: get_input
    type: input
    prompt: "What to analyze?"
    output_model:
      input: str

  - name: detailed_analysis
    type: llm
    prompt: "Provide detailed analysis of {input}"

  - name: quick_summary
    type: llm
    prompt: "Briefly summarize {input}"

flow:
  - from: START
    to: get_mode
  - from: get_mode
    to: get_input
  - from: get_input
    edges:
      - to: detailed_analysis
        condition: "lambda x: x['mode'] == 'detailed'"
      - to: quick_summary
        condition: "lambda x: x['mode'] == 'quick'"
```

Run with parameters to skip prompts:
```bash
astonish flows run analyzer -p get_mode="detailed" -p get_input="My data"
```

## Best Practices

1. **Document required parameters** in your flow description
2. **Validate inputs** early in the flow
3. **Use meaningful names** like `input_file` not `f`
4. **Quote values** to avoid shell interpretation issues

## Next Steps

- **[Automation](/astonish/cli/automation/)** — Use parameters in scripts
- **[YAML Reference](/astonish/concepts/yaml/)** — Complete variable syntax
