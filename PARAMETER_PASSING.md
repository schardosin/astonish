# Parameter Passing in Astonish

This document explains how to pass parameters to Astonish agents, allowing for more automated workflows.

## Overview

Astonish now supports passing parameters directly to agents, which can be used to pre-populate input fields that would normally require user interaction. This is particularly useful for:

- Automating agent execution in scripts
- Integrating agents into larger workflows
- Running agents with predefined inputs

## Usage

### Command Line

You can pass parameters to an agent using the `-p` or `--param` flag with key=value format. You can use multiple flags for multiple parameters:

```bash
astonish agents run simple_question_answer_loop -p get_question="Who was Albert Einstein" -p continue_loop=no
```

### Using File Content

You can use shell expansion to read file content as a parameter value:

```bash
astonish agents run simple_question_answer_loop -p get_question="$(<question.txt)" -p continue_loop=no
```

This reads the content of `question.txt` and passes it as the value for the `get_question` parameter.

### Using Environment Variables

You can use environment variables as parameter values:

```bash
# Set an environment variable
export QUESTION="Who was Albert Einstein"

# Use it in the command
astonish agents run simple_question_answer_loop -p get_question="$QUESTION" -p continue_loop=no
```

## Parameter Format

Parameters are specified in key=value format where:

- Keys are the **node names** defined in the agent's YAML file
- Values are the values you want to assign to those nodes' output fields

For example, if your agent has an input node with:

```yaml
- name: get_topic
  type: input
  prompt: "What topic would you like to write about?"
  output_model:
    user_request: str
```

You can pass a parameter with the key `get_topic` to pre-populate this node's output:

```
-p get_topic="Artificial Intelligence"
```

## Behavior

When a parameter is provided for an input node:

1. The node will check if there's a parameter with its name in the parameters dictionary
2. If there is, it will use that value instead of prompting the user
3. The prompt will still be displayed to show what question would have been asked
4. The agent will continue execution as if the user had provided the input

If a parameter is not provided for an input node, the agent will prompt the user for input as usual.

## Example

Consider this simple agent:

```yaml
description: Simple Agent to Respond to User Questions in a Loop
nodes:
- name: get_question
  type: input
  prompt: |
    What is your question?
  output_model:
    question: str
- name: answer_question
  type: llm
  system: |
    You are a helpful assistant.
  prompt: |
    Answer the following question: "{question}"
  output_model:
    answer: str
  user_message:
    - answer
- name: continue_loop
  type: input
  prompt: |
    Do you want to continue asking questions?
  output_model:
    continue: str
  options:
    - "yes"
    - "no"
flow:
- from: START
  to: get_question
- from: get_question
  to: answer_question
- from: answer_question
  to: continue_loop
- from: continue_loop
  edges:
  - to: get_question
    condition: "lambda x: x['continue'] == 'yes'"
  - to: END
    condition: "lambda x: x['continue'] == 'no'"
```

You would call it like this:

```bash
astonish agents run simple_question_answer_loop -p get_question="Who was Albert Einstein" -p continue_loop=no
```
