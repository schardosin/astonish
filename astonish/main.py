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
    print("1. SAP AI Core")
    
    choice = input("Enter the number of your choice: ")
    
    if choice == '1':
        provider_name = "sap_ai_core"
        try:
            provider = AIProviderFactory.get_provider(provider_name)
            provider.setup()
            print("LLM initialized successfully!")
        except Exception as e:
            print(f"Error: {e}") 
    else:
        print("Invalid choice or not implemented yet.")

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