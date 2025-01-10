import configparser
import os
import appdirs
from astonish.providers.ai_provider_interface import AIProvider
from astonish.providers.ollama_provider import OllamaProvider
from astonish.providers.sap_ai_core_provider import SAPAICoreProvider
from astonish.factory.ai_provider_factory import AIProviderFactory
from astonish.globals import CONFIG_FILE
import astonish.globals as globals

class LLMManager:
    _instance = None

    @staticmethod
    def get_llm(schema=None) -> AIProvider:
        default_provider = globals.config.get('GENERAL', 'default_provider', fallback='ollama')
        default_model = globals.config.get('GENERAL', 'default_model', fallback='llama2:latest')

        if LLMManager._instance is None or default_provider == 'ollama':
            LLMManager._instance = LLMManager._create_llm(default_provider, default_model, schema)
        return LLMManager._instance

    @staticmethod
    def _create_llm(default_provider, default_model, schema=None) -> AIProvider:
        # Get the provider instance
        provider = AIProviderFactory.get_provider(default_provider)

        # Configure the provider with the default model
        if isinstance(provider, OllamaProvider):
            return provider.get_llm(default_model, False, schema)
        elif isinstance(provider, SAPAICoreProvider):
            # Configure SAP AI Core provider (you may need to adjust this based on its specific configuration needs)
            return provider.get_llm(default_model, False)
        else:
            raise ValueError(f"Unsupported provider: {default_provider}")
