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

## Using Parameters in Flows

Reference parameters with curly braces:

```yaml
nodes:
  - name: analyze
    type: llm
    prompt: "Analyze the following: {input}"
```

When you run:
```bash
astonish flows run analyzer -p input="Check this data"
```

The prompt becomes:
```
Analyze the following: Check this data
```

## Default Values

If a parameter isn't provided, the variable shows as-is:

```yaml
prompt: "Hello {name}, welcome!"
```

Running without `-p name=...`:
```
Hello {name}, welcome!  # Literal text
```

:::tip
Always provide required parameters or add defaults in your flow logic.
:::

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

Use parameters to control flow behavior:

```yaml
nodes:
  - name: router
    type: input
    output_model:
      mode: str

  - name: detailed_analysis
    type: llm
    prompt: "Provide detailed analysis of {input}"

  - name: quick_summary
    type: llm
    prompt: "Briefly summarize {input}"

flow:
  - from: router
    edges:
      - to: detailed_analysis
        condition: "lambda x: x['mode'] == 'detailed'"
      - to: quick_summary
        condition: "lambda x: x['mode'] == 'quick'"
```

Run:
```bash
astonish flows run analyzer -p mode="detailed" -p input="..."
```

## Best Practices

1. **Document required parameters** in your flow description
2. **Validate inputs** early in the flow
3. **Use meaningful names** like `input_file` not `f`
4. **Quote values** to avoid shell interpretation issues

## Next Steps

- **[Automation](/cli/automation/)** — Use parameters in scripts
- **[YAML Reference](/concepts/yaml/)** — Complete variable syntax
