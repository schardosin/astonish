import json
from astonish.tools.tool_base import ToolBase
from tavily import TavilyClient
from typing import Union, List, Dict

class Tool(ToolBase):
    required_config = {
        "api_key": {
            "type": "string",
            "description": "API key for TavilyClient"
        }
    }

    def __init__(self, config):
        api_key = config.get('api_key')

        if not api_key:
            raise ValueError("API key for Tavily Search is not set. Please run 'astonish setup tool tavily_search' to configure.")

        self.client = TavilyClient(api_key=api_key)

    def execute(self, query: Union[str, List, Dict]) -> str:
        if isinstance(query, str):
            return self._search_single(query)
        elif isinstance(query, list):
            return self._search_multiple(query)
        elif isinstance(query, dict):
            return self._search_dict(query)
        else:
            raise ValueError(f"Unsupported query type: {type(query)}")

    def _search_single(self, query: str) -> str:
        formatted_query = self._format_query(query)
        result = self.client.search(formatted_query)
        return f"Search Results for '{formatted_query}':\n{result}"

    def _search_multiple(self, queries: List[str]) -> str:
        results = []
        for query in queries:
            formatted_query = self._format_query(query)
            print(f"Searching for '{formatted_query}'")
            result = self.client.search(formatted_query)
            results.append(f"Search Results for '{formatted_query}':\n{result}")
        return "\n\n".join(results)

    def _search_dict(self, query_dict: Dict[str, str]) -> str:
        results = []
        for key, value in query_dict.items():
            formatted_value = self._format_query(value)
            result = self.client.search(formatted_value)
            results.append(f"Search Results for '{key}': '{formatted_value}':\n{result}")
        return "\n\n".join(results)

    def _format_query(self, query: Union[str, Dict, List]) -> str:
        if isinstance(query, str):
            try:
                json_data = json.loads(query)
                return self._format_json(json_data)
            except json.JSONDecodeError:
                return query
        elif isinstance(query, (dict, list)):
            return self._format_json(query)
        else:
            return str(query)

    def _format_json(self, json_data: Union[Dict, List]) -> str:
        if isinstance(json_data, dict):
            return " ".join(f"{key} {value}" for key, value in json_data.items())
        elif isinstance(json_data, list):
            return " ".join(self._format_json(item) if isinstance(item, (dict, list)) else str(item) for item in json_data)
        else:
            return str(json_data)