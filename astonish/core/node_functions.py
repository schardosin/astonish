"""
Node functions module for Astonish.
This module contains functions for creating and managing node functions.
"""
import json
import re
import asyncio
import traceback
import inquirer
import readline
import astonish.globals as globals
from typing import Dict, Any, Union, Optional, List
from langchain_core.messages import SystemMessage, HumanMessage, BaseMessage
from langchain_core.prompts import ChatPromptTemplate, BasePromptTemplate
from langchain_core.tools import BaseTool
from langchain_core.runnables import Runnable
from langchain_core.language_models.base import BaseLanguageModel
from langchain.output_parsers import PydanticOutputParser
from langchain.schema import OutputParserException
from pydantic import ValidationError, BaseModel
from astonish.tools.internal_tools import tools as internal_tools_list
from astonish.core.llm_manager import LLMManager
from astonish.core.utils import format_prompt, print_ai, print_output, console
from astonish.core.error_handler import create_error_feedback, handle_node_failure
from astonish.core.format_handler import execute_tool
from astonish.core.utils import request_tool_execution
from astonish.core.json_utils import clean_and_fix_json
from astonish.core.react_planning import ToolDefinition, ReactStepOutput, run_react_planning_step, format_react_step_for_scratchpad
from astonish.core.output_model_utils import create_output_model, _format_final_output
from astonish.core.state_management import update_state
from astonish.core.ui_utils import print_user_messages, print_chat_prompt, print_state

def create_node_function(node_config, mcp_client):
    """Creates the appropriate node function based on node_config type."""
    node_type = node_config.get('type')
    if node_type == 'input':
        return create_input_node_function(node_config)
    elif node_type == 'llm':
        use_tools_flag = bool(node_config.get('tools', False))
        return create_llm_node_function(node_config, mcp_client, use_tools=use_tools_flag)
    else:
        raise ValueError(f"Unsupported node type: {node_type}")

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
            # Use readline for better input handling
            readline.set_completer(lambda text, state: None)
            readline.parse_and_bind('tab: complete')
            console.print("You:", style="yellow", end=" ")
            user_input = input()

        new_state = state.copy()
        new_state[output_field] = user_input
        print_user_messages(new_state, node_config)
        print_state(new_state, node_config)
        return new_state

    return node_function

def create_llm_node_function(node_config: Dict[str, Any], mcp_client: Any, use_tools: bool):
    output_model_config = node_config.get('output_model', {})
    OutputModel = create_output_model(output_model_config)
    parser = PydanticOutputParser(pydantic_object=OutputModel) if OutputModel else None

    async def node_function(state: dict) -> dict:
        node_name = node_config.get('name', 'Unnamed LLM Node')
        print_output(f"Processing {node_name}")
        try:
            system_message_content = format_prompt(node_config.get('system', ''), state, node_config)
            human_message_content = format_prompt(node_config['prompt'], state, node_config)
            llm = LLMManager.get_llm()
        except Exception as e:
            console.print(f"Error preparing node {node_name}: {e}", style="red")
            error_state = handle_node_failure(state.copy(), node_name, e, 0, message_prefix="Preparation Error")
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
            print_output(f"Fetching and preparing tools...")
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

            for i in range(max_iterations):
                print_output(f"--- Tool reasoning: Iteration {i + 1} of a maximum of {max_iterations} ---")
                react_step_output = await run_react_planning_step(input_question=human_message_content, agent_scratchpad=agent_scratchpad, system_message_content=system_message_content, llm=llm, tool_definitions=filtered_tool_defs, node_name=f"{node_name} Planner", print_prompt=node_config.get('print_prompt', False))
                status = react_step_output['status']
                thought = react_step_output.get('thought')

                if status == 'action':
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
                             parsed_args = {} if not cleaned_input else json.loads(cleaned_input)
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
                elif status == 'final_answer': 
                    final_answer_text = react_step_output['answer']
                    _react_final_output = {"output": final_answer_text}
                    final_answer_found = True
                    break
                elif status == 'error': 
                    error_message = f"ReAct planning step failed. Raw Response: {react_step_output['raw_response']}"
                    console.print(f"[{node_name}] Error: {error_message}", style="red")
                    last_react_error = error_message
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
            llm_response_content: Optional[str] = None
            direct_call_parsed_output: Union[BaseModel, str, None] = None
            last_error = None
            while retry_count < max_retries:
                 try:
                      globals.logger.info(f"Attempt {retry_count + 1}/{max_retries} for direct LLM call...")
                      
                      response_chunks = []
                      async for chunk in llm.astream(messages):
                          globals.logger.info(f"LLM chunk response: {chunk}")
                          response_chunks.append(chunk)
                      
                      # Merge the chunks
                      if response_chunks:
                          # Create a complete response by concatenating all chunk contents
                          full_content = ""
                          for chunk in response_chunks:
                              if hasattr(chunk, 'content'):
                                  full_content += chunk.content
                          
                          # Use the last chunk as a template for the response object
                          llm_response = response_chunks[-1]
                          if hasattr(llm_response, 'content'):
                              llm_response.content = full_content
                      else:
                          llm_response = None
                          
                      llm_response_content = llm_response.content if hasattr(llm_response, 'content') else str(llm_response)
                      if parser and schema_valid_for_format_direct:
                            cleaned_content = clean_and_fix_json(llm_response_content)
                            if not cleaned_content: 
                                raise OutputParserException("Received empty response.")
                            direct_call_parsed_output = parser.parse(cleaned_content)
                            globals.logger.info("Successfully parsed and validated JSON output.")
                      else: 
                        direct_call_parsed_output = llm_response_content
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
