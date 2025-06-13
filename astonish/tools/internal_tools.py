import subprocess
from typing import Dict, Union, List, Any, Optional
from langchain_core.tools import tool
from pydantic import BaseModel, Field
from pykwalify.core import Core
import tempfile
from unidiff import PatchSet
from astonish.core.utils import try_extract_stdout_from_string
import json
import ast
from enum import Enum

# Define input schemas
class ReadFileInput(BaseModel):
    file_path: str = Field(..., description="The path to the file to be read.")

class WriteFileInput(BaseModel):
    file_path: str = Field(..., description="The path to the file where content will be written.")
    content: Union[str, List[str]] = Field(..., description="The content to write to the file. Can be a single string or a list of strings (each list item will be a new line).")

class ExecuteCommandInput(BaseModel):
    command: str = Field(..., description="The shell command to execute.")

class ValidateGenericYAMLInput(BaseModel):
    schema_yaml: str = Field(..., description="YAML schema definition (as a string).")
    content_yaml: str = Field(..., description="YAML content to validate (as a string).")

class GitDiffAddLineNumbersInput(BaseModel):
    """Input schema for the git_diff_add_line_numbers tool."""
    diff_content: str = Field(..., description="The entire content of the PR diff (git diff format).")

class GitSplitChunkDiffInput(BaseModel):
    """Input schema for the git_split_chunk_diff tool."""
    diff_content: str = Field(..., description="The entire content of the PR diff (git diff format).")
    add_line_numbers: bool = Field(default=False, description="If True, adds line numbers to each line within the diff chunk.")


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

# Define tools using args_schema

@tool("read_file", args_schema=ReadFileInput)
def read_file(file_path: str) -> str:
    """
    Read the contents of a file. Requires 'file_path'
    """
    with open(file_path, 'r') as file:
        return file.read()

# Processing steps
# 1. The input 'content' (string or list of strings) is resolved into a single preliminary string.
#    - If 'content' is a list, its elements are joined by newlines.
#    - If 'content' is already a string, it's used as is.
# 2. This preliminary string is then checked - if it represents a JSON/dict with an 'stdout' key
#    (e.g., output from shell_command), the value of 'stdout' is extracted. This extracted
#    value can itself be a string or a list of strings.
# 3. If no 'stdout' is extracted, the preliminary string from step 1 is used.
# 4. The resulting content (either the extracted 'stdout' or the preliminary string) is then
#    prepared for file writing:
#    - If it's a list, items are joined by newlines.
#    - If it's a string, it's used directly.
# Requires 'file_path' and 'content'.
@tool("write_file", args_schema=WriteFileInput)
def write_file(file_path: str, content: Union[str, List[str]]) -> str:
    """
    Write content to a file with intelligent 'stdout' extraction.
    """
    preliminary_string_to_check: str
    if isinstance(content, list):
        preliminary_string_to_check = "\n".join(map(str, content))
    elif isinstance(content, str):
        preliminary_string_to_check = content
    else:
        return f"Error: Initial content must be a string or a list of strings. Received type: {type(content)}"

    content_after_stdout_check: Union[str, List[str]]
    extracted_stdout = try_extract_stdout_from_string(preliminary_string_to_check)

    if extracted_stdout is not None:
        content_after_stdout_check = extracted_stdout
    else:
        content_after_stdout_check = preliminary_string_to_check

    final_text_to_write: str
    if isinstance(content_after_stdout_check, list):
        final_text_to_write = "\n".join(map(str, content_after_stdout_check))
    elif isinstance(content_after_stdout_check, str):
        final_text_to_write = content_after_stdout_check
    else:
        return (f"Error: Internal error. Content for writing resolved to an unexpected type "
                f"after processing: {type(content_after_stdout_check)}.")

    try:
        with open(file_path, 'w') as file:
            file.write(final_text_to_write)
        return f"Content successfully written to {file_path}"
    except Exception as e:
        return f"Error writing to file {file_path}: {str(e)}"

@tool("shell_command", args_schema=ExecuteCommandInput)
def shell_command(command: str) -> Dict[str, str]:
    """
    Execute a shell command safely without hanging.
    """
    try:
        result = subprocess.run(
            command,
            shell=True,
            capture_output=True,
            text=True,
            stdin=subprocess.DEVNULL,
            timeout=120
        )
        return {"stdout": result.stdout, "stderr": result.stderr}
    except subprocess.TimeoutExpired:
        return {"stdout": "", "stderr": "Command timed out."}

