import subprocess
from typing import Dict, Type
from langchain_core.tools import tool
from pydantic import BaseModel, Field

# Define input schemas
class ReadFileInput(BaseModel):
    file_path: str = Field(..., description="The path to the file to be read.")

class WriteFileInput(BaseModel):
    file_path: str = Field(..., description="The path to the file where content will be written.")
    content: str = Field(..., description="The content to write to the file.")

class ExecuteCommandInput(BaseModel):
    command: str = Field(..., description="The shell command to execute.")

# Define tools using args_schema

@tool("read_file", args_schema=ReadFileInput)
def read_file(file_path: str) -> str:
    """
    Read the contents of a file.
    """
    with open(file_path, 'r') as file:
        return file.read()

@tool("write_file", args_schema=WriteFileInput)
def write_file(file_path: str, content: str) -> str:
    """
    Write content to a file.
    """
    with open(file_path, 'w') as file:
        file.write(content)
    return f"Content successfully written to {file_path}"

@tool("shell_command", args_schema=ExecuteCommandInput)
def shell_command(command: str) -> Dict[str, str]:
    """
    Execute a shell command and return its output.
    """
    result = subprocess.run(command, shell=True, capture_output=True, text=True)
    return {"stdout": result.stdout, "stderr": result.stderr}

# Export the list of tools
tools = [read_file, write_file, shell_command]
