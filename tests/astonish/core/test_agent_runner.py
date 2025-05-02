import pytest
from unittest.mock import patch, MagicMock, AsyncMock
from astonish.core.agent_runner import run_agent

@pytest.fixture
def mock_config():
    return {
        'nodes': [
            {
                'name': 'node1',
                'output_model': {'field1': 'str', 'field2': 'int'},
                'limit_counter_field': 'counter'
            },
            {
                'name': 'node2',
                'output_model': {'field3': 'bool'}
            }
        ],
        'flow': [
            {'from': 'START', 'to': 'node1'},
            {'from': 'node1', 'to': 'node2'},
            {'from': 'node2', 'to': 'END'}
        ]
    }

@pytest.mark.asyncio
async def test_run_agent(mock_config):
    with patch('astonish.core.agent_runner.set_debug') as mock_set_debug, \
         patch('astonish.core.agent_runner.load_agents', return_value=mock_config) as mock_load_agents, \
         patch('astonish.globals.initialize_mcp_tools') as mock_initialize_mcp_tools, \
         patch('astonish.core.agent_runner.AsyncSqliteSaver') as mock_async_sqlite_saver, \
         patch('astonish.core.agent_runner.build_graph') as mock_build_graph, \
         patch('astonish.core.agent_runner.run_graph') as mock_run_graph, \
         patch('astonish.core.agent_runner.print_ai') as mock_print_ai:

        mock_initialize_mcp_tools.return_value = AsyncMock()
        mock_async_sqlite_saver.from_conn_string.return_value.__aenter__.return_value = MagicMock()

        # Mock the initial state
        expected_initial_state = {
            'field1': None,
            'field2': None,
            'field3': None,
            'counter': 0
        }

        await run_agent('test_agent')

        mock_set_debug.assert_called_once_with(False)
        mock_load_agents.assert_called_once_with('test_agent')
        mock_initialize_mcp_tools.assert_called_once()
        mock_async_sqlite_saver.from_conn_string.assert_called_once_with(":memory:")
        mock_build_graph.assert_called_once()
        mock_run_graph.assert_called_once()
        mock_print_ai.assert_called_once_with("Bye! Bye!")

        # Check if initial_state is correctly set
        call_args = mock_run_graph.call_args
        assert call_args is not None, "run_graph was not called"
        initial_state = call_args.args[1]  # Access the second positional argument (initial_state)
        assert initial_state == expected_initial_state

@pytest.mark.asyncio
async def test_run_agent_exception():
    with patch('astonish.core.agent_runner.load_agents', side_effect=Exception("Test exception")), \
         patch('astonish.core.agent_runner.print_ai') as mock_print_ai, \
         patch('astonish.core.agent_runner.set_debug'), \
         patch('astonish.globals.initialize_mcp_tools', return_value=AsyncMock()):

        with pytest.raises(Exception, match="Test exception"):
            await run_agent('test_agent')

        # "Bye! Bye!" is not printed when an exception occurs
        mock_print_ai.assert_not_called()

if __name__ == '__main__':
    pytest.main()
