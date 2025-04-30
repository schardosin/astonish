---
sidebar_position: 3
---

# Advanced Flows

This tutorial covers advanced techniques for creating complex agentic flows in Astonish, including loops, conditional branching, error handling, and more.

## Prerequisites

Before you begin, make sure you have:

1. [Installed Astonish](/docs/getting-started/installation)
2. [Configured an AI provider](/docs/getting-started/configuration)
3. Basic understanding of [Agentic Flows](/docs/concepts/agentic-flows) and [YAML Configuration](/docs/concepts/yaml-configuration)
4. Experience with [Creating Agents](/docs/tutorials/creating-agents)

## Creating Loops

Loops allow your agent to repeat a set of nodes multiple times. This is useful for processing lists of items, implementing retry logic, or creating conversational agents.

### Basic Loop with Counter

The simplest way to create a loop is using a counter variable and conditional edges:

```yaml
nodes:
  - name: initialize_counter
    type: llm
    prompt: |
      Initialize the counter to 0.
    output_model:
      counter: int

  - name: process_iteration
    type: llm
    prompt: |
      Processing iteration {counter}.
    output_model:
      result: str
      counter: int

  - name: check_counter
    type: llm
    prompt: |
      Check if counter {counter} has reached the limit of 5.
      If counter < 5, increment it by 1.
      If counter >= 5, keep it as is.
    output_model:
      counter: int
      limit_reached: bool

flow:
  - from: START
    to: initialize_counter
  - from: initialize_counter
    to: process_iteration
  - from: process_iteration
    to: check_counter
  - from: check_counter
    edges:
      - to: process_iteration
        condition: "lambda x: not x['limit_reached']"
      - to: END
        condition: "lambda x: x['limit_reached']"
```

### Using the `limit` and `limit_counter_field` Properties

Astonish provides built-in support for limiting the number of times a node can be executed in a loop:

```yaml
nodes:
  - name: process_item
    type: llm
    prompt: |
      Process item {current_index} from the list: {items}
    output_model:
      processed_item: str
      current_index: int
    limit: 10  # Maximum number of iterations
    limit_counter_field: loop_counter  # Counter variable
```

### Processing Lists

A common use case for loops is processing a list of items:

```yaml
nodes:
  - name: get_items
    type: llm
    prompt: |
      Generate a list of 5 items to process.
    output_model:
      items: list
      current_index: int  # Initialize to 0

  - name: process_item
    type: llm
    prompt: |
      Process item at index {current_index} from the list:
      {items}
      
      Current item: {items[current_index]}
    output_model:
      processed_item: str
      processed_items: list

  - name: update_index
    type: llm
    prompt: |
      Current index: {current_index}
      Total items: {items}
      
      Increment the index by 1 and check if we've processed all items.
    output_model:
      current_index: int
      is_complete: bool

flow:
  - from: START
    to: get_items
  - from: get_items
    to: process_item
  - from: process_item
    to: update_index
  - from: update_index
    edges:
      - to: process_item
        condition: "lambda x: not x['is_complete']"
      - to: END
        condition: "lambda x: x['is_complete']"
```

## Conditional Branching

Conditional branching allows your agent to take different paths based on conditions. This is useful for implementing decision trees, handling different user inputs, or adapting to different scenarios.

### Simple Branching

Here's a simple example of conditional branching:

```yaml
nodes:
  - name: get_user_preference
    type: input
    prompt: |
      Do you prefer a detailed or concise response?
    output_model:
      preference: str
    options:
      - "Detailed"
      - "Concise"

  - name: generate_detailed_response
    type: llm
    system: |
      You provide detailed, comprehensive responses.
    prompt: |
      Generate a detailed response about {topic}.
    output_model:
      response: str
    user_message:
      - response

  - name: generate_concise_response
    type: llm
    system: |
      You provide concise, to-the-point responses.
    prompt: |
      Generate a concise response about {topic}.
    output_model:
      response: str
    user_message:
      - response

flow:
  - from: START
    to: get_user_preference
  - from: get_user_preference
    edges:
      - to: generate_detailed_response
        condition: "lambda x: x['preference'] == 'Detailed'"
      - to: generate_concise_response
        condition: "lambda x: x['preference'] == 'Concise'"
  - from: generate_detailed_response
    to: END
  - from: generate_concise_response
    to: END
```

