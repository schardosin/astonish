import configparser
import os
from gen_ai_hub.proxy.langchain.init_models import init_llm
from gen_ai_hub.proxy.core.proxy_clients import get_proxy_client
from astonish.providers.ai_provider_interface import AIProvider
from typing import List

class SAPAICore(AIProvider):
    def __init__(self):
        self.proxy_client = None

    def setup(self):
        print("Setting up SAP AI Core...")
        
        config = configparser.ConfigParser()
        config_path = os.path.expanduser('~/.astonish/config.ini')
        
        # Default values and examples
        defaults = {
            'client_id': ('', 'your-client-id'),
            'client_secret': ('', 'your-client-secret'),
            'auth_url': ('', 'https://<tenant-id>.authentication.sap.hana.ondemand.com'),
            'base_url': ('', 'https://api.ai.internalprod.eu-central-1.aws.ml.hana.ondemand.com/v2'),
            'resource_group': ('default', 'default')
        }

        # Load existing configuration if it exists
        if os.path.exists(config_path):
            config.read(config_path)
        else:
            config['SAP_AI_CORE'] = {}

        # Input new values
        for key, (default, example) in defaults.items():
            current_value = config.get('SAP_AI_CORE', key, fallback='')
            if current_value:
                new_value = input(f"Enter {key} (current: {current_value}): ").strip()
            else:
                new_value = input(f"Enter {key} (example: {example}): ").strip()
            config['SAP_AI_CORE'][key] = new_value if new_value else (current_value or default)

        # Initialize the proxy client
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

        # Add general section with default provider and model
        if 'GENERAL' not in config:
            config['GENERAL'] = {}
        config['GENERAL']['default_provider'] = 'sap_ai_core'
        config['GENERAL']['default_model'] = default_model

        # Ensure the directory exists
        os.makedirs(os.path.dirname(config_path), exist_ok=True)

        # Write the configuration
        with open(config_path, 'w') as configfile:
            config.write(configfile)

        print(f"\nSAP AI Core configuration saved successfully.")
        print(f"Default model set to: {default_model}")

    def _initialize_proxy_client(self):
        config = configparser.ConfigParser()
        config_path = os.path.expanduser('~/.astonish/config.ini')
        
        if not os.path.exists(config_path):
            raise FileNotFoundError("Configuration file not found. Please run setup() first.")
        
        config.read(config_path)
        
        # Set environment variables from the configuration
        os.environ["AICORE_AUTH_URL"] = config.get('SAP_AI_CORE', 'auth_url')
        os.environ["AICORE_CLIENT_ID"] = config.get('SAP_AI_CORE', 'client_id')
        os.environ["AICORE_CLIENT_SECRET"] = config.get('SAP_AI_CORE', 'client_secret')
        os.environ["AICORE_RESOURCE_GROUP"] = config.get('SAP_AI_CORE', 'resource_group')
        os.environ["AICORE_BASE_URL"] = config.get('SAP_AI_CORE', 'base_url')

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

        # Initialize and return the LLM
        llm = init_llm(model_name, proxy_client=self.proxy_client, streaming=streaming)
        return llm
    