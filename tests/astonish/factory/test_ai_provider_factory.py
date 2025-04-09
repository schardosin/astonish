import pytest
from astonish.factory.ai_provider_factory import AIProviderFactory
from astonish.providers.ai_provider_interface import AIProvider
from astonish.providers.sap_ai_core_provider import SAPAICoreProvider
from astonish.providers.ollama_provider import OllamaProvider

class MockProvider(AIProvider):
    def setup(self):
        pass
    def get_llm(self, model_name: str, **kwargs):
        pass
    def get_supported_models(self):
        return []

def test_register_provider():
    AIProviderFactory.register_provider("mock", "Mock Provider", MockProvider)
    assert "mock" in dict(AIProviderFactory.get_registered_providers())

def test_register_invalid_provider():
    class InvalidProvider:
        pass
    with pytest.raises(TypeError):
        AIProviderFactory.register_provider("invalid", "Invalid Provider", InvalidProvider)

def test_get_provider():
    provider = AIProviderFactory.get_provider("sap_ai_core")
    assert isinstance(provider, SAPAICoreProvider)

    provider = AIProviderFactory.get_provider("ollama")
    assert isinstance(provider, OllamaProvider)

def test_get_invalid_provider():
    with pytest.raises(ValueError):
        AIProviderFactory.get_provider("non_existent_provider")

def test_get_registered_providers():
    providers = AIProviderFactory.get_registered_providers()
    assert ("sap_ai_core", "SAP AI Core") in providers
    assert ("ollama", "Ollama") in providers

if __name__ == "__main__":
    pytest.main()
