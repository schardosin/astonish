---
sidebar_position: 3
---

# Tools

Tools are a powerful feature in Astonish that allow agents to interact with external systems and perform actions beyond simple text generation. They enable agents to read files, write data, execute shell commands, and more.

## What are Tools?

Tools are functions that agents can use to perform specific tasks. They are invoked by the AI model during the execution of an LLM node and can:

- Read and write files
- Execute shell commands
- Validate YAML content
- Perform web searches (via MCP)
- And much more, depending on the available tools

## Built-in Tools

Astonish comes with several built-in tools that are always available:

### read_file

Reads the contents of a file at the specified path.

```yaml
- name: read_document
  type: llm
  prompt: |
    Read the contents of the file at path: {file_path}
  output_model:
    file_content: str
  tools: true
  tools_selection:
    - read_file
```

When the AI model invokes this tool, it will:
1. Request the file path
2. Read the file contents
3. Return the contents as a string

### write_file

Writes content to a file at the specified path.

```yaml
- name: save_summary
  type: llm
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

When the AI model invokes this tool, it will:
1. Request the file path and content
2. Write the content to the file
3. Return a confirmation message

### shell_command

Executes a shell command and returns the output.

```yaml
- name: list_files
  type: llm
  prompt: |
    List the files in the directory: {directory_path}
  output_model:
    file_list: str
  tools: true
  tools_selection:
    - shell_command
```

When the AI model invokes this tool, it will:
1. Request the command to execute
2. Run the command in a shell
3. Return the stdout and stderr output

### validate_yaml_with_schema

Validates YAML content against a schema.

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

When the AI model invokes this tool, it will:
1. Request the YAML content and schema
2. Validate the content against the schema
3. Return validation results or errors

## MCP Tools

In addition to built-in tools, Astonish supports MCP (Model Context Protocol) tools. These are external tools provided by MCP servers that can extend the capabilities of your agents.

To use MCP tools:

1. Configure the MCP server in the MCP configuration file
2. Enable the tools in your agent's YAML configuration

```yaml
- name: search_web
  type: llm
  prompt: |
    Search the web for information about: {search_query}
  output_model:
    search_results: str
  tools: true
  tools_selection:
    - tavily_search  # An MCP tool for web search
```

## Using Tools in Agents

To use tools in an agent, you need to:

1. Set `tools: true` in the LLM node configuration
2. Specify the tools to use in the `tools_selection` array
3. Write a prompt that instructs the AI model to use the tools

### Example: File Reader Agent

```yaml
description: An agent that reads a file and summarizes its content
nodes:
  - name: get_file_path
    type: input
    prompt: |
      Please enter the path to the file you want to read:
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

  - name: summarize_content
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
    to: read_file_content
  - from: read_file_content
    to: summarize_content
  - from: summarize_content
    to: END
```

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

## Tool Input Schemas

Each tool has an input schema that defines the parameters it accepts. The AI model will generate input that conforms to this schema when using the tool.

For example, the `read_file` tool has the following input schema:

```typescript
class ReadFileInput {
  file_path: string;  // The path to the file to be read
}
```

The AI model will generate JSON input like:

```json
{
  "file_path": "/path/to/file.txt"
}
```

## Tool Output

Tools return their results as strings or structured data. The AI model will process this output and incorporate it into its response.

For example, the `read_file` tool returns the file contents as a string, which the AI model can then process and summarize.

## Creating Custom MCP Tools

You can extend Astonish's capabilities by creating custom MCP tools. This involves:

1. Creating an MCP server that implements the tools
2. Configuring the server in the MCP configuration file
3. Using the tools in your agents

For more information on creating custom MCP tools, see the [MCP Tools](/docs/api/tools/mcp-tools) documentation.

## Best Practices

1. **Be specific in prompts**: Clearly instruct the AI model on when and how to use tools
2. **Handle errors**: Consider what might happen if a tool fails and provide guidance
3. **Use tools judiciously**: Only enable tools that are necessary for the node's function
4. **Consider security**: Be careful with tools that can modify the system or access sensitive data
5. **Test thoroughly**: Test your agents with various inputs to ensure tools are used correctly

## Next Steps

To learn more about tools in Astonish, check out:

- [Internal Tools](/docs/api/tools/internal-tools) for details on built-in tools
- [MCP Tools](/docs/api/tools/mcp-tools) for information on extending Astonish with custom tools
- [Tutorials](/docs/tutorials/using-tools) for examples of using tools in agents
