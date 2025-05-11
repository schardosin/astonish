import pytest
from unittest.mock import patch, MagicMock
import os
import configparser
from astonish.providers.ollama_provider import OllamaProvider
import astonish.globals as globals

@pytest.fixture
def mock_config():
    config = configparser.ConfigParser()
    config['OLLAMA'] = {'base_url': 'http://test:11434'}
    config['GENERAL'] = {'default_provider': 'ollama', 'default_model': 'test_model'}
    return config

@pytest.fixture
def ollama_provider():
    return OllamaProvider()

def test_setup(ollama_provider, mock_config):
    with patch('builtins.input', side_effect=['http://test:11434', '1']), \
         patch('astonish.globals.config', mock_config), \
         patch('astonish.globals.config_path', '/mock/config/path'), \
         patch('os.path.exists', return_value=True), \
         patch('os.makedirs'), \
         patch('builtins.open', MagicMock()), \
         patch.object(ollama_provider, 'get_supported_models', return_value=['model1', 'model2']):
        
        ollama_provider.setup()
        
        assert mock_config['OLLAMA']['base_url'] == 'http://test:11434'
        assert mock_config['GENERAL']['default_provider'] == 'ollama'
        assert mock_config['GENERAL']['default_model'] == 'model1'

def test_get_supported_models(ollama_provider):
    with patch('requests.get') as mock_get:
        mock_response = MagicMock()
        mock_response.json.return_value = {'models': [{'name': 'model1'}, {'name': 'model2'}]}
        mock_get.return_value = mock_response
        
        models = ollama_provider.get_supported_models()
        
        assert models == ['model1', 'model2']

def test_get_llm(ollama_provider, mock_config):
    with patch('os.path.exists', return_value=True), \
         patch('astonish.globals.config', mock_config), \
         patch('astonish.globals.config_path', '/mock/config/path'):
        
        # Ensure the mock_config has the correct structure
        mock_config['OLLAMA'] = {'base_url': 'http://test:11434'}
        
        llm = ollama_provider.get_llm('test_model')
        print(f"LLM object: {llm}")
        
        assert hasattr(llm, 'model')
        assert hasattr(llm, 'num_ctx')
        assert hasattr(llm, 'base_url')
        assert llm.model == 'test_model'
        assert llm.num_ctx == 8192
        assert llm.base_url == 'http://test:11434'

def test_get_llm_no_config(ollama_provider):
    with patch('os.path.exists', return_value=False):
        with pytest.raises(FileNotFoundError):
            ollama_provider.get_llm('test_model')

if __name__ == '__main__':
    pytest.main()
