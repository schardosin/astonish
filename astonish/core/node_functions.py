import json
import re
import asyncio
import traceback
import inquirer
import readline
import astonish.globals as globals
from typing import TypedDict, Union, Optional, get_args, get_origin, Dict, Any, List, Callable, Coroutine, Type
from langchain_core.messages import SystemMessage, HumanMessage, BaseMessage
from langchain_core.prompts import ChatPromptTemplate, BasePromptTemplate
from langchain_core.tools import BaseTool
from langchain_core.runnables import Runnable
from langchain_core.language_models.base import BaseLanguageModel
from langchain.output_parsers import PydanticOutputParser
from langchain.schema import OutputParserException
from pydantic import Field, ValidationError, create_model, BaseModel
from astonish.tools.internal_tools import tools as internal_tools_list
from astonish.core.llm_manager import LLMManager
from astonish.core.utils import format_prompt, print_ai, print_output, console
from astonish.core.error_handler import create_error_feedback, handle_node_failure
from astonish.core.format_handler import execute_tool_with_corrected_input

class ToolDefinition(TypedDict):
    name: str
    description: str
    input_type: str
    input_schema_definition: Optional[Any]
    tool_executor: Callable[..., Coroutine[Any, Any, str]]
    tool_instance: Optional[BaseTool]

def create_output_model(output_model_config: Dict[str, str]) -> Optional[Type[BaseModel]]:
    """
    Creates a Pydantic model dynamically, handling lowercase type names from YAML
    and complex/generic types using eval.
    """
    if not output_model_config or not isinstance(output_model_config, dict):
        return None

    fields = {}
    type_lookup = {
        "str": str,
        "int": int,
        "float": float,
        "bool": bool,
        "any": Any,
        "list": List,
        "dict": Dict,
    }
    eval_context = {
         "Union": Union, "Optional": Optional, "List": List, "Dict": Dict,
         "str": str, "int": int, "float": float, "bool": bool, "Any": Any,
         "BaseModel": BaseModel
         }

    for field_name, field_type_str in output_model_config.items():
        field_type = Any # Default to Any on error
        try:
            normalized_type_str = field_type_str.strip()
            normalized_type_lower = normalized_type_str.lower()

            if normalized_type_lower in type_lookup:
                field_type = type_lookup[normalized_type_lower]

            elif any(c in normalized_type_str for c in ['[', '|', 'Optional', 'Union']):
                try:
                     field_type = eval(normalized_type_str, globals(), eval_context)
                except NameError:
                     console.print(f"Warning: Eval failed to find type components within '{normalized_type_str}'. Defaulting field '{field_name}' to Any.", style="yellow")

                     field_type = Any
                except Exception as e_eval:
                     console.print(f"Error evaluating complex type '{normalized_type_str}': {e_eval}", style="red")
                     field_type = Any
            else:
                 console.print(f"Warning: Unknown or non-generic type '{normalized_type_str}' for field '{field_name}', defaulting to Any.", style="yellow")
                 field_type = Any

            if field_type is not Any:
                origin = get_origin(field_type)
                args = get_args(field_type)

                if origin is Union and type(None) in args:
                    non_none_args = tuple(arg for arg in args if arg is not type(None))
                    if len(non_none_args) == 1: field_type = Optional[non_none_args[0]]
                    elif len(non_none_args) > 1: field_type = Optional[Union[non_none_args]]
                    else: field_type = Optional[type(None)]

            fields[field_name] = (field_type, Field(description=f"{field_name} field"))

        except Exception as e:
            console.print(f"Error processing field '{field_name}' with type string '{field_type_str}': {e}", style="red")
            fields[field_name] = (Any, Field(description=f"{field_name} field (processing error)"))

    model_name = f"DynamicOutputModel_{abs(hash(json.dumps(output_model_config, sort_keys=True)))}"
    try:
         Model = create_model(model_name, **fields)
         return Model
    except Exception as e:
         console.print(f"Failed to create Pydantic model '{model_name}': {e}", style="red")
         return None

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
    # Check if the current state already has an error
    if state.get('_error') is not None:
        # Don't update state if there's already an error
        return state
    
    new_state = state.copy()
    output_field_name = next(iter(node_config.get('output_model', {})), 'agent_final_answer')
    
    # Handle different output types
    if isinstance(output, BaseModel):
        new_state.update(output.model_dump(exclude_unset=True))
    elif isinstance(output, dict):
        # Check for error information
        if "_error" in output:
            # Propagate error information to the state
            new_state['_error'] = output["_error"]
            
            # If there's an error, return a minimal state with just the error info
            # This helps avoid state conflicts
            if output["_error"] and not output["_error"].get('recoverable', True):
                return {
                    '_error': output["_error"],
                    '_end': False  # Let the error handler node set this to True
                }
        
        # Update the output field
        if "output" in output:
            new_state[output_field_name] = output.get("output", "")
    elif isinstance(output, str):
        new_state[output_field_name] = output
    elif output is None:
        globals.logger.warning("Received None output for state update.")
    
    # Update counter for limited nodes
    limit_counter_field = node_config.get('limit_counter_field')
    limit = node_config.get('limit')
    if limit_counter_field and limit and '_error' not in new_state:
        counter = new_state.get(limit_counter_field, 0) + 1
        if counter > limit:
            counter = 1
        new_state[limit_counter_field] = counter
    
    return new_state

