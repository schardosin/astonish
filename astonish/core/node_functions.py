"""
Node functions module for Astonish.
This module contains functions for creating and managing node functions.
"""
import json
import asyncio
import traceback
import inquirer
import astonish.globals as globals
from typing import Dict, Any, Union, Optional, List
from langchain_core.messages import SystemMessage, HumanMessage, BaseMessage
from langchain_core.prompts import ChatPromptTemplate
from langchain.output_parsers import PydanticOutputParser
from langchain.schema import OutputParserException
from pydantic import ValidationError, BaseModel
from astonish.tools.internal_tools import tools as internal_tools_list
from astonish.core.llm_manager import LLMManager
from astonish.core.utils import format_prompt, print_ai, print_output, console, remove_think_tags
from astonish.core.error_handler import create_error_feedback, handle_node_failure
from astonish.core.format_handler import execute_tool
from astonish.core.utils import request_tool_execution
from astonish.core.json_utils import clean_and_fix_json
from astonish.core.react_planning import ToolDefinition, run_react_planning_step, format_react_step_for_scratchpad
from astonish.core.output_model_utils import create_output_model, _format_final_output
from astonish.core.state_management import update_state
from astonish.core.ui_utils import print_user_messages, print_chat_prompt, print_state
from astonish.core.utils import try_extract_stdout_from_string
from rich.prompt import Prompt

def create_node_function(node_config, mcp_client):
    """Creates the appropriate node function based on node_config type."""
    node_type = node_config.get('type')
    if node_type == 'input':
        return create_input_node_function(node_config)
    elif node_type == 'output':
        return create_output_node_function(node_config)
    elif node_type == 'llm':
        use_tools_flag = bool(node_config.get('tools', False))
        return create_llm_node_function(node_config, mcp_client, use_tools=use_tools_flag)
    elif node_type == 'tool':
        return create_tool_node_function(node_config, mcp_client)
    elif node_type == 'update_state':
        return create_update_state_node_function(node_config)
    else:
        raise ValueError(f"Unsupported node type: {node_type}")

def create_output_node_function(node_config: Dict[str, Any]):
    """Creates an output node function that formats and prints data from the state."""

    def node_function(state: dict) -> dict:
        if state.get('_error') is not None:
            return state # Propagate errors

        node_name = node_config.get('name', 'Unnamed Output Node')
        globals.logger.info(f"[{node_name}] Processing output node.")
        print_output(f"\n--- Node {node_name} ---")

        prompt_template = node_config.get('prompt')
        if not prompt_template:
            globals.logger.warning(f"[{node_name}] No 'prompt' template provided in configuration. Node will display nothing specific.")
            new_state = state.copy()
            print_user_messages(new_state, node_config)
            print_state(new_state, node_config)
            globals.logger.info(f"[{node_name}] Output node processing complete (no prompt template).")
            return new_state

        try:
            formatted_output_message = format_prompt(prompt_template, state, node_config)
            print_ai(formatted_output_message)
            #console.print(formatted_output_message, style="green")

        except Exception as e:
            globals.logger.error(f"[{node_name}] Error formatting output message: {e}\n{traceback.format_exc()}")
            error_state = state.copy()
            return handle_node_failure(error_state, node_name, e, user_message=f"I encountered an error while trying to display information for the '{node_name}' step.")

        new_state = state.copy()

        print_user_messages(new_state, node_config)
        print_state(new_state, node_config)
        globals.logger.info(f"[{node_name}] Output node processing complete.")
        return new_state

    return node_function


