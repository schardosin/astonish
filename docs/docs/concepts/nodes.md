---
sidebar_position: 2
---

# Nodes

Nodes are the fundamental building blocks of agentic flows in Astonish. Each node represents a specific step in the workflow and performs a particular function, such as getting user input, processing information with an AI model, or using tools to interact with external systems.

## Node Types

Astonish supports five main types of nodes:

1. **Input Nodes**: Used to get information from the user
2. **Output Nodes**: Used to format and display information to the user
3. **LLM Nodes**: Used to process information using AI models and tools
4. **Tool Nodes**: Used to directly execute tools without LLM involvement
5. **Update State Nodes**: Used to directly manipulate the state without LLM involvement

### Output Nodes

Output nodes are used to format and display information to the user. They iterate through the `user_message` array and display each item - either as a literal string or by resolving state variable names.

#### Configuration

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | A unique identifier for the node |
| `type` | string | Must be `"output"` |
| `user_message` | array | **Required.** Array of strings and/or state variable names to display |

#### Example

```yaml
- name: display_results
  type: output
  user_message:
    - "Search Results for:"
    - search_query
    - "---"
    - search_results
```

In this example, the output node displays literal text ("Search Results for:" and "---") interspersed with state variable values (`search_query` and `search_results`). Items are joined with spaces when displayed.


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

#### Raw Tool Output

LLM nodes can store the raw output of tools directly in the state using the `raw_tool_output` field. This is useful when the tool output is large or complex and you want to avoid having the LLM process it.

```yaml
- name: get_pr_diff
  type: llm
  system: |
    You are a GitHub CLI expert. Your task is to use the 'gh' command to retrieve the diff for a specific pull request.
  prompt: |
    Use the 'gh pr diff' command to get the diff for PR number {selected_pr}.
    IMPORTANT: The tool will return the raw diff. Your final task for this step is to confirm its retrieval.
  output_model:
    retrieval_status: str
  tools: true
  tools_selection:
    - shell_command
  raw_tool_output:
    pr_diff: str
```

In this example, the raw output of the `shell_command` tool is stored directly in the state variable `pr_diff`, while the LLM's response is stored in `retrieval_status`.

### Tool Nodes

Tool nodes execute tools directly without involving an LLM. This is useful for operations that don't require AI reasoning, such as data processing, file operations, or API calls.

#### Configuration

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | A unique identifier for the node |
| `type` | string | Must be `"tool"` |
| `args` | object | Arguments to pass to the tool |
| `tools_selection` | array | List of tools the node can use (first tool in the list is used) |
| `output_model` | object | Defines the variable names and types for the tool's output |

#### Example

```yaml
- name: chunk_pr
  type: tool
  args:
    diff_content: {pr_diff}
  tools_selection:
    - chunk_pr_diff
  output_model:
    pr_chunks: list
```

In this example, the `chunk_pr_diff` tool is executed with the `diff_content` argument, and the result is stored in the `pr_chunks` variable.

### Update State Nodes

Update State nodes provide direct manipulation of the state without requiring an LLM or tool execution. They are useful for operations like overwriting variables, appending to lists, or other state manipulations that don't require complex reasoning.

#### Configuration

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | A unique identifier for the node |
| `type` | string | Must be `"update_state"` |
| `action` | string | The action to perform (`"overwrite"` or `"append"`) |
| `source_variable` | string (optional) | The name of a state variable to use as the source value |
| `value` | any (optional) | A literal value to use (alternative to source_variable) |
| `output_model` | object | Must define exactly one target variable for the update |
| `user_message` | array (optional) | Variables to display to the user after processing |

Either `source_variable` or `value` must be provided, but not both.

#### Example: Overwrite Action

```yaml
- name: reset_counter
  type: update_state
  action: overwrite
  value: 0
  output_model:
    counter: int
```

In this example, the `counter` variable in the state is set to 0.

#### Example: Append Action

```yaml
- name: add_to_results
  type: update_state
  action: append
  source_variable: current_result
  output_model:
    all_results: list
```

In this example, the value of `current_result` is appended to the `all_results` list in the state.

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

#### `raw_tool_output`

An object mapping state variable names to types for storing raw tool output directly in the state. This is useful for large or complex tool outputs that you don't want the LLM to process.

```yaml
raw_tool_output:
  pr_diff: str
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

### Tool Node Specific Fields

#### `args`

An object mapping argument names to values for the tool. Values can be literals or references to state variables using curly braces.

```yaml
args:
  file_path: "/path/to/file.txt"
  content: {generated_content}
```

#### `tools_selection`

An array of tool names that the node can use. The first tool in the list will be executed.

```yaml
tools_selection:
  - chunk_pr_diff
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
3. For output nodes:
   - The prompt template is formatted with variables from the state
   - The formatted message is displayed to the user
   - The state remains unchanged
4. For LLM nodes:
   - The prompt is sent to the AI model along with the system message
   - If tools are enabled, the AI can use the specified tools
   - The AI's response is parsed according to the output_model and stored in the state
5. For Tool nodes:
   - The specified tool is executed with the provided arguments
   - The tool's output is stored in the state according to the output_model
6. For Update State nodes:
   - The specified action is performed on the state
   - For "overwrite", the target variable is set to the source value or literal value
   - For "append", the source value or literal value is appended to the target list
7. If user_message is specified, the corresponding variables are displayed to the user
8. The flow continues to the next node as defined in the flow section

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
