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

def create_node_function(node_config, mcp_manager):
    """Creates the appropriate node function based on node_config type."""
    node_type = node_config.get('type')
    if node_type == 'input':
        return create_input_node_function(node_config)
    elif node_type == 'output':
        return create_output_node_function(node_config)
    elif node_type == 'llm':
        use_tools_flag = bool(node_config.get('tools', False))
        return create_llm_node_function(node_config, mcp_manager, use_tools=use_tools_flag)
    elif node_type == 'tool':
        return create_tool_node_function(node_config, mcp_manager)
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
                                
                if item_to_append is not None and item_to_append != "":
                    if isinstance(item_to_append, list):
                        new_state[target_variable_name].extend(item_to_append)
                        globals.logger.info(f"[{node_name}] Extended list with items from the source list. New list size: {len(new_state[target_variable_name])}.")
                    else:
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

def create_tool_node_function(node_config: Dict[str, Any], mcp_manager: Any):
    """Creates a tool node function that directly executes a tool without LLM involvement."""

    async def node_function(state: dict) -> dict:
        if state.get('_error') is not None:
            return state

        node_name = node_config.get('name', 'Unnamed Tool Node')
        parallel_config = node_config.get('parallel')

        # Get all available tools once
        all_tools = []
        if mcp_manager:
            try:
                active_session = mcp_manager.get_session()
                if active_session:
                    external_tools = active_session.get_tools() or []
                    all_tools.extend(external_tools)
            except Exception as e:
                console.print(f"Error fetching MCP tools: {e}", style="red")
        if isinstance(internal_tools_list, list):
            all_tools.extend(internal_tools_list)

        tool_registry = {}
        for tool_obj in all_tools:
            try:
                tool_name_attr = getattr(tool_obj, 'name', None)
                if not tool_name_attr or not isinstance(tool_name_attr, str): continue
                input_schema = getattr(tool_obj, 'args_schema', None)
                raw_executor = getattr(tool_obj, 'arun', getattr(tool_obj, '_arun', getattr(tool_obj, 'run', getattr(tool_obj, '_run', None))))
                if not callable(raw_executor): continue
                executor = raw_executor
                if not asyncio.iscoroutinefunction(raw_executor):
                    def wrapped_sync_executor_factory(sync_func):
                        async def wrapped_executor(*args, **kwargs): return await asyncio.to_thread(sync_func, *args, **kwargs)
                        return wrapped_executor
                    executor = wrapped_sync_executor_factory(raw_executor)
                tool_registry[tool_name_attr] = {
                    "name": tool_name_attr, "description": getattr(tool_obj, 'description', 'No description available.'),
                    "input_type": getattr(tool_obj, 'input_type', 'JSON_SCHEMA' if input_schema else 'STRING'),
                    "input_schema_definition": input_schema, "tool_executor": executor, "tool_instance": tool_obj
                }
            except Exception as e:
                console.print(f"[{node_name}] Error processing tool definition for {getattr(tool_obj, 'name', 'unknown')}: {e}", style="red")

        if parallel_config:
            # Parallel execution logic
            print_output(f"\n--- Node {node_name} (Parallel) ---")
            list_key = parallel_config.get('forEach', '').strip('{}')
            item_name = parallel_config.get('as')
            index_as_key = parallel_config.get('index_as')
            max_concurrency = parallel_config.get('maxConcurrency', 5)

            if not all([list_key, item_name]):
                return handle_node_failure(state, node_name, ValueError("Parallel config for tool node requires 'forEach' and 'as' keys."))
            input_list = state.get(list_key)
            if not isinstance(input_list, list):
                return handle_node_failure(state, node_name, TypeError(f"State variable '{list_key}' for parallel execution must be a list."))
            if not input_list:
                print_output(f"[ℹ️ Info] Parallel input list '{list_key}' is empty. Skipping.", color="yellow")
                return state

            output_model_config = node_config.get('output_model', {})
            if len(output_model_config) != 1:
                return handle_node_failure(state, node_name, ValueError("Parallel tool node 'output_model' must define exactly one target list variable."))
            output_key = next(iter(output_model_config.keys()))

            new_state = state.copy()
            new_state[output_key] = []
            aggregation_lock, semaphore = asyncio.Lock(), asyncio.Semaphore(max_concurrency)

            # --- Start: Tool Execution Queue and Consumer ---
            tool_execution_queue = asyncio.Queue()
            consumer_task = None

            async def tool_consumer():
                while True:
                    request = await tool_execution_queue.get()
                    if request is None:
                        tool_execution_queue.task_done()
                        break
                    
                    future = request.pop('future')
                    tool_call_info_for_approval = request.pop('approval_info')
                    
                    try:
                        approve = await asyncio.to_thread(request_tool_execution, tool_call_info_for_approval)
                        if approve:
                            result = await execute_tool(**request)
                        else:
                            tool_name = tool_call_info_for_approval['name']
                            result = {"_error": {"message": f"User denied execution of tool '{tool_name}'.", "user_message": f"Execution of tool '{tool_name}' was denied."}}
                        future.set_result(result)
                    except Exception as e:
                        future.set_exception(e)
                    finally:
                        tool_execution_queue.task_done()

            consumer_task = asyncio.create_task(tool_consumer())
            # --- End: Tool Execution Queue and Consumer ---

            async def _worker(item, index):
                worker_node_name = f"{node_name}-w{index+1}"
                async with semaphore:
                    scoped_state = state.copy()
                    scoped_state[item_name] = item
                    if index_as_key:
                        scoped_state[index_as_key] = index

                    try:
                        print_output(f"[⚡️ Parallel] Worker {index+1}/{len(input_list)} starting for tool node '{node_name}'.", color="cyan")
                        tools_selection = node_config.get('tools_selection')
                        if not (tools_selection and isinstance(tools_selection, list) and len(tools_selection) > 0):
                            raise ValueError("Parallel tool node requires 'tools_selection' to be a list with one tool.")
                        tool_name = tools_selection[0]

                        if tool_name not in tool_registry:
                            raise ValueError(f"Tool '{tool_name}' not found in tool registry.")

                        args_config = node_config.get('args', {})
                        tool_args = {}
                        for arg_name, arg_value in args_config.items():
                            if isinstance(arg_value, dict) and len(arg_value) == 1:
                                state_key = next(iter(arg_value.keys()))
                                if state_key in scoped_state:
                                    tool_args[arg_name] = scoped_state[state_key]
                                else:
                                    globals.logger.warning(f"[{worker_node_name}] State key '{state_key}' for arg '{arg_name}' not found in scoped state.")
                            elif isinstance(arg_value, str) and '{' in arg_value and '}' in arg_value:
                                tool_args[arg_name] = format_prompt(arg_value, scoped_state, node_config)
                            else:
                                tool_args[arg_name] = arg_value

                        tool_def = tool_registry[tool_name]
                        input_string = json.dumps(tool_args) if tool_def['input_type'] == 'JSON_SCHEMA' else (str(next(iter(tool_args.values()), '')) if tool_args else '')
                        
                        # --- Start: Queueing tool execution request ---
                        future = asyncio.get_running_loop().create_future()
                        tool_call_info_for_approval = {"name": tool_name, "args": tool_args, "auto_approve": node_config.get('tools_auto_approval', False)}
                        request = {
                            "future": future,
                            "approval_info": tool_call_info_for_approval,
                            "node_name": worker_node_name,
                            "tool_name": tool_name,
                            "input_string": input_string,
                            "tool_registry": tool_registry
                        }
                        await tool_execution_queue.put(request)
                        execution_result = await future
                        # --- End: Queueing tool execution request ---

                        if isinstance(execution_result, dict) and "_error" in execution_result:
                            error_message = execution_result["_error"].get('user_message', "Tool execution failed.")
                            globals.logger.error(f"[{worker_node_name}] Tool execution failed: {error_message}")
                            return None

                        tool_output = execution_result.get("output") if isinstance(execution_result, dict) else execution_result

                        async with aggregation_lock:
                            if tool_output is not None:
                                new_state[output_key].append(tool_output)

                        return None
                    except Exception as e:
                        globals.logger.error(f"[{worker_node_name}] Failed: {e}\n{traceback.format_exc()}")
                        return e

            tasks = [asyncio.create_task(_worker(item, i)) for i, item in enumerate(input_list)]
            results = await asyncio.gather(*tasks, return_exceptions=True)
            
            # --- Start: Cleanup consumer ---
            if consumer_task:
                await tool_execution_queue.put(None)
                await consumer_task
            # --- End: Cleanup consumer ---

            failures = [res for res in results if isinstance(res, Exception)]
            if failures:
                globals.logger.error(f"[{node_name}] {len(failures)}/{len(input_list)} parallel tool tasks failed:")
                for failure in failures: globals.logger.error(f"  - {type(failure).__name__}: {failure}")

            print_output(f"[✅ Parallel] Node {node_name} finished processing all items.", color="green")
            print_user_messages(new_state, node_config)
            print_state(new_state, node_config)
            return new_state

        else:
            # Original single-tool execution logic (remains unchanged)
            print_output(f"\n--- Node {node_name} ---")
            tools_selection = node_config.get('tools_selection')
            tool_name = tools_selection[0] if tools_selection and isinstance(tools_selection, list) and len(tools_selection) > 0 else node_name

            args_config = node_config.get('args', {})
            tool_args = {}
            for arg_name, arg_value in args_config.items():
                if isinstance(arg_value, dict) and len(arg_value) == 1:
                    state_key = next(iter(arg_value.keys()))
                    if state_key in state:
                        tool_args[arg_name] = state[state_key]
                    else:
                         globals.logger.warning(f"[{node_name}] State key '{state_key}' for arg '{arg_name}' not found in state.")
                elif isinstance(arg_value, str) and '{' in arg_value and '}' in arg_value:
                    tool_args[arg_name] = format_prompt(arg_value, state, node_config)
                else:
                    tool_args[arg_name] = arg_value

            if tool_name not in tool_registry:
                return handle_node_failure(state.copy(), node_name, ValueError(f"Tool '{tool_name}' not found in tool registry."))

            try:
                tool_def = tool_registry[tool_name]
                input_string = json.dumps(tool_args) if tool_def['input_type'] == 'JSON_SCHEMA' else (str(next(iter(tool_args.values()), '')) if tool_args else '')

                tool_call_info_for_approval = {"name": tool_name, "args": tool_args, "auto_approve": node_config.get('tools_auto_approval', False)}
                approve = await asyncio.to_thread(request_tool_execution, tool_call_info_for_approval)

                if not approve:
                    globals.logger.info(f"[{node_name}] Tool execution denied by user.")
                    return handle_node_failure(state.copy(), node_name, Exception(f"User denied execution of tool '{tool_name}'."))

                execution_result = await execute_tool(node_name=node_name, tool_name=tool_name, input_string=input_string, tool_registry=tool_registry)

                new_state = state.copy()
                if isinstance(execution_result, dict) and "_error" in execution_result:
                    return handle_node_failure(new_state, node_name, Exception(execution_result["_error"].get('user_message', "Tool execution failed.")))

                processed_output_for_state = execution_result.get("output") if isinstance(execution_result, dict) else execution_result
                output_model_config = node_config.get('output_model', {})

                if output_model_config and processed_output_for_state is not None:
                    NodeOutputModel = create_output_model(output_model_config)
                    if NodeOutputModel:
                        try:
                            if isinstance(processed_output_for_state, (dict, list)):
                                processed_output_for_state = NodeOutputModel.model_validate(processed_output_for_state)
                            elif isinstance(processed_output_for_state, str):
                                parser = PydanticOutputParser(pydantic_object=NodeOutputModel)
                                formatted_value = await _format_final_output(final_text=processed_output_for_state, parser=parser, node_name=node_name)
                                processed_output_for_state = formatted_value
                        except (ValidationError, OutputParserException) as e:
                            globals.logger.warning(f"[{node_name}] Failed to validate/parse tool output with Pydantic model: {e}. Using raw output.")
                        except Exception as e:
                            globals.logger.error(f"[{node_name}] Unexpected error processing tool output with model: {e}. Using raw output.")

                new_state = update_state(new_state, processed_output_for_state, node_config)
                print_user_messages(new_state, node_config)
                print_state(new_state, node_config)
                return new_state

            except Exception as e:
                return handle_node_failure(state.copy(), node_name, e)

    return node_function