def create_custom_react_prompt_template(tools_definitions: List[ToolDefinition]) -> str:
    """
    Generates a ReAct prompt string including detailed tool input requirements,
    WITH property descriptions, enums, defaults, and escaping schema braces.
    Removes the problematic literal JSON example from instructions.
    """
    tool_strings = []
    for tool_def in tools_definitions:
        input_desc = ""; input_type = tool_def.get('input_type', 'STRING'); schema_def = tool_def.get('input_schema_definition')

        if input_type == 'JSON_SCHEMA':
            schema_desc_str = "[Schema not provided or invalid]"
            properties = {}
            required_list = []
            if schema_def:
                try:
                    if isinstance(schema_def, type) and issubclass(schema_def, BaseModel):
                        schema_json = schema_def.model_json_schema()
                        properties = schema_json.get("properties", {})
                        required_list = schema_json.get("required", [])
                    elif isinstance(schema_def, dict): # Handle raw JSON schema dict
                        properties = schema_def.get("properties", {})
                        required_list = schema_def.get("required", [])

                    if properties:
                        prop_details_list = []
                        for name, details in properties.items():
                            prop_type = details.get('type', 'unknown')
                            prop_desc = details.get('description', '') # Get description
                            prop_enum = details.get('enum') # Get allowed values
                            prop_default = details.get('default') # Get default value

                            is_required = name in required_list
                            req_marker = " (required)" if is_required else ""

                            # Construct the detail string for this property
                            detail_str = f"'{name}' ({prop_type}{req_marker})"
                            if prop_desc:
                                detail_str += f": {prop_desc}" # Add description
                            if prop_enum:
                                enum_strs = [f'"{v}"' if isinstance(v, str) else str(v) for v in prop_enum]
                                detail_str += f" (must be one of: [{', '.join(enum_strs)}])" # List allowed values
                            if prop_default is not None:
                                 detail_str += f" (default: {json.dumps(prop_default)})" # Show default

                            prop_details_list.append(detail_str.replace("{", "{{").replace("}", "}}"))

                        schema_desc_str = "; ".join(prop_details_list)
                    else:
                         schema_desc_str = "[No properties found in schema]"
                except Exception as e:
                    schema_desc_str = f"[Error extracting schema details: {e}]"

            input_desc = f"Input Type: Requires a JSON object string with properties: {schema_desc_str}. IMPORTANT: Generate a valid JSON string containing required properties and only essential optional properties based on the user request."

        elif input_type == 'STRING':
             input_desc = "Input Type: Plain String"
             if schema_def and isinstance(schema_def, str):
                  escaped_example = schema_def.replace("{", "{{").replace("}", "}}")
                  input_desc += f". Expected format/example: {escaped_example}"
        else:
             input_desc = f"Input Type: {input_type}"
             if schema_def and isinstance(schema_def, str):
                 input_desc += f". Tool expects: {schema_def.replace('{','{{').replace('}','}}')}"


        tool_name = tool_def.get('name', 'UnnamedTool'); tool_description = tool_def.get('description', 'No description.')
        tool_strings.append(f"- {tool_name}: {tool_description} {input_desc}")

    formatted_tools = "\n".join(tool_strings) if tool_strings else "No tools available."
    tool_names = ", ".join([tool_def['name'] for tool_def in tools_definitions]) if tools_definitions else "N/A"

    # Template instructions remain largely the same, but the LLM now has better context from formatted_tools
    template = f"""Answer the following questions as best you can. You have access to the following tools:
{formatted_tools}

Use the following format STRICTLY:

Question: the input question you must answer
Thought: Analyze the question and available tools. Determine the single best Action to take. Identify the essential arguments required by that Action's schema based on the Question and the tool description (especially paying attention to property descriptions, required fields, and allowed values like enums). Use sensible defaults for optional arguments unless the Question specifies otherwise.
Action: the action to take, must be one of [{tool_names}]
Action Input: Provide the exact input required for the selected Action. If Input Type requires a JSON object string, generate a *single, valid JSON object string* containing ONLY the essential properties identified in your Thought process, matching the required types and allowed values (enums) mentioned in the tool description. If Input Type is STRING, provide the plain string. Do NOT add explanations before or after the Action Input line. IMPORTANT: After writing the Action Input line, STOP generating immediately. Wait for the Observation.
Observation: the result of the action
... (this Thought/Action/Action Input/Observation can repeat N times)
Thought: I now know the final answer based on my thoughts and observations.
Final Answer: the final answer to the original input question

Begin!

Question: {{input}}
Thought:{{agent_scratchpad}}"""

    return template

