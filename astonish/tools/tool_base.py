from abc import ABC, abstractmethod

class ToolBase(ABC):
    @abstractmethod
    def execute(self, query: str) -> str:
        pass