import os
import subprocess
import platform
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

    editor = get_default_editor()
    
    try:
        subprocess.run([editor, config_path], check=True)
        globals.logger.info(f"Successfully opened MCP config file for editing: {config_path}")
        return f"Successfully opened MCP config file for editing: {config_path}"
    except subprocess.CalledProcessError as e:
        globals.logger.error(f"Failed to open editor: {e}")
        return f"Error: Failed to open editor: {e}"
    except Exception as e:
        globals.logger.error(f"Unexpected error while editing MCP config: {e}")
        return f"Error: Unexpected error while editing MCP config: {e}"

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

def get_default_editor():
    """
    Get the default text editor based on the operating system.
    """
    system = platform.system()
    
    if system == "Windows":
        return "notepad.exe"
    elif system in ["Linux", "Darwin"]:  # Linux or macOS
        return os.environ.get("EDITOR", "vi")
    else:
        globals.logger.warning(f"Unsupported operating system: {system}. Defaulting to 'vi'.")
        return "vi"
