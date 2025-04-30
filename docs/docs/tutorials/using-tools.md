---
sidebar_position: 2
---

# Using Tools

This tutorial will guide you through the process of using tools in Astonish agents, from basic built-in tools to advanced MCP tools.

## Prerequisites

Before you begin, make sure you have:

1. [Installed Astonish](/docs/getting-started/installation)
2. [Configured an AI provider](/docs/getting-started/configuration)
3. Basic understanding of [Agentic Flows](/docs/concepts/agentic-flows) and [Tools](/docs/concepts/tools)

## Understanding Tools in Astonish

Tools in Astonish allow agents to interact with external systems and perform actions beyond simple text generation. They enable agents to:

- Read and write files
- Execute shell commands
- Validate YAML content
- Perform web searches (via MCP)
- And much more, depending on the available tools

## Using Built-in Tools

Astonish comes with several built-in tools that are always available:

### Reading Files

The `read_file` tool allows agents to read the contents of files:

```yaml
- name: read_document
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

When this node is executed, the AI model will:
1. Recognize that it needs to read a file
2. Use the `read_file` tool with the file path from the prompt
3. Store the file contents in the `file_content` variable

### Writing Files

The `write_file` tool allows agents to write content to files:

```yaml
- name: save_summary
  type: llm
  system: |
    You are a file writing assistant.
  prompt: |
    Save the following summary to a file:
    {summary}
    
    File path: {output_path}
  output_model:
    save_result: str
  tools: true
  tools_selection:
    - write_file
```

When this node is executed, the AI model will:
1. Recognize that it needs to write to a file
2. Use the `write_file` tool with the file path and content
3. Store the result message in the `save_result` variable

### Executing Shell Commands

The `shell_command` tool allows agents to execute shell commands:

```yaml
- name: list_files
  type: llm
  system: |
    You are a system command assistant.
  prompt: |
    List the files in the directory: {directory_path}
  output_model:
    file_list: str
  tools: true
  tools_selection:
    - shell_command
```

When this node is executed, the AI model will:
1. Recognize that it needs to execute a shell command
2. Use the `shell_command` tool with the appropriate command (e.g., `ls {directory_path}`)
3. Store the command output in the `file_list` variable

### Validating YAML

The `validate_yaml_with_schema` tool allows agents to validate YAML content against a schema:

```yaml
- name: validate_config
  type: llm
  system: |
    You are a YAML validation assistant.
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

When this node is executed, the AI model will:
1. Recognize that it needs to validate YAML
2. Use the `validate_yaml_with_schema` tool with the YAML content and schema
3. Store the validation results in the `validation_result` variable

## Tool Approval

By default, tool usage requires user approval. This means that when an AI model wants to use a tool, the user will be prompted to approve or deny the tool usage.

You can configure automatic approval for tools using the `tools_auto_approval` field:

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
  tools_auto_approval: true  # Tools will be used without user approval
```

## Using MCP Tools

MCP (Model Context Protocol) tools extend the capabilities of Astonish by connecting to external services. To use MCP tools:

1. Configure the MCP server in the MCP configuration file
2. Enable the tools in your agent's YAML configuration

### Configuring MCP Servers

To configure an MCP server, use the `astonish tools edit` command to open the MCP configuration file:

```bash
astonish tools edit
```

Add your MCP server configuration:

```json
{
  "mcpServers": {
    "tavily": {
      "command": "node",
      "args": ["/path/to/tavily-server.js"],
      "env": {
        "TAVILY_API_KEY": "your-api-key"
      }
    }
  }
}
```

### Using MCP Tools in Agents

Once you've configured an MCP server, you can use its tools in your agents:

```yaml
- name: search_web
  type: llm
  system: |
    You are a web search assistant.
  prompt: |
    Search the web for information about: {search_query}
  output_model:
    search_results: str
  tools: true
  tools_selection:
    - tavily_search  # An MCP tool for web search
