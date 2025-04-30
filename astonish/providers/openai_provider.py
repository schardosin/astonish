import os
import astonish.globals as globals
from openai import OpenAI
from langchain_openai import ChatOpenAI
from astonish.providers.ai_provider_interface import AIProvider
from typing import List

class OpenAIProvider(AIProvider):
    def __init__(self):
        self.api_key = None
        self.client = None

    def setup(self):
        print("Setting up OpenAI...")
        
        # Default values and examples
        defaults = {
            'api_key': ('', 'your-openai-api-key'),
        }
        # Load existing configuration if it exists
        if os.path.exists(globals.config_path):
            globals.config.read(globals.config_path)
        
        # Ensure OPENAI section exists
        if 'OPENAI' not in globals.config:
            globals.config['OPENAI'] = {}

        # Input new values
        for key, (default, example) in defaults.items():
            current_value = globals.config['OPENAI'].get(key, '')
            if current_value:
                new_value = input(f"Enter {key} (current: {current_value}): ").strip()
            else:
                new_value = input(f"Enter {key} (example: {example}): ").strip()
            globals.config['OPENAI'][key] = new_value if new_value else (current_value or default)

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
        globals.config['GENERAL']['default_provider'] = 'openai'
        globals.config['GENERAL']['default_model'] = default_model

        # Write the configuration
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        print(f"\nOpenAI configuration saved successfully.")
        print(f"Default model set to: {default_model}")

    def _initialize_api_key(self):
        if not os.path.exists(globals.config_path):
            raise FileNotFoundError("Configuration file not found. Please run setup() first.")
        
        globals.config.read(globals.config_path)
        
        # Set API key from the configuration
        self.api_key = globals.config.get('OPENAI', 'api_key')
        os.environ["OPENAI_API_KEY"] = self.api_key
        self.client = OpenAI(api_key=self.api_key)

    def get_supported_models(self) -> List[str]:
        if not self.client:
            self._initialize_api_key()

        try:
            models = self.client.models.list()
            model_names = [model.id for model in models.data if model.id.startswith("gpt")]
            if not model_names:
                raise Exception("No GPT models found from OpenAI API.")
            return model_names
        except Exception as e:
            raise Exception(f"Failed to fetch models: {str(e)}")

    def get_llm(self, model_name: str, streaming: bool = True):
        if not self.api_key:
            self._initialize_api_key()

        # Initialize and return the LLM
        llm = ChatOpenAI(
            model=model_name,
            openai_api_key=self.api_key,
            streaming=streaming,
            max_tokens=4096,
            temperature=0
        )
        return llm