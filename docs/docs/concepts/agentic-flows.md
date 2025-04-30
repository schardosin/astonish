---
sidebar_position: 1
---

# Agentic Flows

Agentic flows are the core concept behind Astonish. They define how AI agents process information and make decisions in a structured, step-by-step manner.

## What is an Agentic Flow?

An agentic flow is a directed graph of nodes that represent different steps in a workflow. Each node performs a specific task, such as:

- Getting input from the user
- Processing information using an AI model
- Using tools to interact with external systems
- Making decisions based on conditions

The flow defines how these nodes are connected, allowing for complex workflows with branching paths, loops, and conditional execution.

## Components of an Agentic Flow

### Nodes

Nodes are the building blocks of an agentic flow. Each node represents a step in the workflow and has a specific type that determines its behavior. Astonish supports the following node types:

#### Input Nodes

Input nodes are used to get information from the user. They display a prompt and collect the user's response.

```yaml
- name: get_user_input
  type: input
  prompt: |
    What would you like to search for?
  output_model:
    search_query: str
```

#### LLM Nodes

LLM (Language Learning Model) nodes use AI models to process information and generate responses. They can also use tools to interact with external systems.

```yaml
- name: process_query
  type: llm
  system: |
    You are a helpful assistant.
  prompt: |
    Generate a response to the user's query: {search_query}
  output_model:
    response: str
```

### Flow

The flow defines how nodes are connected. It consists of edges that specify the source node, destination node, and optional conditions.

```yaml
flow:
  - from: START
    to: get_user_input
  - from: get_user_input
    to: process_query
  - from: process_query
    to: END
```

### Conditional Edges

Conditional edges allow for branching paths in the flow based on conditions. They use lambda functions to evaluate conditions based on the current state.

```yaml
flow:
  - from: check_condition
    edges:
      - to: path_a
        condition: "lambda x: x['condition'] == True"
      - to: path_b
        condition: "lambda x: x['condition'] == False"
```

### Loops

Loops can be implemented using conditional edges and counter variables. This allows for iterative processing of data.

```yaml
flow:
  - from: process_item
    edges:
      - to: process_item
        condition: "lambda x: x['index'] < len(x['items'])"
      - to: finish
        condition: "lambda x: x['index'] >= len(x['items'])"
```

## State Management

Agentic flows maintain a state dictionary that stores variables and their values. Each node can read from and write to this state, allowing for data to be passed between nodes.

The state is initialized with variables defined in the output models of nodes and is updated as the flow executes.

## Tools

Nodes can use tools to interact with external systems, such as reading files, executing shell commands, or making API calls. Tools are specified in the node configuration and are executed by the AI model when needed.

```yaml
- name: read_file_node
  type: llm
  prompt: |
    Read the file at path: {file_path}
  output_model:
    file_content: str
  tools: true
  tools_selection:
    - read_file
```

## Visualization

Astonish provides a way to visualize agentic flows using the `flow` command:

```bash
astonish agents flow my_agent
```

This generates an ASCII representation of the flow, showing how nodes are connected.

## Example Flow

Here's an example of a complete agentic flow that reads a file and summarizes its content:

```yaml
description: File summarizer agent
nodes:
  - name: get_file_path
    type: input
    prompt: |
      Please enter the path to the file you want to summarize:
    output_model:
      file_path: str

  - name: read_file
    type: llm
    system: |
      You are a file reading assistant.
    prompt: |
      Read the contents of the file at path: {file_path}
    output_model:
      file_content: str
    tools: true
    tools_selection:
      - read_file

  - name: summarize
    type: llm
    system: |
      You are a summarization expert.
    prompt: |
      Summarize the following content:
      {file_content}
    output_model:
      summary: str
    user_message:
      - summary

flow:
  - from: START
    to: get_file_path
  - from: get_file_path
    to: read_file
  - from: read_file
    to: summarize
  - from: summarize
    to: END
```

## Benefits of Agentic Flows

Agentic flows provide several benefits:

1. **Modularity**: Each node performs a specific task, making the flow easy to understand and modify.
2. **Reusability**: Nodes and flows can be reused across different agents.
3. **Flexibility**: Conditional edges and loops allow for complex workflows.
4. **Transparency**: The flow can be visualized, making it easy to understand how the agent works.
5. **Control**: The flow provides fine-grained control over the agent's behavior.

## Next Steps

To learn more about agentic flows, check out:

- [Nodes](/docs/concepts/nodes) for more details on node types and configuration
- [YAML Configuration](/docs/concepts/yaml-configuration) for the full specification of agentic flows
- [Tutorials](/docs/tutorials/creating-agents) for examples of creating agentic flows
