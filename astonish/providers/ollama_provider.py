import os
import astonish.globals as globals
from astonish.providers.ai_provider_interface import AIProvider
#from langchain_community.llms import Ollama
from langchain_ollama import ChatOllama
from typing import List

class OllamaProvider(AIProvider):
    def __init__(self):
        self.base_url = None

    def setup(self):
        print("Setting up Ollama...")
        
        # Default values and examples
        defaults = {
            'base_url': ('http://localhost:11434', 'http://localhost:11434')
        }

        # Load existing configuration if it exists
        if os.path.exists(globals.config_path):
            globals.config.read(globals.config_path)

        # Ensure the OLLAMA section exists
        if 'OLLAMA' not in globals.config:
            globals.config['OLLAMA'] = {}

        # Input new values
        for key, (default, example) in defaults.items():
            current_value = globals.config['OLLAMA'].get(key, '')
            if current_value:
                new_value = input(f"Enter {key} (current: {current_value}): ").strip()
            else:
                new_value = input(f"Enter {key} (example: {example}): ").strip()
            globals.config['OLLAMA'][key] = new_value if new_value else (current_value or default)

        os.makedirs(os.path.dirname(globals.config_path), exist_ok=True)
        os.makedirs(os.path.dirname(globals.config_path)+'/agents', exist_ok=True)
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        globals.config.read(globals.config_path)
        self.base_url = globals.config['OLLAMA']['base_url']

        # Get supported models
        supported_models = self.get_supported_models()

        print("\nSupported models:")
        for i, model in enumerate(supported_models, 1):
            print(f"{i}. {model}")

        # Ask user to select a default model
        while True:
            try:
                selection = int(input("\nSelect the number of the model you want to use as default: "))
                if 1 <= selection <= len(supported_models):
                    default_model = supported_models[selection - 1]
                    break
                else:
                    print("Invalid selection. Please choose a number from the list.")
            except ValueError:
                print("Invalid input. Please enter a number.")

        # Ensure GENERAL section exists
        if 'GENERAL' not in globals.config:
            globals.config['GENERAL'] = {}

        # Add general section with default provider and model
        globals.config['GENERAL']['default_provider'] = 'ollama'
        globals.config['GENERAL']['default_model'] = default_model

        # Write the configuration
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        print(f"\nOllama configuration saved successfully.")
        print(f"Default model set to: {default_model}")

    def get_supported_models(self) -> List[str]:
        import requests

        try:
            response = requests.get(f"{self.base_url}/api/tags")
            response.raise_for_status()
            models = response.json()['models']
            return [model['name'] for model in models]
        except requests.RequestException as e:
            print(f"Error fetching models: {e}")
            return []

    def get_llm(self, model_name: str, streaming: bool = True, schema = None):
        if not os.path.exists(globals.config_path):
            raise FileNotFoundError("Configuration file not found. Please run setup() first.")
        
        globals.config.read(globals.config_path)
        
        base_url = globals.config.get('OLLAMA', 'base_url')

        # Initialize and return the Ollama LLM
        llm =  ChatOllama(
            base_url=base_url,
            model=model_name,
            num_ctx=4096,
            streaming=streaming,
            format=schema
        )

        return llm