def create_update_state_node_function(node_config: Dict[str, Any]):
    """
    Creates an update_state node function for direct state manipulation.
    The target variable is specified as the key in the 'output_model'.
    """

    def node_function(state: dict) -> dict:
        if state.get('_error') is not None:
            return state

        node_name = node_config.get('name', 'Unnamed UpdateState Node')
        print_output(f"\n--- Node {node_name} ---")
        globals.logger.info(f"[{node_name}] Starting state update operation.")

        new_state = state.copy()

        output_model_config = node_config.get('output_model')
        action = node_config.get('action')
        source_variable = node_config.get('source_variable')
        value_provided = 'value' in node_config
        literal_value = node_config.get('value') if value_provided else None

        # Validate output_model and extract target_variable_name
        if not isinstance(output_model_config, dict) or len(output_model_config) != 1:
            return handle_node_failure(
                new_state, node_name,
                ValueError("'output_model' must be a dictionary defining exactly one target variable for update_state node.")
            )
        target_variable_name = next(iter(output_model_config.keys()))
        if not action:
            return handle_node_failure(new_state, node_name, ValueError("'action' is required for update_state node (e.g., 'overwrite', 'append')."))

        print_output(f"[ℹ️ Info] Setting value for '{target_variable_name}' to '{literal_value}'", color="yellow")

        try:
            if action == 'overwrite':
                if source_variable is not None:
                    if source_variable in new_state:
                        new_state[target_variable_name] = try_extract_stdout_from_string(new_state[source_variable])
                        globals.logger.info(f"[{node_name}] Overwrote '{target_variable_name}' with value from '{source_variable}'.")
                    else:
                        raise KeyError(f"Source variable '{source_variable}' not found in state.")
                elif value_provided:
                    new_state[target_variable_name] = literal_value
                    globals.logger.info(f"[{node_name}] Overwrote '{target_variable_name}' with literal value: {literal_value}.")
                else:
                    raise ValueError("For 'overwrite' action, either 'source_variable' or 'value' must be provided.")

            elif action == 'append':
                item_to_append = None
                source_for_append_defined = False

                if source_variable is not None:
                    if source_variable in new_state:
                        if source_variable in new_state:
                            value = new_state[source_variable]
                            item_to_append = None
                            source_for_append_defined = False

                            # Check if the value is a string
                            if isinstance(value, str):
                                item_to_append = try_extract_stdout_from_string(value)
                                source_for_append_defined = True
                                globals.logger.info(f"[{node_name}] Preparing to append extracted string value from '{source_variable}' to '{target_variable_name}'.")
                            # Check if the value is a list
                            elif isinstance(value, list):
                                item_to_append = value  # Use the list directly
                                source_for_append_defined = True
                                globals.logger.info(f"[{node_name}] Preparing to append list from '{source_variable}' to '{target_variable_name}'.")
                            # Optional: Handle cases where it's neither str nor list, if necessary
                            else:
                                globals.logger.warning(f"[{node_name}] Value from '{source_variable}' is neither a string nor a list. Cannot determine item to append.")
                    else:
                        raise KeyError(f"Source variable '{source_variable}' not found in state for append action.")
                elif value_provided:
                    item_to_append = literal_value
                    source_for_append_defined = True
                    globals.logger.info(f"[{node_name}] Preparing to append literal value to '{target_variable_name}'. Value: {literal_value}")
                
                if not source_for_append_defined:
                    raise ValueError("For 'append' action, either 'source_variable' or 'value' must be provided.")

                if target_variable_name not in new_state or new_state[target_variable_name] is None:
                    if target_variable_name in new_state and new_state[target_variable_name] is None:
                        globals.logger.info(f"[{node_name}] Target variable '{target_variable_name}' was None. Resetting to an empty list for append.")
                    else:
                        globals.logger.info(f"[{node_name}] Target variable '{target_variable_name}' not found. Initializing as an empty list for append.")
                    new_state[target_variable_name] = []
                elif not isinstance(new_state[target_variable_name], list):
                    raise TypeError(f"Target variable '{target_variable_name}' must be a list to append to, but found type: {type(new_state[target_variable_name])}.")
                                
                if item_to_append not in ("", None):
                    new_state[target_variable_name].append(item_to_append)
                    globals.logger.info(f"[{node_name}] Appended item to '{target_variable_name}'. New list size: {len(new_state[target_variable_name])}.")
                else:
                    globals.logger.info(f"[{node_name}] Skipped appending empty or None item to '{target_variable_name}'.")

            else:
                raise ValueError(f"Unsupported action '{action}' for update_state node. Must be 'overwrite' or 'append'.")

        except (ValueError, KeyError, TypeError) as e:
            return handle_node_failure(new_state, node_name, e)
        except Exception as e:
            return handle_node_failure(new_state, node_name, e)

        print_user_messages(new_state, node_config)
        print_state(new_state, node_config)
        globals.logger.info(f"[{node_name}] State update operation completed successfully.")
        return new_state

    return node_function

def create_input_node_function(node_config):
    """Creates input node function."""
    def node_function(state: dict):
        if state.get('_error') is not None:
            return state
        
        node_name = node_config.get('name')
        output_field = next(iter(node_config.get('output_model', {'user_input': 'str'})), 'user_input')
        
        parameters = state.get('_parameters', {})
        globals.logger.info(f"Node '{node_name}' checking for parameters. Available parameters: {parameters}")
        
        if node_name in parameters:            
            formatted_prompt = format_prompt(node_config['prompt'], state, node_config)
            print_ai(formatted_prompt)

            param_value = parameters[node_name]
            console.print(f"Using provided parameter for node '{node_name}': {param_value}", style="cyan")
            globals.logger.info(f"Using parameter for node '{node_name}': {param_value}")
            
            new_state = state.copy()
            new_state[output_field] = param_value
            print_user_messages(new_state, node_config)
            print_state(new_state, node_config)
            return new_state
        
        formatted_prompt = format_prompt(node_config['prompt'], state, node_config)
        print_ai(formatted_prompt)

        options = node_config.get('options')
        if options:
            expanded_options = []
            for option in options:
                if option in state and isinstance(state[option], list):
                    expanded_options.extend(state[option])
                else:
                    expanded_options.append(option)

            questions = [
                inquirer.List(
                    'user_choice',
                    message=f"Choose an option",
                    choices=expanded_options,
                ),
            ]
            answers = inquirer.prompt(questions)
            user_input = answers['user_choice']
        else:
            user_input = Prompt.ask("[yellow]You[/yellow]")

        new_state = state.copy()
        new_state[output_field] = user_input
        print_user_messages(new_state, node_config)
        print_state(new_state, node_config)
        return new_state

    return node_function

