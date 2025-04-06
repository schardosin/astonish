#!/usr/bin/env python3
import asyncio
import astonish.globals as globals

async def main(args=None):
    from astonish.core.agent_runner import run_agent, print_flow

    if args is None:
        args = parse_arguments()

    # Set up logger based on verbose flag
    globals.setup_logger(verbose=args.verbose)
    globals.load_config()
    globals.load_mcp_config()

    if args.command == "setup":
        if args.type == "provider":
            globals.logger.info("Starting provider setup process...")
            setup()
        elif args.type == "tool":
            globals.logger.info(f"Starting tool setup process for {args.tool_name}...")
            setup_tool(args.tool_name)
        elif args.type == None:
            globals.logger.info("Starting provider setup process...")
            setup()
        else:
            globals.logger.error(f"Unknown setup type: {args.type}")
            print(f"Unknown setup type: {args.type}")
    elif args.command == "run":
        globals.logger.info(f"Running task: {args.task}")
        await run_agent(args.task)
    elif args.command == "flow":
        globals.logger.info(f"Printing flow for task: {args.task}")
        print_flow(args.task)
    elif args.command == "tools":
        if args.tools_command == "list":
            globals.logger.info("Listing available tools...")
            await list_tools()
        elif args.tools_command == "edit":
            globals.logger.info("Editing MCP configuration...")
            from astonish.tools.mcp_config_editor import edit_mcp_config
            result = edit_mcp_config()
            print(result)
        else:
            globals.logger.error(f"Unknown tools command: {args.tools_command}")
            print(f"Unknown tools command: {args.tools_command}")
    else:
        globals.logger.error(f"Unknown command: {args.command}")
        print(f"Unknown command: {args.command}")

def setup():
    from astonish.factory.ai_provider_factory import AIProviderFactory

    globals.logger.info("Starting setup process")
    print("Select a provider to configure:")
    
    try:
        globals.logger.info("Loading Registered Providers")
        providers = AIProviderFactory.get_registered_providers()
        if not providers:
            globals.logger.warning("No providers found")
            print("No providers found.")
            return
    except Exception as e:
        globals.logger.error("Failed to fetch providers", exc_info=True)
        print(f"Error: Could not fetch providers: {e}")
        return

    # Display the list of providers
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
        globals.logger.info(f"{display_name} configured successfully!")
    except Exception as e:
        globals.logger.error("Error during provider setup", exc_info=True)
        print(f"Error: {e}")

def setup_tool(tool_name):
    import importlib
    import configparser
    import os
    
    try:
        # Try to import the tool
        module = importlib.import_module(f"astonish.tools.{tool_name}")
        tool_class = getattr(module, 'Tool')
        
        # Get the required configuration
        required_config = tool_class.required_config
        
        # Load existing configuration if it exists
        if os.path.exists(globals.config_path):
            globals.config.read(globals.config_path)
        else:
            globals.config = configparser.ConfigParser()

        # Ensure there's a section for the tool
        if tool_name not in globals.config:
            globals.config[tool_name] = {}

        # Prompt for each required configuration item
        for key, details in required_config.items():
            current_value = globals.config.get(tool_name, key, fallback='')
            if current_value:
                new_value = input(f"Enter {details['description']} (current: {current_value}): ").strip()
            else:
                new_value = input(f"Enter {details['description']}: ").strip()
            globals.config[tool_name][key] = new_value if new_value else current_value

        # Save the configuration
        os.makedirs(os.path.dirname(globals.config_path), exist_ok=True)
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        print(f"{tool_name} configured successfully!")
        globals.logger.info(f"{tool_name} configured successfully!")
    except Exception as e:
        globals.logger.error(f"Error during {tool_name} setup", exc_info=True)
        print(f"Error: {e}")


def parse_arguments():
    import argparse

    parser = argparse.ArgumentParser(
        description="Astonish AI Companion.",
        usage="astonish [OPTIONS] COMMAND",
        add_help=False
    )
    
    parser.add_argument("-h", "--help", action="help", help="Show this help message and exit")
    parser.add_argument("-v", "--verbose", action="store_true", help="Enable verbose output")
    parser.add_argument("--version", action="version", version="%(prog)s 1.0")

    subparsers = parser.add_subparsers(dest="command", help="Available commands")

    # Setup command
    setup_parser = subparsers.add_parser("setup", help="Configure the application")
    setup_subparsers = setup_parser.add_subparsers(dest="type", help="Setup type")
    
    # Provider setup
    provider_setup_parser = setup_subparsers.add_parser("provider", help="Configure a provider")
    
    # Tool setup
    tool_setup_parser = setup_subparsers.add_parser("tool", help="Configure a tool")
    tool_setup_parser.add_argument("tool_name", help="Name of the tool to configure")

    # Run command
    run_parser = subparsers.add_parser("run", help="Run a specific agentic workflow")
    run_parser.add_argument("task", help="Name of the agentic workflow to run")

    # Flow command
    flow_parser = subparsers.add_parser("flow", help="Print the flow of a specific agentic workflow")
    flow_parser.add_argument("task", help="Name of the agentic workflow to print flow for")

    # Tools command
    tools_parser = subparsers.add_parser("tools", help="Manage tools")
    tools_subparsers = tools_parser.add_subparsers(dest="tools_command", help="Tools management commands")
    
    # Tools list command
    tools_list_parser = tools_subparsers.add_parser("list", help="List available tools")
    
    # Tools edit command
    tools_edit_parser = tools_subparsers.add_parser("edit", help="Edit MCP configuration")

    args = parser.parse_args()
    if args.command is None:
        parser.print_help()
        exit(1)

    return args

async def list_tools():
    from astonish.tools.internal_tools import tools
    
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
            print("Available tools:")
            for tool in all_tools:
                print(f"  - {tool.name}: {tool.description}")
    except Exception as e:
        globals.logger.error(f"Error in list_tools: {str(e)}")
        print(f"An error occurred in list_tools: {str(e)}")

def run():
    asyncio.run(main())

if __name__ == "__main__":
    run()
