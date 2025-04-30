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

Here's a complete example of an agent that searches the web and summarizes the results:

```yaml
description: Web search and summarization agent
nodes:
  - name: get_query
    type: input
    prompt: |
      What would you like to search for?
    output_model:
      search_query: str

  - name: search_web
    type: llm
    system: |
      You are a research assistant that performs high-quality web searches.
    prompt: |
      Please perform a web search to gather useful information on the following topic:
      
      Topic: "{search_query}"
      
      Make sure to include credible sources.
    output_model:
      search_results: list
    tools: true
    tools_selection:
      - web_search
    tools_auto_approval: false

  - name: summarize_results
    type: llm
    system: |
      You are a summarization expert.
    prompt: |
      Summarize the following search results:
      
      {search_results}
      
      Provide a concise summary that covers the main points.
    output_model:
      summary: str
    user_message:
      - summary

  - name: ask_for_more
    type: input
    prompt: |
      Would you like to search for something else?
    output_model:
      continue_search: str
    options:
      - "Yes"
      - "No"

flow:
  - from: START
    to: get_query
  - from: get_query
    to: search_web
  - from: search_web
    to: summarize_results
  - from: summarize_results
    to: ask_for_more
  - from: ask_for_more
    edges:
      - to: get_query
        condition: "lambda x: x['continue_search'] == 'Yes'"
      - to: END
        condition: "lambda x: x['continue_search'] == 'No'"
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
