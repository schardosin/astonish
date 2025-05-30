#!/usr/bin/env python3
import argparse
import asyncio

# Constants for version information
AUTHOR = "Rafael Schardosin Silva"
PROJECT_NAME = "Astonish AI companion"
GITHUB_LINK = "https://github.com/schardosin/astonish"

def get_version():
    from importlib.metadata import version, PackageNotFoundError
    try:
        return version("astonish")
    except PackageNotFoundError:
        return "unknown"

def version_info():
    from astonish import logo
    version_data = {
        "version": get_version(),
        "name": PROJECT_NAME,
        "author": AUTHOR,
        "github": GITHUB_LINK
    }
    print(logo.ASCII_LOGO)
    print(f"{version_data['name']}")
    print(f"Version: {version_data['version']}")
    print(f"Author: {version_data['author']}")
    print(f"GitHub: {version_data['github']}")

class VersionAction(argparse.Action):
    def __call__(self, parser, namespace, values, option_string=None):
        version_info()
        parser.exit()

async def main(args=None):
    if args is None:
        args = parse_arguments()

    if args.command == "setup":
        await handle_setup_command(args)
    elif args.command == "agents":
        await handle_agents_command(args)
    elif args.command == "tools":
        await handle_tools_command(args)
    elif args.command == "config":
        await handle_config_command(args)
    else:
        print(f"Unknown command: {args.command}")

async def handle_setup_command(args):
    import astonish.globals as globals
    globals.setup_logger(verbose=args.verbose)
    globals.load_config()
    globals.load_mcp_config()

    if args.type in [None, "provider"]:
        from astonish.factory.ai_provider_factory import AIProviderFactory
        await setup(AIProviderFactory)
    else:
        print(f"Unknown setup type: {args.type}")

async def handle_agents_command(args):
    import astonish.globals as globals
    globals.setup_logger(verbose=args.verbose)
    globals.load_config()
    globals.load_mcp_config()

    if args.agents_command == "run":
        from astonish.core.agent_runner import run_agent
        parameters = parse_parameters(args.params)
        await run_agent(args.task, parameters)
    elif args.agents_command == "flow":
        from astonish.core.graph_builder import print_flow
        print_flow(args.task)
    elif args.agents_command == "list":
        from astonish.core.utils import list_agents
        await list_agents()
    elif args.agents_command == "edit":
        from astonish.core.utils import edit_agent
        result = edit_agent(args.agent)
        print(result)
    else:
        print(f"Unknown agents command: {args.agents_command}")

async def handle_tools_command(args):
    import astonish.globals as globals
    globals.setup_logger(verbose=args.verbose)
    globals.load_config()
    globals.load_mcp_config()

    if args.tools_command == "list":
        await list_tools(args)
    elif args.tools_command == "edit":
        from astonish.tools.mcp_config_editor import edit_mcp_config
        result = edit_mcp_config()
        print(result)
    else:
        print(f"Unknown tools command: {args.tools_command}")

async def handle_config_command(args):
    import astonish.globals as globals
    import os
    globals.setup_logger(verbose=args.verbose)
    globals.load_config()
    
    if args.config_command == "edit":
        result = globals.open_editor(globals.config_path)
        print(result)
    elif args.config_command == "show":
        if os.path.exists(globals.config_path):
            with open(globals.config_path, 'r') as file:
                print(file.read())
        else:
            print(f"Config file not found at {globals.config_path}")
    elif args.config_command == "directory":
        print(globals.config_dir)
    else:
        print(f"Unknown config command: {args.config_command}")

def parse_parameters(params):
    if not params:
        return None
    parameters = {}
    for param in params:
        if '=' in param:
            key, value = param.split('=', 1)
            parameters[key.strip()] = value.strip()
        else:
            print(f"Warning: Ignoring malformed parameter: {param} (missing '=')")
    return parameters

async def setup(AIProviderFactory):
    print("Select a provider to configure:")
    try:
        providers = AIProviderFactory.get_registered_providers()
        if not providers:
            print("No providers found.")
            return
    except Exception as e:
        print(f"Error: Could not fetch providers: {e}")
        return

    for i, (provider_name, display_name) in enumerate(providers, 1):
        print(f"{i}. {display_name}")

    while True:
        choice = input("Enter the number of your choice: ")
        try:
            choice_index = int(choice) - 1
            if 0 <= choice_index < len(providers):
                provider_name, display_name = providers[choice_index]
                break
            else:
                print("Invalid choice. Please select a number from the list.")
        except ValueError:
            print("Invalid input. Please enter a number.")

    try:
        provider = AIProviderFactory.get_provider(provider_name)
        provider.setup()
        print(f"{display_name} configured successfully!")
    except Exception as e:
        print(f"Error during provider setup: {e}")

