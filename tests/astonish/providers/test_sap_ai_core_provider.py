import pytest
from unittest.mock import patch, MagicMock
import os
import configparser
from astonish.providers.sap_ai_core_provider import SAPAICoreProvider
from gen_ai_hub.proxy.core.proxy_clients import BaseProxyClient
import astonish.globals as globals

@pytest.fixture
def mock_config():
    config = configparser.ConfigParser()
    config['SAP_AI_CORE'] = {
        'client_id': 'test_client_id',
        'client_secret': 'test_client_secret',
        'auth_url': 'https://test.authentication.sap.hana.ondemand.com',
        'base_url': 'https://test.ai.internalprod.eu-central-1.aws.ml.hana.ondemand.com/v2',
        'resource_group': 'test_group'
    }
    config['GENERAL'] = {'default_provider': 'sap_ai_core', 'default_model': 'test_model'}
    return config

@pytest.fixture
def sap_ai_core_provider():
    return SAPAICoreProvider()

def test_setup(sap_ai_core_provider, mock_config):
    with patch('builtins.input', side_effect=['', '', '', '', '', '1']), \
         patch('astonish.globals.config', mock_config), \
         patch('astonish.globals.config_path', '/mock/config/path'), \
         patch('os.path.exists', return_value=True), \
         patch('os.makedirs'), \
         patch('builtins.open', MagicMock()), \
         patch.object(sap_ai_core_provider, 'get_supported_models', return_value=['model1', 'model2']), \
         patch.object(sap_ai_core_provider, '_initialize_proxy_client'):
        
        sap_ai_core_provider.setup()
        
        assert mock_config['SAP_AI_CORE']['client_id'] == 'test_client_id'
        assert mock_config['SAP_AI_CORE']['client_secret'] == 'test_client_secret'
        assert mock_config['SAP_AI_CORE']['auth_url'] == 'https://test.authentication.sap.hana.ondemand.com'
        assert mock_config['SAP_AI_CORE']['base_url'] == 'https://test.ai.internalprod.eu-central-1.aws.ml.hana.ondemand.com/v2'
        assert mock_config['SAP_AI_CORE']['resource_group'] == 'test_group'
        assert mock_config['GENERAL']['default_provider'] == 'sap_ai_core'
        assert mock_config['GENERAL']['default_model'] == 'model1'

def test_get_supported_models(sap_ai_core_provider):
    mock_deployment1 = MagicMock()
    mock_deployment1.model_name = 'model1'
    mock_deployment2 = MagicMock()
    mock_deployment2.model_name = 'model2'
    
    sap_ai_core_provider.proxy_client = MagicMock()
    sap_ai_core_provider.proxy_client.deployments = [mock_deployment1, mock_deployment2]
    
    models = sap_ai_core_provider.get_supported_models()
    assert models == ['model1', 'model2']

def test_get_llm(sap_ai_core_provider, mock_config):
    with patch('os.path.exists', return_value=True), \
         patch('astonish.globals.config', mock_config), \
         patch('astonish.globals.config_path', '/mock/config/path'), \
         patch('astonish.providers.sap_ai_core_provider.init_llm') as mock_init_llm, \
         patch('gen_ai_hub.proxy.core.proxy_clients.get_proxy_client') as mock_get_proxy_client, \
         patch('gen_ai_hub.proxy.core.proxy_clients.ProxyClients.get_proxy_cls_name', return_value='MockProxyClient'), \
         patch('gen_ai_hub.proxy.langchain.init_models.Catalog.retrieve') as mock_catalog_retrieve, \
         patch.object(sap_ai_core_provider, '_initialize_proxy_client'):
        
        mock_proxy_client = MagicMock(spec=BaseProxyClient)
        mock_get_proxy_client.return_value = mock_proxy_client
        sap_ai_core_provider.proxy_client = mock_proxy_client

        mock_catalog_retrieve.return_value = MagicMock()
        mock_init_llm.return_value = MagicMock()

        llm = sap_ai_core_provider.get_llm('test_model')
        
        mock_init_llm.assert_called_once_with(
            'test_model',
            proxy_client=mock_proxy_client,
            streaming=True,
            max_tokens=8192,
            temperature=0.7
        )
        assert llm == mock_init_llm.return_value

def test_get_llm_no_config(sap_ai_core_provider):
    with patch('os.path.exists', return_value=False):
        with pytest.raises(FileNotFoundError):
            sap_ai_core_provider.get_llm('test_model')

if __name__ == '__main__':
    pytest.main()