```

## Example: File Processing Agent

Here's an example of an agent that reads a file, processes its content, and writes the results to a new file:

```yaml
description: File processing agent that reads, processes, and writes files
nodes:
  - name: get_input_file
    type: input
    prompt: |
      Please enter the path to the input file:
    output_model:
      input_file: str

  - name: get_output_file
    type: input
    prompt: |
      Please enter the path for the output file:
    output_model:
      output_file: str

  - name: read_input_file
    type: llm
    system: |
      You are a file reading assistant.
    prompt: |
      Read the contents of the file at path: {input_file}
    output_model:
      file_content: str
    tools: true
    tools_selection:
      - read_file

  - name: process_content
    type: llm
    system: |
      You are a text processing expert.
    prompt: |
      Process the following content:
      
      {file_content}
      
      Extract all dates in the format YYYY-MM-DD and create a list.
    output_model:
      processed_content: str

  - name: write_output_file
    type: llm
    system: |
      You are a file writing assistant.
    prompt: |
      Write the following content to the file at path: {output_file}
      
      {processed_content}
    output_model:
      write_result: str
    tools: true
    tools_selection:
      - write_file
    user_message:
      - write_result

flow:
  - from: START
    to: get_input_file
  - from: get_input_file
    to: get_output_file
  - from: get_output_file
    to: read_input_file
  - from: read_input_file
    to: process_content
  - from: process_content
    to: write_output_file
  - from: write_output_file
    to: END
```

## Example: Web Research Agent with MCP Tools

Here's an example of an agent that uses MCP tools to perform web research:

```yaml
description: Web research agent using MCP tools
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
      - tavily_search
    tools_auto_approval: false

  - name: generate_summary
    type: llm
    system: |
      You are a summarization expert.
    prompt: |
      Create a comprehensive summary about {research_topic} based on these search results:
      
      {search_results}
      
      The summary should be well-structured and informative.
    output_model:
      summary: str
    user_message:
      - summary

flow:
  - from: START
    to: get_research_topic
  - from: get_research_topic
    to: search_web
  - from: search_web
    to: generate_summary
  - from: generate_summary
    to: END
```

## Combining Multiple Tools

You can enable multiple tools in a single node:

```yaml
- name: advanced_processing
  type: llm
  system: |
    You are an advanced processing assistant.
  prompt: |
    Process the data in file: {input_file}
    Save the results to: {output_file}
    Execute any necessary system commands to complete the task.
  output_model:
    processing_result: str
  tools: true
  tools_selection:
    - read_file
    - write_file
    - shell_command
```

## Best Practices

1. **Be specific in prompts**: Clearly instruct the AI model on when and how to use tools
2. **Handle errors**: Consider what might happen if a tool fails and provide guidance
3. **Use tools judiciously**: Only enable tools that are necessary for the node's function
4. **Consider security**: Be careful with tools that can modify the system or access sensitive data
5. **Test thoroughly**: Test your agents with various inputs to ensure tools are used correctly
6. **Use tool approval**: Consider which tools should require user approval and which can be auto-approved

## Troubleshooting

### Tool Not Found

If you encounter a "Tool not found" error, check that:

1. The tool name is spelled correctly in the `tools_selection` array
2. For MCP tools, the MCP server is properly configured and running
3. For built-in tools, you're using the correct name (`read_file`, `write_file`, `shell_command`, or `validate_yaml_with_schema`)

### Permission Denied

If you encounter a "Permission denied" error when using file tools:

1. Check that the file paths are correct
2. Ensure the user running Astonish has permission to read/write the specified files
3. For shell commands, ensure the user has permission to execute the command

### Tool Execution Failed

If a tool execution fails:

1. Check the error message for specific details
2. Verify that the tool's input parameters are correct
3. For MCP tools, check that the MCP server is running and properly configured
4. Try running the tool manually to see if it works outside of Astonish

### MCP Server Not Connected

If an MCP server is not connecting:

1. Check that the server is properly configured in the MCP configuration file
2. Verify that the server command and arguments are correct
3. Ensure any required environment variables are set
4. Check the server logs for error messages

## Next Steps

Now that you know how to use tools in Astonish, you can:

1. Learn about [Advanced Flows](/docs/tutorials/advanced-flows) for more complex agent patterns
2. Explore the [API Reference](/docs/api/tools/internal-tools) for more details on built-in tools
3. Check out the [MCP Tools](/docs/api/tools/mcp-tools) documentation for creating custom tools
