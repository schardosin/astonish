import os
import astonish.globals as globals
from langchain_openai import ChatOpenAI
from astonish.providers.ai_provider_interface import AIProvider
from typing import List, Dict

class GroqProvider(AIProvider):
    def __init__(self):
        self.api_key = None
        self.base_url = "https://api.groq.com/openai/v1"
        self.client = None

    def setup(self):
        print("Setting up Groq...")

        defaults = {
            'api_key': ('', 'your-groq-api-key'),
        }

        if os.path.exists(globals.config_path):
            globals.config.read(globals.config_path)

        if 'GROQ' not in globals.config:
            globals.config['GROQ'] = {}

        for key, (default, example) in defaults.items():
            current_value = globals.config['GROQ'].get(key, '')
            if current_value:
                new_value = input(f"Enter {key} (current: {current_value}): ").strip()
            else:
                new_value = input(f"Enter {key} (example: {example}): ").strip()
            globals.config['GROQ'][key] = new_value if new_value else (current_value or default)

        os.makedirs(os.path.dirname(globals.config_path), exist_ok=True)
        os.makedirs(os.path.dirname(globals.config_path) + '/agents', exist_ok=True)
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        globals.config.read(globals.config_path)
        self._initialize_api_key()

        supported_models = self.get_supported_models()
        print("\nSupported models:")
        for i, model in enumerate(supported_models, 1):
            print(f"{i}. {model}")

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

        if 'GENERAL' not in globals.config:
            globals.config['GENERAL'] = {}
        
        globals.config['GENERAL']['default_provider'] = 'groq'
        globals.config['GENERAL']['default_model'] = default_model

        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        print(f"\nGroq configuration saved successfully.")
        print(f"Default model set to: {default_model}")

    def _initialize_api_key(self):
        if not os.path.exists(globals.config_path):
            raise FileNotFoundError("Configuration file not found. Please run setup() first.")

        globals.config.read(globals.config_path)

        self.api_key = globals.config.get('GROQ', 'api_key')
        os.environ["GROQ_API_KEY"] = self.api_key

    def get_supported_models(self) -> List[Dict[str, str]]:
        import requests

        try:
            headers = {
                "Authorization": f"Bearer {self.api_key}",
                "Content-Type": "application/json"
            }

            response = requests.get(f"{self.base_url}/models", headers=headers)

            if response.status_code != 200:
                raise Exception(f"Failed to fetch models: {response.status_code} - {response.text}")

            models = response.json()['data']
            model_names = [model['id'] for model in models]

            if not model_names:
                raise Exception("No models found from Anthropic API.")

            return model_names

        except requests.RequestException as e:
            print(f"Error fetching models: {e}")
            return []

    def get_llm(self, model_name: str, streaming: bool = True):
        if not self.api_key:
            self._initialize_api_key()

        llm = ChatOpenAI(
            model=model_name,
            base_url=self.base_url,
            openai_api_key=self.api_key,
            streaming=streaming,
            max_tokens=4096,
            temperature=0
        )
        return llm
