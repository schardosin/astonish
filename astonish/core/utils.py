import yaml
import os
import appdirs
import astonish.globals as globals
from importlib import resources
import re
import inquirer
from rich import print
from rich.console import Console
from rich.syntax import Syntax

console = Console()

def print_rich(content):
    """
    This function formats text with code blocks (like YAML, JSON, etc.) and preserves indentation for normal text.
    It prints the content with proper formatting, including syntax highlighting for code blocks.
    """
    # Initialize the console
    console = Console()

    # Regex to detect code blocks, e.g., ```yaml, ```json, etc.
    code_block_pattern = re.compile(r'```(\w+)?\n(.*?)```', re.DOTALL)

    # Initialize position to keep track of current line
    current_position = 0
    last_position = 0
    
    # Find all code blocks
    for match in code_block_pattern.finditer(content):
        # Print the regular text before this code block (preserving indentation)
        if match.start() > last_position:
            normal_text = content[last_position:match.start()]
            console.print(normal_text)
        
        # Get the content inside the code block and its language
        language = match.group(1) or 'text'  # Default to plain text if no language is specified
        code_content = match.group(2)
        
        # Create the Syntax object for syntax highlighting
        syntax = Syntax(code_content, language)
        console.print(syntax)
        
        # Update the last_position to after the code block
        last_position = match.end()

    # If there's remaining regular content after the last code block
    if last_position < len(content):
        normal_text = content[last_position:]
        console.print(normal_text)

def format_message(message):
    # Bold text between ** **
    return re.sub(r'\*\*(.*?)\*\*', r'[bold]\1[/bold]', message)

def print_ai(message):
    formatted = format_message(message)
    print_rich(f"[green]AI:[/green] {formatted}")

def print_user_prompt(message):
    print_rich(f"[yellow]{message}[/yellow]", end="")

def print_section(title):
    border = "=" * 40
    print_rich(f"[blue bold]{border}[/blue bold]")
    print_rich(f"[blue bold]{title.center(40)}[/blue bold]")
    print_rich(f"[blue bold]{border}[/blue bold]\n")

def print_output(output, color="cyan"):
    print_rich(f"[{color}]{output}[/{color}]")

def print_dict(dictionary, key_color="magenta", value_color="cyan"):
    for key, value in dictionary.items():
        print_rich(f"[{key_color}]{key}:[/{key_color}] [{value_color}]{value}[/{value_color}]")

def load_agents(agent_name):
    try:
        with resources.path('astonish.agents', f"{agent_name}.yaml") as agent_path:
            with open(agent_path, 'r') as file:
                return yaml.safe_load(file)
    except FileNotFoundError:
        config_dir = appdirs.user_config_dir("astonish")
        config_agents_path = os.path.join(config_dir, "agents", f"{agent_name}.yaml")
        if os.path.exists(config_agents_path):
            with open(config_agents_path, 'r') as file:
                return yaml.safe_load(file)
        else:
            raise FileNotFoundError(f"Agent {agent_name} not found in astonish.agents or {config_agents_path}")

def edit_agent(agent_name):
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
    import ast
    def _eval(node):
        if isinstance(node, ast.Name):
            return context[node.id]
        elif isinstance(node, ast.Subscript):
            target = _eval(node.value)
            key = _eval(node.slice)
            return target[key]
        elif isinstance(node, ast.Constant):
            return node.value
        elif isinstance(node, ast.Attribute):
            value = _eval(node.value)
            return getattr(value, node.attr)
        else:
            raise ValueError(f"Unsupported expression element: {ast.dump(node)}")

    tree = ast.parse(expr, mode='eval')
    return str(_eval(tree.body))

def format_prompt(prompt: str, state: dict, node_config: dict) -> str:
    format_context = {**state, **node_config}

    if not isinstance(prompt, str):
         globals.logger.warning(f"Prompt is not a string, returning as is. Type: {type(prompt)}")
         return prompt

    DUMMY_ESCAPE = "§§§"
    prompt = prompt.replace("{{", f"{DUMMY_ESCAPE}open_brace§")
    prompt = prompt.replace("}}", f"{DUMMY_ESCAPE}close_brace§")

    formatted_prompt = re.sub(
        r"(?<!\{)\{([^{}]+)\}(?!\})",
        lambda match: _evaluate_placeholder(match.group(1), format_context),
        prompt
    )

    formatted_prompt = formatted_prompt.replace(f"{DUMMY_ESCAPE}open_brace§", "{")
    formatted_prompt = formatted_prompt.replace(f"{DUMMY_ESCAPE}close_brace§", "}")

    return formatted_prompt

def request_tool_execution(tool):
    from rich.panel import Panel
    from rich.console import Group
    from rich.text import Text
    from rich.syntax import Syntax

    try:
        tool_name = tool['name']
        args = tool['args']
        auto_approve = tool.get('auto_approve', False)

        if auto_approve:
            console.print(f"[green]Auto-approving tool '{tool_name}' execution.[/green]")
            return True

        # Create a Group of formatted items
        body_lines = [
            Text(f"Tool: ", style="yellow") + Text(tool_name, style="bold"),
            Text("\n** Arguments **\n", style="yellow")
        ]

        for key, value in args.items():
            body_lines.append(Text(f"{key}:", style="yellow"))
            if isinstance(value, (dict, list)):
                value_str = yaml.dump(value, default_flow_style=False)
            else:
                value_str = str(value)

            syntax = Syntax(value_str, "yaml", indent_guides=True, theme="monokai", word_wrap=True)
            body_lines.append(syntax)

        panel = Panel(Group(*body_lines), title="Tool Execution", border_style="cyan", expand=False)
        console.print(panel)

        # Prompt user for confirmation
        questions = [
            inquirer.List(
                'approval',
                message="Do you approve this execution?",
                choices=['Yes', 'No'],
            )
        ]
        answers = inquirer.prompt(questions)

        if answers['approval'] == 'Yes':
            console.print("[green]Tool execution approved.[/green]")
            return True
        else:
            console.print("[red]Tool execution denied.[/red]")
            return False

    except KeyError as e:
        console.print(f"[red]Error: Missing required field in tool object: {e}[/red]")
    return False


async def list_agents():
    print_section("Available Agents")
    agents_found = False

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
                    print_rich(f"[red]Error loading agent {agent_name}: {e}[/red]")
    except Exception as e:
        print_rich(f"[yellow]Error accessing astonish.agents: {e}[/yellow]")

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
                    print_rich(f"[red]Error loading agent {agent_name}: {e}[/red]")

    if not agents_found:
        print_rich(f"[yellow]No agents found in astonish.agents or {config_agents_path}[/yellow]")
