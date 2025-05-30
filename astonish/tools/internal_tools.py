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

class ChunkPRDiffInput(BaseModel):
    diff_content: str = Field(..., description="The entire content of the PR diff (git diff format).")

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

@tool("chunk_pr_diff", args_schema=ChunkPRDiffInput)
def chunk_pr_diff(diff_content: str) -> List[Dict[str, Any]]:
    """
    Parses a PR diff string (git diff format) and breaks it down into reviewable chunks,
    including line numbers for each line in the diff.
    """

    def extract_diff_from_jsonish_input(input_str: str) -> Union[str, None]:
        """
        Detects and extracts the diff content from a JSON or pseudo-JSON string
        where the diff is stored under a key like 'stdout'.
        Returns the raw diff string or None if it cannot be extracted.
        """
        stripped = input_str.strip()
        if not stripped:
            return None

        # Try proper JSON first
        try:
            parsed = json.loads(stripped, strict=False)
            if isinstance(parsed, dict) and 'stdout' in parsed:
                return parsed['stdout']
        except json.JSONDecodeError:
            pass

        # Fallback to Python-style dicts (e.g., using single quotes)
        try:
            parsed = ast.literal_eval(stripped)
            if isinstance(parsed, dict) and 'stdout' in parsed:
                return parsed['stdout']
        except (ValueError, SyntaxError):
            pass

        # Not JSON or dict-like
        return input_str

    diff_content = extract_diff_from_jsonish_input(diff_content) or "" # Ensure it's a string
    if not diff_content.strip():
        return [{
            "file_path": "N/A",
            "chunk_type": "empty_diff_input",
            "content": "The provided PR diff content was empty or whitespace only.",
            "metadata": None
        }]

    try:
        # PatchSet expects an iterable of lines. Using splitlines(True) preserves newlines.
        patch_set = PatchSet(diff_content.splitlines(keepends=True))
    except Exception as e:
        return [{
            "file_path": "N/A",
            "chunk_type": "diff_parse_error",
            "content": f"Critical error while trying to parse the PR diff. Error: {str(e)}\n\nFirst 500 chars of diff (approx):\n{diff_content[:500]}",
            "metadata": None
        }]

    review_chunks: List[Dict[str, Any]] = []

    if not patch_set.modified_files and not patch_set.added_files and not patch_set.removed_files:
         return [{
            "file_path": "N/A",
            "chunk_type": "no_files_in_diff",
            "content": "The PR diff was parsed, but no modified, added, or removed files were identified by the parser.",
            "metadata": None
        }]

    for patched_file in patch_set:
        file_path = patched_file.path
        file_level_info_parts = []

        old_mode = getattr(patched_file, 'old_mode', None)
        new_mode = getattr(patched_file, 'new_mode', None)

        if patched_file.is_added_file:
            file_level_info_parts.append(f"File added: {file_path}")
            if new_mode: 
                file_level_info_parts.append(f"New file mode: {new_mode}")
        elif patched_file.is_removed_file:
            file_level_info_parts.append(f"File removed: {file_path}")
            if old_mode: 
                file_level_info_parts.append(f"Old file mode was: {old_mode}")
        else:
            file_level_info_parts.append(f"File modified: {file_path}")
            if old_mode is not None and new_mode is not None and old_mode != new_mode:
                file_level_info_parts.append(f"Mode changed from {old_mode} to {new_mode}.")
            elif new_mode is not None and old_mode is None :
                 file_level_info_parts.append(f"File mode is {new_mode} (old mode not specified in diff).")

        file_context_header = "\n".join(file_level_info_parts)

        if patched_file.is_binary_file:
            review_chunks.append({
                "file_path": file_path,
                "chunk_type": "binary_file_summary",
                "content": file_context_header + "\nThis is a binary file. Review manually if necessary.",
                "metadata": {"is_binary": True}
            })
            continue

        try:
            hunks_from_file = list(patched_file)
        except TypeError:
            review_chunks.append({
                "file_path": file_path,
                "chunk_type": "hunk_iteration_error",
                "content": file_context_header + "\nError: Could not iterate over hunks for this file.",
                "metadata": None
            })
            continue

        if not hunks_from_file:
            review_chunks.append({
                "file_path": file_path,
                "chunk_type": "file_info_no_hunks",
                "content": file_context_header + "\nNo content hunks to review for this file.",
                "metadata": None
            })
            continue

        # Process each hunk as a separate chunk
        for hunk_num, hunk in enumerate(hunks_from_file):
            hunk_details_parts = [
                f"Hunk {hunk_num + 1}/{len(hunks_from_file)} for file '{file_path}'.",
                f"Changes affect lines from ~{hunk.source_start} (old file) and ~{hunk.target_start} (new file)."
            ]
            if hunk.section_header:
                hunk_details_parts.append(f"Context: {hunk.section_header.strip()}")

            hunk_context_summary = "\n".join(hunk_details_parts)

            formatted_hunk_lines = []
            # Iterate through each line in the hunk
            for line in hunk:
                # Get old and new line numbers, use spaces if None
                old_ln = str(line.source_line_no or '').rjust(4)
                new_ln = str(line.target_line_no or '').rjust(4)
                
                # Get the line content, remove the original +,-,' ' and any trailing newline
                content = line.value[1:].rstrip('\n')
                
                # Reconstruct the line with the line type, numbers, and content
                formatted_hunk_lines.append(f"{line.line_type}{old_ln} {new_ln} {content}")

            # Join the formatted lines into a single string for the diff block
            diff_section_with_lines = "\n".join(formatted_hunk_lines)

            chunk_content_for_llm = (
                f"{hunk_context_summary}\n"
                "Relevant diff section (Format: <+/-><old_ln><new_ln><content>) to review:\n"
                "```diff\n"
                f"{diff_section_with_lines}\n"
                "```"
            )

            review_chunks.append({
                "file_path": file_path,
                "chunk_type": "hunk",
                "content": chunk_content_for_llm,
                "metadata": {
                    "source_start_line": hunk.source_start,
                    "source_length": hunk.source_length,
                    "target_start_line": hunk.target_start,
                    "target_length": hunk.target_length,
                    "section_header": hunk.section_header.strip() if hunk.section_header else None,
                    "hunk_index_in_file": hunk_num + 1,
                    "total_hunks_in_file": len(hunks_from_file)
                }
            })

    if not review_chunks and diff_content and diff_content.strip():
        review_chunks.append({
            "file_path": "N/A",
            "chunk_type": "no_reviewable_content_extracted",
            "content": "The PR diff was processed, but no reviewable file changes or hunks were extracted.",
            "metadata": None
        })

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
tools = [read_file, write_file, shell_command, validate_yaml_with_schema, chunk_pr_diff, perform_calculation, filter_json]
