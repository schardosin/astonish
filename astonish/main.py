#!/usr/bin/env python3

import argparse
import sys
from astonish.factory.ai_provider_factory import AIProviderFactory

def main(args=None):
    if args is None:
        args = parse_arguments()

    if args.setup:
        setup()
    else:
        # Your existing main application logic here
        print(f"Hello, {args.name}!")
        if args.verbose:
            print("Verbose mode is on")

def setup():
    print("Select a provider to configure:")
    
    # Get the list of registered providers
    providers = AIProviderFactory.get_registered_providers()
    
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
    except Exception as e:
        print(f"Error: {e}")

def parse_arguments():
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
    
    return parser.parse_args()


if __name__ == "__main__":
    main()