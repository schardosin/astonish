import yaml
import os
import appdirs
import astonish.globals as globals
from importlib import resources
from colorama import Fore, Style, init as colorama_init

def setup_colorama():
    colorama_init(autoreset=True)

def print_ai(message):
    print(f"{Fore.GREEN}AI: {Style.RESET_ALL}{message}")

def print_user_prompt(message):
    print(f"{Fore.YELLOW}{message}{Style.RESET_ALL}", end="")

def print_section(title):
    print(f"\n{Fore.BLUE}{Style.BRIGHT}{'=' * 40}")
    print(f"{title.center(40)}")
    print(f"{'=' * 40}{Style.RESET_ALL}\n")

def print_output(output):
    print(f"{Fore.CYAN}{output}{Style.RESET_ALL}")

def print_dict(dictionary, key_color=Fore.MAGENTA, value_color=Fore.CYAN):
    for key, value in dictionary.items():
        print(f"{key_color}{key}: {Style.RESET_ALL}{value_color}{value}{Style.RESET_ALL}")

def load_agents(agent_name):
    # Try to load from astonish.agents first
    try:
        with resources.path('astonish.agents', f"{agent_name}.yaml") as agent_path:
            with open(agent_path, 'r') as file:
                return yaml.safe_load(file)
    except FileNotFoundError:
        # If not found, try to load from config_path/agents
        config_dir = appdirs.user_config_dir("astonish")
        config_agents_path = os.path.join(config_dir, "agents", f"{agent_name}.yaml")
        if os.path.exists(config_agents_path):
            with open(config_agents_path, 'r') as file:
                return yaml.safe_load(file)
        else:
            raise FileNotFoundError(f"Agent {agent_name} not found in astonish.agents or {config_agents_path}")

def edit_agent(agent_name):
    """
    Edit an agent configuration file in the user's config directory.
    If the file doesn't exist, return an error message.
    """
    config_dir = appdirs.user_config_dir("astonish")
    config_agents_path = os.path.join(config_dir, "agents", f"{agent_name}.yaml")

    if os.path.exists(config_agents_path):
        try:
            return globals.open_editor(config_agents_path)
        except Exception as e:
            error_message = f"Error opening agent file: {str(e)}"
            globals.logger.error(error_message)
            return error_message
    else:
        error_message = f"Agent '{agent_name}' doesn't exist or is not editable."
        globals.logger.warning(error_message)
        return error_message

def format_prompt(prompt: str, state: dict, node_config: dict):
    state_dict = dict(state)
    state_dict['state'] = state
    format_dict = {**state_dict, **node_config}
    return prompt.format(**format_dict)

def request_tool_execution(tool):
    """
    Prompt the user for approval before executing a tool command.
    Accepts only 'yes', 'no', 'y', or 'n' as valid inputs (case-insensitive).
    Keeps prompting until a valid response is received.

    Parameters:
    - tool (dict): Dictionary containing tool execution details.

    Returns:
    - bool: True if the user approves, False otherwise.
    """
    try:
        tool_name = tool['name']
        args = tool['args']

        prompt_message = f"\nTool Execution Request:\n"
        prompt_message += f"Tool Name: {tool_name}\n"
        prompt_message += "Arguments:\n"
        
        for key, value in args.items():
            prompt_message += f"  {key}: {value}\n"
        
        prompt_message += "Do you approve this execution? (yes/no): "

        while True:
            user_input = input(f"{Fore.YELLOW}{prompt_message}{Style.RESET_ALL}").strip().lower()
            if user_input in ['yes', 'y']:
                return True
            elif user_input in ['no', 'n']:
                return False
            else:
                print(f"{Fore.RED}Invalid input. Please enter 'yes' or 'no'.{Style.RESET_ALL}")

    except KeyError as e:
        print(f"{Fore.RED}Error: Missing required field in tool object: {e}{Style.RESET_ALL}")

    return False

async def list_agents():
    """
    List all available agents, including their names and descriptions.
    """
    setup_colorama()
    print_section("Available Agents")

    agents_found = False

    # List agents from astonish.agents
    try:
        agents_dir = resources.files('astonish.agents')
        for agent_file in agents_dir.iterdir():
            if agent_file.name.endswith('.yaml'):
                agent_name = os.path.splitext(agent_file.name)[0]
                try:
                    agent_data = load_agents(agent_name)
                    description = agent_data.get('description', 'No description available')
                    print_dict({agent_name: description})
                    agents_found = True
                except Exception as e:
                    print(f"{Fore.RED}Error loading agent {agent_name}: {e}{Style.RESET_ALL}")
    except Exception as e:
        print(f"{Fore.YELLOW}Error accessing astonish.agents: {e}{Style.RESET_ALL}")

    # List agents from config_path/agents
    config_dir = appdirs.user_config_dir("astonish")
    config_agents_path = os.path.join(config_dir, "agents")
    if os.path.exists(config_agents_path):
        for agent_file in os.listdir(config_agents_path):
            if agent_file.endswith('.yaml'):
                agent_name = os.path.splitext(agent_file)[0]
                try:
                    agent_data = load_agents(agent_name)
                    description = agent_data.get('description', 'No description available')
                    print_dict({agent_name: description})
                    agents_found = True
                except Exception as e:
                    print(f"{Fore.RED}Error loading agent {agent_name}: {e}{Style.RESET_ALL}")

    if not agents_found:
        print(f"{Fore.YELLOW}No agents found in astonish.agents or {config_agents_path}{Style.RESET_ALL}")
