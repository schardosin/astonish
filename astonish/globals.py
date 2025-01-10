import configparser, os, appdirs
import logging
from logging.handlers import RotatingFileHandler

CONFIG_FILE = "config.ini"
LOG_FILE = "astonish.log"

config = configparser.ConfigParser()
config_dir = appdirs.user_config_dir("astonish")
os.makedirs(config_dir, exist_ok=True)
config_path = os.path.join(config_dir, CONFIG_FILE)
logger_path = os.path.join(config_dir, LOG_FILE)

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
