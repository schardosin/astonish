import subprocess
from typing import Dict, Union, List, Any
from langchain_core.tools import tool
from pydantic import BaseModel, Field
from pykwalify.core import Core
import tempfile
from unidiff import PatchSet
import json
import ast

# Define input schemas
class ReadFileInput(BaseModel):
    file_path: str = Field(..., description="The path to the file to be read.")

class WriteFileInput(BaseModel):
    file_path: str = Field(..., description="The path to the file where content will be written.")
    content: str = Field(..., description="The content to write to the file.")

class ExecuteCommandInput(BaseModel):
    command: str = Field(..., description="The shell command to execute.")

class ValidateGenericYAMLInput(BaseModel):
    schema_yaml: str = Field(..., description="YAML schema definition (as a string).")
    content_yaml: str = Field(..., description="YAML content to validate (as a string).")

class ChunkPRDiffInput(BaseModel):
    diff_content: str = Field(..., description="The entire content of the PR diff (git diff format).")

# Define tools using args_schema

@tool("read_file", args_schema=ReadFileInput)
def read_file(file_path: str) -> str:
    """
    Read the contents of a file. Requires 'file_path'
    """
    with open(file_path, 'r') as file:
        return file.read()

@tool("write_file", args_schema=WriteFileInput)
def write_file(file_path: str, content: str) -> str:
    """
    Write content to a file. Requires 'file_path' and 'content'.
    """
    with open(file_path, 'w') as file:
        file.write(content)
    return f"Content successfully written to {file_path}"

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
    Returns a success message or a list of validation errors.
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
    Parses a PR diff string (git diff format) and breaks it down into reviewable chunks.
    Chunks are primarily by file, and then by individual hunk within each file.
    Each chunk is a dictionary containing 'file_path', 'chunk_type', 'content', and optional 'metadata'.
    This tool helps in reviewing large PRs by dividing them into smaller, manageable pieces of context.
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
            parsed = json.loads(stripped)
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
        return None

    diff_content = extract_diff_from_jsonish_input(diff_content)
    if not diff_content.strip():
        return [{
            "file_path": "N/A",
            "chunk_type": "empty_diff_input",
            "content": "The provided PR diff content was empty or whitespace only.",
            "metadata": None
        }]

    try:
        # PatchSet expects an iterable of lines. Using splitlines(True) preserves newlines for diff format.
        patch_set = PatchSet(diff_content.splitlines(keepends=True))
    except Exception as e:
        # This might happen with fundamentally malformed diffs not caught by PatchSet's internal checks
        return [{
            "file_path": "N/A",
            "chunk_type": "diff_parse_error",
            "content": f"Critical error while trying to parse the PR diff. Error: {str(e)}\n\nFirst 500 chars of diff (approx):\n{diff_content[:500]}",
            "metadata": None
        }]

    review_chunks: List[Dict[str, Any]] = []

    if not patch_set.modified_files and not patch_set.added_files and not patch_set.removed_files:
        # Check if PatchSet itself found no files, which might indicate a very minimal or non-standard diff
         return [{
            "file_path": "N/A",
            "chunk_type": "no_files_in_diff",
            "content": "The PR diff was parsed, but no modified, added, or removed files were identified by the parser.",
            "metadata": None
        }]


    for patched_file in patch_set: # Iterates through PatchedFile objects
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
            if old_mode: # For deleted files, old_mode is relevant
                file_level_info_parts.append(f"Old file mode was: {old_mode}")
        else: # Modified file
            file_level_info_parts.append(f"File modified: {file_path}")
            if old_mode is not None and new_mode is not None and old_mode != new_mode:
                file_level_info_parts.append(f"Mode changed from {old_mode} to {new_mode}.")
            # Handle cases where one mode might be present if diff is unusual, though less common for modified
            elif new_mode is not None and old_mode is None : # Should be rare for non-added files
                 file_level_info_parts.append(f"File mode is {new_mode} (old mode not specified in diff).")

        # This header provides overall context for changes within this file
        file_context_header = "\n".join(file_level_info_parts)

        if patched_file.is_binary_file:
            review_chunks.append({
                "file_path": file_path,
                "chunk_type": "binary_file_summary", # LLM can't review binary diff lines
                "content": file_context_header + "\nThis is a binary file. Review manually if necessary.",
                "metadata": {"is_binary": True}
            })
            continue # Skip hunk processing for binary files

        try:
            # Treat PatchedFile as directly iterable to get its Hunk objects and convert to a list.
            # This aligns with the observation that patched_file[0] works.
            hunks_from_file = list(patched_file)
        except TypeError: 
            # This fallback is in case a PatchedFile instance is somehow not iterable as expected.
            review_chunks.append({
                "file_path": file_path,
                "chunk_type": "hunk_iteration_error",
                "content": file_context_header + "\nError: Could not iterate over hunks for this file.",
                "metadata": None
            })
            continue # Skip to next patched_file
        
        if not hunks_from_file: # Check if the resulting list is empty
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
                f"Hunk {hunk_num + 1}/{len(hunks_from_file)} for file '{file_path}'.", # Use len(hunks_from_file)
                f"Changes affect lines from ~{hunk.source_start} (old file) and ~{hunk.target_start} (new file)."
            ]
            if hunk.section_header:
                hunk_details_parts.append(f"Context: {hunk.section_header.strip()}")
            
            hunk_context_summary = "\n".join(hunk_details_parts)
            
            chunk_content_for_llm = (
                f"{hunk_context_summary}\n"
                "Relevant diff section to review:\n"
                "```diff\n"
                f"{str(hunk).strip()}\n"
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
            "content": "The PR diff was processed, but no reviewable file changes or hunks were extracted. The diff might be malformed for detailed parsing or only contain metadata without actual code/text changes.",
            "metadata": None
        })
    
    return review_chunks

# Export the list of tools
tools = [read_file, write_file, shell_command, validate_yaml_with_schema, chunk_pr_diff]
