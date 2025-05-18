---
sidebar_position: 4
---

# YAML Configuration

Astonish uses YAML files to define agentic flows. This document provides a comprehensive reference for the YAML configuration format, including all available fields, their meanings, and examples.

## Overview

An Astonish agent is defined by a YAML file with the following structure:

```yaml
description: A brief description of what the agent does
nodes:
  - name: node1
    type: input
    # ... node configuration ...
  - name: node2
    type: llm
    # ... node configuration ...
  # ... more nodes ...
flow:
  - from: START
    to: node1
  - from: node1
    to: node2
  # ... more flow connections ...
  - from: nodeN
    to: END
```

## Top-Level Fields

### `description`

A string that describes the purpose and functionality of the agent.

```yaml
description: An agent that searches the web for information and summarizes the results
```

### `nodes`

An array of node objects that define the steps in the workflow. Each node represents a specific action or decision point in the agent's execution.

```yaml
nodes:
  - name: get_query
    type: input
    # ... node configuration ...
  - name: search_web
    type: llm
    # ... node configuration ...
```

### `flow`

An array of connection objects that define how nodes are connected. Each connection specifies a source node (`from`) and a destination node (`to`), optionally with conditions for branching paths.

```yaml
flow:
  - from: START
    to: get_query
  - from: get_query
    to: search_web
  - from: search_web
    to: END
```

## Node Configuration

### Common Fields

These fields are available for all node types:

#### `name`

A unique identifier for the node. This is used to reference the node in the flow.

```yaml
name: get_user_query
```

#### `type`

The type of the node. Must be one of `"input"`, `"llm"`, or `"tool"`.

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

### Input Node Fields

#### `options`

An array of predefined options for the user to choose from. If provided, the user will be presented with a selection menu instead of a free-form input field.

```yaml
options:
  - "Option 1"
  - "Option 2"
  - "Option 3"
```

### LLM Node Fields

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

### Tool Node Fields

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

## Flow Configuration

### Basic Connections

The simplest form of connection is a direct link from one node to another:

```yaml
flow:
  - from: node1
    to: node2
```

### Special Nodes

There are two special nodes in the flow:

- `START`: The entry point of the flow
- `END`: The exit point of the flow

```yaml
flow:
  - from: START
    to: first_node
  - from: last_node
    to: END
```

### Conditional Edges

For branching paths, you can use conditional edges with lambda functions:

```yaml
flow:
  - from: check_condition
    edges:
      - to: path_a
        condition: "lambda x: x['condition'] == True"
      - to: path_b
        condition: "lambda x: x['condition'] == False"
```

The lambda function takes the state dictionary as input and returns a boolean indicating whether the edge should be followed.

## Variable Interpolation

You can include variables from the state in prompts using curly braces:

```yaml
prompt: |
  Generate a response to the user's query: {search_query}
  
  Previous results:
  {previous_results}
```

## Complete Example

Here's a complete example of an agent that reviews a pull request using both LLM and Tool nodes:

```yaml
description: PR Review Agentic Flow
nodes:
  - name: list_prs
    type: llm
    system: |
      You are a GitHub CLI expert. Your task is to list all open pull requests in the current repository.
    prompt: |
      Use the 'gh pr list' command to list all open pull requests.

      Format the output as:
      123: Title of PR 1
      456: Title of PR 2
      789: Title of PR 3
    output_model:
      pr_list: list
    tools: true
    tools_selection:
      - shell_command

  - name: select_pr
    type: input
    prompt: |
      Please select a pull request from the list below by entering its number:
      {pr_list}
    output_model:
      selected_pr: str
    options: [pr_list]

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

  - name: chunk_pr
    type: tool
    args:
      diff_content: {pr_diff}
    tools_selection:
      - chunk_pr_diff
    output_model:
      pr_chunks: list
      current_index: int

  - name: review_chunk
    type: llm
    system: |
      You are a code review assistant. Your task is to review a chunk of code and provide feedback.
    prompt: |
      Review the following chunk of code:
      {pr_chunks[current_index]}
      Provide your feedback on this chunk.
    output_model:
      chunk_review: str

  - name: collect_reviews
    type: llm
    prompt: |
      collected_reviews:
      {collected_reviews}

      Append the following review to the collected reviews:
      {chunk_review}
    output_model:
      collected_reviews: list

  - name: increment_index
    type: llm
    prompt: |
      Increment current_index: {current_index}. Output current_index + 1.
    output_model:
      current_index: int

  - name: show_reviews
    type: llm
    system: |
      You are a summarization assistant. Your task is to present the collected reviews to the user.
    prompt: |
      Here are the reviews for the pull request:
      {collected_reviews}
    output_model:
      final_summary: str
    user_message:
      - final_summary

flow:
  - from: START
    to: list_prs
  - from: list_prs
    to: select_pr
  - from: select_pr
    to: get_pr_diff
  - from: get_pr_diff
    to: chunk_pr
  - from: chunk_pr
    to: review_chunk
  - from: review_chunk
    to: collect_reviews
  - from: collect_reviews
    to: increment_index
  - from: increment_index
    edges:
      - to: review_chunk
        condition: "lambda x: x['current_index'] < len(x['pr_chunks'])"
      - to: show_reviews
        condition: "lambda x: not x['current_index'] < len(x['pr_chunks'])"
```

## Best Practices

1. **Use descriptive names**: Give nodes clear, descriptive names that indicate their purpose
2. **Keep prompts focused**: Each node should have a specific purpose and a focused prompt
3. **Use system messages**: Set appropriate system messages for LLM nodes to guide the AI's behavior
4. **Validate user input**: Use input nodes with options to restrict user input to valid choices
5. **Handle errors**: Use conditional edges to handle potential errors in the flow
6. **Use tools judiciously**: Only enable tools that are necessary for the node's function
7. **Document your YAML**: Add comments to explain complex parts of the configuration
8. **Test thoroughly**: Test your agent with various inputs to ensure it behaves as expected

## Validation

Astonish validates YAML configurations against a schema to ensure they are well-formed. You can use the `validate_yaml_with_schema` tool to validate your configurations:

```yaml
- name: validate_config
  type: llm
  prompt: |
    Validate the following YAML configuration against the schema:
    
    Configuration:
    {yaml_content}
    
    Schema:
    {yaml_schema}
  output_model:
    validation_result: str
  tools: true
  tools_selection:
    - validate_yaml_with_schema
```

## Next Steps

To learn more about YAML configuration in Astonish, check out:

- [Agentic Flows](/docs/concepts/agentic-flows) for an overview of how nodes are connected
- [Nodes](/docs/concepts/nodes) for details on node types and configuration
- [Tools](/docs/concepts/tools) for information on using tools in your agents
- [Tutorials](/docs/tutorials/creating-agents) for examples of creating agents