@tool("validate_yaml_with_schema", args_schema=ValidateGenericYAMLInput)
def validate_yaml_with_schema(schema_yaml: str, content_yaml: str) -> Dict[str, Union[str, List[str]]]:
    """
    Validate YAML content against a provided YAML schema using pykwalify.
    """
    try:
        # Write schema and content to temp files
        with tempfile.NamedTemporaryFile('w+', suffix=".yaml", delete=False) as schema_file, \
             tempfile.NamedTemporaryFile('w+', suffix=".yaml", delete=False) as content_file:
            schema_file.write(schema_yaml)
            content_file.write(content_yaml)
            schema_file.flush()
            content_file.flush()

            # Run pykwalify
            core = Core(source_file=content_file.name, schema_files=[schema_file.name])
            core.validate()

        return {"message": "YAML is valid."}
    
    except Exception as e:
        print(f"Validation error: {e}")
        return {"errors": [str(e)]}

@tool("git_diff_add_line_numbers", args_schema=GitDiffAddLineNumbersInput)
def git_diff_add_line_numbers(diff_content: str) -> str:
    """
    Parses a PR diff string or a patch snippet and adds line numbers to 
    each line of change, returning the formatted result as a single string.
    """
    processed_diff = diff_content.strip()
    is_partial_patch = False

    if processed_diff.startswith('@@') and '--- a/' not in processed_diff:
        is_partial_patch = True
        processed_diff = "--- a/file.patch\n+++ b/file.patch\n" + processed_diff

    try:
        patch_set = PatchSet(processed_diff.splitlines(keepends=True))
        if not any([patch_set.modified_files, patch_set.added_files, patch_set.removed_files]):
            return "No file changes were found in the provided diff."
            
    except Exception as e:
        error_context = "This may be due to an unrecognized diff format."
        return f"Critical error while parsing the PR diff: {str(e)}. {error_context}"

    formatted_diff_parts = []
    for patched_file in patch_set:
        if patched_file.is_binary_file:
            source = patched_file.source_file or 'source_binary'
            target = patched_file.target_file or 'target_binary'
            formatted_diff_parts.append(f"--- a/{source}\n+++ b/{target}\nBinary files differ\n")
            continue

        if not is_partial_patch:
            formatted_diff_parts.append(str(patched_file.header))

        for hunk in patched_file:
            # --- FIX IS HERE ---
            # Manually reconstruct the hunk header from its attributes.
            hunk_header = (
                f"@@ -{hunk.source_start},{hunk.source_length} "
                f"+{hunk.target_start},{hunk.target_length} @@"
                f" {hunk.section_header}".rstrip() # Use rstrip to remove trailing space if no section_header
            )
            formatted_diff_parts.append(hunk_header)
            # --- END OF FIX ---

            for line in hunk:
                old_ln = str(line.source_line_no or '').rjust(4)
                new_ln = str(line.target_line_no or '').rjust(4)
                content = line.value.rstrip('\n\r')
                formatted_diff_parts.append(f"{line.line_type}{old_ln} {new_ln} {content}")
    
    return "\n".join(formatted_diff_parts)

