class ProviderError(Exception):
    """Base exception for provider-related errors."""

class SapAICoreRateLimitError(ProviderError):
    """Raised when SAP AI Core rate limits a request."""

class SapAICoreAuthError(ProviderError):
    """Raised when SAP AI Core authentication fails."""
