---
sidebar_position: 3
---

# Tools

Tools are a powerful feature in Astonish that allow agents to interact with external systems and perform actions beyond simple text generation. They enable agents to read files, write data, execute shell commands, and more.

## What are Tools?

Tools are functions that agents can use to perform specific tasks. They can be:

- Invoked by the AI model during the execution of an LLM node
- Executed directly through a Tool node without LLM involvement

Tools can:
- Read and write files
- Execute shell commands
- Validate YAML content
- Perform web searches (via MCP)
- Process and transform data
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

### chunk_pr_diff

Parses a PR diff string (git diff format) and breaks it down into reviewable chunks.

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

When this tool is executed, it will:
1. Parse the PR diff content
2. Break it down into reviewable chunks by file and hunk
3. Return a list of chunks, each containing file path, chunk type, content, and metadata
4. This is particularly useful for reviewing large PRs by dividing them into smaller, manageable pieces

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

There are two ways to use tools in Astonish:

### 1. Using Tools with LLM Nodes

In this approach, the AI model decides when and how to use tools based on the prompt:

1. Set `tools: true` in the LLM node configuration
2. Specify the tools to use in the `tools_selection` array
3. Write a prompt that instructs the AI model to use the tools

### 2. Direct Tool Execution with Tool Nodes

For operations that don't require AI reasoning, you can execute tools directly:

1. Set `type: tool` in the node configuration
2. Specify the tool to use in the `tools_selection` array
3. Provide arguments to the tool in the `args` object

### Example: File Reader Agent with LLM

```yaml
description: An agent that reads a file and summarizes its content using LLM
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

### Example: Direct Tool Execution

```yaml
description: An agent that processes a PR diff using direct tool execution
nodes:
  - name: get_pr_diff
    type: llm
    system: |
      You are a GitHub CLI expert.
    prompt: |
      Use the 'gh pr diff' command to get the diff for PR number {selected_pr}.
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

flow:
  - from: START
    to: get_pr_diff
  - from: get_pr_diff
    to: chunk_pr
  - from: chunk_pr
    to: END
```

## Tool Approval

By default, tool usage in LLM nodes requires user approval. This means that when an AI model wants to use a tool, the user will be prompted to approve or deny the tool usage.

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

Note that Tool nodes (direct tool execution) do not require approval as they are explicitly configured in the flow.

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

Tools return their results as strings or structured data. There are two ways to handle tool output:

### 1. LLM Processing

By default, in LLM nodes, the AI model will process the tool output and incorporate it into its response. For example, the `read_file` tool returns the file contents as a string, which the AI model can then process and summarize.

### 2. Raw Tool Output

For large or complex tool outputs, you can use the `raw_tool_output` field in LLM nodes to store the tool output directly in the state without LLM processing:

```yaml
- name: get_pr_diff
  type: llm
  prompt: |
    Use the 'gh pr diff' command to get the diff for PR number {selected_pr}.
  output_model:
    retrieval_status: str
  tools: true
  tools_selection:
    - shell_command
  raw_tool_output:
    pr_diff: str
```

In this example, the raw output of the `shell_command` tool is stored directly in the state variable `pr_diff`, while the LLM's response is stored in `retrieval_status`.

### 3. Direct Tool Output

In Tool nodes, the tool output is stored directly in the state according to the `output_model` configuration:

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

In this example, the output of the `chunk_pr_diff` tool is stored directly in the `pr_chunks` variable.

## Creating Custom MCP Tools

You can extend Astonish's capabilities by creating custom MCP tools. This involves:

1. Creating an MCP server that implements the tools
2. Configuring the server in the MCP configuration file
3. Using the tools in your agents through either LLM nodes or Tool nodes

For more information on creating custom MCP tools, see the [MCP Tools](/docs/api/tools/mcp-tools) documentation.

## Improved ReAct Pattern

Astonish implements an improved ReAct (Reasoning and Acting) pattern for tool usage in LLM nodes. This pattern allows the AI model to:

1. **Reason** about what tool to use and how to use it
2. **Act** by executing the chosen tool
3. **Observe** the results of the tool execution
4. **Continue reasoning** based on the observations

The ReAct pattern is implemented automatically when you enable tools in an LLM node. The AI model will:

1. Generate a thought about what to do next
2. Choose a tool to execute
3. Provide input for the tool
4. Receive the tool's output as an observation
5. Generate a new thought based on the observation
6. Either use another tool or provide a final answer

This pattern enables more complex reasoning and multi-step tool usage within a single node.

## Best Practices

1. **Choose the right approach**:
   - Use LLM nodes with tools when you need AI reasoning to decide when and how to use tools
   - Use Tool nodes for direct execution when the operation is straightforward and doesn't require AI reasoning

2. **Be specific in prompts**: Clearly instruct the AI model on when and how to use tools

3. **Use raw_tool_output for large data**: When dealing with large outputs like file contents or API responses, use the `raw_tool_output` field to avoid overwhelming the LLM

4. **Handle errors**: Consider what might happen if a tool fails and provide guidance

5. **Use tools judiciously**: Only enable tools that are necessary for the node's function

6. **Consider security**: Be careful with tools that can modify the system or access sensitive data

7. **Test thoroughly**: Test your agents with various inputs to ensure tools are used correctly

## Next Steps

To learn more about tools in Astonish, check out:

- [Internal Tools](/docs/api/tools/internal-tools) for details on built-in tools
- [MCP Tools](/docs/api/tools/mcp-tools) for information on extending Astonish with custom tools
- [Tutorials](/docs/tutorials/using-tools) for examples of using tools in agents
