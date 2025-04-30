---
sidebar_position: 1
---

# Internal Tools

Astonish comes with several built-in tools that are always available to agents. These tools allow agents to interact with the file system and execute shell commands.

## Overview

The internal tools are defined in the `internal_tools.py` module and are automatically available to all agents. They provide basic functionality for reading and writing files, executing shell commands, and validating YAML content.

## Available Tools

### read_file

Reads the contents of a file at the specified path.

#### Input Schema

```python
class ReadFileInput(BaseModel):
    file_path: str = Field(..., description="The path to the file to be read.")
```

#### Returns

- `str`: The contents of the file as a string.

#### Example

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

### write_file

Writes content to a file at the specified path.

#### Input Schema

```python
class WriteFileInput(BaseModel):
    file_path: str = Field(..., description="The path to the file where content will be written.")
    content: str = Field(..., description="The content to write to the file.")
```

#### Returns

- `str`: A confirmation message indicating that the content was successfully written.

#### Example

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

### shell_command

Executes a shell command and returns the output.

#### Input Schema

```python
class ExecuteCommandInput(BaseModel):
    command: str = Field(..., description="The shell command to execute.")
```

#### Returns

- `Dict[str, str]`: A dictionary containing the stdout and stderr output of the command.

#### Example

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

### validate_yaml_with_schema

Validates YAML content against a schema.

#### Input Schema

```python
class ValidateGenericYAMLInput(BaseModel):
    schema_yaml: str = Field(..., description="YAML schema definition (as a string).")
    content_yaml: str = Field(..., description="YAML content to validate (as a string).")
```

#### Returns

- `Dict[str, Union[str, List[str]]]`: A dictionary containing either a success message or a list of validation errors.

#### Example

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

## Implementation Details

### Tool Registration

The internal tools are registered using the `@tool` decorator from LangChain:

```python
@tool("read_file", args_schema=ReadFileInput)
def read_file(file_path: str) -> str:
    """
    Read the contents of a file. Requires 'file_path'
    """
    with open(file_path, 'r') as file:
        return file.read()
```

### Tool Export

The tools are exported as a list that can be used by the agent runner:

```python
# Export the list of tools
tools = [read_file, write_file, shell_command, validate_yaml_with_schema]
```

## Security Considerations

The internal tools have access to the file system and can execute shell commands, which could potentially be used maliciously. To mitigate this risk:

1. The `shell_command` tool has a timeout of 120 seconds to prevent long-running commands
2. The `shell_command` tool captures output to prevent interactive commands
3. Tool usage requires user approval by default, allowing users to review and deny potentially harmful operations

## Related Modules

- [MCP Tools](/docs/api/tools/mcp-tools): External tools provided by MCP servers
