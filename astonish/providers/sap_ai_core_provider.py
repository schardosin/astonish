import os
import astonish.globals as globals
from gen_ai_hub.proxy.langchain.init_models import init_llm
from gen_ai_hub.proxy.core.proxy_clients import get_proxy_client
from astonish.providers.ai_provider_interface import AIProvider
from typing import List

class SAPAICoreProvider(AIProvider):
    def __init__(self):
        self.proxy_client = None

    def setup(self):
        print("Setting up SAP AI Core...")
        
        # Default values and examples
        defaults = {
            'client_id': ('', 'your-client-id'),
            'client_secret': ('', 'your-client-secret'),
            'auth_url': ('', 'https://<tenant-id>.authentication.sap.hana.ondemand.com'),
            'base_url': ('', 'https://api.ai.internalprod.eu-central-1.aws.ml.hana.ondemand.com/v2'),
            'resource_group': ('default', 'default')
        }

        # Load existing configuration if it exists
        if os.path.exists(globals.config_path):
            globals.config.read(globals.config_path)
        
        # Ensure SAP_AI_CORE section exists
        if 'SAP_AI_CORE' not in globals.config:
            globals.config['SAP_AI_CORE'] = {}

        # Input new values
        for key, (default, example) in defaults.items():
            current_value = globals.config['SAP_AI_CORE'].get(key, '')
            if current_value:
                new_value = input(f"Enter {key} (current: {current_value}): ").strip()
            else:
                new_value = input(f"Enter {key} (example: {example}): ").strip()
            globals.config['SAP_AI_CORE'][key] = new_value if new_value else (current_value or default)

        os.makedirs(os.path.dirname(globals.config_path), exist_ok=True)
        os.makedirs(os.path.dirname(globals.config_path)+'/agents', exist_ok=True)
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        globals.config.read(globals.config_path)
        self._initialize_proxy_client()

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
        globals.config['GENERAL']['default_provider'] = 'sap_ai_core'
        globals.config['GENERAL']['default_model'] = default_model

        # Write the configuration
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        print(f"\nSAP AI Core configuration saved successfully.")
        print(f"Default model set to: {default_model}")

    def _initialize_proxy_client(self):
        if not os.path.exists(globals.config_path):
            raise FileNotFoundError("Configuration file not found. Please run setup() first.")
        
        globals.config.read(globals.config_path)
        
        # Set environment variables from the configuration
        os.environ["AICORE_AUTH_URL"] = globals.config.get('SAP_AI_CORE', 'auth_url')
        os.environ["AICORE_CLIENT_ID"] = globals.config.get('SAP_AI_CORE', 'client_id')
        os.environ["AICORE_CLIENT_SECRET"] = globals.config.get('SAP_AI_CORE', 'client_secret')
        os.environ["AICORE_RESOURCE_GROUP"] = globals.config.get('SAP_AI_CORE', 'resource_group')
        os.environ["AICORE_BASE_URL"] = globals.config.get('SAP_AI_CORE', 'base_url')

        # Initialize the proxy client
        self.proxy_client = get_proxy_client("gen-ai-hub")
        self.proxy_client.refresh_instance_cache()

    def get_supported_models(self) -> List[str]:
        if not self.proxy_client:
            self._initialize_proxy_client()

        model_names = [deployment.model_name for deployment in self.proxy_client.deployments]
        
        return sorted(list(set(model_names)))

    def get_llm(self, model_name: str, streaming: bool = True):
        if not self.proxy_client:
            self._initialize_proxy_client()

        # o1 and o3-mini require temperature=1
        if model_name in ["o1", "o3-mini"]:
            temperature = 1
        else:
            temperature = 0

        # Initialize and return the LLM
        llm = init_llm(model_name, proxy_client=self.proxy_client, streaming=streaming, max_tokens=4096, temperature=temperature)
        return llm
    