# --- Node Creation Functions ---
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
        # Check if there's an error in the state
        if state.get('_error') is not None:
            return state  # Return the state unchanged if there's an error
        
        formatted_prompt = format_prompt(node_config['prompt'], state, node_config)
        print_ai(formatted_prompt)
        output_field = next(iter(node_config.get('output_model', {'user_input': 'str'})), 'user_input')

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
    """
    Creates LLM node function. Uses custom ReAct loop if tools are needed,
    adding a final formatting step if node requires structured output.
    Direct call otherwise.
    """
    output_model_config = node_config.get('output_model', {})
    OutputModel = create_output_model(output_model_config)
    parser = PydanticOutputParser(pydantic_object=OutputModel) if OutputModel else None

    tool_registry: Dict[str, ToolDefinition] = {}

    async def node_function(state: dict) -> dict:
        node_name = node_config.get('name', 'Unnamed LLM Node')
        print_output(f"Processing {node_name}")

        limit_counter_field = node_config.get('limit_counter_field'); limit = node_config.get('limit')
        current_counter = state.get(limit_counter_field, 0)
        if limit_counter_field and limit:
             next_counter = current_counter + 1; log_counter = next_counter if next_counter <= limit else 1
             print_output(f"Processing {node_name} (Cycle {log_counter}/{limit})")
        try:
            system_message_content = format_prompt(node_config.get('system',''), state, node_config)
            human_message_content = format_prompt(node_config['prompt'], state, node_config)
            llm = LLMManager.get_llm()
        except Exception as e: console.print(f"Error preparing node {node_name}: {e}", style="red"); return state

        final_output_for_state: Union[BaseModel, str, Dict, None] = None
        new_state = state.copy() # Work on a copy

        async def run_react_logic_with_tools(
            active_mcp_client: Optional[Any],
            node_config: Dict[str, Any],
            system_message_content: str,
            human_message_content: str,
            llm: BaseLanguageModel,
            tool_registry: Dict[str, ToolDefinition],
            internal_tools_list: List[Any]
            ) -> Union[Dict, str, None]:
            """
            Handles tool fetching, prompting, LLM call, parsing, conditional input processing,
            validation, **human tool approval**, and execution for a single ReAct step.

            Returns:
                A dictionary like {"output": "result_text"} on success/final answer/error/denial,
                or None if a critical setup error occurred.
            """
            node_name = node_config.get('name', 'Unnamed ReAct Node')
            processed_output_react: Union[Dict, str, None] = {"output": f"Error: ReAct logic failed to produce output for {node_name}"}

            try:
                tool_registry.clear(); filtered_tool_defs: List[ToolDefinition] = []; all_fetched_tools: List[Any] = []
                if active_mcp_client:
                    try:
                        globals.logger.info(f"[{node_name}] Fetching external tools via MCP client...")
                        external_tools_data = active_mcp_client.get_tools() or []
                        if isinstance(external_tools_data, list): all_fetched_tools.extend(external_tools_data); globals.logger.info(f"[{node_name}] Fetched {len(external_tools_data)} external tools.")
                        else: console.print(f"[{node_name}] Warning: mcp_client.get_tools() did not return a list.", style="yellow")
                    except Exception as e: console.print(f"[{node_name}] Warning: MCP client error getting tools: {e}", style="red")
                if isinstance(internal_tools_list, list): all_fetched_tools.extend(internal_tools_list)
                tool_selection = node_config.get('tools_selection'); processed_tool_names = set()
                for tool_obj in all_fetched_tools:
                    try:
                        tool_name = getattr(tool_obj, 'name', None)
                        if not tool_name or not isinstance(tool_name, str) or tool_name in processed_tool_names: continue
                        if tool_selection and isinstance(tool_selection, list) and tool_name not in tool_selection: continue
                        input_schema = getattr(tool_obj, 'args_schema', None)
                        input_type = getattr(tool_obj, 'input_type', 'JSON_SCHEMA' if input_schema else 'STRING')
                        executor = getattr(tool_obj, 'arun', getattr(tool_obj, '_arun', getattr(tool_obj, 'run', getattr(tool_obj, '_run', None))))

                        if not callable(executor): continue
                        tool_def: ToolDefinition = {"name": tool_name, "description": getattr(tool_obj, 'description', 'No description available.'), "input_type": input_type, "input_schema_definition": input_schema, "tool_executor": executor, "tool_instance": tool_obj}
                        filtered_tool_defs.append(tool_def); tool_registry[tool_name] = tool_def; processed_tool_names.add(tool_name)
                    except Exception as e: console.print(f"[{node_name}] Error processing tool definition for {getattr(tool_obj, 'name', 'unknown')}: {e}", style="red")
                if not filtered_tool_defs: console.print(f"Warning: No valid tools available for ReAct node {node_name}. Agent may only reason.", style="yellow")

                custom_prompt_template_str = create_custom_react_prompt_template(filtered_tool_defs)
                react_system_message = system_message_content + "\n\n" + custom_prompt_template_str
                custom_react_prompt = ChatPromptTemplate.from_messages([("system", react_system_message), ("human", "{input}"),])

                globals.logger.info(f"[{node_name}] Invoking LLM for custom ReAct step...")
                chain: Runnable = custom_react_prompt | llm
                invoke_input = {"input": human_message_content}
                if "agent_scratchpad" in custom_react_prompt.input_variables: invoke_input["agent_scratchpad"] = ""

                llm_response = await chain.ainvoke(invoke_input)
                response_text = llm_response.content if hasattr(llm_response, 'content') else str(llm_response)
                globals.logger.info(f"[{node_name}] LLM Raw Response:\n{response_text}")

                action_match = re.search(r"^\s*Action:\s*([\w.-]+)", response_text, re.MULTILINE | re.IGNORECASE)
                input_string_from_llm = ""
                action_input_line_match = re.search(r"^\s*Action Input:\s*(.*)", response_text, re.MULTILINE | re.IGNORECASE)
                if action_input_line_match:
                    raw_input_line = action_input_line_match.group(1).strip()
                    json_match = re.match(r"^(\{.*?\})\s*$", raw_input_line, re.DOTALL)
                    if json_match: input_string_from_llm = json_match.group(1)
                    else:
                        input_string_from_llm = raw_input_line.split('\n')[0].strip()
                        if raw_input_line != input_string_from_llm: console.print(f"[{node_name}] Warning: Truncated Action Input. Using: '{input_string_from_llm}'", style="yellow")

                if action_match:
                    tool_name = action_match.group(1).strip()
                    globals.logger.info(f"[{node_name}] LLM selected Action: {tool_name}")
                    globals.logger.info(f"[{node_name}] LLM provided Action Input string (parsed): '{input_string_from_llm}'")

                    if tool_name in tool_registry:
                        tool_definition = tool_registry[tool_name]
                        tool_input_type = tool_definition['input_type']; tool_schema_def = tool_definition['input_schema_definition']
                        tool_executor = tool_definition['tool_executor']; tool_args_for_execution: Any = None; observation: str = ""

                        try:
                            if tool_input_type == 'STRING':
                                globals.logger.info(f"[{node_name}] Processing as STRING input.")
                                tool_args_for_execution = input_string_from_llm
                            elif tool_input_type == 'JSON_SCHEMA':
                                globals.logger.info(f"[{node_name}] Processing as JSON_SCHEMA input.")
                                if not tool_schema_def: raise ValueError(f"No schema definition found for JSON tool '{tool_name}'")
                                
                                # Process JSON input with error handling
                                try:
                                    cleaned_json_string = input_string_from_llm.removeprefix("```json").removesuffix("```").strip()
                                    parsed_args = {} if not cleaned_json_string else json.loads(cleaned_json_string)
                                    
                                    if isinstance(tool_schema_def, type) and issubclass(tool_schema_def, BaseModel):
                                        validated_args = tool_schema_def(**parsed_args)
                                        tool_args_for_execution = validated_args.model_dump(mode='json')
                                        globals.logger.info(f"[{node_name}] JSON Validation successful (Pydantic).")
                                    else:
                                        tool_args_for_execution = parsed_args
                                        globals.logger.info(f"[{node_name}] JSON Validation skipped.")
                                        
                                except (json.JSONDecodeError, ValidationError, TypeError) as json_error:
                                    # Get retry configuration
                                    max_json_retries = node_config.get('max_json_retries', 2)
                                    
                                    if max_json_retries > 0:
                                        # Import the LLM error handler
                                        from astonish.core.error_handler import handle_llm_error
                                        
                                        # Try to fix the JSON processing issue with retries
                                        json_result = await handle_llm_error(
                                            node_name=node_name,
                                            error_type="json_processing",
                                            error_details=str(json_error),
                                            response_text=response_text,
                                            system_message=system_message_content,
                                            human_message=human_message_content,
                                            llm=llm,
                                            state=state,
                                            max_retries=max_json_retries
                                        )
                                        
                                        # Check if we got a successful result or an error
                                        if "_error" in json_result:
                                            # JSON correction failed after retries
                                            return json_result
                                        else:
                                            # JSON correction succeeded, extract the corrected output
                                            # and continue with tool execution
                                            return json_result
                                    else:
                                        # No retries configured, re-raise the error
                                        raise
                            else: raise ValueError(f"Unsupported tool input_type: '{tool_input_type}'")

                            globals.logger.info(f"[{node_name}] Requesting user approval for tool '{tool_name}'...")
                            args_display = tool_args_for_execution
                            if isinstance(args_display, dict):
                                try: args_display = json.dumps(args_display, indent=2)
                                except TypeError: args_display = str(args_display)
                            else: args_display = str(args_display)

                            approve = False
                            try:
                                from astonish.core.utils import request_tool_execution
                                tool_call_info_for_approval = {"name": tool_name, "args": tool_args_for_execution, "auto_approve": node_config.get('tools_auto_approval', False)}
                                approve = await asyncio.to_thread(request_tool_execution, tool_call_info_for_approval)

                            except ImportError: console.print(f"Error: 'request_tool_execution' not imported...", style="red")
                            except NameError: console.print(f"Error: 'request_tool_execution' not found...", style="red")
                            except Exception as approval_err: console.print(f"Error during tool approval: {approval_err}", style="red"); console.print(f"Traceback:\n{traceback.format_exc()}", style="red")

                            observation: str
                            if approve:
                                globals.logger.info(f"[{node_name}] Execution approved by user.")
                                # --- Execute Tool (only if approved) ---
                                globals.logger.info(f"[{node_name}] Executing tool '{tool_name}'...")
                                executor_is_async = asyncio.iscoroutinefunction(tool_executor)
                                if executor_is_async:
                                    tool_result = await tool_executor(tool_args_for_execution)
                                else:
                                    tool_result = await asyncio.to_thread(tool_executor, tool_args_for_execution)

                                observation = str(tool_result)
                                globals.logger.info(f"[{node_name}] Tool Observation: {observation}")

                            else:
                                globals.logger.info(f"[{node_name}] Execution denied by user or approval failed.")
                                observation = f"User denied execution of tool '{tool_name}'."

                            processed_output_react = {"output": observation}
                        except (json.JSONDecodeError, ValidationError, ValueError) as proc_error:
                             error_message = f"Error processing input for tool '{tool_name}': {proc_error}"
                             console.print(f"[{node_name}] {error_message}", style="red")
                             processed_output_react = {
                                 "output": f"Error: Input processing failed - {proc_error}",
                                 "_error": {
                                     'node': node_name,
                                     'message': error_message,
                                     'type': 'InputProcessingError',
                                     'user_message': f"I apologize, but I was unable to process the input for tool '{tool_name}'. The error was: {proc_error}",
                                     'recoverable': False
                                 }
                             }
                        except Exception as exec_error:
                             error_message = f"Error executing tool '{tool_name}': {exec_error}"
                             console.print(f"[{node_name}] {error_message}", style="red")
                             globals.logger.error(f"Traceback:\n{traceback.format_exc()}")
                             processed_output_react = {
                                 "output": f"Error: Tool execution failed - {exec_error}",
                                 "_error": {
                                     'node': node_name,
                                     'message': error_message,
                                     'type': 'ToolExecutionError',
                                     'user_message': f"I apologize, but I was unable to execute the tool '{tool_name}'. The error was: {exec_error}",
                                     'recoverable': False
                                 }
                             }

                    else: # Tool name parsed but not found in registry
                        error_message = f"Error: LLM selected unknown Action: {tool_name}"
                        console.print(f"[{node_name}] {error_message}", style="red")
                        processed_output_react = {
                            "output": error_message,
                            "_error": {
                                'node': node_name,
                                'message': error_message,
                                'type': 'UnknownToolError',
                                'user_message': f"I apologize, but I tried to use a tool that doesn't exist: '{tool_name}'.",
                                'recoverable': False
                            }
                        }

                elif "Final Answer:" in response_text:
                    final_answer = response_text.split("Final Answer:")[-1].strip()
                    print_output(f"[{node_name}] LLM provided Final Answer: {final_answer}")
                    processed_output_react = {"output": final_answer}
                else:
                    warning_message = f"Warning: LLM response for {node_name} did not provide Action or Final Answer."
                    console.print(f"{warning_message}", style="yellow")
                    
                    # Check if we should retry with format correction
                    max_format_retries = node_config.get('max_format_retries', 2)
                    treat_as_error = node_config.get('strict_format', True)
                    
                    if treat_as_error and max_format_retries > 0:
                        # Instead of using the error handler, we'll handle the format violation directly
                        # This ensures we use the same prompt template with the tools information
                        
                        # Get current retry count
                        retry_count_key = "_format_violation_retry_count"
                        current_retry = state.get(retry_count_key, 0)
                        
                        if current_retry >= max_format_retries:
                            # We've exhausted retries, create an error state
                            error_message = f"The AI model did not follow the required format for {node_name} after {max_format_retries} attempts."
                            console.print(f"[{node_name}] {error_message}", style="red")
                            
                            processed_output_react = {
                                "output": f"Error: Format violation - {error_message}",
                                "_error": {
                                    'node': node_name,
                                    'message': error_message,
                                    'type': 'FormatViolationError',
                                    'user_message': f"I apologize, but I was unable to process the '{node_name}' step correctly. The AI model did not follow the required format after {max_format_retries} attempts.",
                                    'recoverable': False
                                }
                            }
                        else:
                            # Increment retry count
                            current_retry += 1
                            print_output(f"Attempting to fix format issue (Retry {current_retry}/{max_format_retries})...", "yellow")
                            
                            # Create feedback for the LLM
                            feedback = (
                                f"Your response did not follow the required format. You must provide either an Action or a Final Answer. "
                                f"Please follow the format strictly:\n\n"
                                f"Thought: your reasoning\n"
                                f"Action: the tool to use (must be one of the tools listed in the system message)\n"
                                f"Action Input: the input for the tool\n\n"
                                f"OR\n\n"
                                f"Thought: your final reasoning\n"
                                f"Final Answer: your final answer"
                            )
                            
                            # Use the same prompt template as before, but add the feedback
                            print_output("Sending format correction feedback to LLM...")
                            
                            # Create a new human message with the original question plus feedback
                            new_human_message = f"{human_message_content}\n\n{feedback}"
                            
                            # Use the same chain as before
                            new_invoke_input = {"input": new_human_message}
                            if "agent_scratchpad" in custom_react_prompt.input_variables:
                                new_invoke_input["agent_scratchpad"] = ""
                            
                            # Call the LLM again with the same prompt template
                            new_response = await chain.ainvoke(new_invoke_input)
                            new_response_text = new_response.content if hasattr(new_response, 'content') else str(new_response)
                            
                            # Check if the new response has Action or Final Answer
                            new_action_match = re.search(r"^\s*Action:\s*([\w.-]+)", new_response_text, re.MULTILINE | re.IGNORECASE)
                            if new_action_match:
                                print_output(f"Format correction successful! LLM now provides an Action.")
                                # Process the corrected response as a new response
                                # This will go through the normal action processing flow
                                response_text = new_response_text
                                # Reset the action_match and other variables to process this new response
                                action_match = new_action_match
                                tool_name = action_match.group(1).strip()
                                
                                # Extract the new Action Input
                                new_action_input_line_match = re.search(r"^\s*Action Input:\s*(.*)", new_response_text, re.MULTILINE | re.IGNORECASE)
                                if new_action_input_line_match:
                                    raw_input_line = new_action_input_line_match.group(1).strip()
                                    json_match = re.match(r"^(\{.*?\})\s*$", raw_input_line, re.DOTALL)
                                    if json_match:
                                        input_string_from_llm = json_match.group(1)
                                    else:
                                        input_string_from_llm = raw_input_line.split('\n')[0].strip()
                                
                                # Process this action using the new format_handler module
                                globals.logger.info(f"[{node_name}] Processing corrected Action: {tool_name}")
                                globals.logger.info(f"[{node_name}] With corrected Action Input: '{input_string_from_llm}'")
                                
                                # Execute the tool with the corrected input
                                tool_result = await execute_tool_with_corrected_input(
                                    node_name=node_name,
                                    tool_name=tool_name,
                                    input_string=input_string_from_llm,
                                    tool_registry=tool_registry,
                                    node_config=node_config
                                )
                                
                                # Use the result from the tool execution
                                processed_output_react = tool_result
                            elif "Final Answer:" in new_response_text:
                                print_output(f"Format correction successful! LLM now provides a Final Answer.")
                                final_answer = new_response_text.split("Final Answer:")[-1].strip()
                                processed_output_react = {"output": final_answer}
                            else:
                                # Still no proper format, but we can't use continue outside a loop
                                # Just set an error state and let the next iteration handle it
                                print_output(f"Format correction failed. LLM still doesn't follow the format.")
                                state[retry_count_key] = current_retry
                                processed_output_react = {
                                    "output": f"Error: Format violation - The AI model still did not follow the required format after retry {current_retry}/{max_format_retries}",
                                    "_error": {
                                        'node': node_name,
                                        'message': f"Format correction failed on retry {current_retry}/{max_format_retries}",
                                        'type': 'FormatViolationError',
                                        'user_message': f"I apologize, but I was unable to process the '{node_name}' step correctly. The AI model did not follow the required format.",
                                        'recoverable': current_retry < max_format_retries  # Only recoverable if we haven't reached max retries
                                    }
                                }
                    elif treat_as_error:
                        # No retries configured, treat as error immediately
                        error_message = (
                            f"The AI model did not follow the required format for {node_name}. "
                            f"It should provide either an Action or a Final Answer, but it returned: "
                            f"\"{response_text[:100]}...\""
                        )
                        console.print(f"[{node_name}] {error_message}", style="red")
                        
                        # Create an error state that will be caught by the error handling system
                        processed_output_react = {
                            "output": f"Error: Format violation - {error_message}",
                            "_error": {
                                'node': node_name,
                                'message': error_message,
                                'type': 'FormatViolationError',
                                'user_message': f"I apologize, but I was unable to process the '{node_name}' step correctly. The AI model did not follow the required format.",
                                'recoverable': False
                            }
                        }
                    else:
                        # If not treating as error, use raw response but log a warning
                        console.print(f"Using raw response as fallback (strict_format=False).", style="yellow")
                        processed_output_react = {"output": response_text}

            except Exception as react_logic_error:
                console.print(f"[{node_name}] Critical error during ReAct step setup or LLM call: {react_logic_error}", style="red")
                console.print(f"Traceback:\n{traceback.format_exc()}", style="red")
                processed_output_react = {
                    "output": f"Error during ReAct step: {react_logic_error}",
                    "_error": {
                        'node': node_name,
                        'message': f"Critical error during ReAct step: {react_logic_error}",
                        'type': 'ReActStepError',
                        'user_message': f"I apologize, but I encountered an error while processing this step. The error was: {react_logic_error}",
                        'recoverable': False
                    }
                }

            # Ensure function always returns a dictionary with 'output' key or None
            if isinstance(processed_output_react, dict) and "output" in processed_output_react: return processed_output_react
            elif isinstance(processed_output_react, str): return {"output": processed_output_react}
            else: return {"output": f"Error: ReAct logic failed to produce valid output for {node_name}"}

        react_step_result: Union[Dict, str, None] = None
        if use_tools:
            if mcp_client:
                 globals.logger.info("Entering MCP client context for tool operations...")
                 try:
                      async with mcp_client as client:
                           # *** PASS ALL REQUIRED ARGUMENTS HERE ***
                           react_step_result = await run_react_logic_with_tools(
                               active_mcp_client=client,
                               node_config=node_config, # Pass node_config
                               system_message_content=system_message_content, # Pass messages
                               human_message_content=human_message_content,
                               llm=llm, # Pass llm
                               tool_registry=tool_registry, # Pass registry
                               internal_tools_list=internal_tools_list # Pass internal tools list
                           )
                 except Exception as e:
                      console.print(f"Error within MCP client context execution: {e}", style="red")
                      console.print(f"Traceback:\n{traceback.format_exc()}", style="red")
                      if react_step_result is None: 
                          react_step_result = {
                              "output": f"Error during MCP context: {e}",
                              "_error": {
                                  'node': node_name,
                                  'message': f"Error within MCP client context execution: {e}",
                                  'type': 'MCPContextError',
                                  'user_message': f"I apologize, but I encountered an error while using external tools. The error was: {e}",
                                  'recoverable': False
                              }
                          }

            else: # Tools requested, but no MCP client
                 print_output("No MCP client provided, using only internal tools for ReAct...")
                 try:
                      # *** PASS ALL REQUIRED ARGUMENTS HERE TOO ***
                      react_step_result = await run_react_logic_with_tools(
                           active_mcp_client=None, # Pass None explicitly
                           node_config=node_config,
                           system_message_content=system_message_content,
                           human_message_content=human_message_content,
                           llm=llm,
                           tool_registry=tool_registry,
                           internal_tools_list=internal_tools_list
                      )
                 except Exception as e:
                     console.print(f"Error during internal tool ReAct execution: {e}", style="red")
                     console.print(f"Traceback:\n{traceback.format_exc()}", style="red")
                     if react_step_result is None: 
                         react_step_result = {
                             "output": f"Error during internal tool operation: {e}",
                             "_error": {
                                 'node': node_name,
                                 'message': f"Error during internal tool ReAct execution: {e}",
                                 'type': 'InternalToolError',
                                 'user_message': f"I apologize, but I encountered an error while using internal tools. The error was: {e}",
                                 'recoverable': False
                             }
                         }

            final_result_text = None
            # Check if react_step_result is a dict with 'output' key and it's a string
            if isinstance(react_step_result, dict) and isinstance(react_step_result.get("output"), str):
                final_result_text = react_step_result["output"]
            elif isinstance(react_step_result, str): # Handle case where result is already error string
                final_result_text = react_step_result
            final_output_for_state = react_step_result

            if parser and final_result_text and isinstance(final_result_text, str) and not final_result_text.lower().startswith("error:"):
                globals.logger.info(f"Attempting to extract result from ReAct text and format into JSON schema for {node_name}...")
                format_instructions = ""; schema_valid_for_format = False
                try:
                    if hasattr(parser.pydantic_object, 'model_json_schema'): format_instructions = parser.get_format_instructions(); schema_valid_for_format = True
                    else: console.print(f"Error: Pydantic V2 model lacks .model_json_schema() for {node_name}", style="red")

                except Exception as e: console.print(f"[{node_name}] Unexpected error getting format instructions: {e}", style="red")

                if schema_valid_for_format:
                    formatting_prompt = (
                        f"Analyze the following text, which is the result (observation) from a previous tool execution. "
                        f"Your goal is to extract the single, most salient value or piece of information requested by the desired output schema "
                        f"(e.g., if the schema asks for 'execution_result', extract the primary outcome like a number, status, or summary; "
                        f"if it asks for 'weather_summary', extract that). "
                        f"Then, format ONLY this extracted value into a JSON object conforming strictly to the schema provided below. "
                        f"Respond ONLY with the JSON object, without any markdown formatting like ```json or explanations.\n\n"
                        f"Tool Observation Text:\n```\n{final_result_text}\n```\n\n"
                        f"Required JSON Schema (extract the relevant value to fit this):\n{format_instructions}"
                    )

                    formatting_messages = [HumanMessage(content=formatting_prompt)]
                    max_format_retries = 2; format_retry_count = 0; parsed_formatted_output = None

                    while format_retry_count < max_format_retries:
                        try:
                            globals.logger.info(f"Attempt {format_retry_count + 1}/{max_format_retries} for JSON formatting...")
                            formatting_response = await llm.ainvoke(formatting_messages)
                            cleaned_content = formatting_response.content.strip().removeprefix("```json").removesuffix("```").strip()
                            parsed_formatted_output = parser.parse(cleaned_content) # Use the node's parser
                            globals.logger.info("Successfully extracted and formatted ReAct result to JSON.")
                            break # Success
                        except (OutputParserException, ValidationError, json.JSONDecodeError) as parse_error:
                            format_retry_count += 1; error_detail = f"{type(parse_error).__name__}: {parse_error}"
                            console.print(f"Warning: Formatting LLM call failed parsing/validation (Attempt {format_retry_count}/{max_format_retries}): {error_detail}", style="yellow")
                            if format_retry_count >= max_format_retries:
                                 console.print(f"Failed to format ReAct result to JSON after retries. Storing raw text result.", style="red")
                                 parsed_formatted_output = final_result_text # Fallback to raw text
                                 break
                            feedback = f"Formatting failed: {error_detail}. Extract the single key result value from the observation text and provide ONLY the valid JSON object matching the schema."
                            formatting_messages = [formatting_messages[0], HumanMessage(content=feedback)] # Add feedback for retry
                        except Exception as format_error:
                             console.print(f"Unexpected error during result formatting LLM call: {format_error}", style="red")
                             parsed_formatted_output = final_result_text # Fallback
                             break
                    final_output_for_state = parsed_formatted_output
                else:
                     console.print(f"Warning: Schema invalid or unavailable for formatting. Using raw ReAct result for {node_name}.", style="yellow")
            else:
                print_output(f"Skipping formatting for {node_name} (No parser, no text result, or result was error).")
        else:
            globals.logger.info(f"Using Direct LLM Call for {node_name}")
            if not parser: console.print(f"Warning: No parser for direct call node {node_name}. Expecting raw text.", style="yellow")
            prompt_to_llm = human_message_content; format_instructions = ""; schema_valid_for_format_direct = False
            if parser:
                try:
                    if hasattr(parser.pydantic_object, 'model_json_schema'): format_instructions = parser.get_format_instructions(); schema_valid_for_format_direct = True
                    else: console.print(f"Error: Pydantic V2 model lacks .model_json_schema() for {node_name}", style="red")

                    if schema_valid_for_format_direct: # Only add instructions if valid
                         prompt_to_llm += f"\n\nIMPORTANT: Respond ONLY with a JSON object conforming to the schema below. Do not include ```json ``` markers or any text outside the JSON object itself.\nSCHEMA:\n{format_instructions}"
                    else:
                         # If instructions failed, proceed without them? Or raise? Let's proceed without for now.
                         console.print(f"Warning: Cannot get format instructions for {node_name}. Asking for text.", style="yellow")
                         # Do not add JSON instructions to prompt_to_llm

                except Exception as e: console.print(f"[{node_name}] Unexpected error getting format instructions: {e}", style="red")

            messages: List[BaseMessage] = [SystemMessage(content=system_message_content), HumanMessage(content=prompt_to_llm)]
            max_retries = node_config.get('max_retries', 3); retry_count = 0
            llm_response_content: Optional[str] = None; direct_call_parsed_output: Union[BaseModel, str, None] = None
            last_error = None

            while retry_count < max_retries:
                 try:
                      globals.logger.info(f"Attempt {retry_count + 1}/{max_retries} for direct LLM call...")
                      llm_response = await llm.ainvoke(messages)
                      llm_response_content = llm_response.content if hasattr(llm_response, 'content') else str(llm_response)

                      # Only attempt parsing if parser exists AND format instructions were likely added
                      if parser and schema_valid_for_format_direct:
                            cleaned_content = llm_response_content.strip().removeprefix("```json").removesuffix("```").strip()
                            if not cleaned_content: raise OutputParserException("Received empty response.")
                            direct_call_parsed_output = parser.parse(cleaned_content)
                            globals.logger.info("Successfully parsed and validated JSON output.")
                      else: # No parser or format instructions not added, use raw text
                            direct_call_parsed_output = llm_response_content
                            if parser: globals.logger.info("Received raw text output (format instructions missing/failed).")
                            else: globals.logger.info("Received raw text output (no parser defined).")
                      break # Success
                 except (OutputParserException, ValidationError, json.JSONDecodeError) as e:
                      retry_count += 1
                      last_error = e
                      error_detail = f"{type(e).__name__}: {e}"
                      globals.logger.error(f"Output parsing/validation failed (Attempt {retry_count}/{max_retries}): {error_detail}")
                      
                      if retry_count >= max_retries:
                          # We've exhausted retries, prepare for graceful termination
                          break
                      
                      # Generate specific feedback for the LLM based on the error
                      feedback = create_error_feedback(e, node_name)
                      print_output(f"Providing feedback to LLM: {feedback[:100]}...")
                      
                      # Add the feedback as a new message for the next attempt
                      messages = messages[:2] + [HumanMessage(content=feedback)]
                 except Exception as e:
                      console.print(f"Unexpected LLM call error: {e}", style="red")
                      last_error = e
                      break

            if direct_call_parsed_output is not None:
                final_output_for_state = direct_call_parsed_output
            elif last_error is not None:
                # Handle the failure after exhausting retries
                new_state = handle_node_failure(new_state, node_name, last_error, max_retries)
                return new_state  # Return early with error state
            else:
                final_output_for_state = f"Error: Max retries reached. Last response maybe: {llm_response_content}"


        # --- Update State & Print ---
        if final_output_for_state is None:
             console.print(f"Error: No output was processed or error captured for node {node_name}. Returning original state.", style="red")
             # Keep new_state as the initial copy
        else:
             new_state = update_state(new_state, final_output_for_state, node_config)

        print_user_messages(new_state, node_config)
        # Conditionally print prompt only for direct call path if enabled
        if not use_tools and node_config.get('print_prompt', False):
            print_chat_prompt(ChatPromptTemplate(messages=messages), node_config)
        print_state(new_state, node_config)

        # Return the potentially updated state dictionary copy
        return new_state

    return node_function

