from astonish.providers.ai_provider_interface import AIProvider
from astonish.providers.sap_ai_core_provider import SAPAICore
from typing import List

class AIProviderFactory:
    _providers = {}

    @classmethod
    def register_provider(cls, name: str, provider_class: type):
        if not issubclass(provider_class, AIProvider):
            raise TypeError("Provider must implement AIProvider interface")
        cls._providers[name] = provider_class

    @classmethod
    def get_provider(cls, name: str) -> AIProvider:
        provider_class = cls._providers.get(name)
        if not provider_class:
            raise ValueError(f"No provider registered with name: {name}")
        return provider_class()
    
    @classmethod
    def get_supported_models(cls, provider_name: str) -> List[str]:
        provider = cls.get_provider(provider_name)
        return provider.get_supported_models()

# Register the SAP AI Core provider
AIProviderFactory.register_provider("sap_ai_core", SAPAICore)