def create_llm_node_function(node_config: Dict[str, Any], mcp_manager: Any, use_tools: bool):
    output_model_config = node_config.get('output_model', {})
    OutputModel = create_output_model(output_model_config)
    parser = PydanticOutputParser(pydantic_object=OutputModel) if OutputModel else None
    
    async def _perform_direct_llm_call(system_message_content, human_message_content, llm, parser, node_config, node_name):
        prompt_to_llm = human_message_content
        # Add format instructions to the prompt if a parser is available. This is a "best effort" enhancement.
        if parser:
            try:
                if hasattr(parser.pydantic_object, 'model_json_schema'): 
                    format_instructions = parser.get_format_instructions()
                    prompt_to_llm += f"\n\nIMPORTANT: Respond ONLY with a JSON object conforming to the schema below. Do not include ```json ``` markers or any text outside the JSON object itself.\nSCHEMA:\n{format_instructions}"
                else: 
                    console.print(f"Error: Pydantic V2 model lacks .model_json_schema() for {node_name}", style="red")
            except Exception as e: 
                console.print(f"[{node_name}] Unexpected error getting format instructions: {e}", style="red")
        
        messages: List[BaseMessage] = [SystemMessage(content=system_message_content), HumanMessage(content=prompt_to_llm)]
        if node_config.get('print_prompt', False):
            print_chat_prompt(ChatPromptTemplate(messages=messages), node_config)
            
        max_retries = node_config.get('max_retries', 3)
        retry_count = 0
        last_error = None
        
        while retry_count < max_retries:
             try:
                  globals.logger.info(f"[{node_name}] Attempt {retry_count + 1}/{max_retries} for direct LLM call...")
                  final_response = await llm.ainvoke(messages)
                  raw_llm_response_content = final_response.content
                  llm_response_content_no_think = remove_think_tags(raw_llm_response_content)
                  globals.logger.info(f"[{node_name}] LLM response (raw): {str(raw_llm_response_content[:200])}...")
                  globals.logger.info(f"[{node_name}] LLM response (tags removed): {str(llm_response_content_no_think[:200])}...")
                  
                  if parser:
                        cleaned_content = clean_and_fix_json(llm_response_content_no_think)
                        if not cleaned_content: 
                            if not llm_response_content_no_think and raw_llm_response_content:
                                 raise OutputParserException("Response became empty after <think> tag removal.")
                            raise OutputParserException("Received empty or unparseable response after cleaning.")
                        parsed_output = parser.parse(cleaned_content)
                        globals.logger.info(f"[{node_name}] Successfully parsed and validated JSON output.")
                        return parsed_output
                  else: 
                    globals.logger.info(f"[{node_name}] No parser defined. Received raw text output.")
                    return llm_response_content_no_think

             except (OutputParserException, ValidationError, json.JSONDecodeError) as e:
                  retry_count += 1
                  last_error = e
                  error_detail = f"{type(e).__name__}: {e}"
                  globals.logger.error(f"[{node_name}] Output parsing/validation failed (Attempt {retry_count}/{max_retries}): {error_detail}")
                  if retry_count >= max_retries: 
                      break
                  feedback = create_error_feedback(e, node_name)
                  print_output(f"Providing feedback to LLM: {feedback[:100]}...")
                  messages = messages[:1] + [messages[1]] + [HumanMessage(content=feedback)]
             except Exception as e: 
                console.print(f"[{node_name}] Unexpected LLM call error: {e}", style="red")
                last_error = e
                break
        raise last_error if last_error else Exception("LLM call failed after multiple retries without a specific parsing error.")

    async def _execute_parallel_llm_append(node_config, state, llm, parser, active_session, react_logic_func):
        node_name = node_config.get('name', 'Unnamed Parallel LLM Node')
        parallel_config = node_config['parallel']
        list_key = parallel_config.get('forEach', '').strip('{}')
        item_name = parallel_config.get('as')
        max_concurrency = parallel_config.get('maxConcurrency', 5)
        index_as_key = parallel_config.get('index_as')
        if not all([list_key, item_name]):
            return handle_node_failure(state, node_name, ValueError("Parallel config requires 'forEach' and 'as' keys."))
        input_list = state.get(list_key)
        if not isinstance(input_list, list):
            return handle_node_failure(state, node_name, TypeError(f"State variable '{list_key}' for parallel execution must be a list."))
        if not input_list:
            print_output(f"[ℹ️ Info] Parallel input list '{list_key}' is empty. Skipping.", color="yellow")
            return state
        output_model_config = node_config.get('output_model', {})
        output_keys = list(output_model_config.keys())
        new_state = state.copy()
        for key in output_keys: new_state[key] = []
        aggregation_lock, semaphore = asyncio.Lock(), asyncio.Semaphore(max_concurrency)
        node_uses_tools = bool(node_config.get('tools', False))
        tool_execution_queue, consumer_task = None, None
        if node_uses_tools:
            is_auto_approved = node_config.get('tools_auto_approval', False)
            if max_concurrency > 1 and not is_auto_approved:
                console.print(f"[⚠️ Warning] Node '{node_name}' has parallel tool calls but 'tools_auto_approval' is false. Prompts will appear sequentially.", style="yellow")
            tool_execution_queue = asyncio.Queue()
            async def tool_consumer():
                while True:
                    request = await tool_execution_queue.get()
                    if request is None:
                        tool_execution_queue.task_done(); break
                    future = request.pop('future')
                    tool_call_info_for_approval = request.pop('approval_info')
                    try:
                        approve = await asyncio.to_thread(request_tool_execution, tool_call_info_for_approval)
                        if approve:
                            result = await execute_tool(**request)
                        else:
                            result = { "_error": { "message": f"User denied execution of tool '{tool_call_info_for_approval['name']}'.", "user_message": f"Execution of tool '{tool_call_info_for_approval['name']}' was denied." } }
                        future.set_result(result)
                    except Exception as e: future.set_exception(e)
                    finally: tool_execution_queue.task_done()
            consumer_task = asyncio.create_task(tool_consumer())
        
        async def _worker(item, index):
            worker_node_name = f"{node_name}-w{index+1}"
            async with semaphore:
                scoped_state = state.copy()
                scoped_state[item_name] = item
                if index_as_key:
                    scoped_state[index_as_key] = index
                try:
                    print_output(f"[⚡️ Parallel] Worker {index+1}/{len(input_list)} starting (max concurrency: {max_concurrency}).", color="cyan")
                    system_prompt = format_prompt(node_config.get('system', ''), scoped_state, node_config)
                    human_prompt = format_prompt(node_config['prompt'], scoped_state, node_config)
                    final_output, raw_outputs = None, {}
                    if node_uses_tools:
                        final_output, raw_outputs = await react_logic_func(
                            active_mcp_session=active_session, system_message_content=system_prompt,
                            human_message_content=human_prompt, node_config=node_config, parser=parser,
                            worker_node_name=worker_node_name, scoped_state=scoped_state, llm=llm,
                            tool_queue=tool_execution_queue
                        )
                    else:
                        final_output = await _perform_direct_llm_call(
                            system_prompt, human_prompt, llm, parser, node_config, worker_node_name
                        )
                    if final_output:
                        list_fields = [k for k, f in final_output.model_fields.items() if 'list' in str(f.annotation).lower()]
                        is_empty = False
                        if list_fields and all(not getattr(final_output, k) for k in list_fields): is_empty = True
                        if not is_empty:
                            globals.logger.info(f"[{worker_node_name}] Found content. Appending results.")
                            async with aggregation_lock:
                                for key in output_keys:
                                    if hasattr(final_output, key):
                                        val = getattr(final_output, key)
                                        if isinstance(val, list): new_state[key].extend(val)
                                        else: new_state[key].append(val)
                                    else:
                                        globals.logger.warning(f"[{worker_node_name}] Key '{key}' not in output. Appending None.")
                                        new_state[key].append(None)
                        else: globals.logger.info(f"[{worker_node_name}] Result is empty. Skipping append.")
                    return None
                except Exception as e:
                    globals.logger.error(f"[{worker_node_name}] Failed: {e}\n{traceback.format_exc()}")
                    return e
        
        tasks = [asyncio.create_task(_worker(item, i)) for i, item in enumerate(input_list)]
        results = await asyncio.gather(*tasks, return_exceptions=True)
        if consumer_task:
            await tool_execution_queue.put(None)
            await consumer_task
        failures = [res for res in results if isinstance(res, Exception)]
        if failures:
            globals.logger.error(f"[{node_name}] {len(failures)}/{len(input_list)} parallel tasks failed:")
            for failure in failures: globals.logger.error(f"  - {failure}")
        print_output(f"[✅ Parallel] Node {node_name} finished processing all items.", color="green")
        print_user_messages(new_state, node_config)
        print_state(new_state, node_config)
        return new_state

    async def node_function(state: dict) -> dict:
        async def _perform_react_logic_with_optional_mcp_session(
            active_mcp_session: Optional[Any], system_message_content: str, human_message_content: str,
            node_config: Dict, parser: Optional[PydanticOutputParser], worker_node_name: str,
            scoped_state: Dict, llm: Any, tool_queue: Optional[asyncio.Queue] = None
        ) -> tuple[Union[BaseModel, str, Dict, None], Dict[str, Any]]:
            agent_scratchpad, tool_registry, filtered_tool_defs = "", {}, []
            max_iterations, final_answer_found = node_config.get('max_react_iterations', 5), False
            raw_outputs_to_store, last_react_error = {}, None
            all_fetched_tools = []
            if active_mcp_session:
                try:
                    if hasattr(active_mcp_session, 'get_tools'):
                         all_fetched_tools.extend(active_mcp_session.get_tools() or [])
                except Exception as e:
                     console.print(f"[{worker_node_name}] Warning: MCP client error during get_tools: {e}", style="red")
            if isinstance(internal_tools_list, list):
                all_fetched_tools.extend(internal_tools_list)
            tool_selection = node_config.get('tools_selection')
            for tool_obj in all_fetched_tools:
                try:
                    tool_name_attr = getattr(tool_obj, 'name', None)
                    if not tool_name_attr or not isinstance(tool_name_attr, str) or tool_name_attr in tool_registry: continue
                    if tool_selection and isinstance(tool_selection, list) and tool_name_attr not in tool_selection: continue
                    input_schema, raw_executor = getattr(tool_obj, 'args_schema', None), getattr(tool_obj, 'arun', getattr(tool_obj, '_arun', getattr(tool_obj, 'run', getattr(tool_obj, '_run', None))))
                    if not callable(raw_executor): continue
                    executor = raw_executor
                    if not asyncio.iscoroutinefunction(raw_executor):
                        def factory(sync_func):
                            async def wrapper(*args, **kwargs): return await asyncio.to_thread(sync_func, *args, **kwargs)
                            return wrapper
                        executor = factory(raw_executor)
                    tool_def: ToolDefinition = { "name": tool_name_attr, "description": getattr(tool_obj, 'description', ''), "input_type": getattr(tool_obj, 'input_type', 'JSON_SCHEMA' if input_schema else 'STRING'), "input_schema_definition": input_schema, "tool_executor": executor, "tool_instance": tool_obj }
                    filtered_tool_defs.append(tool_def)
                    tool_registry[tool_name_attr] = tool_def
                except Exception as e: 
                     console.print(f"[{worker_node_name}] Error processing tool definition: {e}", style="red")
            current_human_message = human_message_content
            for i in range(max_iterations):
                print_output(f"[{worker_node_name} 🧠 Thinking] Iteration {i + 1}/{max_iterations}", color="yellow")
                max_retries, retry_count, react_step_output = node_config.get('max_retries', 3), 0, None
                while retry_count < max_retries:
                    try:
                        react_step_output = await run_react_planning_step(
                            input_question=current_human_message, agent_scratchpad=agent_scratchpad,
                            system_message_content=system_message_content, llm=llm, tool_definitions=filtered_tool_defs,
                            node_name=f"{worker_node_name} Planner", print_prompt=node_config.get('print_prompt', False)
                        )
                        if react_step_output.get('status') != 'error': break
                        retry_count += 1; error_message = f"ReAct planning step failed. Raw Response: {react_step_output.get('raw_response')}"
                        if retry_count < max_retries:
                            console.print(f"[⚠️ Warning] Error (Attempt {retry_count}/{max_retries}): {error_message}", style="yellow")
                            feedback = create_error_feedback(error_message, worker_node_name)
                            print_output(f"Providing feedback to LLM: {feedback[:100]}...")
                            current_human_message = f"{human_message_content}\n\nPrevious attempt failed. Error: {error_message}\nYou must follow the specified format."
                        else: last_react_error = error_message
                    except Exception as e:
                        retry_count += 1; error_message = f"Exception during ReAct planning: {type(e).__name__}: {e}"
                        if retry_count < max_retries:
                            console.print(f"[{worker_node_name}] Exception (Attempt {retry_count}/{max_retries}): {error_message}", style="yellow")
                            feedback = create_error_feedback(e, worker_node_name)
                            print_output(f"Providing feedback to LLM: {feedback[:100]}...")
                            current_human_message = f"{human_message_content}\n\nPrevious attempt failed. Error: {error_message}\nYou must follow the specified format."
                        else:
                            last_react_error = error_message; break
                if last_react_error: break
                status, thought = react_step_output.get('status'), react_step_output.get('thought')
                if status == 'final_answer':
                    _react_final_output = {"output": remove_think_tags(react_step_output.get('answer',''))}
                    final_answer_found = True; break
                if status == 'action':
                    tool_name, tool_input_str, observation = react_step_output.get('tool'), react_step_output.get('tool_input'), ""
                    tool_args_for_approval = tool_input_str
                    try:
                        cleaned_input = clean_and_fix_json(tool_input_str)
                        if cleaned_input: tool_args_for_approval = json.loads(cleaned_input)
                    except (json.JSONDecodeError, TypeError):
                        globals.logger.debug(f"[{worker_node_name}] Tool input not valid JSON, using raw string for approval.")
                    approval_info = { "name": tool_name, "args": tool_args_for_approval, "auto_approve": node_config.get('tools_auto_approval', False) }
                    if tool_queue:
                        future = asyncio.get_running_loop().create_future()
                        request = { "approval_info": approval_info, "node_name": worker_node_name, "tool_name": tool_name, "input_string": tool_input_str, "tool_registry": tool_registry, "future": future }
                        await tool_queue.put(request)
                        execution_result_dict = await future
                    else:
                        approve = await asyncio.to_thread(request_tool_execution, approval_info)
                        if approve:
                            execution_result_dict = await execute_tool(node_name=worker_node_name, tool_name=tool_name, input_string=tool_input_str, tool_registry=tool_registry)
                        else:
                            execution_result_dict = { "_error": { "message": f"User denied execution of tool '{tool_name}'.", "user_message": f"Execution of tool '{tool_name}' was denied." } }
                    raw_tool_output_content = execution_result_dict.get("output", "Tool executed with no output.")
                    if isinstance(execution_result_dict, dict) and "_error" in execution_result_dict:
                         observation = execution_result_dict["_error"].get('user_message', "Tool execution failed.")
                    elif node_config.get('raw_tool_output') and isinstance(node_config.get('raw_tool_output'), dict) and len(node_config.get('raw_tool_output')) == 1:
                        key_to_store = next(iter(node_config.get('raw_tool_output').keys()))
                        raw_outputs_to_store[key_to_store] = raw_tool_output_content
                        observation = f"Tool '{tool_name}' executed successfully. Its output has been directly stored in the agent's state under the key '{key_to_store}'."
                        globals.logger.info(f"[{worker_node_name}] {observation}")
                    else:
                        observation = str(raw_tool_output_content)
                    agent_scratchpad += format_react_step_for_scratchpad(thought, tool_name, tool_input_str, observation)
            if not final_answer_found:
                 raise Exception(f"ReAct loop finished. Last error: {last_react_error}" if last_react_error else "Max iterations reached.")
            final_text = _react_final_output.get("output") if isinstance(_react_final_output, dict) else str(_react_final_output)
            final_parsed_output = await _format_final_output(final_text, parser, worker_node_name) if parser and isinstance(final_text, str) else final_text
            return final_parsed_output, raw_outputs_to_store

        async def _execute_logic_with_session(active_session: Optional[Any]) -> dict:
            node_name = node_config.get('name', 'Unnamed LLM Node')
            llm = LLMManager.get_llm()
            print_output(f"\n--- Node {node_name} ---")
            parallel_config = node_config.get('parallel')
            if parallel_config and node_config.get('output_action') == 'append':
                return await _execute_parallel_llm_append(node_config, state, llm, parser, active_session, _perform_react_logic_with_optional_mcp_session)
            print_output(f"[ℹ️ Info] Starting LLM node processing for {node_name}", color="yellow")
            try:
                system_message_content = format_prompt(node_config.get('system', ''), state, node_config)
                human_message_content = format_prompt(node_config['prompt'], state, node_config)
            except Exception as e:
                return handle_node_failure(state.copy(), node_name, e)
            final_output, raw_outputs, new_state = None, {}, state.copy()
            if use_tools:
                try:
                    final_output, raw_outputs = await _perform_react_logic_with_optional_mcp_session(
                        active_mcp_session=active_session, system_message_content=system_message_content,
                        human_message_content=human_message_content, node_config=node_config, parser=parser,
                        worker_node_name=node_name, scoped_state=state, llm=llm
                    )
                except Exception as e:
                    return handle_node_failure(new_state, node_name, e)
            else:
                try:
                    final_output = await _perform_direct_llm_call(
                        system_message_content, human_message_content, llm, parser, node_config, node_name
                    )
                except Exception as e:
                    return handle_node_failure(new_state, node_name, e)
            if raw_outputs:
                new_state.update(raw_outputs)
                globals.logger.info(f"[{node_name}] Updated state with raw tool outputs: {list(raw_outputs.keys())}")
            if final_output is not None:
                 new_state = update_state(new_state, final_output, node_config)
            print_user_messages(new_state, node_config)
            print_state(new_state, node_config)
            return new_state
        
        if mcp_manager and use_tools:
            active_session = mcp_manager.get_session()
            return await _execute_logic_with_session(active_session)
        else:
            return await _execute_logic_with_session(None)

    return node_function
