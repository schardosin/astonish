import pytest
from unittest.mock import patch, MagicMock
from astonish.core.llm_manager import LLMManager
from astonish.providers.ollama_provider import OllamaProvider
from astonish.providers.sap_ai_core_provider import SAPAICoreProvider
from astonish.factory.ai_provider_factory import AIProviderFactory

@pytest.fixture(autouse=True)
def reset_llm_manager():
    LLMManager._instance = None
    yield
    LLMManager._instance = None

@pytest.fixture
def mock_config():
    with patch('astonish.globals.config') as mock_config:
        mock_config.get.side_effect = lambda section, key, fallback=None: {
            ('GENERAL', 'default_provider'): 'ollama',
            ('GENERAL', 'default_model'): 'llama2:latest'
        }.get((section, key), fallback)
        yield mock_config

def test_get_llm_ollama(mock_config):
    with patch('astonish.factory.ai_provider_factory.AIProviderFactory.get_provider') as mock_get_provider:
        mock_ollama = MagicMock(spec=OllamaProvider)
        mock_get_provider.return_value = mock_ollama

        llm = LLMManager.get_llm()

        mock_get_provider.assert_called_once_with('ollama')
        mock_ollama.get_llm.assert_called_once_with('llama2:latest', False, None)
        assert llm == mock_ollama.get_llm.return_value

def test_get_llm_sap_ai_core(mock_config):
    with patch('astonish.globals.config') as mock_config, \
         patch('astonish.factory.ai_provider_factory.AIProviderFactory.get_provider') as mock_get_provider:
        mock_config.get.side_effect = lambda section, key, fallback=None: {
            ('GENERAL', 'default_provider'): 'sap_ai_core',
            ('GENERAL', 'default_model'): 'test_model'
        }.get((section, key), fallback)
        
        mock_sap_ai_core = MagicMock(spec=SAPAICoreProvider)
        mock_get_provider.return_value = mock_sap_ai_core

        llm = LLMManager.get_llm()

        mock_get_provider.assert_called_once_with('sap_ai_core')
        mock_sap_ai_core.get_llm.assert_called_once_with('test_model', False)
        assert llm == mock_sap_ai_core.get_llm.return_value

def test_get_llm_unsupported_provider(mock_config):
    with patch('astonish.globals.config') as mock_config, \
         patch('astonish.factory.ai_provider_factory.AIProviderFactory.get_provider') as mock_get_provider:
        mock_config.get.side_effect = lambda section, key, fallback=None: {
            ('GENERAL', 'default_provider'): 'unsupported_provider',
            ('GENERAL', 'default_model'): 'test_model'
        }.get((section, key), fallback)
        
        mock_unsupported = MagicMock()
        mock_get_provider.return_value = mock_unsupported

        with pytest.raises(ValueError, match="Unsupported provider: unsupported_provider"):
            LLMManager.get_llm()

def test_get_llm_singleton():
    with patch('astonish.factory.ai_provider_factory.AIProviderFactory.get_provider') as mock_get_provider, \
         patch('astonish.globals.config') as mock_config:
        mock_config.get.side_effect = lambda section, key, fallback=None: {
            ('GENERAL', 'default_provider'): 'ollama',
            ('GENERAL', 'default_model'): 'llama2:latest'
        }.get((section, key), fallback)
        
        mock_ollama = MagicMock(spec=OllamaProvider)
        mock_get_provider.return_value = mock_ollama

        llm1 = LLMManager.get_llm()
        llm2 = LLMManager.get_llm()

        assert llm1 == llm2
        assert mock_get_provider.call_count == 1  # Should only be called once due to singleton pattern
        mock_ollama.get_llm.assert_called_once_with('llama2:latest', False, None)

if __name__ == '__main__':
    pytest.main()
