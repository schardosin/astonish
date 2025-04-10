import configparser, os, appdirs
import logging
from logging.handlers import RotatingFileHandler
import subprocess
import platform


CONFIG_FILE = "config.ini"
LOG_FILE = "astonish.log"
MCP_CONFIG_FILE = "mcp_config.json"

config = configparser.ConfigParser()
config_dir = appdirs.user_config_dir("astonish")
os.makedirs(config_dir, exist_ok=True)
config_path = os.path.join(config_dir, CONFIG_FILE)
logger_path = os.path.join(config_dir, LOG_FILE)
mcp_config_path = os.path.join(config_dir, MCP_CONFIG_FILE)
mcp_config = None

# Configure the logger
logging.basicConfig(
    level=logging.INFO,  # Default log level
    format="%(asctime)s - %(levelname)s - %(message)s",
    handlers=[
        RotatingFileHandler(logger_path, maxBytes=5 * 1024 * 1024, backupCount=3),  # 5 MB files
    ],
)

logger = logging.getLogger("astonish")

def setup_logger(verbose=False):
    logger.setLevel(logging.DEBUG if verbose else logging.INFO)

    # File handler
    file_handler = RotatingFileHandler(logger_path, maxBytes=5 * 1024 * 1024, backupCount=3)
    file_handler.setFormatter(logging.Formatter("%(asctime)s - %(levelname)s - %(message)s"))
    logger.addHandler(file_handler)

    # Optional terminal handler for verbose mode
    if verbose:
        console_handler = logging.StreamHandler()
        console_handler.setFormatter(logging.Formatter("%(levelname)s: %(message)s"))
        logger.addHandler(console_handler)

    return logger

def load_config():
    # Load existing configuration if it exists
    if os.path.exists(config_path):
        config.read(config_path)

def load_mcp_config():
    import json

    global mcp_config
    default_config = {
        "mcpServers": {}
    }

    # Load existing MCP configuration if it exists
    if os.path.exists(mcp_config_path):
        try:
            with open(mcp_config_path, 'r') as mcp_config_file:
                loaded_config = json.load(mcp_config_file)
            
            # Check if 'mcpServers' key exists in the loaded config
            if "mcpServers" not in loaded_config:
                logger.warning(f"'mcpServers' key not found in {mcp_config_path}. Using default configuration.")
                mcp_config = default_config
            else:
                mcp_config = loaded_config
        except json.JSONDecodeError:
            logger.warning(f"Error decoding JSON from {mcp_config_path}. Using default configuration.")
            mcp_config = default_config
        except Exception as e:
            logger.warning(f"Error reading {mcp_config_path}: {str(e)}. Using default configuration.")
            mcp_config = default_config
    else:
        logger.warning(f"MCP config file not found at {mcp_config_path}. Using default configuration.")
        mcp_config = default_config

    return mcp_config

async def initialize_mcp_tools():
    from langchain_mcp_adapters.client import MultiServerMCPClient

    global mcp_config
    if mcp_config is None:
        logger.error("MCP config is not loaded. Cannot initialize MCP tools.")
        return None
    
    logger.info("Initializing MCP client...")
    try:
        mcp_client = MultiServerMCPClient(mcp_config['mcpServers'])
        logger.info("Successfully initialized MCP client")
        
        return mcp_client
    except Exception as e:
        logger.error(f"Failed to initialize MCP client: {str(e)}")
        return None

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
        logger.warning(f"Unsupported operating system: {system}. Defaulting to 'vi'.")
        return "vi"

def open_editor(file_path):
    """
    Open a file in the default text editor.
    """
    editor = get_default_editor()
    try:
        subprocess.run([editor, file_path], check=True)
        logger.info(f"Successfully opened file for editing: {file_path}")
        return f"Successfully opened file for editing: {file_path}"
    except subprocess.CalledProcessError as e:
        logger.error(f"Failed to open editor: {e}")
        return f"Error: Failed to open editor: {e}"
    except Exception as e:
        logger.error(f"Unexpected error while opening file: {e}")
        return f"Error: Unexpected error while opening file: {e}"