def create_tool_node_function(node_config: Dict[str, Any], mcp_client: Any):
    """Creates a tool node function that directly executes a tool without LLM involvement."""
    
    async def node_function(state: dict) -> dict:
        if state.get('_error') is not None:
            return state
        
        node_name = node_config.get('name', 'Unnamed Tool Node')
        print_output(f"\n--- Node {node_name} ---")
        
        # Get tool name from tools_selection if available, otherwise use the node name
        tools_selection = node_config.get('tools_selection')
        if tools_selection and isinstance(tools_selection, list) and len(tools_selection) > 0:
            tool_name = tools_selection[0]  # Use the first tool in the list
            globals.logger.info(f"[{node_name}] Using tool '{tool_name}' from tools_selection")
        else:
            tool_name = node_name  # Default to node name if tools_selection is not specified
            globals.logger.info(f"[{node_name}] No tools_selection specified, using node name as tool name")
        
        # Extract arguments from state based on node config
        args_config = node_config.get('args', {})
        tool_args = {}
        
        for arg_name, arg_value in args_config.items():
            # Check if the arg_value is a template that needs to be formatted
            if isinstance(arg_value, str) and arg_value.startswith('{') and arg_value.endswith('}'):
                # Extract the state key from the template
                state_key = arg_value[1:-1]  # Remove the curly braces
                if state_key in state:
                    tool_args[arg_name] = state[state_key]
                    globals.logger.info(f"[{node_name}] Injected state value '{state_key}' into argument '{arg_name}'")
                else:
                    console.print(f"Warning: State key '{state_key}' not found for argument '{arg_name}' in tool node '{node_name}'", style="yellow")
                    globals.logger.warning(f"State key '{state_key}' not found for argument '{arg_name}' in tool node '{node_name}'")
            # Check if the arg_value is a direct reference to a state key (YAML object)
            elif isinstance(arg_value, dict) and len(arg_value) == 1:
                # In YAML, {pr_diff} becomes {'pr_diff': None}
                state_key = next(iter(arg_value))
                if state_key in state:
                    tool_args[arg_name] = state[state_key]
                    globals.logger.info(f"[{node_name}] Injected state value '{state_key}' into argument '{arg_name}'")
                else:
                    console.print(f"Warning: State key '{state_key}' not found for argument '{arg_name}' in tool node '{node_name}'", style="yellow")
                    globals.logger.warning(f"State key '{state_key}' not found for argument '{arg_name}' in tool node '{node_name}'")
            # Check if the argument name exists directly in the state
            elif arg_name in state:
                tool_args[arg_name] = state[arg_name]
            # Check if the arg_value is a string that might contain templates
            elif isinstance(arg_value, str) and '{' in arg_value and '}' in arg_value:
                formatted_value = format_prompt(arg_value, state, node_config)
                tool_args[arg_name] = formatted_value
                globals.logger.info(f"[{node_name}] Formatted string value for argument '{arg_name}'. Original: '{arg_value}', Formatted: '{formatted_value}'")
            else:
                tool_args[arg_name] = arg_value
                globals.logger.info(f"[{node_name}] Used literal value for argument '{arg_name}'. Value: '{arg_value}'")

            
        # Get all available tools
        all_tools = []
        if mcp_client:
            try:
                external_tools = []
                async with mcp_client as active_session:
                    external_tools = active_session.get_tools() or []
                all_tools.extend(external_tools)
                globals.logger.info(f"[{node_name}] Fetched {len(external_tools)} external tools via MCP.")
            except Exception as e:
                console.print(f"Error fetching MCP tools: {e}", style="red")
                globals.logger.error(f"MCP client error during get_tools: {e}")
        
        if isinstance(internal_tools_list, list):
            all_tools.extend(internal_tools_list)
            globals.logger.info(f"[{node_name}] Added {len(internal_tools_list)} internal tools.")
        
        # Find the tool in the registry
        tool_registry = {}
        for tool_obj in all_tools:
            try:
                tool_name_attr = getattr(tool_obj, 'name', None)
                if not tool_name_attr or not isinstance(tool_name_attr, str):
                    continue
                
                input_schema = getattr(tool_obj, 'args_schema', None)
                input_type = getattr(tool_obj, 'input_type', 'JSON_SCHEMA' if input_schema else 'STRING')
                raw_executor = getattr(tool_obj, 'arun', getattr(tool_obj, '_arun', getattr(tool_obj, 'run', getattr(tool_obj, '_run', None))))
                
                if not callable(raw_executor):
                    continue
                
                executor = raw_executor
                if not asyncio.iscoroutinefunction(raw_executor):
                    def wrapped_sync_executor_factory(sync_func):
                        async def wrapped_executor(*args, **kwargs): 
                            return await asyncio.to_thread(sync_func, *args, **kwargs)
                        return wrapped_executor
                    executor = wrapped_sync_executor_factory(raw_executor)
                
                tool_registry[tool_name_attr] = {
                    "name": tool_name_attr,
                    "description": getattr(tool_obj, 'description', 'No description available.'),
                    "input_type": input_type,
                    "input_schema_definition": input_schema,
                    "tool_executor": executor,
                    "tool_instance": tool_obj
                }
            except Exception as e:
                console.print(f"[{node_name}] Error processing tool definition for {getattr(tool_obj, 'name', 'unknown')}: {e}", style="red")
                globals.logger.error(f"Tool processing error: {e}")
        
        if tool_name not in tool_registry:
            error_message = f"Tool '{tool_name}' not found in tool registry"
            console.print(f"[{node_name}] {error_message}", style="red")
            new_state = state.copy()
            new_state['_error'] = {
                'node': node_name,
                'message': error_message,
                'type': 'ToolNotFoundError',
                'user_message': f"I couldn't find the tool '{tool_name}' needed for this operation.",
                'recoverable': False
            }
            return new_state
        
        # Execute the tool
        try:
            # Convert tool_args to the format expected by the tool
            tool_def = tool_registry[tool_name]
            input_type = tool_def['input_type']
            input_string = ""
            
            if input_type == 'JSON_SCHEMA':
                input_string = json.dumps(tool_args)
            else:
                # For STRING input type, just use the first argument value if available
                if tool_args:
                    input_string = str(next(iter(tool_args.values())))
            
            # Request tool execution approval
            approve = False
            tool_args_for_approval = tool_args
            try:
                tool_call_info_for_approval = {
                    "name": tool_name,
                    "args": tool_args_for_approval,
                    "auto_approve": node_config.get('tools_auto_approval', False)
                }
                approve = await asyncio.to_thread(request_tool_execution, tool_call_info_for_approval)
            except Exception as approval_err:
                console.print(f"Error during tool approval process: {approval_err}", style="red")
                globals.logger.error(f"Tool approval error: {traceback.format_exc()}")
                new_state = state.copy()
                new_state['_error'] = {
                    'node': node_name,
                    'message': f"Error during approval step: {approval_err}",
                    'type': 'ToolApprovalError',
                    'user_message': f"I encountered an error while requesting approval for the tool: {approval_err}",
                    'recoverable': False
                }
                return new_state
            
            if not approve:
                console.print(f"[{node_name}] Tool execution denied by user.", style="yellow")
                globals.logger.info(f"[{node_name}] Tool execution denied by user.")
                new_state = state.copy()
                new_state['_error'] = {
                    'node': node_name,
                    'message': f"User denied execution of tool '{tool_name}'.",
                    'type': 'ToolExecutionDenied',
                    'user_message': f"You denied the execution of tool '{tool_name}'.",
                    'recoverable': False
                }
                return new_state
            
            # Execute the tool
            execution_result = await execute_tool(
                node_name=node_name,
                tool_name=tool_name,
                input_string=input_string,
                tool_registry=tool_registry
            )
            
            # Update state with tool output
            new_state = state.copy()
            
            if isinstance(execution_result, dict):
                if "_error" in execution_result:
                    error_message = execution_result["_error"].get('user_message', execution_result["_error"].get('message', "Tool execution failed."))
                    console.print(f"[{node_name}] Tool execution failed: {error_message}", style="red")
                    new_state['_error'] = {
                        'node': node_name,
                        'message': error_message,
                        'type': 'ToolExecutionError',
                        'user_message': f"I encountered an error while executing the tool: {error_message}",
                        'recoverable': False
                    }
                    return new_state

                tool_raw_output = execution_result.get("output")
                # Initialize processed_output_for_state with the raw output.
                # This will be used if no specific parsing/formatting is applied or if it fails.
                processed_output_for_state = tool_raw_output

                output_model_config = node_config.get('output_model', {})

                if output_model_config and tool_raw_output is not None:
                    NodeOutputModel = create_output_model(output_model_config)
                    if NodeOutputModel:
                        node_parser = PydanticOutputParser(pydantic_object=NodeOutputModel) # Create parser for the model

                        if isinstance(tool_raw_output, str):
                            globals.logger.info(f"[{node_name}] Tool output is a string. Attempting formatting via _format_final_output.")
                            try:
                                formatted_value = await _format_final_output(
                                    final_text=tool_raw_output,
                                    parser=node_parser, # parser for the tool node's output model
                                    node_name=node_name
                                )
                                processed_output_for_state = formatted_value
                                if processed_output_for_state is not tool_raw_output:
                                    globals.logger.info(f"[{node_name}] Tool output string successfully processed by _format_final_output.")
                                else:
                                    globals.logger.info(f"[{node_name}] _format_final_output returned original text. This might be expected if model is not single-field or input was unsuitable.")
                                    if processed_output_for_state == tool_raw_output and not (hasattr(NodeOutputModel, 'model_fields') and len(NodeOutputModel.model_fields) == 1):
                                        globals.logger.info(f"[{node_name}] Model is not single-field or _format_final_output did not change text. Attempting general JSON parsing for multi-field model.")
                                        try:
                                            cleaned_json_str = clean_and_fix_json(tool_raw_output)
                                            if cleaned_json_str:
                                                # Use the existing node_parser created for NodeOutputModel
                                                parsed_json_model = node_parser.parse(cleaned_json_str)
                                                processed_output_for_state = parsed_json_model
                                                globals.logger.info(f"[{node_name}] Successfully parsed tool's JSON string output into multi-field model.")
                                        except (json.JSONDecodeError, OutputParserException, ValidationError) as e_parse:
                                            globals.logger.warning(f"[{node_name}] Failed to parse tool's string output as JSON for multi-field model: {e_parse}. Output remains as is.")
                                        except Exception as e_multi_parse:
                                             globals.logger.error(f"[{node_name}] Unexpected error parsing tool's string output for multi-field model: {e_multi_parse}. Output remains as is.")

                            except Exception as e_format:
                                globals.logger.error(f"[{node_name}] Error calling _format_final_output for tool output: {e_format}. Using raw output as fallback.")
                                # processed_output_for_state remains tool_raw_output

                        elif isinstance(tool_raw_output, (dict, list)):
                            # If output is already dict/list, validate/convert with Pydantic model
                            globals.logger.info(f"[{node_name}] Tool output is dict/list. Attempting Pydantic validation.")
                            try:
                                processed_output_for_state = NodeOutputModel.model_validate(tool_raw_output)
                                globals.logger.info(f"[{node_name}] Successfully validated/converted tool output dict/list with Pydantic model.")
                            except ValidationError as e_validate:
                                globals.logger.warning(f"[{node_name}] Failed to validate tool output dict/list with Pydantic model: {e_validate}. Using raw output as fallback.")
                            except Exception as e_model_validate:
                                globals.logger.error(f"[{node_name}] Unexpected error validating tool output dict/list: {e_model_validate}. Using raw output as fallback.")
                        # Add other type checks for tool_raw_output if necessary

                # Use the processed_output_for_state (which could be a Pydantic model, a formatted value, or the raw output)
                # to update the state.
                new_state = update_state(new_state, processed_output_for_state, node_config)
                # Log based on whether processing changed the output.
                if processed_output_for_state is not tool_raw_output:
                    globals.logger.info(f"[{node_name}] Stored processed/formatted tool output in state.")
                elif tool_raw_output is not None: # Only log if there was some output initially
                    globals.logger.info(f"[{node_name}] Stored raw tool output in state (no specific formatting applied or formatting failed).")

            else: # execution_result is not a dict
                globals.logger.warning(f"[{node_name}] Tool execution result was not a dictionary: {execution_result}. Attempting to update state directly.")
                # If there's an output model, we could still try to format/validate if execution_result is a string/dict/list
                processed_output_for_state = execution_result
                output_model_config = node_config.get('output_model', {})
                if output_model_config and isinstance(execution_result, str):
                    NodeOutputModel = create_output_model(output_model_config)
                    if NodeOutputModel:
                        node_parser = PydanticOutputParser(pydantic_object=NodeOutputModel)
                        try:
                            globals.logger.info(f"[{node_name}] Tool output (non-dict) is a string. Attempting formatting via _format_final_output.")
                            formatted_value = await _format_final_output(
                                final_text=execution_result, parser=node_parser, node_name=node_name
                            )
                            processed_output_for_state = formatted_value
                        except Exception as e_format_alt:
                             globals.logger.error(f"[{node_name}] Error calling _format_final_output for non-dict tool output: {e_format_alt}.")
                elif output_model_config and isinstance(execution_result, (dict,list)):
                     NodeOutputModel = create_output_model(output_model_config)
                     if NodeOutputModel:
                        try:
                            processed_output_for_state = NodeOutputModel.model_validate(execution_result)
                        except ValidationError as e_val_alt:
                            globals.logger.warning(f"[{node_name}] Failed to validate non-dict tool output with model: {e_val_alt}.")

                new_state = update_state(new_state, processed_output_for_state, node_config)


            print_user_messages(new_state, node_config)
            print_state(new_state, node_config)
            return new_state
            
        except Exception as e:
            error_message = f"Error executing tool '{tool_name}': {e}"
            console.print(f"[{node_name}] {error_message}", style="red")
            globals.logger.error(f"Tool execution error: {traceback.format_exc()}")
            new_state = state.copy()
            new_state['_error'] = {
                'node': node_name,
                'message': error_message,
                'type': 'ToolExecutionError',
                'user_message': f"I encountered an error while executing the tool: {e}",
                'recoverable': False
            }
            return new_state
    
    return node_function

