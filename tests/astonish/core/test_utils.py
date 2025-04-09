import pytest
from unittest.mock import patch, mock_open, MagicMock
import os
from astonish.core.utils import (
    format_prompt, print_dict, load_agents,
    print_ai, print_user_prompt, print_section,
    setup_colorama, print_output, edit_agent,
    request_tool_execution, list_agents
)
from colorama import Fore, Style, init as colorama_init
import yaml
import astonish.globals as globals
from importlib import resources

def test_format_prompt():
    # Test case 1: Basic formatting
    prompt = "Hello, {name}!"
    state = {"name": "Alice"}
    node_config = {}
    result = format_prompt(prompt, state, node_config)
    assert result == "Hello, Alice!"

    # Test case 2: Using both state and node_config
    prompt = "The {color} {animal} jumped over the {obstacle}."
    state = {"color": "brown", "animal": "fox"}
    node_config = {"obstacle": "fence"}
    result = format_prompt(prompt, state, node_config)
    assert result == "The brown fox jumped over the fence."

    # Test case 3: Nested state access
    prompt = "The {state[color]} {animal} jumped over the {obstacle}."
    state = {"color": "red", "animal": "rabbit"}
    node_config = {"obstacle": "wall"}
    result = format_prompt(prompt, state, node_config)
    assert result == "The red rabbit jumped over the wall."

def test_format_prompt_missing_key():
    prompt = "Hello, {missing_key}!"
    state = {}
    node_config = {}
    with pytest.raises(KeyError):
        format_prompt(prompt, state, node_config)

def test_print_dict(capsys):
    test_dict = {"key1": "value1", "key2": "value2"}
    print_dict(test_dict)
    captured = capsys.readouterr()
    expected_output = f"{Fore.MAGENTA}key1: {Style.RESET_ALL}{Fore.CYAN}value1{Style.RESET_ALL}\n" \
                      f"{Fore.MAGENTA}key2: {Style.RESET_ALL}{Fore.CYAN}value2{Style.RESET_ALL}\n"
    assert captured.out == expected_output

@pytest.mark.asyncio
@patch('astonish.core.utils.resources.files')
@patch('astonish.core.utils.os.listdir')
@patch('astonish.core.utils.appdirs.user_config_dir')
@patch('astonish.core.utils.load_agents')
@patch('os.path.exists')
async def test_list_agents(mock_exists, mock_load_agents, mock_user_config_dir, mock_listdir, mock_resources_files, capsys):
    # Mock the package agents
    class MockFile:
        def __init__(self, name):
            self.name = name

    mock_package_agents = MagicMock()
    mock_package_agents.iterdir.return_value = [
        MockFile("agent1.yaml"),
        MockFile("agent2.yaml")
    ]
    mock_resources_files.return_value = mock_package_agents

    # Mock the config agents
    mock_user_config_dir.return_value = '/mock/config/dir'
    mock_exists.return_value = True  # Simulate that the config agents directory exists
    mock_listdir.return_value = ['agent3.yaml', 'agent4.yaml']

    # Mock load_agents function
    mock_load_agents.side_effect = [
        {'description': 'Description for agent1'},
        {'description': 'Description for agent2'},
        {'description': 'Description for agent3'},
        {'description': 'Description for agent4'}
    ]

    # Call the function
    await list_agents()

    # Check the output
    captured = capsys.readouterr()
    print("Actual output:")
    print(captured.out)
    
    assert "Available Agents" in captured.out
    assert "agent1: Description for agent1" in captured.out
    assert "agent2: Description for agent2" in captured.out
    assert "agent3: Description for agent3" in captured.out
    assert "agent4: Description for agent4" in captured.out

    # Verify that the correct paths were checked
    mock_exists.assert_any_call('/mock/config/dir/agents')
    mock_listdir.assert_any_call('/mock/config/dir/agents')

@pytest.mark.asyncio
@patch('astonish.core.utils.resources.files')
@patch('astonish.core.utils.os.listdir')
@patch('astonish.core.utils.appdirs.user_config_dir')
async def test_list_agents_no_agents(mock_user_config_dir, mock_listdir, mock_resources_files, capsys):
    # Mock empty package and config directories
    mock_package_agents = MagicMock()
    mock_package_agents.iterdir.return_value = []
    mock_resources_files.return_value = mock_package_agents
    mock_listdir.return_value = []

    # Mock user config directory
    mock_user_config_dir.return_value = '/mock/config/dir'

    # Call the function
    await list_agents()

    # Check the output
    captured = capsys.readouterr()
    assert "No agents found" in captured.out

@patch('builtins.input')
def test_request_tool_execution(mock_input):
    tool = {
        'name': 'test_tool',
        'args': {'arg1': 'value1', 'arg2': 'value2'}
    }

    # Test approval
    mock_input.return_value = 'yes'
    assert request_tool_execution(tool) == True

    # Test rejection
    mock_input.return_value = 'no'
    assert request_tool_execution(tool) == False

    # Test invalid input then approval
    mock_input.side_effect = ['invalid', 'y']
    assert request_tool_execution(tool) == True

    # Test invalid input then rejection
    mock_input.side_effect = ['invalid', 'n']
    assert request_tool_execution(tool) == False

@patch('builtins.input')
def test_request_tool_execution_error(mock_input):
    # Test with missing 'name' key
    tool = {'args': {'arg1': 'value1'}}
    assert request_tool_execution(tool) == False

    # Test with missing 'args' key
    tool = {'name': 'test_tool'}
    assert request_tool_execution(tool) == False

