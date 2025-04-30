---
sidebar_position: 2
---

# Nodes

Nodes are the fundamental building blocks of agentic flows in Astonish. Each node represents a specific step in the workflow and performs a particular function, such as getting user input, processing information with an AI model, or using tools to interact with external systems.

## Node Types

Astonish supports two main types of nodes:

1. **Input Nodes**: Used to get information from the user
2. **LLM Nodes**: Used to process information using AI models and tools

### Input Nodes

Input nodes are used to collect information from the user. They display a prompt and store the user's response in the state.

#### Configuration

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | A unique identifier for the node |
| `type` | string | Must be `"input"` |
| `prompt` | string | The text to display to the user |
| `output_model` | object | Defines the variable name and type for the user's response |
| `options` | array (optional) | A list of predefined options for the user to choose from |
| `user_message` | array (optional) | Variables to display to the user after processing |

#### Example

```yaml
- name: get_topic
  type: input
  prompt: |
    What topic do you want to research?
  output_model:
    research_topic: str
  options:
    - "Artificial Intelligence"
    - "Climate Change"
    - "Quantum Computing"
```

### LLM Nodes

LLM (Language Learning Model) nodes use AI models to process information and generate responses. They can also use tools to interact with external systems.

#### Configuration

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | A unique identifier for the node |
| `type` | string | Must be `"llm"` |
| `system` | string (optional) | System message for the AI model |
| `prompt` | string | The prompt to send to the AI model |
| `output_model` | object | Defines the variable names and types for the AI's response |
| `tools` | boolean (optional) | Whether the node can use tools |
| `tools_selection` | array (optional) | List of tools the node can use |
| `tools_auto_approval` | boolean (optional) | Whether tool usage requires user approval |
| `user_message` | array (optional) | Variables to display to the user after processing |
| `print_state` | boolean (optional) | Whether to print the state after processing |
| `print_prompt` | boolean (optional) | Whether to print the prompt sent to the AI model |
| `limit` | integer (optional) | Maximum number of times the node can be executed in a loop |
| `limit_counter_field` | string (optional) | Variable name for the loop counter |

#### Example

```yaml
- name: search_web
  type: llm
  system: |
    You are a research assistant that performs high-quality web searches.
  prompt: |
    Please perform a web search to gather useful information on the following topic:
    
    Topic: "{research_topic}"
    
    Make sure to include credible sources.
  output_model:
    search_results: list
  tools: true
  tools_selection:
    - web_search
  tools_auto_approval: false
```

## Node Fields

### Common Fields

#### `name`

A unique identifier for the node. This is used to reference the node in the flow.

```yaml
name: get_user_input
```

#### `type`

The type of the node. Must be either `"input"` or `"llm"`.

```yaml
type: input
```

#### `prompt`

The text to display to the user (for input nodes) or send to the AI model (for LLM nodes). Can include variables from the state using curly braces.

```yaml
prompt: |
  What would you like to search for?
```

#### `output_model`

Defines the variable names and types for the node's output. The variables will be added to the state and can be used by other nodes.

```yaml
output_model:
  search_query: str
  results_count: int
```

#### `user_message`

An array of variable names to display to the user after the node is processed. The variables must be defined in the output_model.

```yaml
user_message:
  - search_results
```

### Input Node Specific Fields

#### `options`

An array of predefined options for the user to choose from. If provided, the user will be presented with a selection menu instead of a free-form input field.

```yaml
options:
  - "Option 1"
  - "Option 2"
  - "Option 3"
```

### LLM Node Specific Fields

#### `system`

The system message to send to the AI model. This is used to set the context and behavior of the AI.

```yaml
system: |
  You are a helpful assistant that provides concise and accurate information.
```

#### `tools`

A boolean indicating whether the node can use tools. If `true`, the node will be able to use tools specified in `tools_selection`.

```yaml
tools: true
```

#### `tools_selection`

An array of tool names that the node can use. The tools must be available in the system.

```yaml
tools_selection:
  - read_file
  - web_search
```

#### `tools_auto_approval`

A boolean indicating whether tool usage requires user approval. If `false`, the user will be prompted to approve each tool usage.

```yaml
tools_auto_approval: false
```

#### `print_state`

A boolean indicating whether to print the state after the node is processed. Useful for debugging.

```yaml
print_state: true
```

#### `print_prompt`

A boolean indicating whether to print the prompt sent to the AI model. Useful for debugging.

```yaml
print_prompt: true
```

#### `limit`

An integer specifying the maximum number of times the node can be executed in a loop. Used in conjunction with `limit_counter_field`.

```yaml
limit: 5
```

#### `limit_counter_field`

The variable name for the loop counter. The counter is incremented each time the node is executed and reset when it reaches the limit.

```yaml
limit_counter_field: iteration_count
```

## Variable Interpolation

Nodes can access variables from the state using curly braces in the prompt field. This allows for dynamic prompts based on previous nodes' outputs.

```yaml
prompt: |
  Generate a response to the user's query: {search_query}
  
  Previous results:
  {previous_results}
```

## Node Execution Flow

When a node is executed:

1. The node's prompt is formatted with variables from the state
2. For input nodes:
   - The prompt is displayed to the user
   - The user's response is stored in the state according to the output_model
3. For LLM nodes:
   - The prompt is sent to the AI model along with the system message
   - If tools are enabled, the AI can use the specified tools
   - The AI's response is parsed according to the output_model and stored in the state
4. If user_message is specified, the corresponding variables are displayed to the user
5. The flow continues to the next node as defined in the flow section

## Best Practices

1. **Use descriptive names**: Give nodes clear, descriptive names that indicate their purpose
2. **Keep prompts focused**: Each node should have a specific purpose and a focused prompt
3. **Use system messages**: Set appropriate system messages for LLM nodes to guide the AI's behavior
4. **Validate user input**: Use input nodes with options to restrict user input to valid choices
5. **Handle errors**: Use conditional edges to handle potential errors in the flow
6. **Use tools judiciously**: Only enable tools that are necessary for the node's function
7. **Document your nodes**: Add comments in the YAML file to explain complex nodes

## Next Steps

To learn more about how nodes fit into the larger agentic flow structure, check out:

- [Agentic Flows](/docs/concepts/agentic-flows) for an overview of how nodes are connected
- [Tools](/docs/concepts/tools) for details on how to use tools in LLM nodes
- [YAML Configuration](/docs/concepts/yaml-configuration) for the full specification of node configuration
