import pytest
from unittest.mock import MagicMock, patch
from astonish.tools.tavily_search import Tool

@pytest.fixture
def mock_tavily_client():
    with patch('astonish.tools.tavily_search.TavilyClient') as mock_client:
        yield mock_client

def test_tool_initialization():
    config = {'api_key': 'test_api_key'}
    tool = Tool(config)
    assert tool.client is not None

def test_tool_initialization_missing_api_key():
    with pytest.raises(ValueError, match="API key for Tavily Search is not set"):
        Tool({})

@pytest.mark.parametrize("query, expected_result", [
    ("test query", "Search Results for 'test query':\nMock search result"),
    (["query1", "query2"], "Search Results for 'query1':\nMock search result\n\nSearch Results for 'query2':\nMock search result"),
    ({"key1": "value1", "key2": "value2"}, "Search Results for 'key1': 'value1':\nMock search result\n\nSearch Results for 'key2': 'value2':\nMock search result"),
])
def test_execute(mock_tavily_client, query, expected_result):
    mock_tavily_client.return_value.search.return_value = "Mock search result"
    tool = Tool({'api_key': 'test_api_key'})
    result = tool.execute(query)
    assert result == expected_result

def test_execute_unsupported_type():
    tool = Tool({'api_key': 'test_api_key'})
    with pytest.raises(ValueError, match="Unsupported query type"):
        tool.execute(123)

def test_format_query():
    tool = Tool({'api_key': 'test_api_key'})
    assert tool._format_query('simple query') == 'simple query'
    assert tool._format_query('{"key": "value"}') == 'key value'
    assert tool._format_query({"key": "value"}) == 'key value'
    assert tool._format_query(["item1", "item2"]) == 'item1 item2'
    assert tool._format_query(["item1", {"key": "value"}]) == 'item1 key value'

if __name__ == '__main__':
    pytest.main()
