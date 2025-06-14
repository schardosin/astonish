"""
State management utilities for Astonish.
This module contains functions for managing state in the agentic flow.
"""
from typing import Dict, Any, Union
from pydantic import BaseModel
import astonish.globals as globals

def update_state(state: Dict[str, Any], output: Union[BaseModel, str, Dict, None], node_config: Dict[str, Any]) -> Dict[str, Any]:
    """
    Update the state with the output from a node execution.
    
    Args:
        state: The current state dictionary
        output: The output from the node execution
        node_config: The node configuration dictionary
        
    Returns:
        The updated state dictionary
    """

    if state.get('_error') is not None:
        return state
    
    new_state = state.copy()
    output_field_name = next(iter(node_config.get('output_model', {})), 'agent_final_answer')
    
    if isinstance(output, BaseModel):
        new_state.update(output.model_dump(exclude_unset=True))
    elif isinstance(output, dict):
        if "_error" in output:
            new_state['_error'] = output["_error"]
            
            if output["_error"] and not output["_error"].get('recoverable', True):
                return {
                    '_error': output["_error"],
                    '_end': False
                }

        if "output" in output:
            new_state[output_field_name] = output.get("output", "")
    elif isinstance(output, (str, int, float, bool)):
        new_state[output_field_name] = output
    elif output is None:
        globals.logger.warning("Received None output for state update.")
    else:
        globals.logger.warning(f"Received unhandled output type: {type(output)}. State will not be updated with this output.")
    
    limit_counter_field = node_config.get('limit_counter_field')
    limit = node_config.get('limit')
    if limit_counter_field and limit and '_error' not in new_state:
        counter = new_state.get(limit_counter_field, 0) + 1
        if counter > limit:
            counter = 1
        new_state[limit_counter_field] = counter
    
    return new_state
