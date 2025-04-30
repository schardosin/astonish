---
sidebar_position: 1
---

# Creating Agents

This tutorial will guide you through the process of creating custom agents in Astonish, from basic concepts to advanced techniques.

## Prerequisites

Before you begin, make sure you have:

1. [Installed Astonish](/docs/getting-started/installation)
2. [Configured an AI provider](/docs/getting-started/configuration)
3. Basic understanding of [Agentic Flows](/docs/concepts/agentic-flows) and [YAML Configuration](/docs/concepts/yaml-configuration)

## Using the Agent Creator

The easiest way to create a new agent is to use the built-in agent creator:

```bash
astonish agents run agents_creator
```

This will start an interactive process that guides you through creating a new agent:

1. You describe what you want the agent to do
2. The agent creator generates a YAML configuration
3. You review and refine the configuration
4. The agent creator saves the final configuration to your config directory

## Creating an Agent Manually

If you prefer to create an agent manually, you can create a YAML file in your config directory:

```bash
# Create the agents directory if it doesn't exist
mkdir -p ~/.config/astonish/agents

# Create a new agent file
touch ~/.config/astonish/agents/my_agent.yaml
```

Then edit the file with your favorite text editor:

```bash
# Open the file in your default editor
astonish agents edit my_agent
```

## Basic Agent Structure

A basic agent consists of:

1. A description
2. One or more nodes
3. A flow that connects the nodes

Here's a simple example of a greeting agent:

```yaml
description: A simple greeting agent
nodes:
  - name: get_name
    type: input
    prompt: |
      What is your name?
    output_model:
      user_name: str

  - name: generate_greeting
    type: llm
    system: |
      You are a friendly assistant.
    prompt: |
      Generate a warm greeting for a user named {user_name}.
    output_model:
      greeting: str
    user_message:
      - greeting

flow:
  - from: START
    to: get_name
  - from: get_name
    to: generate_greeting
  - from: generate_greeting
    to: END
```

## Adding User Input

Input nodes allow your agent to collect information from the user:

```yaml
- name: get_preference
  type: input
  prompt: |
    Do you prefer a formal or casual greeting?
  output_model:
    greeting_style: str
  options:
    - "Formal"
    - "Casual"
```

The `options` field provides a list of choices for the user, making it easier to handle the input in subsequent nodes.

## Using AI Processing

LLM nodes use AI models to process information and generate responses:

```yaml
- name: generate_greeting
  type: llm
  system: |
    You are a friendly assistant that generates greetings.
  prompt: |
    Generate a {greeting_style} greeting for a user named {user_name}.
  output_model:
    greeting: str
  user_message:
    - greeting
```

The `system` field sets the context for the AI model, while the `prompt` field provides specific instructions. The `user_message` field specifies which variables to display to the user after processing.

## Using Tools

Tools allow your agent to interact with external systems. Here's an example of using the `read_file` tool:

```yaml
- name: read_file_content
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
```

The `tools` field enables tool usage, and the `tools_selection` field specifies which tools the node can use.

## Creating Loops

You can create loops in your agent using conditional edges and counter variables:

```yaml
- name: process_item
  type: llm
  prompt: |
    Process item {current_index} from the list: {items}
    Current index: {current_index}
    Total items: {total_items}
  output_model:
    processed_item: str
    current_index: int
  limit: 10
  limit_counter_field: loop_counter

- name: increment_index
  type: llm
  prompt: |
    Increment the current index: {current_index}
  output_model:
    current_index: int

flow:
  - from: previous_node
    to: process_item
  - from: process_item
    to: increment_index
  - from: increment_index
    edges:
      - to: process_item
        condition: "lambda x: x['current_index'] < x['total_items']"
      - to: next_node
        condition: "lambda x: x['current_index'] >= x['total_items']"
```

The `limit` and `limit_counter_field` fields prevent infinite loops by limiting the number of times the node can be executed.

## Adding Branching Logic

You can create branching paths in your agent using conditional edges:

```yaml
- name: check_condition
  type: llm
  prompt: |
    Check if the user's query requires web search.
    Query: {user_query}
  output_model:
    needs_search: bool

flow:
  - from: previous_node
    to: check_condition
  - from: check_condition
    edges:
      - to: search_web
        condition: "lambda x: x['needs_search'] == True"
      - to: generate_response
        condition: "lambda x: x['needs_search'] == False"
```

The `edges` field specifies multiple possible destinations based on conditions.

## Advanced Example: Web Research Agent

Here's a more advanced example of an agent that performs web research:

```yaml
description: Web research agent that searches for information and summarizes the results
nodes:
  - name: get_research_topic
    type: input
    prompt: |
      What topic would you like to research?
    output_model:
      research_topic: str

  - name: search_web
    type: llm
    system: |
      You are a research assistant that performs high-quality web searches.
    prompt: |
      Search the web for information about: {research_topic}
      
      Return a list of search results with titles and snippets.
    output_model:
      search_results: list
    tools: true
    tools_selection:
      - web_search
    tools_auto_approval: false

  - name: extract_key_points
    type: llm
    system: |
      You are an information extraction expert.
    prompt: |
      Extract the key points from these search results:
      
      {search_results}
      
      Identify the most important facts and insights.
    output_model:
      key_points: list

  - name: generate_summary
    type: llm
    system: |
      You are a summarization expert.
    prompt: |
      Create a comprehensive summary about {research_topic} based on these key points:
      
      {key_points}
      
      The summary should be well-structured and informative.
    output_model:
      summary: str
    user_message:
      - summary

  - name: ask_for_more
    type: input
    prompt: |
      Would you like to research another topic?
    output_model:
      continue_research: str
    options:
      - "Yes"
      - "No"

flow:
  - from: START
    to: get_research_topic
  - from: get_research_topic
    to: search_web
  - from: search_web
    to: extract_key_points
  - from: extract_key_points
    to: generate_summary
  - from: generate_summary
    to: ask_for_more
  - from: ask_for_more
    edges:
      - to: get_research_topic
        condition: "lambda x: x['continue_research'] == 'Yes'"
      - to: END
        condition: "lambda x: x['continue_research'] == 'No'"
```

This agent:
1. Gets a research topic from the user
2. Searches the web for information
3. Extracts key points from the search results
4. Generates a comprehensive summary
5. Asks if the user wants to research another topic
6. Either starts over or ends the flow

## Testing Your Agent

Once you've created your agent, you can test it by running:

```bash
astonish agents run my_agent
```

You can also visualize the flow to ensure it's structured correctly:

```bash
astonish agents flow my_agent
```

## Best Practices

1. **Start simple**: Begin with a basic agent and add complexity gradually
2. **Test frequently**: Test your agent after each significant change
3. **Use descriptive names**: Give nodes clear, descriptive names
4. **Keep prompts focused**: Each node should have a specific purpose
5. **Handle errors**: Use conditional edges to handle potential errors
6. **Document your agent**: Add comments to explain complex parts
7. **Use the agent creator**: Let the agent creator generate the initial YAML and then refine it

## Next Steps

Now that you know how to create agents, you can:

1. Learn about [Using Tools](/docs/tutorials/using-tools) to extend your agents' capabilities
2. Explore [Advanced Flows](/docs/tutorials/advanced-flows) for more complex agent patterns
3. Check out the [API Reference](/docs/api/core/agent-runner) for more details on Astonish's internals
