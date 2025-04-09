import pytest
from unittest.mock import patch, mock_open
import subprocess
from astonish.tools.internal_tools import read_file, write_file, shell_command

def test_read_file():
    mock_content = "This is a test file content."
    with patch("builtins.open", mock_open(read_data=mock_content)):
        result = read_file.invoke({"file_path": "test_file.txt"})
    assert result == mock_content

def test_write_file():
    mock_file = mock_open()
    with patch("builtins.open", mock_file):
        result = write_file.invoke({"file_path": "test_file.txt", "content": "Test content"})
    
    mock_file.assert_called_once_with("test_file.txt", "w")
    mock_file().write.assert_called_once_with("Test content")
    assert result == "Content successfully written to test_file.txt"

@patch("subprocess.run")
def test_shell_command(mock_subprocess_run):
    mock_subprocess_run.return_value = subprocess.CompletedProcess(
        args="echo 'Hello, World!'",
        returncode=0,
        stdout="Hello, World!\n",
        stderr=""
    )
    
    result = shell_command.invoke({"command": "echo 'Hello, World!'"})
    
    mock_subprocess_run.assert_called_once_with(
        "echo 'Hello, World!'",
        shell=True,
        capture_output=True,
        text=True
    )
    assert result == {"stdout": "Hello, World!\n", "stderr": ""}

@patch("subprocess.run")
def test_shell_command_with_error(mock_subprocess_run):
    mock_subprocess_run.return_value = subprocess.CompletedProcess(
        args="invalid_command",
        returncode=1,
        stdout="",
        stderr="Command not found: invalid_command\n"
    )
    
    result = shell_command.invoke({"command": "invalid_command"})
    
    mock_subprocess_run.assert_called_once_with(
        "invalid_command",
        shell=True,
        capture_output=True,
        text=True
    )
    assert result == {"stdout": "", "stderr": "Command not found: invalid_command\n"}

if __name__ == "__main__":
    pytest.main()
