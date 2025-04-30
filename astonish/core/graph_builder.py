from typing import TypedDict, Any
from langgraph.graph import StateGraph, END
from astonish.core.node_functions import create_node_function
import astonish.globals as globals
from astonish.core.utils import load_agents
from astonish.core.error_handler import create_error_handler_node, is_error_state

def build_graph(node_config, mcp_client, checkpointer, include_error_handler=True):
    # Create a dictionary to store all unique fields from output_models
    all_fields = {}
    for node in node_config['nodes']:
        if 'output_model' in node:
            all_fields.update(node['output_model'])
        if 'limit_counter_field' in node:
            all_fields[node['limit_counter_field']] = 'int'
    
    # Add error tracking fields
    all_fields['_error'] = 'dict'
    all_fields['_end'] = 'bool'
    
    # Ensure all fields have valid type strings
    for field_name, type_str in list(all_fields.items()):
        if not isinstance(type_str, str):
            globals.logger.warning(f"Field '{field_name}' has non-string type: {type_str}. Converting to 'Any'")
            all_fields[field_name] = 'Any'

    # Create TypedDict with all unique fields
    typed_dict_fields = {}
    for name, type_str in all_fields.items():
        try:
            typed_dict_fields[name] = eval(type_str)
        except (NameError, SyntaxError) as e:
            globals.logger.warning(f"Error evaluating type '{type_str}' for field '{name}': {e}. Using 'Any' instead.")
            typed_dict_fields[name] = Any
    
    builder = StateGraph(TypedDict('AgentState', typed_dict_fields))

    # Add a special error handling node only if include_error_handler is True
    if include_error_handler:
        builder.add_node("_error_handler", create_error_handler_node())
    
    # Add regular nodes
    for node in node_config['nodes']:
        builder.add_node(node['name'], create_node_function(node, mcp_client))

    # Add edges from the flow configuration
    for edge in node_config['flow']:
        if edge['from'] == 'START':
            builder.set_entry_point(edge['to'])
        elif 'edges' in edge:
            conditions = {}
            for sub_edge in edge['edges']:
                try:
                    source_node_config = next((node for node in node_config['nodes'] if node['name'] == edge['from']), None)
                    if source_node_config is None:
                        globals.logger.error(f"Could not find source node '{edge['from']}' for condition")
                        source_node_config = {}
                    condition = create_condition_function(sub_edge['condition'], source_node_config)
                except Exception as e:
                    globals.logger.error(f"Error creating condition function: {e}")
                    # Use a default condition that always returns False
                    condition = lambda state: False
                conditions[END if sub_edge['to'] == 'END' else sub_edge['to']] = condition

            default_state = END
            combined_condition_function = create_combined_condition_function(conditions, default_state)
            all_possible_transitions = {state: state for state in conditions.keys()}
            all_possible_transitions[default_state] = default_state

            builder.add_conditional_edges(edge['from'], combined_condition_function, all_possible_transitions)
        else:
            builder.add_edge(edge['from'], END if edge['to'] == 'END' else edge['to'])
    
    # Add conditional edges to check for error state after each node
    # These edges need to be added AFTER the regular edges to ensure they take precedence
    if include_error_handler:
        for node in node_config['nodes']:
            node_name = node['name']
            # Add a conditional edge that checks for errors
            # Use a path_map to explicitly map the return value of is_error_state to the next node
            # This ensures that the error edge takes precedence over regular edges
            builder.add_conditional_edges(
                node_name,
                is_error_state,  # Function from error_handler.py that returns the next node name
                {"_error_handler": "_error_handler", END: END}  # Explicitly map the return values
            )

    return builder.compile(checkpointer=checkpointer)

def safe_eval_condition(condition: str, state: dict, node_config: dict) -> bool:
    """
    Safely evaluates a condition string and returns the result.
    
    Args:
        condition: The condition string to evaluate (e.g., "lambda x, config: x['counter'] > 5")
        state: The current state dictionary
        node_config: The node configuration dictionary
        
    Returns:
        The result of the condition evaluation, or False if an error occurs
    """
    try:
        if not condition or not isinstance(condition, str):
            globals.logger.error(f"Invalid condition: {condition}")
            return False
            
        parts = condition.split(":", 1)
        if len(parts) != 2:
            globals.logger.error(f"Malformed condition (missing colon): {condition}")
            return False
            
        lambda_body = parts[1].strip()
        func = eval(f"lambda x, config: {lambda_body}")
        
        if not callable(func):
            globals.logger.error(f"Evaluated condition is not callable: {func}")
            return False
            
        result = func(state, node_config)
        return bool(result)  # Ensure boolean result
        
    except Exception as e:
        globals.logger.error(f"Error evaluating condition '{condition}': {e}")
        return False

def create_condition_function(condition: str, node_config: dict):
    return lambda state: safe_eval_condition(condition, state, node_config)

def create_combined_condition_function(conditions, default_state):
    def combined_condition_function(*args, **kwargs):
        for state, condition in conditions.items():
            if condition(*args, **kwargs):
                return state
        return default_state
    return combined_condition_function

async def run_graph(graph, initial_state, thread):
    """
    Run the graph with the given initial state and thread configuration.
    
    Args:
        graph: The compiled graph to run
        initial_state: The initial state dictionary
        thread: The thread configuration
        
    Returns:
        The final state after running the graph
    """
    try:
        # Ensure initial state has required error tracking fields
        if '_error' not in initial_state:
            initial_state['_error'] = None
        if '_end' not in initial_state:
            initial_state['_end'] = False
            
        # Run the graph
        try:
            final_state = await graph.ainvoke(initial_state, thread)
            
            # Check if we terminated due to an error
            if '_error' in final_state and final_state['_error'] is not None:
                error_info = final_state.get('_error')
                if isinstance(error_info, dict) and not error_info.get('recoverable', True):
                    # We already displayed the error message in the error handler node
                    return final_state
            
            return final_state
        except Exception as e:
            if "Can receive only one value per step" in str(e) or "INVALID_CONCURRENT_GRAPH_UPDATE" in str(e):
                # This is likely due to a state conflict when error handling is triggered
                globals.logger.error(f"Graph execution state conflict: {e}")
                
                # Create a clean error state without the conflicting keys
                error_state = {
                    '_error': {
                        'message': str(e),
                        'type': type(e).__name__,
                        'recoverable': False,
                        'node': 'graph_execution',
                        'user_message': "I apologize, but I encountered an error while processing your request."
                    },
                    '_end': True
                }
                
                return error_state
            else:
                # Other unexpected errors
                raise e
    except Exception as e:
        globals.logger.error(f"Unexpected error during graph execution: {e}")
        
        # Create a proper error state
        error_state = {
            '_error': {
                'message': str(e),
                'type': type(e).__name__,
                'recoverable': False,
                'node': 'graph_execution',
                'user_message': f"I apologize, but an unexpected error occurred while processing your request."
            },
            '_end': True
        }
        
        return error_state

def print_flow(agent):
    config = load_agents(agent)
    # Pass include_error_handler=False for visualization
    graph = build_graph(config, None, None, include_error_handler=False)
    graph_obj = graph.get_graph()
    graph_obj.print_ascii()
