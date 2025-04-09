import pytest
import os
import json
from unittest.mock import patch, mock_open
from astonish.tools import mcp_config_editor

@pytest.fixture
def mock_globals():
    with patch('astonish.tools.mcp_config_editor.globals') as mock_globals:
        mock_globals.mcp_config_path = '/mock/path/to/mcp_config.json'
        mock_globals.open_editor.return_value = "Editor opened successfully"
        yield mock_globals

def test_edit_mcp_config_existing_file(mock_globals):
    with patch('os.path.exists', return_value=True):
        result = mcp_config_editor.edit_mcp_config()
    
    assert result == "Editor opened successfully"
    mock_globals.open_editor.assert_called_once_with('/mock/path/to/mcp_config.json')

def test_edit_mcp_config_new_file(mock_globals):
    with patch('os.path.exists', return_value=False), \
         patch('astonish.tools.mcp_config_editor.create_default_config') as mock_create:
        result = mcp_config_editor.edit_mcp_config()
    
    mock_create.assert_called_once_with('/mock/path/to/mcp_config.json')
    assert result == "Editor opened successfully"
    mock_globals.open_editor.assert_called_once_with('/mock/path/to/mcp_config.json')

def test_create_default_config(mock_globals):
    mock_file = mock_open()
    with patch('os.makedirs') as mock_makedirs, \
         patch('builtins.open', mock_file):
        mcp_config_editor.create_default_config('/mock/path/to/mcp_config.json')
    
    mock_makedirs.assert_called_once_with('/mock/path/to', exist_ok=True)
    mock_file.assert_called_once_with('/mock/path/to/mcp_config.json', 'w')
    handle = mock_file()
    
    # Check that write was called at least once
    assert handle.write.call_count > 0
    
    # Combine all write calls to get the full content
    written_content = ''.join(call.args[0] for call in handle.write.call_args_list)
    assert json.loads(written_content) == {"mcpServers": {}}

def test_create_default_config_error(mock_globals):
    with patch('os.makedirs', side_effect=Exception("Mock error")), \
         pytest.raises(Exception, match="Mock error"):
        mcp_config_editor.create_default_config('/mock/path/to/mcp_config.json')
    
    mock_globals.logger.error.assert_called_once_with(
        "Failed to create default MCP config file: Mock error"
    )

if __name__ == '__main__':
    pytest.main()
