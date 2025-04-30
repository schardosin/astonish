import pytest
from unittest.mock import patch, MagicMock
from astonish.core.graph_builder import (
    build_graph,
    safe_eval_condition,
    create_condition_function,
    create_combined_condition_function,
    print_flow
)

@pytest.fixture
def sample_node_config():
    return {
        'nodes': [
            {
                'name': 'node1',
                'type': 'input',
                'output_model': {'field1': 'str', 'field2': 'int'},
                'limit_counter_field': 'counter'
            },
            {
                'name': 'node2',
                'type': 'process',
                'output_model': {'field3': 'bool'}
            }
        ],
        'flow': [
            {'from': 'START', 'to': 'node1'},
            {
                'from': 'node1',
                'edges': [
                    {'condition': 'lambda x: x["field2"] > 5', 'to': 'node2'},
                    {'condition': 'lambda x: x["field2"] <= 5', 'to': 'END'}
                ]
            },
            {'from': 'node2', 'to': 'END'}
        ]
    }

def test_build_graph(sample_node_config):
    with patch('astonish.core.graph_builder.StateGraph') as mock_state_graph, \
         patch('astonish.core.graph_builder.create_node_function') as mock_create_node_function:
        mock_compiled_graph = MagicMock()
        mock_state_graph.return_value.compile.return_value = mock_compiled_graph
        
        result = build_graph(sample_node_config, None, None, include_error_handler=False)
        
        assert result == mock_compiled_graph
        mock_state_graph.assert_called_once()
        assert mock_state_graph.return_value.add_node.call_count == 2
        mock_state_graph.return_value.set_entry_point.assert_called_with('node1')
        mock_state_graph.return_value.add_conditional_edges.assert_called()
        mock_state_graph.return_value.compile.assert_called_once()

def test_safe_eval_condition():
    state = {'field1': 'test', 'field2': 10}
    node_config = {}
    
    assert safe_eval_condition('lambda x, config: x["field2"] > 5', state, node_config) == True
    assert safe_eval_condition('lambda x, config: x["field2"] < 5', state, node_config) == False
    assert safe_eval_condition('lambda x, config: x["field1"] == "test"', state, node_config) == True
    assert safe_eval_condition('lambda x, config: invalid_syntax', state, node_config) == False

def test_create_condition_function():
    node_config = {}
    condition_func = create_condition_function('lambda x, config: x["value"] > 10', node_config)
    
    assert condition_func({'value': 15}) == True
    assert condition_func({'value': 5}) == False

def test_create_combined_condition_function():
    conditions = {
        'state1': lambda x: isinstance(x['value'], (int, float)) and x['value'] < 0,
        'state2': lambda x: isinstance(x['value'], (int, float)) and 0 <= x['value'] <= 10,
        'state3': lambda x: isinstance(x['value'], (int, float)) and x['value'] > 10
    }
    default_state = 'default'
    
    combined_func = create_combined_condition_function(conditions, default_state)
    
    assert combined_func({'value': -5}) == 'state1'
    assert combined_func({'value': 5}) == 'state2'
    assert combined_func({'value': 15}) == 'state3'
    assert combined_func({'value': 'invalid'}) == 'default'

@patch('astonish.core.graph_builder.load_agents')
@patch('astonish.core.graph_builder.build_graph')
def test_print_flow(mock_build_graph, mock_load_agents):
    mock_graph = MagicMock()
    mock_build_graph.return_value = mock_graph
    mock_load_agents.return_value = {'nodes': [], 'flow': []}
    
    print_flow('test_agent')
    
    mock_load_agents.assert_called_once_with('test_agent')
    mock_build_graph.assert_called_once()
    mock_graph.get_graph.assert_called_once()
    mock_graph.get_graph.return_value.print_ascii.assert_called_once()

if __name__ == '__main__':
    pytest.main()
