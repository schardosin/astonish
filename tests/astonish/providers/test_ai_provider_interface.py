import pytest
from astonish.providers.ai_provider_interface import AIProvider

class MockAIProvider(AIProvider):
    def setup(self):
        self.is_setup = True

    def get_llm(self, model_name: str, **kwargs):
        return f"LLM: {model_name}"

    def get_supported_models(self):
        return ["model1", "model2", "model3"]

def test_ai_provider_interface():
    provider = MockAIProvider()

    # Test setup
    provider.setup()
    assert provider.is_setup == True

    # Test get_llm
    llm = provider.get_llm("test_model")
    assert llm == "LLM: test_model"

    # Test get_supported_models
    models = provider.get_supported_models()
    assert models == ["model1", "model2", "model3"]

def test_ai_provider_abstract_methods():
    # Attempt to instantiate the abstract base class
    with pytest.raises(TypeError):
        AIProvider()

    # Create a subclass missing an abstract method
    class IncompleteProvider(AIProvider):
        def setup(self):
            pass

        def get_llm(self, model_name: str, **kwargs):
            pass

    # Attempt to instantiate the incomplete subclass
    with pytest.raises(TypeError):
        IncompleteProvider()