### Complex Decision Trees

You can create more complex decision trees by chaining conditional branches:

```yaml
nodes:
  - name: get_query_type
    type: input
    prompt: |
      What type of information are you looking for?
    output_model:
      query_type: str
    options:
      - "Technical"
      - "Business"
      - "General"

  - name: get_technical_detail_level
    type: input
    prompt: |
      What level of technical detail do you need?
    output_model:
      detail_level: str
    options:
      - "Beginner"
      - "Intermediate"
      - "Advanced"

  # ... more nodes for different paths ...

flow:
  - from: START
    to: get_query_type
  - from: get_query_type
    edges:
      - to: get_technical_detail_level
        condition: "lambda x: x['query_type'] == 'Technical'"
      - to: get_business_sector
        condition: "lambda x: x['query_type'] == 'Business'"
      - to: get_general_topic
        condition: "lambda x: x['query_type'] == 'General'"
  # ... more flow connections for different paths ...
```

## Error Handling

Robust agents should handle errors gracefully. You can implement error handling using conditional branches and state variables.

### Basic Error Handling

```yaml
nodes:
  - name: try_operation
    type: llm
    prompt: |
      Try to perform the operation on {input_data}.
      If successful, set success to true and store the result.
      If it fails, set success to false and provide an error message.
    output_model:
      result: str
      success: bool
      error_message: str

  - name: handle_success
    type: llm
    prompt: |
      Operation succeeded with result: {result}
      Process the successful result.
    output_model:
      processed_result: str
    user_message:
      - processed_result

  - name: handle_error
    type: llm
    prompt: |
      Operation failed with error: {error_message}
      Provide guidance on how to resolve the issue.
    output_model:
      error_guidance: str
    user_message:
      - error_guidance

flow:
  - from: START
    to: try_operation
  - from: try_operation
    edges:
      - to: handle_success
        condition: "lambda x: x['success']"
      - to: handle_error
        condition: "lambda x: not x['success']"
  - from: handle_success
    to: END
  - from: handle_error
    to: END
```

### Retry Logic

You can implement retry logic for operations that might fail temporarily:

```yaml
nodes:
  - name: initialize_retry
    type: llm
    prompt: |
      Initialize retry counter to 0.
    output_model:
      retry_count: int
      max_retries: int  # Set to 3

  - name: attempt_operation
    type: llm
    prompt: |
      Attempt operation (retry {retry_count} of {max_retries}).
    output_model:
      success: bool
      result: str
      error: str
      retry_count: int

  - name: handle_result
    type: llm
    prompt: |
      Check if operation succeeded or if we've reached max retries.
      Current retry count: {retry_count}
      Max retries: {max_retries}
      Success: {success}
    output_model:
      should_retry: bool
      retry_count: int
      final_result: str
    user_message:
      - final_result

flow:
  - from: START
    to: initialize_retry
  - from: initialize_retry
    to: attempt_operation
  - from: attempt_operation
    to: handle_result
  - from: handle_result
    edges:
      - to: attempt_operation
        condition: "lambda x: x['should_retry']"
      - to: END
        condition: "lambda x: not x['should_retry']"
```

## Combining Multiple Techniques

Real-world agents often combine multiple advanced techniques. Here's an example of an agent that processes a list of items with retry logic and error handling:

