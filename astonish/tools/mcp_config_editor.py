import os
from astonish import globals

def edit_mcp_config():
    """
    Open the MCP config file in the default text editor.
    If the file doesn't exist, create it with a default structure.
    """
    config_path = globals.mcp_config_path
    
    if not os.path.exists(config_path):
        globals.logger.info(f"MCP config file not found at {config_path}. Creating a new one.")
        create_default_config(config_path)

    return globals.open_editor(config_path)

def create_default_config(config_path):
    """
    Create a default MCP config file with an empty mcpServers object.
    """
    default_config = {
        "mcpServers": {}
    }
    
    try:
        os.makedirs(os.path.dirname(config_path), exist_ok=True)
        with open(config_path, 'w') as config_file:
            import json
            json.dump(default_config, config_file, indent=2)
        globals.logger.info(f"Created default MCP config file at {config_path}")
    except Exception as e:
        globals.logger.error(f"Failed to create default MCP config file: {e}")
        raise
