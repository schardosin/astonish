from abc import ABC, abstractmethod
from typing import List

class AIProvider(ABC):
    @abstractmethod
    def setup(self):
        """Set up the configuration for the AI provider."""
        pass

    @abstractmethod
    def get_llm(self, model_name: str, **kwargs):
        """Get the language model from the provider."""
        pass

    @abstractmethod
    def get_supported_models(self) -> List[str]:
        """Return a list of supported model names."""
        pass