```yaml
description: Advanced agent that processes a list of items with retry logic and error handling
nodes:
  - name: get_items
    type: input
    prompt: |
      Enter a list of items to process (comma-separated):
    output_model:
      items_text: str

  - name: parse_items
    type: llm
    prompt: |
      Parse the following comma-separated list into an array:
      {items_text}
      
      Initialize current_index to 0.
    output_model:
      items: list
      current_index: int
      total_items: int

  - name: process_item
    type: llm
    prompt: |
      Process item {current_index + 1} of {total_items}:
      Current item: {items[current_index]}
      
      Initialize retry_count to 0.
      Max retries: 3
    output_model:
      retry_count: int
      max_retries: int
      current_item: str
      processed_results: list

  - name: attempt_processing
    type: llm
    prompt: |
      Attempt to process item: {current_item}
      Retry {retry_count} of {max_retries}
    output_model:
      success: bool
      result: str
      error: str
      retry_count: int

  - name: handle_attempt_result
    type: llm
    prompt: |
      Check result of processing attempt:
      Success: {success}
      Result: {result}
      Error: {error}
      Retry count: {retry_count}
      Max retries: {max_retries}
      
      Determine if we should retry or move on.
    output_model:
      should_retry: bool
      retry_count: int
      processed_results: list
      current_item_complete: bool

  - name: update_index
    type: llm
    prompt: |
      Current index: {current_index}
      Total items: {total_items}
      
      Increment the index and check if we've processed all items.
    output_model:
      current_index: int
      all_items_processed: bool

  - name: summarize_results
    type: llm
    prompt: |
      Summarize the results of processing all items:
      {processed_results}
    output_model:
      summary: str
    user_message:
      - summary

flow:
  - from: START
    to: get_items
  - from: get_items
    to: parse_items
  - from: parse_items
    to: process_item
  - from: process_item
    to: attempt_processing
  - from: attempt_processing
    to: handle_attempt_result
  - from: handle_attempt_result
    edges:
      - to: attempt_processing
        condition: "lambda x: x['should_retry']"
      - to: update_index
        condition: "lambda x: x['current_item_complete']"
  - from: update_index
    edges:
      - to: process_item
        condition: "lambda x: not x['all_items_processed']"
      - to: summarize_results
        condition: "lambda x: x['all_items_processed']"
  - from: summarize_results
    to: END
```

## Conversational Agents

You can create conversational agents that maintain a dialogue with the user:

```yaml
description: Conversational agent that maintains a dialogue with the user
nodes:
  - name: initialize_conversation
    type: llm
    prompt: |
      Initialize a conversation with the user.
      Set conversation_history to an empty list.
      Generate a greeting message.
    output_model:
      conversation_history: list
      greeting: str
    user_message:
      - greeting

  - name: get_user_input
    type: input
    prompt: |
      {greeting}
    output_model:
      user_input: str

  - name: process_user_input
    type: llm
    system: |
      You are a conversational assistant that maintains context throughout a conversation.
    prompt: |
      Process the user's input and generate a response.
      
      Conversation history:
      {conversation_history}
      
      User input:
      {user_input}
      
      Update the conversation history to include this exchange.
    output_model:
      response: str
      conversation_history: list
      should_continue: bool
    user_message:
      - response

  - name: check_continuation
    type: input
    prompt: |
      Would you like to continue the conversation? (yes/no)
    output_model:
      continue_conversation: str
    options:
      - "yes"
      - "no"

flow:
  - from: START
    to: initialize_conversation
  - from: initialize_conversation
    to: get_user_input
  - from: get_user_input
    to: process_user_input
  - from: process_user_input
    to: check_continuation
  - from: check_continuation
    edges:
      - to: get_user_input
        condition: "lambda x: x['continue_conversation'] == 'yes'"
      - to: END
        condition: "lambda x: x['continue_conversation'] == 'no'"
```

## Best Practices

1. **Plan your flow carefully**: Before implementing complex flows, sketch out the nodes and connections
2. **Use descriptive names**: Give nodes and variables clear, descriptive names
3. **Keep state manageable**: Don't overload the state with too many variables
4. **Test incrementally**: Build and test your flow in small increments
5. **Handle edge cases**: Consider what might go wrong and add appropriate error handling
6. **Use comments**: Add comments in your YAML file to explain complex logic
7. **Visualize the flow**: Use `astonish agents flow <agent>` to visualize and verify your flow

## Next Steps

Now that you've learned about advanced flows in Astonish, you can:

1. Explore the [API Reference](/docs/api/core/agent-runner) for more details on Astonish's internals
2. Check out the [MCP Tools](/docs/api/tools/mcp-tools) documentation for extending your agents' capabilities
3. Study the sample agents in the Astonish repository for more examples and inspiration