def create_llm_node_function(node_config: Dict[str, Any], mcp_client: Any, use_tools: bool):
    output_model_config = node_config.get('output_model', {})
    OutputModel = create_output_model(output_model_config)
    parser = PydanticOutputParser(pydantic_object=OutputModel) if OutputModel else None

    async def node_function(state: dict) -> dict:
        node_name = node_config.get('name', 'Unnamed LLM Node')
        print_output(f"\n--- Node {node_name} ---")
        print_output(f"[ℹ️ Info] Starting LLM node processing for {node_name}", color="yellow")
        try:
            system_message_content = format_prompt(node_config.get('system', ''), state, node_config)
            human_message_content = format_prompt(node_config['prompt'], state, node_config)
            llm = LLMManager.get_llm()
        except Exception as e:
            console.print(f"Error preparing node {node_name}: {e}", style="red")
            error_state = handle_node_failure(state.copy(), node_name, e, 0)
            return error_state

        final_output_for_state: Union[BaseModel, str, Dict, None] = None
        new_state = state.copy()

        async def _perform_react_logic_with_optional_mcp_session(
            active_mcp_session: Optional[Any]
        ) -> Union[BaseModel, str, Dict, None]:
            _react_final_output: Union[BaseModel, str, Dict, None] = None

            agent_scratchpad: str = ""
            tool_registry: Dict[str, ToolDefinition] = {}
            filtered_tool_defs: List[ToolDefinition] = []
            max_iterations = node_config.get('max_react_iterations', 5)
            final_answer_found = False
            last_react_error = None

            # --- Fetch Tools ---
            print_output(f"[ℹ️ Info] Fetching and preparing tools...", color="yellow")
            all_fetched_tools: List[Any] = []
            if active_mcp_session:
                 globals.logger.info(f"[{node_name}] Fetching external tools via active MCP session...")
                 try:
                      external_tools_data = active_mcp_session.get_tools() or []
                      if isinstance(external_tools_data, list):
                          all_fetched_tools.extend(external_tools_data)
                          globals.logger.info(f"[{node_name}] Fetched {len(external_tools_data)} external tools via MCP.")
                      else:
                          console.print(f"[{node_name}] Warning: MCP client.get_tools() did not return a list.", style="yellow")
                 except Exception as e:
                     console.print(f"[{node_name}] Warning: MCP client error during get_tools: {e}", style="red")
                     globals.logger.error(f"MCP client Traceback during get_tools:\n{traceback.format_exc()}")

            if isinstance(internal_tools_list, list):
                all_fetched_tools.extend(internal_tools_list)
                globals.logger.info(f"[{node_name}] Added {len(internal_tools_list)} internal tools.")

            # --- Process Tools ---
            tool_selection = node_config.get('tools_selection')
            processed_tool_names = set()
            for tool_obj in all_fetched_tools:
                 try:
                      tool_name_attr = getattr(tool_obj, 'name', None)
                      if not tool_name_attr or not isinstance(tool_name_attr, str) or tool_name_attr in processed_tool_names: continue
                      if tool_selection and isinstance(tool_selection, list) and tool_name_attr not in tool_selection: continue
                      input_schema = getattr(tool_obj, 'args_schema', None)
                      input_type = getattr(tool_obj, 'input_type', 'JSON_SCHEMA' if input_schema else 'STRING')
                      raw_executor = getattr(tool_obj, 'arun', getattr(tool_obj, '_arun', getattr(tool_obj, 'run', getattr(tool_obj, '_run', None))))
                      if not callable(raw_executor): continue
                      executor = raw_executor
                      if not asyncio.iscoroutinefunction(raw_executor):
                           def wrapped_sync_executor_factory(sync_func):
                               async def wrapped_executor(*args, **kwargs): 
                                   return await asyncio.to_thread(sync_func, *args, **kwargs)
                               return wrapped_executor
                           executor = wrapped_sync_executor_factory(raw_executor)
                      tool_def: ToolDefinition = {"name": tool_name_attr, "description": getattr(tool_obj, 'description', 'No description available.'), "input_type": input_type, "input_schema_definition": input_schema, "tool_executor": executor, "tool_instance": tool_obj}
                      filtered_tool_defs.append(tool_def)
                      tool_registry[tool_name_attr] = tool_def
                      processed_tool_names.add(tool_name_attr)
                 except Exception as e: 
                     console.print(f"[{node_name}] Error processing tool definition for {getattr(tool_obj, 'name', 'unknown')}: {e}", style="red")
                 globals.logger.error(f"Tool processing Traceback:\n{traceback.format_exc()}")
            if not filtered_tool_defs: console.print(f"Warning: No valid tools available for ReAct node {node_name}. Agent may only reason.", style="yellow")

            max_retries = node_config.get('max_retries', 3)
            
            for i in range(max_iterations):
                print_output(f"[🧠 Thinking] Tool reasoning: Iteration {i + 1} of a maximum of {max_iterations}", color="yellow")
                
                # Add retry logic for the ReAct planning step
                retry_count = 0
                react_step_output = None
                current_system_message = system_message_content
                current_human_message = human_message_content
                
                while retry_count < max_retries:
                    try:
                        globals.logger.info(f"[{node_name}] ReAct planning attempt {retry_count + 1}/{max_retries}")
                        react_step_output = await run_react_planning_step(
                            input_question=current_human_message, 
                            agent_scratchpad=agent_scratchpad, 
                            system_message_content=current_system_message, 
                            llm=llm, 
                            tool_definitions=filtered_tool_defs, 
                            node_name=f"{node_name} Planner", 
                            print_prompt=node_config.get('print_prompt', False)
                        )
                        
                        status = react_step_output['status']
                        thought = react_step_output.get('thought')
                        
                        if status != 'error':
                            # If successful, break out of the retry loop
                            break
                        
                        # If we got an error but have retries left
                        retry_count += 1
                        if retry_count < max_retries:
                            error_message = f"ReAct planning step failed. Raw Response: {react_step_output['raw_response']}"
                            console.print(f"[⚠️ Warning] Error (Attempt {retry_count}/{max_retries}): {error_message}", style="yellow")
                            
                            # Create feedback for the LLM
                            feedback = create_error_feedback(error_message, node_name)
                            print_output(f"Providing feedback to LLM: {feedback[:100]}...")
                            
                            # Add feedback to the human message for the next attempt
                            current_human_message = f"{human_message_content}\n\nPrevious attempt failed with error: {error_message}\n\nYou must follow the Thought/Action/Action Input/Observation cycle. Do not skip steps. Only use 'Final Answer:' when the entire task is complete. DO NOT return {error_message} alone again."
                        else:
                            # Max retries reached, log the final error
                            error_message = f"ReAct planning step failed after {max_retries} attempts. Raw Response: {react_step_output['raw_response']}"
                            console.print(f"[{node_name}] Error: {error_message}", style="red")
                            last_react_error = error_message
                    except Exception as e:
                        retry_count += 1
                        error_message = f"Exception during ReAct planning: {type(e).__name__}: {e}"
                        console.print(f"[{node_name}] Exception (Attempt {retry_count}/{max_retries}): {error_message}", style="yellow")
                        
                        if retry_count >= max_retries:
                            console.print(f"[{node_name}] Max retries reached with exceptions", style="red")
                            last_react_error = error_message
                            break
                        
                        # Create feedback for the LLM
                        feedback = create_error_feedback(e, node_name)
                        print_output(f"Providing feedback to LLM: {feedback[:100]}...")
                        
                        # Add feedback to the human message for the next attempt
                        current_human_message = f"{human_message_content}\n\nPrevious attempt failed with error: {error_message}\n\nYou must follow the Thought/Action/Action Input/Observation cycle. Do not skip steps. Only use 'Final Answer:' when the entire task is complete. DO NOT return {error_message} alone again."
                
                # If we've exhausted all retries and still have an error, break out of the main loop
                if status == 'error':
                    break

                if status == 'final_answer': 
                    raw_final_answer_text = react_step_output['answer']
                    final_answer_text = remove_think_tags(raw_final_answer_text)
                    globals.logger.info(f"[{node_name}] ReAct final answer (raw): '{str(raw_final_answer_text)[:100]}...'")
                    globals.logger.info(f"[{node_name}] ReAct final answer (tags removed): '{str(final_answer_text)[:100]}...'")                    
                    print_output(f"[✅ Success] Tool executed successfully.", color="yellow")
                    _react_final_output = {"output": final_answer_text}
                    final_answer_found = True
                    break
                elif status == 'action':
                    tool_name = react_step_output['tool']
                    tool_input_str = react_step_output['tool_input']
                    observation = ""
                    
                    if not tool_name or tool_name not in tool_registry: 
                        console.print(f"[{node_name}] Error: LLM planned to use unknown tool '{tool_name}'.", style="red")
                        observation = f"Error: Tool '{tool_name}' not found or not available."
                        last_react_error = observation
                        agent_scratchpad += format_react_step_for_scratchpad(thought, tool_name, tool_input_str, observation)
                        break
                    tool_definition_for_exec = tool_registry[tool_name]
                    approve = False
                    tool_args_for_approval: Union[Dict, str] = tool_input_str
                    try:
                        if tool_definition_for_exec['input_type'] == 'JSON_SCHEMA':
                             # Clean and fix the JSON before parsing
                             cleaned_input = clean_and_fix_json(tool_input_str)
                             parsed_args = {} if not cleaned_input else json.loads(cleaned_input, strict=False)
                             schema_def = tool_definition_for_exec['input_schema_definition']
                             if isinstance(schema_def, type) and issubclass(schema_def, BaseModel): 
                                validated_args_model = schema_def(**parsed_args)
                                tool_args_for_approval = validated_args_model.model_dump()
                             else: tool_args_for_approval = parsed_args
                        tool_call_info_for_approval = { "name": tool_name, "args": tool_args_for_approval, "auto_approve": node_config.get('tools_auto_approval', False) }
                        approve = await asyncio.to_thread(request_tool_execution, tool_call_info_for_approval)
                    except (json.JSONDecodeError, ValidationError) as val_err: 
                        err_type = type(val_err).__name__
                        console.print(f"[{node_name}] Error: Input for tool '{tool_name}' failed {err_type}: {val_err}", style="red")
                        observation = f"Error: Input {err_type} for tool '{tool_name}'. Error: {val_err}"
                        approve = False
                    except Exception as approval_err: 
                        console.print(f"Error during tool approval process: {approval_err}", style="red")
                        observation = f"Error during approval step: {approval_err}"
                        approve = False

                    if approve:
                        globals.logger.info(f"[{node_name}] Execution approved. Executing tool '{tool_name}' via helper...")
                        execution_result_dict = await execute_tool(
                            node_name=node_name,
                            tool_name=tool_name,
                            input_string=tool_input_str, # Assuming input_string here means tool_input_str from context
                            tool_registry=tool_registry
                        )

                        parsed_store_raw_output_key: Optional[str] = None # Will hold the key like 'pr_diff'
                        raw_output_config = node_config.get('raw_tool_output')

                        if raw_output_config:
                            if isinstance(raw_output_config, dict) and len(raw_output_config) == 1:
                                parsed_store_raw_output_key = next(iter(raw_output_config.keys()))
                                # raw_output_type_hint = raw_output_config[parsed_store_raw_output_key] # Optional: if you need the type string ("str")
                                globals.logger.info(
                                    f"[{node_name}] Node is configured to store raw tool output to state key: '{parsed_store_raw_output_key}'."
                                )
                            else:
                                globals.logger.error(
                                    f"[{node_name}] Configuration error: 'raw_tool_output' in node config must be a dictionary "
                                    f"with exactly one entry (e.g., {{'my_key': 'str_type_hint'}}). "
                                    f"Found: {raw_output_config}. Raw output will not be stored directly for this action."
                                )

                        raw_tool_output_content: Any
                        if isinstance(execution_result_dict, dict):
                            if "_error" in execution_result_dict:
                                observation = execution_result_dict["_error"].get('user_message', execution_result_dict["_error"].get('message', "Tool execution failed."))
                                console.print(f"[{node_name}] Tool execution failed: {observation}", style="red")
                                last_react_error = observation # Assuming last_react_error is defined in the outer scope
                            else:
                                raw_tool_output_content = execution_result_dict.get("output", "Tool executed but produced no output.")

                                if parsed_store_raw_output_key: # Check if a valid key was parsed
                                    new_state[parsed_store_raw_output_key] = raw_tool_output_content # Store raw output directly
                                    observation = f"Tool '{tool_name}' executed successfully. Its output has been directly stored in the agent's state under the key '{parsed_store_raw_output_key}'."
                                    globals.logger.info(f"[{node_name}] Raw tool output stored to state key '{parsed_store_raw_output_key}'. Observation for LLM: {observation}")
                                else:
                                    observation = str(raw_tool_output_content) # Original behavior: use full output as observation
                                    globals.logger.info(f"[{node_name}] Tool Observation (first 200 chars): {observation[:200]}...")
                        else:
                            raw_tool_output_content = str(execution_result_dict)

                            if parsed_store_raw_output_key: # Check if a valid key was parsed
                                new_state[parsed_store_raw_output_key] = raw_tool_output_content # Store raw output directly
                                observation = f"Tool '{tool_name}' executed successfully. Its output (non-dict) has been directly stored in the agent's state under the key '{parsed_store_raw_output_key}'."
                                globals.logger.warning(f"[{node_name}] Raw tool output (non-dict) stored to state key '{parsed_store_raw_output_key}'. Observation for LLM: {observation}")
                            else:
                                observation = str(raw_tool_output_content) # Original behavior: use full output as observation
                                globals.logger.warning(f"[{node_name}] Unexpected tool execution result type (non-dict). Observation for LLM (first 200 chars): {observation[:200]}...")
                            
                    elif not observation: 
                        globals.logger.info(f"[{node_name}] Execution denied by user.")
                        observation = f"User denied execution of tool '{tool_name}'."
                    # Assumes format_react_step_for_scratchpad exists
                    agent_scratchpad += format_react_step_for_scratchpad(thought, tool_name, tool_input_str, observation)
                    continue
                elif status == 'error':
                    # This will only be reached if all retries were exhausted
                    # The error message and last_react_error are already set in the retry loop
                    break

            if not final_answer_found:
                if last_react_error: 
                    message = f"ReAct loop finished due to error: {last_react_error}"
                    _react_final_output = { "output": f"Error: {message}", "_error": { 'node': node_name, 'message': message, 'type': 'ReActLoopError', 'user_message': f"I apologize, I encountered an error during my reasoning process: {last_react_error}", 'recoverable': False } }
                else: 
                    message = f"ReAct loop finished after reaching max iterations ({max_iterations}) without a final answer."
                    history_summary = agent_scratchpad[-500:]
                    _react_final_output = { "output": message, "_error": { 'node': node_name, 'message': message, 'type': 'MaxIterationsReached', 'user_message': f"I couldn't reach a final answer within the allowed steps ({max_iterations}). My last steps involved:\n{history_summary}", 'recoverable': False } }

            final_result_text_from_react = None
            if isinstance(_react_final_output, dict) and "_error" not in _react_final_output: 
                final_result_text_from_react = _react_final_output.get("output")
            if parser and final_result_text_from_react and isinstance(final_result_text_from_react, str):
                 formatted_or_original_text = await _format_final_output( final_result_text_from_react, parser, node_name )
                 _react_final_output = formatted_or_original_text
            return _react_final_output

        if use_tools:
            if mcp_client:
                globals.logger.info(f"[{node_name}] Using MCP client context for ReAct logic...")
                try:
                    async with mcp_client as active_session:
                        final_output_for_state = await _perform_react_logic_with_optional_mcp_session(
                            active_mcp_session=active_session
                        )
                except Exception as e_mcp_ctx:
                    console.print(f"[{node_name}] Critical error within MCP client context management: {e_mcp_ctx}", style="red")
                    globals.logger.error(f"MCP Context Management Traceback:\n{traceback.format_exc()}")
                    final_output_for_state = { "_error": { 'node': node_name, 'message': f"MCP Context Management Error: {e_mcp_ctx}", 'type': 'MCPContextError', 'user_message': f"A critical error occurred with external tools: {e_mcp_ctx}", 'recoverable': False } }
            else:
                globals.logger.info(f"[{node_name}] No MCP client, running ReAct logic with internal tools only.")
                final_output_for_state = await _perform_react_logic_with_optional_mcp_session(
                    active_mcp_session=None
                )
        else:
            globals.logger.info(f"Using Direct LLM Call for {node_name}")
            prompt_to_llm = human_message_content
            format_instructions = ""
            schema_valid_for_format_direct = False
            if parser:
                try:
                    if hasattr(parser.pydantic_object, 'model_json_schema'): 
                        format_instructions = parser.get_format_instructions()
                        schema_valid_for_format_direct = True
                    else: 
                        console.print(f"Error: Pydantic V2 model lacks .model_json_schema() for {node_name}", style="red")
                    if schema_valid_for_format_direct: 
                        prompt_to_llm += f"\n\nIMPORTANT: Respond ONLY with a JSON object conforming to the schema below. Do not include ```json ``` markers or any text outside the JSON object itself.\nSCHEMA:\n{format_instructions}"
                    else: 
                        console.print(f"Warning: Cannot get format instructions for {node_name}. Asking for text.", style="yellow")
                except Exception as e: 
                    console.print(f"[{node_name}] Unexpected error getting format instructions: {e}", style="red")
            
            messages: List[BaseMessage] = [SystemMessage(content=system_message_content), HumanMessage(content=prompt_to_llm)]
            max_retries = node_config.get('max_retries', 3)
            retry_count = 0
            raw_llm_response_content: Optional[str] = None 
            llm_response_content_no_think: Optional[str] = None
            direct_call_parsed_output: Union[BaseModel, str, None] = None
            last_error = None
            while retry_count < max_retries:
                 try:
                      globals.logger.info(f"Attempt {retry_count + 1}/{max_retries} for direct LLM call...")
                      

                      final_response = await llm.ainvoke(messages)
                      raw_llm_response_content = final_response.content
                          
                      llm_response_content_no_think = remove_think_tags(raw_llm_response_content)
                      globals.logger.info(f"LLM response (raw): {str(raw_llm_response_content[:200])}...")
                      globals.logger.info(f"LLM response (tags removed): {str(llm_response_content_no_think[:200])}...")
                      if parser and schema_valid_for_format_direct:
                            cleaned_content = clean_and_fix_json(llm_response_content_no_think)
                            if not cleaned_content: 
                                if not llm_response_content_no_think and raw_llm_response_content:
                                     raise OutputParserException("Response became empty after <think> tag removal. Original may have only contained tags.")
                                raise OutputParserException("Received empty or unparseable response after tag removal and cleaning.")
                            direct_call_parsed_output = parser.parse(cleaned_content)
                            globals.logger.info("Successfully parsed and validated JSON output.")
                      else: 
                        direct_call_parsed_output = llm_response_content_no_think
                        globals.logger.info("Received raw text output (no parser or format instructions failed/missing).")
                      break
                 except (OutputParserException, ValidationError, json.JSONDecodeError) as e:
                      retry_count += 1
                      last_error = e
                      error_detail = f"{type(e).__name__}: {e}"
                      globals.logger.error(f"Output parsing/validation failed (Attempt {retry_count}/{max_retries}): {error_detail}")
                      if retry_count >= max_retries: 
                          break

                      feedback = create_error_feedback(e, node_name)
                      print_output(f"Providing feedback to LLM: {feedback[:100]}...")
                      messages = messages[:1] + [messages[1]] + [HumanMessage(content=feedback)]
                 except Exception as e: 
                    console.print(f"Unexpected LLM call error: {e}", style="red")
                    last_error = e
                    break
            if direct_call_parsed_output is not None: 
                final_output_for_state = direct_call_parsed_output
            elif last_error is not None:
                 new_state = handle_node_failure(new_state, node_name, last_error, max_retries)
                 return new_state
            else: 
                final_output_for_state = f"Error: Max retries reached or unexpected state. Last response: {llm_response_content}"

        # --- Update State & Print ---
        if final_output_for_state is None:
             console.print(f"Error: No output was processed or error captured for node {node_name}. Returning original state.", style="red")
             if new_state.get('_error') is None:
                 new_state['_error'] = { 'node': node_name, 'message': 'Node failed to produce output.', 'type': 'OutputMissingError', 'recoverable': False }
        else:
             new_state = update_state(new_state, final_output_for_state, node_config)

        print_user_messages(new_state, node_config)
        if not use_tools and node_config.get('print_prompt', False):
            if 'messages' in locals(): print_chat_prompt(ChatPromptTemplate(messages=messages), node_config)
            else: print_output(f"Cannot print prompt for {node_name}, 'messages' not defined.", "yellow")
        print_state(new_state, node_config)

        return new_state

    return node_function