@tool("git_split_chunk_diff", args_schema=GitSplitChunkDiffInput)
def git_split_chunk_diff(diff_content: str, add_line_numbers: bool = False) -> List[Dict[str, Any]]:
    """
    Parses a PR diff string and breaks it down into reviewable chunks,
    one for each hunk of change.
    """
    if not diff_content.strip():
        return [{"file_path": "N/A", "chunk_type": "empty_diff_input", "content": "The provided PR diff content was empty.", "metadata": None}]

    try:
        patch_set = PatchSet(diff_content.splitlines(keepends=True))
    except Exception as e:
        return [{"file_path": "N/A", "chunk_type": "diff_parse_error", "content": f"Critical error parsing PR diff: {str(e)}", "metadata": None}]

    review_chunks: List[Dict[str, Any]] = []
    for patched_file in patch_set:
        file_path = patched_file.path
        if patched_file.is_binary_file:
            review_chunks.append({
                "file_path": file_path, "chunk_type": "binary_file_summary",
                "content": f"File: {file_path}\nThis is a binary file. Review manually.",
                "metadata": {"is_binary": True}
            })
            continue

        hunks_from_file = list(patched_file)
        if not hunks_from_file:
            continue

        for hunk_num, hunk in enumerate(hunks_from_file):
            hunk_context_summary = f"File: '{file_path}' | Hunk {hunk_num + 1}/{len(hunks_from_file)}"
            
            formatted_hunk_lines = []
            for line in hunk:
                content = line.value.rstrip('\n\r')
                if add_line_numbers:
                    old_ln = str(line.source_line_no or '').rjust(4)
                    new_ln = str(line.target_line_no or '').rjust(4)
                    formatted_hunk_lines.append(f"{line.line_type}{old_ln} {new_ln} {content}")
                else:
                    formatted_hunk_lines.append(f"{line.line_type}{content}")
            
            diff_section = "\n".join(formatted_hunk_lines)
            format_header = "(<+/-><old#><new#><content>)" if add_line_numbers else "(<+/-><content>)"

            chunk_content = (
                f"{hunk_context_summary}\n"
                f"Relevant diff section {format_header}:\n"
                "```diff\n"
                f"{diff_section}\n"
                "```"
            )

            review_chunks.append({
                "file_path": file_path, "chunk_type": "hunk",
                "content": chunk_content,
                "metadata": {
                    "source_start_line": hunk.source_start, "target_start_line": hunk.target_start,
                    "hunk_index": hunk_num + 1, "total_hunks": len(hunks_from_file)
                }
            })

    if not review_chunks and diff_content and diff_content.strip():
        return [{"file_path": "N/A", "chunk_type": "no_reviewable_content", "content": "No reviewable file changes were extracted.", "metadata": None}]

    return review_chunks

# If 'current_value' is None (or not provided), it defaults to 0. This is useful for
# initializing variables or performing operations on variables that might not exist yet.

# For example
# - To initialize or set a variable 'my_var' to 5: current_value=None, operation='set', operand=5 (Result: 5)
# - To increment 'my_var' (currently 10) by 1: current_value=10, operation='add', operand=1 (Result: 11)
# - To initialize 'my_var' to 0 if it doesn't exist: current_value=None, operation='add', operand=0 (Result: 0)
# - To initialize 'my_var' to 7 if it doesn't exist, via an add: current_value=None, operation='add', operand=7 (Result: 7)

# Supported operations 
# 'add', 'subtract', 'multiply', 'divide', 'set'.

# Returns the calculated numerical result, or an error string for invalid operations (e.g., division by zero).
@tool("perform_calculation", args_schema=PerformCalculationInput)
def perform_calculation(current_value: Optional[Union[int, float]], operation: MathOperation, operand: Union[int, float]) -> Union[int, float, str]:
    """
    Performs a specified mathematical operation on a 'current_value' and an 'operand'.
    """
    # Default current_value to 0.0 if it's None
    # Using float for internal calculation to handle division and mixed types, then convert to int if possible.
    val_to_use = 0.0 if current_value is None else float(current_value)
    operand_float = float(operand)

    result: Union[float, str] # Intermediate result can be float or error string

    if operation == MathOperation.ADD:
        result = val_to_use + operand_float
    elif operation == MathOperation.SUBTRACT:
        result = val_to_use - operand_float
    elif operation == MathOperation.MULTIPLY:
        result = val_to_use * operand_float
    elif operation == MathOperation.DIVIDE:
        if operand_float == 0:
            return "Error: Division by zero."
        result = val_to_use / operand_float
    elif operation == MathOperation.SET:
        result = operand_float
    else:
        # This case should ideally not be reached if MathOperation enum and Pydantic validation are used
        return f"Error: Unknown or unsupported operation '{operation}'."

    # If the result is an error string, return it
    if isinstance(result, str):
        return result

    # Try to return an int if the result is mathematically an integer
    # (e.g., 5.0 becomes 5, but 5.5 remains 5.5)
    if result == int(result):
        return int(result)
    return result