def print_user_messages(state: Dict[str, Any], node_config: Dict[str, Any]):
    """Prints messages defined in node_config, substituting state variables."""
    user_message_fields = node_config.get('user_message', [])
    if not isinstance(user_message_fields, list):
         print(f"Warning: 'user_message' in node config is not a list.", style="yellow")
         return
    for field_or_template in user_message_fields:
        if isinstance(field_or_template, str):
            if field_or_template in state and state[field_or_template] is not None:
                 print_ai(str(state[field_or_template])) # Print the value directly
                 continue

            try:
                 formatted_msg = format_prompt(field_or_template, state, node_config)
                 print_ai(formatted_msg)
            except Exception as e:
                 console.print(f"Error formatting user message '{field_or_template}': {e}", style="red")
                 print_ai(field_or_template) # Print original on error
        else:
             console.print(f"Warning: Item in 'user_message' is not a string: {field_or_template}", style="yellow")

def print_chat_prompt(chat_prompt: BasePromptTemplate, node_config: Dict[str, Any]):
    """Prints formatted chat prompt messages if enabled in config."""
    print_prompt_flag = node_config.get('print_prompt', False)
    node_name = node_config.get('name', 'Unknown Node')
    if print_prompt_flag and hasattr(chat_prompt, 'messages'):
        console.print(f"ChatPrompt for {node_name}:", style="blue")
        try:
            for i, message in enumerate(chat_prompt.messages, 1):
                role = "Unknown"; content = str(message); color = "red";
                if isinstance(message, SystemMessage): role = "System"; content = message.content; color = "magenta"
                elif isinstance(message, HumanMessage): role = "Human"; content = message.content; color = "yellow"
                content_preview = content
                print(f"  {color}{role} {i}: {content_preview}")
        except Exception as e: console.print(f"Error printing chat prompt: {e}", style="red")
        print("-" * 20)


def print_state(state: Dict[str, Any], node_config: Dict[str, Any]):
    """Prints the current state dictionary if enabled in config."""
    print_state_flag = node_config.get('print_state', False)
    node_name = node_config.get('name', 'Unknown Node')
    if print_state_flag:
        print_output(f"Current State after {node_name}:")
        try:
            state_str = json.dumps(state, indent=2, default=lambda o: f"<non-serializable: {type(o).__name__}>")
            console.print(f"{state_str}", style="green")
        except Exception as e: print(f"Could not serialize state to JSON. Error: {e}. Raw state:\n{state}", style="red")
        print("-" * 20)
