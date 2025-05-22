---
sidebar_position: 1
---

# Internal Tools

Astonish comes with several built-in tools that are always available to agents. These tools allow agents to interact with the file system and execute shell commands.

## Overview

The internal tools are defined in the `internal_tools.py` module and are automatically available to all agents. They provide basic functionality for reading and writing files, executing shell commands, and validating YAML content.

## Available Tools

### perform_calculation

Performs a specified mathematical operation on a current value and an operand.

#### Input Schema

```python
class MathOperation(str, Enum):
    """Defines the supported mathematical operations."""
    ADD = "add"
    SUBTRACT = "subtract"
    MULTIPLY = "multiply"
    DIVIDE = "divide"
    SET = "set"

class PerformCalculationInput(BaseModel):
    current_value: Optional[Union[int, float]] = Field(None, description="The current numerical value. If None (e.g., for initialization or if a state variable is not yet set), it will be treated as 0.")
    operation: MathOperation = Field(..., description="The mathematical operation to perform (e.g., 'add', 'set').")
    operand: Union[int, float] = Field(..., description="The second value for the operation (e.g., the amount to add, the value to set).")
```

#### Returns

- `Union[int, float, str]`: The result of the calculation, or an error message if the operation fails (e.g., division by zero).

#### Example

```yaml
- name: initialize_index
  type: tool
  args:
    current_value: {current_index}
    operation: "set"
    operand: 0
  tools_selection:
    - perform_calculation
  output_model:
    current_index: int

- name: increment_index
  type: tool
  args:
    current_value: {current_index}
    operation: "add"
    operand: 1
  tools_selection:
    - perform_calculation
  output_model:
    current_index: int
```

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

### chunk_pr_diff

Parses a PR diff string (git diff format) and breaks it down into reviewable chunks.

#### Input Schema

```python
class ChunkPRDiffInput(BaseModel):
    diff_content: str = Field(..., description="The entire content of the PR diff (git diff format).")
```

#### Returns

- `List[Dict[str, Any]]`: A list of chunks, each containing file path, chunk type, content, and metadata.

#### Example

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
tools = [read_file, write_file, shell_command, validate_yaml_with_schema, chunk_pr_diff, perform_calculation]
```

## Security Considerations

The internal tools have access to the file system and can execute shell commands, which could potentially be used maliciously. To mitigate this risk:

1. The `shell_command` tool has a timeout of 120 seconds to prevent long-running commands
2. The `shell_command` tool captures output to prevent interactive commands
3. Tool usage requires user approval by default, allowing users to review and deny potentially harmful operations

## Related Modules

- [MCP Tools](/docs/api/tools/mcp-tools): External tools provided by MCP servers