def parse_arguments():
    parser = argparse.ArgumentParser(
        description="Astonish AI Companion.",
        usage="astonish [OPTIONS] COMMAND",
        add_help=False
    )
    
    parser.add_argument("-h", "--help", action="help", help="Show this help message and exit")
    parser.add_argument("-v", "--verbose", action="store_true", help="Enable verbose output")
    parser.add_argument("--version", action=VersionAction, nargs=0, help="Show version information and exit")

    subparsers = parser.add_subparsers(dest="command", help="Available commands")

    setup_parser = subparsers.add_parser("setup", help="Configure the application")
    setup_parser.add_argument("type", nargs='?', choices=['provider'], help="Setup type")

    agents_parser = subparsers.add_parser("agents", help="Manage and run agents")
    agents_subparsers = agents_parser.add_subparsers(dest="agents_command", help="Agents management commands")
    
    agents_run_parser = agents_subparsers.add_parser("run", help="Run a specific agentic workflow")
    agents_run_parser.add_argument("task", help="Name of the agentic workflow to run")
    agents_run_parser.add_argument("-p", "--param", action="append", dest="params", 
                                   help="Parameter to pass to the agent in key=value format. Can be used multiple times.")
    
    agents_subparsers.add_parser("flow", help="Print the flow of a specific agentic workflow").add_argument("task", help="Name of the agentic workflow to print flow for")
    agents_subparsers.add_parser("list", help="List all available agents")
    agents_subparsers.add_parser("edit", help="Edit a specific agent").add_argument("agent", help="Name of the agent to edit")

    tools_parser = subparsers.add_parser("tools", help="Manage tools")
    tools_subparsers = tools_parser.add_subparsers(dest="tools_command", help="Tools management commands")
    
    tools_subparsers.add_parser("list", help="List available tools").add_argument("--json", action="store_true", help="Output in JSON format")
    tools_subparsers.add_parser("edit", help="Edit MCP configuration")

    config_parser = subparsers.add_parser("config", help="Manage configuration")
    config_subparsers = config_parser.add_subparsers(dest="config_command", help="Configuration management commands")
    
    config_subparsers.add_parser("edit", help="Open config.ini in default editor")
    config_subparsers.add_parser("show", help="Print config.ini contents")
    config_subparsers.add_parser("directory", help="Print the configuration directory path")

    args = parser.parse_args()
    if args.command is None:
        parser.print_help()
        exit(1)
    elif args.command == "agents" and args.agents_command is None:
        agents_parser.print_help()
        exit(1)
    elif args.command == "config" and args.config_command is None:
        config_parser.print_help()
        exit(1)

    return args

async def list_tools(args=None):
    import astonish.globals as globals
    from astonish.tools.internal_tools import tools
    from astonish.core.utils import print_output, print_rich
    import json
    
    # Get args from main if not provided
    if args is None:
        args = parse_arguments()
    
    print("Initializing MCP tools...")
    mcp_client = await globals.initialize_mcp_tools()
    
    if mcp_client is None:
        print("Failed to initialize MCP tools. Please check your configuration.")
        print(f"MCP Config: {globals.mcp_config}")
        return

    try:
        print("\nFetching tools...")
        async with mcp_client as client:
            all_tools = client.get_tools() + tools
        
        if not all_tools:
            print("No tools available.")
        else:
            # Check if JSON output is requested
            if hasattr(args, 'json') and args.json:
                # Format tools as JSON
                tools_json = []
                for tool in all_tools:
                    tools_json.append({
                        "name": tool.name,
                        "description": tool.description
                    })
                print(json.dumps(tools_json, indent=2))
            else:
                # Standard output format
                print_output("Available tools:")
                for tool in all_tools:
                    print_rich(f"```yaml\n  - {tool.name}: {tool.description} \n```")
    except Exception as e:
        globals.logger.error(f"Error in list_tools: {str(e)}")
        print(f"An error occurred in list_tools: {str(e)}")

def run():
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        print("\nBye Bye!")
    except Exception as e:
        print(f"An unexpected error occurred: {e}")

if __name__ == "__main__":
    run()
