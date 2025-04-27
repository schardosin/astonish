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
    _provider = None
    _model = None

    @classmethod
    def get_llm(cls, schema=None) -> AIProvider:
        default_provider = globals.config.get('GENERAL', 'default_provider', fallback='ollama')
        default_model = globals.config.get('GENERAL', 'default_model', fallback='llama2:latest')

        if cls._instance is None or default_provider != cls._provider or default_model != cls._model:
            cls._instance = cls._create_llm(default_provider, default_model, schema)
            cls._provider = default_provider
            cls._model = default_model
        return cls._instance

    @classmethod
    def _create_llm(cls, default_provider, default_model, schema=None) -> AIProvider:
        # Get the provider instance
        provider = AIProviderFactory.get_provider(default_provider)

        # Configure the provider with the default model
        if isinstance(provider, OllamaProvider):
            return provider.get_llm(default_model, False, schema)
        else:
            return provider.get_llm(default_model, False)