@patch('astonish.core.utils.appdirs.user_config_dir')
@patch('os.path.exists')
@patch('astonish.globals.open_editor')
def test_edit_agent(mock_open_editor, mock_exists, mock_user_config_dir, tmp_path):
    mock_user_config_dir.return_value = str(tmp_path)
    mock_exists.return_value = True
    mock_open_editor.return_value = "Editor opened successfully"

    result = edit_agent("test_agent")
    assert result == "Editor opened successfully"

    mock_exists.return_value = False
    result = edit_agent("nonexistent_agent")
    assert "doesn't exist or is not editable" in result

@patch('astonish.core.utils.appdirs.user_config_dir')
@patch('os.path.exists')
@patch('astonish.globals.open_editor')
def test_edit_agent_error(mock_open_editor, mock_exists, mock_user_config_dir, tmp_path):
    mock_user_config_dir.return_value = str(tmp_path)
    mock_exists.return_value = True
    mock_open_editor.side_effect = Exception("Editor error")

    with patch('astonish.globals.logger.error') as mock_logger_error:
        result = edit_agent("test_agent")
        assert "Error opening agent file" in result
        mock_logger_error.assert_called_once()

def test_setup_colorama():
    with patch('astonish.core.utils.colorama_init') as mock_init:
        setup_colorama()
        mock_init.assert_called_once_with(autoreset=True)

def test_print_output(capsys):
    output = "Test output"
    print_output(output)
    captured = capsys.readouterr()
    expected_output = f"{Fore.CYAN}{output}{Style.RESET_ALL}\n"
    assert captured.out == expected_output

def test_print_ai(capsys):
    message = "Hello, I'm an AI!"
    print_ai(message)
    captured = capsys.readouterr()
    expected_output = f"{Fore.GREEN}AI: {Style.RESET_ALL}{message}\n"
    assert captured.out == expected_output

def test_print_user_prompt(capsys):
    message = "Enter your name: "
    print_user_prompt(message)
    captured = capsys.readouterr()
    expected_output = f"{Fore.YELLOW}{message}{Style.RESET_ALL}"
    assert captured.out == expected_output

def test_print_section(capsys):
    title = "Test Section"
    print_section(title)
    captured = capsys.readouterr()
    expected_output = (
        f"\n{Fore.BLUE}{Style.BRIGHT}{'=' * 40}\n"
        f"{title.center(40)}\n"
        f"{'=' * 40}{Style.RESET_ALL}\n\n"
    )
    assert captured.out == expected_output

@pytest.fixture
def mock_yaml_content():
    return """
    name: test_agent
    description: This is a test agent
    """

@patch('astonish.core.utils.resources.path')
@patch('astonish.core.utils.appdirs.user_config_dir')
def test_load_agents_from_package(mock_user_config_dir, mock_resources_path, mock_yaml_content, tmp_path):
    # Mock the package path
    mock_package_path = tmp_path / "package_agents"
    mock_package_path.mkdir()
    mock_agent_file = mock_package_path / "test_agent.yaml"
    mock_agent_file.write_text(mock_yaml_content)
    mock_resources_path.return_value.__enter__.return_value = mock_agent_file

    # Test loading from package
    result = load_agents("test_agent")
    assert result == yaml.safe_load(mock_yaml_content)

@patch('astonish.core.utils.resources.path')
@patch('astonish.core.utils.appdirs.user_config_dir')
@patch('os.path.exists')
def test_load_agents_from_config(mock_exists, mock_user_config_dir, mock_resources_path, mock_yaml_content, tmp_path):
    # Mock the config path
    mock_config_dir = tmp_path
    mock_agents_dir = mock_config_dir / "agents"
    mock_agents_dir.mkdir(parents=True, exist_ok=True)
    mock_agent_file = mock_agents_dir / "test_agent.yaml"
    mock_agent_file.write_text(mock_yaml_content)
    mock_user_config_dir.return_value = str(mock_config_dir)
    
    # Mock package path to raise FileNotFoundError
    mock_resources_path.side_effect = FileNotFoundError

    # Mock os.path.exists to return True for the config file
    mock_exists.return_value = True

    # Test loading from config
    result = load_agents("test_agent")
    assert result == yaml.safe_load(mock_yaml_content)

    # Verify that the correct path was checked
    expected_path = os.path.join(str(mock_config_dir), "agents", "test_agent.yaml")
    mock_exists.assert_called_once_with(expected_path)

@patch('astonish.core.utils.resources.path')
@patch('astonish.core.utils.appdirs.user_config_dir')
def test_load_agents_not_found(mock_user_config_dir, mock_resources_path, tmp_path):
    # Mock both package and config paths to not find the file
    mock_resources_path.side_effect = FileNotFoundError
    mock_user_config_dir.return_value = str(tmp_path)

    # Test file not found
    with pytest.raises(FileNotFoundError):
        load_agents("nonexistent_agent")

def test_print_dict_custom_colors(capsys):
    test_dict = {"key1": "value1", "key2": "value2"}
    print_dict(test_dict, key_color=Fore.RED, value_color=Fore.GREEN)
    captured = capsys.readouterr()
    expected_output = f"{Fore.RED}key1: {Style.RESET_ALL}{Fore.GREEN}value1{Style.RESET_ALL}\n" \
                      f"{Fore.RED}key2: {Style.RESET_ALL}{Fore.GREEN}value2{Style.RESET_ALL}\n"
    assert captured.out == expected_output
