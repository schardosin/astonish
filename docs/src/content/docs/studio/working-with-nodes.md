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
| **Input** | Capture user input | When you need information from the user |
| **LLM** | Call an AI model | The most versatile node—handles AI, tools, and user messages |
| **Tool** | Call MCP tool directly | When you know exactly what tool to call (saves tokens) |
| **State** | Modify variables | When aggregating data, like appending to a list |
| **Output** | Display a message | When showing results from earlier steps at a different point |

---

## LLM Node

The **LLM node** is the most versatile and commonly used node type. It can:

- Call AI models to process, analyze, or generate content
- Use MCP tools when enabled
- Display messages directly to the user via **User Message**

In most cases, the LLM node handles everything you need.

![LLM Node Configuration](/astonish/images/nodes_llm.webp)
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

![Input Node Configuration](/astonish/images/nodes_input.webp)
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

Calls an MCP tool directly without AI inference. This saves tokens when you know exactly which tool to call with specific parameters.

![Tool Node Configuration](/astonish/images/nodes_tool.webp)
*Configuring a Tool node*

### Properties

| Property | Required | Description |
|----------|----------|-------------|
| **Name** | Yes | Unique identifier |
| **Tool Selection** | Yes | Which tool to call |

### When to Use

- You know exactly which tool to call (no AI decision needed)
- The parameters are determined by previous steps
- You want to save tokens by skipping inference

---

## Output Node

Displays a message to the user from state variables. Useful when you want to show results from earlier steps at a different point in your flow.

### Properties

| Property | Required | Description |
|----------|----------|-------------|
| **Name** | Yes | Unique identifier |
| **User Message** | Yes | State variables to display |

### When to Use

- Showing results collected from multiple earlier steps
- Displaying a summary after processing is complete
- Outputting data that was saved to state earlier in the flow

### Message Format

Reference state variables to display:

```yaml
- name: show_results
  type: output
  user_message:
    - summary
    - analysis_result
```

---

## State Node

Modifies the flow's state variables directly. Great for aggregating data across multiple steps.

### Properties

| Property | Required | Description |
|----------|----------|-------------|
| **Name** | Yes | Unique identifier |
| **Source Variable** | Yes | The variable to read from |
| **Action** | Yes | Operation to perform (e.g., `append`) |
| **Output Model** | Yes | Where to store the result |

### When to Use

- Appending items to a list (e.g., collecting results from a loop)
- Aggregating data from multiple iterations
- Building up a collection of results

### Example: Appending to a List

```yaml
- name: update_search_history
  type: update_state
  source_variable: search_query
  action: append
  output_model:
    search_history: list
```

This takes the `search_query` variable and appends it to the `search_history` list.

---

## Editing a Node

Double-click any node on the canvas to open the editor dialog at the bottom of the screen. From here you can:
- Configure the node's properties
- Set prompts and parameters
- Choose tools and outputs

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
