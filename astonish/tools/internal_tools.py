import subprocess
from typing import Dict, Type, Union, List, Any
from langchain_core.tools import tool
from pydantic import BaseModel, Field
from pykwalify.core import Core
import tempfile
from unidiff import PatchSet

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
        if patched_file.is_added_file:
            file_level_info_parts.append(f"File added: {file_path}")
        elif patched_file.is_removed_file:
            file_level_info_parts.append(f"File removed: {file_path}")
        else: # Modified file
            file_level_info_parts.append(f"File modified: {file_path}")

        if patched_file.old_mode != patched_file.new_mode and patched_file.new_mode is not None:
            file_level_info_parts.append(f"Mode changed from {patched_file.old_mode} to {patched_file.new_mode}.")
        
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

        if not patched_file.hunks:
            # Handles cases like: new empty file, file with only mode change, or empty diff for the file
            review_chunks.append({
                "file_path": file_path,
                "chunk_type": "file_info_no_hunks", # LLM can acknowledge this
                "content": file_context_header + "\nNo content hunks to review for this file.",
                "metadata": None
            })
            continue # No hunks to process

        # Process each hunk as a separate chunk
        for hunk_num, hunk in enumerate(patched_file.hunks):
            hunk_details_parts = [
                f"Hunk {hunk_num + 1}/{len(patched_file.hunks)} for file '{file_path}'."
                # Information about line numbers in source/target
                f"Changes affect lines from ~{hunk.source_start} (old file) and ~{hunk.target_start} (new file)."
            ]
            if hunk.section_header: # Often contains function/class name context
                hunk_details_parts.append(f"Context: {hunk.section_header.strip()}")
            
            hunk_context_summary = "\n".join(hunk_details_parts)
            
            # The content to be reviewed by the LLM
            chunk_content_for_llm = (
                f"{hunk_context_summary}\n"
                "Relevant diff section to review:\n"
                "```diff\n"
                f"{str(hunk).strip()}\n" # str(hunk) gives the formatted diff hunk
                "```"
            )
            
            review_chunks.append({
                "file_path": file_path,
                "chunk_type": "hunk",
                "content": chunk_content_for_llm,
                "metadata": { # Useful for downstream processing or structured feedback
                    "source_start_line": hunk.source_start,
                    "source_length": hunk.source_length,
                    "target_start_line": hunk.target_start,
                    "target_length": hunk.target_length,
                    "section_header": hunk.section_header.strip() if hunk.section_header else None,
                    "hunk_index_in_file": hunk_num + 1,
                    "total_hunks_in_file": len(patched_file.hunks)
                }
            })
            
    if not review_chunks and diff_content.strip():
        review_chunks.append({
            "file_path": "N/A",
            "chunk_type": "no_reviewable_content_extracted",
            "content": "The PR diff was processed, but no reviewable file changes or hunks were extracted. The diff might be malformed for detailed parsing or only contain metadata without actual code/text changes.",
            "metadata": None
        })
    
    return review_chunks

# Export the list of tools
tools = [read_file, write_file, shell_command, validate_yaml_with_schema, chunk_pr_diff]
