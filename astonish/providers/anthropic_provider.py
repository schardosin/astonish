import os
import astonish.globals as globals
from langchain_anthropic import ChatAnthropic
from astonish.providers.ai_provider_interface import AIProvider
from typing import List

class AnthropicProvider(AIProvider):
    def __init__(self):
        self.api_key = None

    def setup(self):
        print("Setting up Anthropic...")
        
        # Default values and examples
        defaults = {
            'api_key': ('', 'your-anthropic-api-key'),
        }

        # Load existing configuration if it exists
        if os.path.exists(globals.config_path):
            globals.config.read(globals.config_path)
        
        # Ensure ANTHROPIC section exists
        if 'ANTHROPIC' not in globals.config:
            globals.config['ANTHROPIC'] = {}

        # Input new values
        for key, (default, example) in defaults.items():
            current_value = globals.config['ANTHROPIC'].get(key, '')
            if current_value:
                new_value = input(f"Enter {key} (current: {current_value}): ").strip()
            else:
                new_value = input(f"Enter {key} (example: {example}): ").strip()
            globals.config['ANTHROPIC'][key] = new_value if new_value else (current_value or default)

        os.makedirs(os.path.dirname(globals.config_path), exist_ok=True)
        os.makedirs(os.path.dirname(globals.config_path)+'/agents', exist_ok=True)
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        globals.config.read(globals.config_path)
        self._initialize_api_key()

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
        globals.config['GENERAL']['default_provider'] = 'anthropic'
        globals.config['GENERAL']['default_model'] = default_model

        # Write the configuration
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        print(f"\nAnthropic configuration saved successfully.")
        print(f"Default model set to: {default_model}")

    def _initialize_api_key(self):
        if not os.path.exists(globals.config_path):
            raise FileNotFoundError("Configuration file not found. Please run setup() first.")
        
        globals.config.read(globals.config_path)
        
        # Set API key from the configuration
        self.api_key = globals.config.get('ANTHROPIC', 'api_key')
        os.environ["ANTHROPIC_API_KEY"] = self.api_key

    def get_supported_models(self) -> List[str]:
        import requests
        
        if not self.api_key:
            self._initialize_api_key()

        url = "https://api.anthropic.com/v1/models"
        headers = {
            "x-api-key": self.api_key,
            "anthropic-version": "2023-06-01"
        }

        response = requests.get(url, headers=headers)
        
        if response.status_code != 200:
            raise Exception(f"Failed to fetch models: {response.status_code} - {response.text}")

        models = response.json()['data']
        model_names = [model['id'] for model in models]

        if not model_names:
            raise Exception("No models found from Anthropic API.")

        return model_names

    def get_llm(self, model_name: str, streaming: bool = True):
        if not self.api_key:
            self._initialize_api_key()

        # Initialize and return the LLM
        llm = ChatAnthropic(
            model=model_name,
            anthropic_api_key=self.api_key,
            streaming=streaming,
            max_tokens=4096,
            temperature=0
        )
        return llm