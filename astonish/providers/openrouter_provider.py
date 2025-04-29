import os
import astonish.globals as globals
from langchain_openai import ChatOpenAI
from astonish.providers.ai_provider_interface import AIProvider
from typing import List, Dict

class OpenRouterProvider(AIProvider):
    def __init__(self):
        self.api_key = None
        self.base_url = None
        self.site_url = None
        self.site_name = None

    def setup(self):
        from collections import defaultdict
        
        print("Setting up OpenRouter...")
        
        # Default values and examples
        defaults = {
            'api_key': ('', 'your-openrouter-api-key'),
            'base_url': ('https://openrouter.ai/api/v1', 'https://openrouter.ai/api/v1')
        }

        # Load existing configuration if it exists
        if os.path.exists(globals.config_path):
            globals.config.read(globals.config_path)
        
        # Ensure OPENROUTER section exists
        if 'OPENROUTER' not in globals.config:
            globals.config['OPENROUTER'] = {}

        # Input new values
        for key, (default, example) in defaults.items():
            current_value = globals.config['OPENROUTER'].get(key, '')
            if current_value:
                new_value = input(f"Enter {key} (current: {current_value}): ").strip()
            else:
                new_value = input(f"Enter {key} (example: {example}): ").strip()
            globals.config['OPENROUTER'][key] = new_value if new_value else (current_value or default)

        os.makedirs(os.path.dirname(globals.config_path), exist_ok=True)
        os.makedirs(os.path.dirname(globals.config_path)+'/agents', exist_ok=True)
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        globals.config.read(globals.config_path)
        self.api_key = globals.config['OPENROUTER']['api_key']
        self.base_url = globals.config['OPENROUTER']['base_url']

        # Get supported models
        supported_models = self.get_supported_models()

        # Sort by group first, then by name
        supported_models.sort(key=lambda x: (x['group'].lower(), x['name'].lower()))

        # Organize models by group
        grouped_models = defaultdict(list)
        for model in supported_models:
            grouped_models[model['group'].upper()].append(model)  # APPEND FULL model dict

        flat_models = []
        print("\nSupported models:")
        index = 1
        for group, models in grouped_models.items():
            print(f"\n{group}:")
            for model in models:
                print(f"{index}. {model['name']}")
                flat_models.append(model)
                index += 1

        while True:
            try:
                selection = int(input("\nSelect the number of the model you want to use as default: "))
                if 1 <= selection <= len(flat_models):
                    selected_model = flat_models[selection - 1]
                    default_model = selected_model['id']  # <-- Only take the ID as the value
                    print(f"You selected: {selected_model['name']} (Group: {selected_model['group']})")
                    break
                else:
                    print("Invalid selection. Please choose a number from the list.")
            except ValueError:
                print("Invalid input. Please enter a number.")

        # Ensure GENERAL section exists
        if 'GENERAL' not in globals.config:
            globals.config['GENERAL'] = {}
        
        # Add general section with default provider and model
        globals.config['GENERAL']['default_provider'] = 'openrouter'
        globals.config['GENERAL']['default_model'] = default_model

        # Write the configuration
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        print(f"\nOpenRouter configuration saved successfully.")
        print(f"Default model set to: {default_model}")

    def get_supported_models(self) -> List[Dict[str, str]]:
        import requests

        try:
            response = requests.get(f"{self.base_url}/models")
            response.raise_for_status()
            models = response.json()['data']

            standardized_models = []
            for model in models:
                full_id = model['id']
                if '/' in full_id:
                    group, name = full_id.split('/', 1)
                else:
                    group = "unknown"
                    name = full_id

                # Highlight free models
                if ":free" in name:
                    display_name = f"[FREE] {name}"
                else:
                    display_name = name

                standardized_models.append({
                    "id": full_id,
                    "name": display_name,  # use display_name instead of raw name
                    "group": group
                })
            
            return standardized_models

        except requests.RequestException as e:
            print(f"Error fetching models: {e}")
            return []


    def get_llm(self, model_name: str, streaming: bool = True):
        from astonish.main import GITHUB_LINK, PROJECT_NAME
        if not os.path.exists(globals.config_path):
            raise FileNotFoundError("Configuration file not found. Please run setup() first.")
                
        globals.config.read(globals.config_path)

        self.api_key = globals.config['OPENROUTER']['api_key']
        self.base_url = globals.config['OPENROUTER']['base_url']
        self.site_url = GITHUB_LINK
        self.site_name = PROJECT_NAME

        return ChatOpenAI(
            openai_api_key=self.api_key,
            openai_api_base=self.base_url,
            model_name=model_name,
            streaming=streaming
        )