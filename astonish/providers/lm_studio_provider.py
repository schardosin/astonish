import os
import requests
import astonish.globals as globals
from astonish.providers.ai_provider_interface import AIProvider
from langchain_openai import ChatOpenAI
from typing import List

class LMStudioProvider(AIProvider):
    def __init__(self):
        self.base_url = None

    def setup(self):
        print("Setting up LM Studio...")

        # Default values and examples
        defaults = {
            'base_url': ('http://localhost:1234/v1', 'http://localhost:1234/v1')
        }

        # Load existing configuration if it exists
        if os.path.exists(globals.config_path):
            globals.config.read(globals.config_path)

        # Ensure the LMSTUDIO section exists
        if 'LMSTUDIO' not in globals.config:
            globals.config['LMSTUDIO'] = {}

        # Input new values
        for key, (default, example) in defaults.items():
            current_value = globals.config['LMSTUDIO'].get(key, '')
            if current_value:
                new_value = input(f"Enter {key} (current: {current_value}): ").strip()
            else:
                new_value = input(f"Enter {key} (example: {example}): ").strip()
            globals.config['LMSTUDIO'][key] = new_value if new_value else (current_value or default)

        os.makedirs(os.path.dirname(globals.config_path), exist_ok=True)
        os.makedirs(os.path.dirname(globals.config_path)+'/agents', exist_ok=True)
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        globals.config.read(globals.config_path)
        self.base_url = globals.config['LMSTUDIO']['base_url']

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
        globals.config['GENERAL']['default_provider'] = 'lm_studio'
        globals.config['GENERAL']['default_model'] = default_model

        # Write the configuration
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        print(f"\nLM Studio configuration saved successfully.")
        print(f"Default model set to: {default_model}")

    def get_supported_models(self) -> List[str]:
        try:
            response = requests.get(f"{self.base_url}/models")
            response.raise_for_status()
            models = response.json()['data']
            return [model['id'] for model in models]
        except requests.RequestException as e:
            print(f"Error fetching models: {e}")
            return []

    def get_llm(self, model_name: str, streaming: bool = True, schema=None):
        if not os.path.exists(globals.config_path):
            raise FileNotFoundError("Configuration file not found. Please run setup() first.")
        
        globals.config.read(globals.config_path)
        
        base_url = globals.config.get('LMSTUDIO', 'base_url')

        # Initialize and return the LM Studio LLM
        llm = ChatOpenAI(
            api_key="lm-studio",
            base_url=base_url,
            model=model_name,
            temperature=0,
        )

        return llm
