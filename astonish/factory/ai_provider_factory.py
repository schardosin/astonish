from astonish.providers.ai_provider_interface import AIProvider
from astonish.providers.sap_ai_core_provider import SAPAICoreProvider
from astonish.providers.ollama_provider import OllamaProvider
from astonish.providers.openrouter_provider import OpenRouterProvider
from astonish.providers.lm_studio_provider import LMStudioProvider
from astonish.providers.anthropic_provider import AnthropicProvider
from astonish.providers.openai_provider import OpenAIProvider
from astonish.providers.groq_provider import GroqProvider
from astonish.providers.google_genai_provider import GoogleAIProvider
from typing import List, Tuple

class AIProviderFactory:
    _providers = {}

    @classmethod
    def register_provider(cls, name: str, display_name: str, provider_class: type):
        if not issubclass(provider_class, AIProvider):
            raise TypeError("Provider must implement AIProvider interface")
        cls._providers[name] = (display_name, provider_class)

    @classmethod
    def get_provider(cls, name: str) -> AIProvider:
        provider_info = cls._providers.get(name)
        if not provider_info:
            raise ValueError(f"No provider registered with name: {name}")
        return provider_info[1]()

    @classmethod
    def get_registered_providers(cls) -> List[Tuple[str, str]]:
        return [(name, info[0]) for name, info in cls._providers.items()]

AIProviderFactory.register_provider("anthropic", "Anthropic", AnthropicProvider)
AIProviderFactory.register_provider("google_genai", "Google GenAI", GoogleAIProvider)
AIProviderFactory.register_provider("groq", "Groq", GroqProvider)
AIProviderFactory.register_provider("lm_studio", "LM Studio", LMStudioProvider)
AIProviderFactory.register_provider("ollama", "Ollama", OllamaProvider)
AIProviderFactory.register_provider("openai", "OpenAI", OpenAIProvider)
AIProviderFactory.register_provider("openrouter", "Openrouter", OpenRouterProvider)
AIProviderFactory.register_provider("sap_ai_core", "SAP AI Core", SAPAICoreProvider)
