import asyncio
import astonish.globals as globals

class MCPManager:
    """
    A singleton-like manager to handle the lifecycle of the MCP client.
    """
    def __init__(self):
        # We'll initialize the client using globals.initialize_mcp_tools() later
        self._client = None
        self._active_session = None

    async def startup(self):
        """
        Enters the client's context to establish and store the active session.
        """
        if not self._active_session:
            try:
                # Initialize MCP client
                self._client = await globals.initialize_mcp_tools()
                if self._client:
                    print("Starting up MCP session...")
                    # This is the equivalent of the 'async with' entry
                    self._active_session = await self._client.__aenter__()
            except Exception as e:
                globals.logger.warning(f"Error initializing MCP tools: {e}")
                self._client = None

    async def shutdown(self):
        """
        Exits the client's context to cleanly close the session.
        """
        if self._active_session:
            print("Shutting down MCP session...")
            # This is the equivalent of the 'async with' exit
            await self._client.__aexit__(None, None, None)
            self._active_session = None

    def get_session(self):
        """
        Provides access to the active session.
        """
        if not self._active_session:
            raise RuntimeError("MCPManager has not been started up. Call startup() first.")
        return self._active_session
