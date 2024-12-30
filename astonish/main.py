#!/usr/bin/env python3
import astonish.globals as globals
from logging.handlers import RotatingFileHandler

def main(args=None):
    if args is None:
        args = parse_arguments()

    # Set up logger based on verbose flag
    globals.setup_logger(verbose=args.verbose)

    if args.setup:
        globals.logger.info("Starting setup process...")
        setup()
    else:
        # Your existing main application logic here
        globals.logger.info(f"Hello, {args.name if args.name else 'User'}!")
        print(f"Hello, {args.name if args.name else 'User'}!")
        if args.verbose:
            print("Verbose mode is on")
            globals.logger.debug("Verbose mode enabled")
        else:
            globals.logger.debug("Verbose mode not enabled")

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

def parse_arguments():
    import argparse

    parser = argparse.ArgumentParser(
        description="Astonish AI Companion.",
        usage="astonish [OPTIONS]",
        add_help=False
    )
    
    app_options = parser.add_argument_group('Application Options')
    app_options.add_argument("--setup", action="store_true", help="Configure the application")
    
    additional_args = parser.add_argument_group('Additional Arguments')
    additional_args.add_argument("-h", "--help", action="help", help="Show this help message and exit")
    additional_args.add_argument("--version", action="version", version="%(prog)s 1.0")
    additional_args.add_argument("--verbose", action="store_true", help="Enable verbose output")

    return parser.parse_args()

if __name__ == "__main__":
    main()
