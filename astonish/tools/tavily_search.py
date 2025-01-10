from astonish.tools.tool_base import ToolBase
from tavily import TavilyClient
from typing import Union, List, Dict

class Tool(ToolBase):
    def __init__(self):
        self.client = TavilyClient(api_key="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")

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
        result = self.client.search(query)
        return f"Search Results for '{query}':\n{result}"

    def _search_multiple(self, queries: List[str]) -> str:
        results = []
        for query in queries:
            result = self.client.search(query)
            results.append(f"Search Results for '{query}':\n{result}")
        return "\n\n".join(results)

    def _search_dict(self, query_dict: Dict[str, str]) -> str:
        results = []
        for key, value in query_dict.items():
            result = self.client.search(value)
            results.append(f"Search Results for '{key}': '{value}':\n{result}")
        return "\n\n".join(results)