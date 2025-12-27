---
title: Working with Nodes
description: Complete guide to all node types in Astonish Studio
sidebar:
  order: 3
---

# Working with Nodes

Nodes are the building blocks of flows. Each node performs a specific action—calling an AI, getting user input, running a tool, or manipulating data.

## Node Types Overview

| Type | Purpose | When to Use |
|------|---------|-------------|
| **LLM** | Call an AI model | When you need AI to process, analyze, or generate |
| **Input** | Get user input | When you need data from the user |
| **Tool** | Call MCP tool directly | When running a specific tool without AI |
| **Output** | Display message | When showing results to the user |
| **Update State** | Modify variables | When transforming or setting data |

---

## LLM Node

The most common node type. Sends a prompt to an AI model and receives a response.

![LLM Node Configuration](/src/assets/placeholder.png)
*Configuring an LLM node*

### Properties

| Property | Required | Description |
|----------|----------|-------------|
| **Name** | Yes | Unique identifier for this node |
| **Prompt** | Yes | The user message sent to the AI |
| **System** | No | System prompt (AI personality/instructions) |
| **Tools** | No | Enable tool calling |
| **Tool Selection** | No | Whitelist specific tools |
| **Output Model** | No | Define structured output variables |
| **User Message** | No | Message to display after execution |

### Using Variables in Prompts

Reference previous node outputs with `{variable_name}`:

```yaml
prompt: "Summarize this text: {input_text}"
```

Variables appear highlighted in purple as you type.

### Enabling Tools

1. Toggle **Tools** to ON
2. (Optional) Select specific tools from **Tool Selection**

When tools are enabled, the AI can call MCP tools during execution.

### Structured Output

Use **Output Model** to extract specific data:

```yaml
output_model:
  summary: str
  sentiment: str
  score: int
```

These become variables for later nodes.

---

## Input Node

Pauses execution to collect input from the user.

![Input Node Configuration](/src/assets/placeholder.png)
*Configuring an Input node*

### Properties

| Property | Required | Description |
|----------|----------|-------------|
| **Name** | Yes | Unique identifier |
| **Prompt** | Yes | Text shown to the user |
| **Options** | No | Predefined choices (dropdown) |
| **Output Model** | Yes | Variable to store the response |

### Free-Text Input

```yaml
- name: get_question
  type: input
  prompt: "What would you like to know?"
  output_model:
    question: str
```

### Multiple Choice

Add **Options** to create a dropdown:

```yaml
- name: select_action
  type: input
  prompt: "What would you like to do?"
  options:
    - "Summarize"
    - "Translate"
    - "Analyze"
  output_model:
    action: str
```

---

## Tool Node

Calls an MCP tool directly without AI involvement.

![Tool Node Configuration](/src/assets/placeholder.png)
*Configuring a Tool node*

### Properties

| Property | Required | Description |
|----------|----------|-------------|
| **Name** | Yes | Unique identifier |
| **Tool Selection** | Yes | Which tool(s) to call |

### When to Use

- Running a specific tool with known parameters
- Avoiding AI overhead when the action is deterministic
- Chaining tool calls in sequence

---

## Output Node

Displays a message to the user without AI processing.

### Properties

| Property | Required | Description |
|----------|----------|-------------|
| **Name** | Yes | Unique identifier |
| **User Message** | Yes | Lines to display |

### Message Format

**User Message** is an array of strings and variable references:

```yaml
user_message:
  - "Your analysis is complete."
  - "Result:"
  - result_variable
```

---

## Update State Node

Modifies the flow's state variables.

### Properties

| Property | Required | Description |
|----------|----------|-------------|
| **Name** | Yes | Unique identifier |
| **Updates** | Yes | Variable assignments |

### Example

```yaml
- name: set_defaults
  type: update_state
  updates:
    counter: 0
    status: "pending"
```

---

## Selecting a Node

Click any node on the canvas to:
- Open the editor panel (right side)
- See its connections
- Access configuration options

## Moving Nodes

Click and drag to reposition nodes on the canvas.

:::tip
Positions are saved in the YAML under `layout`. They don't affect execution—only visual organization.
:::

## Deleting Nodes

1. Select the node
2. Press **Delete** or **Backspace**
3. Connected edges are also removed

---

## Next Steps

- **[Connecting Edges](/studio/connecting-edges/)** — Add logic and branching
- **[Running & Debugging](/studio/running-debugging/)** — Test your flows
