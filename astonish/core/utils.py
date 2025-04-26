import yaml
import os
import appdirs
import astonish.globals as globals
from importlib import resources
from colorama import Fore, Style, init as colorama_init
import re
import inquirer

def setup_colorama():
    colorama_init(autoreset=True)

def format_message(message):
    # Apply bold (via Style.BRIGHT)
    message = re.sub(r'\*\*(.*?)\*\*', Style.BRIGHT + r'\1' + Style.RESET_ALL, message)

    return message

def print_ai(message):
    formatted = format_message(message)
    print(f"{Fore.GREEN}AI: {Style.RESET_ALL}{formatted}")

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

def _evaluate_placeholder(expr: str, context: dict) -> str:
    """
    Safely evaluates simple expressions found within {}.
    Supports:
        - Simple variable names: {variable}
        - Dictionary key access (string keys): {variable['key']}
        - Basic attribute access: {variable.attribute}
    
    Args:
        expr: The string expression inside the braces (e.g., "variable", "variable['key']").
        context: The dictionary ({**state, **node_config}) to look up variables in.

    Returns:
        The resolved string value, or the original placeholder string if resolution fails.
    """
    expr = expr.strip() # Remove leading/trailing whitespace

    try:
        if '.' not in expr and '[' not in expr:
            if expr in context:
                return str(context[expr])
            else:
                raise KeyError(f"Variable '{expr}' not found")

        dict_match = re.fullmatch(r"(\w+)\[(['\"])(.*?)\2\]", expr)
        if dict_match:
            var_name, _, key = dict_match.groups()
            if var_name in context:
                target_dict = context[var_name]
                if isinstance(target_dict, dict):
                    if key in target_dict:
                        return str(target_dict[key])
                    else:
                        raise KeyError(f"Key '{key}' not found in dict '{var_name}'")
                else:
                    raise TypeError(f"Variable '{var_name}' is not a dictionary")
            else:
                raise KeyError(f"Base variable '{var_name}' not found")

        attr_match = re.fullmatch(r"(\w+)\.(\w+)", expr)
        if attr_match:
            var_name, attr_name = attr_match.groups()
            if var_name in context:
                target_obj = context[var_name]
                if hasattr(target_obj, attr_name):
                    return str(getattr(target_obj, attr_name))
                else:
                    raise AttributeError(f"Object '{var_name}' has no attribute '{attr_name}'")
            else:
                raise KeyError(f"Base variable '{var_name}' not found")

        # If none of the patterns match, it's an unsupported expression
        raise ValueError(f"Unsupported expression format: {expr}")

    except (KeyError, AttributeError, IndexError, TypeError, ValueError) as e:
        globals.logger.warning(f"Could not resolve placeholder '{{{expr}}}': {type(e).__name__}: {e}. Leaving placeholder unchanged.")
        # Return the original placeholder string on any error
        return f"{{{expr}}}"

def format_prompt(prompt: str, state: dict, node_config: dict) -> str:
    """
    Formats the prompt string using custom logic to handle {var} and {var['key']} syntax.
    It does NOT use str.format() directly to allow for dictionary access within braces.
    
    Args:
        prompt: The template string (using {} delimiters).
        state: The state dictionary.
        node_config: The node configuration dictionary.

    Returns:
        The formatted string, with unresolved placeholders left intact.
    """
    # Combine state and node_config. node_config overwrites state on conflicts.
    format_context = {**state, **node_config}

    if not isinstance(prompt, str):
         logger.warning(f"Prompt is not a string, returning as is. Type: {type(prompt)}")
         return prompt

    formatted_prompt = re.sub(
        r"\{([^}]+)\}", 
        lambda match: _evaluate_placeholder(match.group(1), format_context), 
        prompt
    )
    
    return formatted_prompt

def request_tool_execution(tool):
    """
    Prompt the user for approval before executing a tool command.
    Parameters:
    - tool (dict): Dictionary containing tool execution details.
    Returns:
    - bool: True if the user approves, False otherwise.
    """
    try:
        tool_name = tool['name']
        args = tool['args']
        auto_approve = tool['auto_approve']

        if auto_approve:
            print(f"{Fore.GREEN}Auto-approving tool '{tool_name}' execution.{Style.RESET_ALL}")
            return True
        
        print(f"\n{Fore.YELLOW}Tool Call Request:{Style.RESET_ALL}")
        print(f"{Fore.YELLOW}Name:{Style.RESET_ALL} {tool_name}")
        print(f"{Fore.YELLOW}Args:{Style.RESET_ALL}")
        
        for key, value in args.items():
            print(f"  {Fore.YELLOW}{key}:{Style.RESET_ALL}")
            value_lines = str(value).split('\n')
            for line in value_lines:
                print(f"{Fore.YELLOW}    {line}{Style.RESET_ALL}")
                
        print()
        questions = [
            inquirer.List('approval',
                          message=f"{Fore.YELLOW}Do you approve this execution?{Style.RESET_ALL}",
                          choices=['Yes', 'No'],
                          ),
        ]
        
        answers = inquirer.prompt(questions)
        if answers['approval'] == 'Yes':
            print(f"{Fore.GREEN}Tool execution approved.{Style.RESET_ALL}")
            return True
        else:
            print(f"{Fore.RED}Tool execution denied.{Style.RESET_ALL}")
            return False
        
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
