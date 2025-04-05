import subprocess
from typing import Dict
from langchain_core.tools import tool

@tool("read_file")
def read_file(file_path: str) -> str:
    """
    Read the contents of a file.

    Args:
        file_path (str): The path to the file to be read.

    Returns:
        str: The contents of the file.

    This function reads the entire contents of a file specified by file_path and returns it as a string.
    Use this when you need to access the contents of a file in the current working directory or its subdirectories.
    """
    with open(file_path, 'r') as file:
        return file.read()

@tool("write_file")
def write_file(file_path: str, content: str) -> str:
    """
    Write content to a file.

    Args:
        file_path (str): The path to the file where content will be written.
        content (str): The content to write to the file.

    Returns:
        str: A confirmation message.

    This function writes the provided content to a file specified by file_path.
    If the file doesn't exist, it will be created. If it does exist, its contents will be overwritten.
    Use this when you need to save data to a file in the current working directory or its subdirectories.
    """
    with open(file_path, 'w') as file:
        file.write(content)
    return f"Content successfully written to {file_path}"

@tool("execute_command")
def execute_command(command: str) -> Dict[str, str]:
    """
    Execute a shell command and return its output.

    Args:
        command (str): The shell command to execute.

    Returns:
        Dict[str, str]: A dictionary containing 'stdout' and 'stderr' keys with their respective outputs.

    This function executes the given shell command in the current working directory and captures both
    the standard output and standard error. Use this for running system commands, especially Git operations.
    Be cautious with the commands you run, as they execute in the context of the application's environment.
    """
    result = subprocess.run(command, shell=True, capture_output=True, text=True)
    return {"stdout": result.stdout, "stderr": result.stderr}

# Export the list of tools
tools = [read_file, write_file, execute_command]