def _get_nested_value(data: Dict, path: List[str]) -> Any:
    """
    Safely retrieves a value from a nested dictionary using a path list.
    Returns None if the path doesn't exist.
    """
    current = data
    for key in path:
        if isinstance(current, dict) and key in current:
            current = current[key]
        elif isinstance(current, list) and key.isdigit():
            try:
                current = current[int(key)]
            except IndexError:
                return None
        else:
            return None
    return current

def _set_nested_value(data: Dict, path: List[str], value: Any):
    """
    Sets a value in a nested dictionary, creating keys if they don't exist.
    """
    for key in path[:-1]:
        data = data.setdefault(key, {})
    data[path[-1]] = value

def _filter_item(item: Any, fields: List[str]) -> Any:
    """
    Recursively filters an item (dict or list) based on the fields.
    """
    if isinstance(item, dict):
        result = {}
        for field in fields:
            path = field.split('.')
            value = _get_nested_value(item, path)

            if value is not None:
                 _set_nested_value(result, path, value)
        return result
    elif isinstance(item, list):
        return [_filter_item(sub_item, fields) for sub_item in item if isinstance(sub_item, dict)]
    else:
        return item

class FilterJsonInput(BaseModel):
    """Input schema for the filter_json tool."""
    json_data: Union[str, List[Dict[str, Any]], Dict[str, Any]] = Field(
        ...,
        description="The JSON data to filter. Can be a JSON string, a Python list of dicts, or a Python dict."
    )
    fields_to_extract: List[str] = Field(
        ...,
        description="A list of fields to extract. Use dot notation for nested fields (e.g., 'user.login', 'head.repo.full_name')."
    )

# This tool helps reduce the amount of data sent to an LLM by extracting only the
# essential information from large JSON responses (e.g., from APIs).

# For example, given a list of PRs, you can extract just the number and title:
# filter_json(json_data=pr_list, fields_to_extract=["number", "title"])

# To extract nested information like the user's login and the head repo name:
# filter_json(json_data=pr_list, fields_to_extract=["number", "title", "user.login", "head.repo.name"])
@tool("filter_json", args_schema=FilterJsonInput)
def filter_json(json_data: Union[str, List[Dict[str, Any]], Dict[str, Any]], fields_to_extract: List[str]) -> Union[List[Dict[str, Any]], Dict[str, Any], str]:
    """
    Filters JSON data (either a single object or a list of objects) to include only a specified set of fields, supporting nested fields via dot notation.
    """
    try:
        if isinstance(json_data, str):
            # Try to handle potential 'stdout' wrapping from shell commands
            try:
                parsed_maybe = json.loads(json_data, strict=False)
                if isinstance(parsed_maybe, dict) and 'stdout' in parsed_maybe:
                    data_str = parsed_maybe['stdout']
                    # If stdout is a string itself (likely JSON), parse it again
                    data = json.loads(data_str, strict=False) if isinstance(data_str, str) else data_str
                else:
                    data = parsed_maybe
            except json.JSONDecodeError:
                # If it's not JSON, maybe it's a Python literal? Risky, but try ast.
                try:
                    import ast
                    parsed_maybe = ast.literal_eval(json_data)
                    if isinstance(parsed_maybe, dict) and 'stdout' in parsed_maybe:
                         data = parsed_maybe['stdout']
                    else:
                         data = parsed_maybe
                except (ValueError, SyntaxError):
                     return f"Error: Input string is not valid JSON or Python literal."

        else:
            data = json_data

    except json.JSONDecodeError as e:
        return f"Error: Invalid JSON input - {str(e)}"
    except Exception as e:
        return f"Error processing input data: {str(e)}"

    if not isinstance(data, (list, dict)):
         return "Error: Parsed data must be a JSON object or a list of JSON objects."

    if isinstance(data, list):
        # Ensure we only process dictionaries within the list
        return [_filter_item(item, fields_to_extract) for item in data if isinstance(item, dict)]
    elif isinstance(data, dict):
        return _filter_item(data, fields_to_extract)
    else:
        # This case should ideally not be reached based on the check above.
        return "Error: Unexpected data type after parsing."

# Export the list of tools
tools = [read_file, write_file, shell_command, validate_yaml_with_schema, git_diff_add_line_numbers, git_split_chunk_diff, perform_calculation, filter_json]
