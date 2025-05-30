import yaml
import os
import appdirs
import json
import ast
import astonish.globals as globals
from importlib import resources
from typing import Union, List
import re
import inquirer
from rich.console import Console
from rich.syntax import Syntax
from rich.markdown import Markdown

console = Console()

def print_rich(content: str):
    """
    Processes text based on the original structure, with a modification:
    - Text outside any ``` blocks is printed directly (handles Rich tags like [green]).
    - ```language ... ``` blocks are syntax highlighted (e.g., python, yaml).
    - ```markdown ... ``` blocks are specifically rendered as Markdown.
    - ``` blocks with no language are treated as plain text.

    NOTE: Markdown syntax outside ```markdown blocks will NOT be rendered.
    """
    # Regex to detect code blocks, captures optional language and content
    code_block_pattern = re.compile(r'```(\w+)?\n(.*?)```', re.DOTALL)

    last_end = 0 # Use last_end to track position

    for match in code_block_pattern.finditer(content):
        start, end = match.span()

        # --- Part 1: Print text BEFORE the current block ---
        normal_text = content[last_end:start]
        if normal_text:
            # Print directly: Processes Rich tags, ignores Markdown syntax
            # Use end="" to avoid adding extra newlines between segments
            console.print(normal_text, end="")

        # --- Part 2: Process the detected block based on language ---
        language = match.group(1) # This is the captured language (or None)
        code_content = match.group(2).strip() # Get content and remove leading/trailing whitespace

        if not code_content: # Skip empty blocks
             last_end = end
             continue

        if language == 'markdown':
            # If language is 'markdown', render content using Markdown renderer
            md = Markdown(code_content)
            console.print(md) # Markdown renderer usually handles its own spacing
        elif language:
            # If language is specified (and not 'markdown'), use Syntax highlighting
            syntax = Syntax(code_content, language, line_numbers=False, word_wrap=True)
            console.print(syntax)
        else:
            # If no language is specified (just ```), treat as plain text syntax
            syntax = Syntax(code_content, "text", line_numbers=False, word_wrap=True)
            console.print(syntax)

        # Update position to the end of the current block
        last_end = end

    # --- Part 3: Print any remaining text AFTER the last block ---
    remaining_text = content[last_end:]
    if remaining_text:
        # Print directly: Processes Rich tags, ignores Markdown syntax
        console.print(remaining_text, end="")

    # Add a final newline if content wasn't empty, ensuring the prompt appears below
    if content and not content.endswith('\n'):
         console.print()

def format_message(message):
    # Bold text between ** **
    return re.sub(r'\*\*(.*?)\*\*', r'[bold]\1[/bold]', message)

def print_ai(message):
    formatted = format_message(message).strip('\n')
    if '\n' in formatted:
        formatted = '\n' + formatted
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
    def _eval_ast_node(node, current_expr_str):
        if isinstance(node, ast.Name):
            if node.id in context:
                return context[node.id]
            else:
                raise NameError(f"Name '{node.id}' not found in context for expression '{{{current_expr_str}}}'")
        elif isinstance(node, ast.Subscript):
            target = _eval_ast_node(node.value, current_expr_str)
            
            if isinstance(node.slice, ast.Index):
                key_node = node.slice.value
            else:
                key_node = node.slice
            
            key = _eval_ast_node(key_node, current_expr_str)
            return target[key]
        elif isinstance(node, ast.Constant): # Handles str, int, float, bool, None
            return node.value
        elif isinstance(node, ast.Attribute):
            target_obj = _eval_ast_node(node.value, current_expr_str)
            return getattr(target_obj, node.attr)
        else:
            globals.logger.error(f"Unsupported AST node type '{type(node).__name__}' in placeholder expression '{{{current_expr_str}}}'. AST: {ast.dump(node)}")
            raise ValueError(f"Unsupported AST node type '{type(node).__name__}' in expression '{{{current_expr_str}}}'")

    try:
        # Attempt to parse the expression string (e.g., "pr_diff", "my_dict['key']")
        tree = ast.parse(expr, mode='eval') # This line can raise SyntaxError
        evaluated_value = _eval_ast_node(tree.body, expr)
        return str(evaluated_value)
    except SyntaxError:
        # If 'expr' is not valid Python syntax (e.g., "user-name")
        globals.logger.warning(f"SyntaxError parsing placeholder expression '{{{expr}}}'. Returning placeholder as is.")
        return f"{{{expr}}}" # Return the original placeholder string (e.g., "{user-name}")
    except (KeyError, NameError, AttributeError, IndexError, TypeError, ValueError) as e:
        # Catch errors during the custom AST evaluation (e.g., key not found, unsupported node)
        globals.logger.warning(f"Evaluation error for placeholder '{{{expr}}}': {type(e).__name__}: {e}. Returning placeholder as is.")
        return f"{{{expr}}}" # Return the original placeholder string
    except Exception as e:
        # Catch any other unexpected errors
        globals.logger.error(f"Unexpected error evaluating placeholder '{{{expr}}}': {type(e).__name__}: {e}. Returning placeholder as is.")
        return f"{{{expr}}}"

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
            print_output(f"[⚠️ Warning] Auto-approving tool '{tool_name}' execution.", color="yellow")
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
            print_output(f"[ℹ️ Info] Tool execution approved.", color="yellow")
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

def remove_think_tags(text: str) -> str:
    """Removes <think>...</think> tags and their content from a string."""
    if not isinstance(text, str):
        return text
    # Remove <think>...</think> blocks and any trailing whitespace, then strip overall result
    return re.sub(r"<think>.*?</think>\s*", "", text, flags=re.DOTALL).strip()

def try_extract_stdout_from_string(text_input: str) -> Union[str, List[str], None]:
    """
    Attempts to extract content from an 'stdout' key if the text_input is a
    JSON or dict-like string (e.g., from shell_command output).
    Returns the extracted content (str or List[str]) or None if not found/parsed
    or if 'stdout' value is not a string or list.
    """
    stripped = text_input.strip()
    if not stripped:
        return None

    parsed_data = None
    try:
        # Try proper JSON first
        parsed_data = json.loads(stripped, strict=False)
    except json.JSONDecodeError:
        # Fallback to Python-style dicts (e.g., using single quotes)
        try:
            parsed_data = ast.literal_eval(stripped)
        except (ValueError, SyntaxError):
            return text_input # Not parsable as JSON or Python literal

    if isinstance(parsed_data, dict) and 'stdout' in parsed_data:
        stdout_value = parsed_data['stdout']
        # Ensure the stdout value is either a string or a list (of any items, will be stringified later)
        if isinstance(stdout_value, (str, list)):
            return stdout_value
    return None
