import pytest
from astonish.tools.tool_base import ToolBase

def test_tool_base_abstract():
    # Test that ToolBase cannot be instantiated directly
    with pytest.raises(TypeError):
        ToolBase()

def test_tool_base_concrete_implementation():
    # Test that a concrete implementation of ToolBase can be instantiated and used
    class ConcreteTool(ToolBase):
        def execute(self, query: str) -> str:
            return f"Executed: {query}"
    
    tool = ConcreteTool()
    assert isinstance(tool, ToolBase)
    assert tool.execute("test query") == "Executed: test query"

def test_tool_base_abstract_method():
    # Test that a class inheriting from ToolBase must implement the execute method
    class IncompleteImplementation(ToolBase):
        pass

    with pytest.raises(TypeError):
        IncompleteImplementation()

if __name__ == "__main__":
    pytest.main()
