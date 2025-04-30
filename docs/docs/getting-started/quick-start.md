---
sidebar_position: 3
---

# Quick Start Guide

This guide will help you get started with Astonish by creating and running your first agentic workflow.

## Prerequisites

Before you begin, make sure you have:

1. [Installed Astonish](/docs/getting-started/installation)
2. [Configured an AI provider](/docs/getting-started/configuration)

## Creating Your First Agent

Astonish comes with a built-in agent creator that helps you design new agentic workflows. Let's use it to create a simple agent.

### Step 1: Run the Agent Creator

```bash
astonish agents run agents_creator
```

This will start the agent creator, which will guide you through the process of creating a new agent.

### Step 2: Describe Your Agent

When prompted, provide a description of what you want your agent to do. For example:

```
I want to create an agent that reads a file, extracts key information, and summarizes it for the user.
```

The agent creator will use this description to generate a YAML configuration for your agent.

### Step 3: Review and Refine

The agent creator will present you with a YAML configuration for your agent. Review it and provide feedback if needed. You can:

- Approve the configuration if it looks good
- Provide feedback to refine the configuration

### Step 4: Save the Agent

Once you're satisfied with the configuration, the agent creator will save it to your config directory. It will provide you with the name of the saved agent.

## Running Your Agent

Now that you've created an agent, let's run it:

```bash
astonish agents run your_agent_name
```

Replace `your_agent_name` with the name of the agent you just created.

## Example: File Summarizer Agent

Let's walk through an example of creating and running a file summarizer agent:

### Step 1: Run the Agent Creator

```bash
astonish agents run agents_creator
```

### Step 2: Describe Your Agent

```
I want to create an agent that reads a file, extracts key information, and summarizes it for the user.
```

### Step 3: Review and Refine

The agent creator will generate a YAML configuration similar to this:

```yaml
description: An agent that reads a file, extracts key information, and summarizes it for the user
nodes:
  - name: get_file_path
    type: input
    prompt: |
      Please enter the path to the file you want to analyze:
    output_model:
      file_path: str

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

  - name: extract_key_info
    type: llm
    system: |
      You are an AI assistant specialized in extracting and summarizing key information from text.
    prompt: |
      Analyze the following file content and extract the core information:

      {file_content}

      Provide a concise summary of the key points.
    output_model:
      summary: str

  - name: present_summary
    type: llm
    system: |
      You are a helpful AI assistant presenting information to users.
    prompt: |
      Present the following summary to the user in a clear and engaging manner:

      {summary}
    output_model:
      final_response: str
    user_message:
      - final_response

flow:
  - from: START
    to: get_file_path
  - from: get_file_path
    to: read_file_content
  - from: read_file_content
    to: extract_key_info
  - from: extract_key_info
    to: present_summary
  - from: present_summary
    to: END
```

### Step 4: Save the Agent

The agent creator will save the configuration and provide you with the name of the saved agent, for example, `file_summarizer`.

### Step 5: Run the Agent

```bash
astonish agents run file_summarizer
```

The agent will:
1. Ask you for the path to a file
2. Read the file using the `read_file` tool
3. Extract key information from the file content
4. Present a summary to you

## Visualizing the Agent Flow

To better understand how your agent works, you can visualize its flow:

```bash
astonish agents flow file_summarizer
```

This will print an ASCII representation of the agent's flow, showing how the nodes are connected.

## Next Steps

Now that you've created and run your first agent, you can:

1. Learn more about [Agentic Flows](/docs/concepts/agentic-flows) to understand how Astonish works
2. Explore the [YAML Configuration](/docs/concepts/yaml-configuration) to customize your agents
3. Check out the [Tutorials](/docs/tutorials/creating-agents) for more advanced examples
4. Learn about [Tools](/docs/concepts/tools) to extend your agents' capabilities
