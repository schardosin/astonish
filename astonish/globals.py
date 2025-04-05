import configparser, os, appdirs
import logging
from logging.handlers import RotatingFileHandler

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
    # Load existing MCP configuration if it exists
    if os.path.exists(mcp_config_path):
        with open(mcp_config_path, 'r') as mcp_config_file:
            mcp_config = json.load(mcp_config_file)
    else:
        logger.warning(f"MCP config file not found at {mcp_config_path}")
        mcp_config = None

async def initialize_mcp_tools():
    from langchain_mcp_adapters.client import MultiServerMCPClient

    global mcp_config
    if mcp_config is None:
        logger.error("MCP config is not loaded. Cannot initialize MCP tools.")
        return None
    
    logger.info("Initializing MCP client...")
    mcp_client = MultiServerMCPClient(mcp_config['mcpServers'])
    
    logger.info("Connecting to MCP servers...")
    for server_name, server_config in mcp_config['mcpServers'].items():
        try:
            logger.info(f"Connecting to server: {server_name}")
            await mcp_client.connect_to_server(server_name, server_config)
            logger.info(f"Successfully connected to server: {server_name}")
        except Exception as e:
            logger.error(f"Failed to connect to server {server_name}: {str(e)}")
    
    logger.info("MCP client initialization complete")
    return mcp_client
