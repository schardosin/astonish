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
from astonish.core.format_handler import execute_tool
from astonish.core.utils import request_tool_execution
from langchain_core.messages import SystemMessage, HumanMessage, BaseMessage
from langchain_core.prompts import ChatPromptTemplate, BasePromptTemplate


def clean_and_fix_json(content: str) -> str:
    """
    Clean and fix JSON content to make it parseable.
    
    Args:
        content: The content to clean and fix
        
    Returns:
        The cleaned and fixed content
    """
    if not content or not isinstance(content, str):
        return ""

    # 1. Attempt to find a JSON object or array using a regex.
    # This is useful if the JSON is embedded within other text.
    # Regex to find content starting with { or [ and ending with } or ] respectively,
    # attempting to match balanced braces/brackets. This is a common heuristic.
    # Note: A perfect regex for balanced structures is complex; this is an approximation.
    json_match = re.search(r"(\{((?:[^{}]*|\{(?:[^{}]*|\{[^{}]*\})*\})*)\}|\[((?:[^\[\]]*|\[(?:[^\[\]]*|\[[^\[\]]*\])*\])*)\])", content)
    
    potential_json_str = ""
    if json_match:
        potential_json_str = json_match.group(0)
        try:
            # Try to parse to see if it's valid JSON
            json.loads(potential_json_str)
            # If it parses, we assume this is our primary JSON content
            return potential_json_str.strip() 
        except json.JSONDecodeError:
            # If parsing fails, it might be that this regex match wasn't the actual JSON,
            # or it's malformed. We'll proceed to other cleaning steps.
            potential_json_str = "" # Reset, as this wasn't clean JSON

    # 2. If no clear JSON structure was extracted via regex,
    #    or if the extracted part was not valid, try cleaning common markdown wrappers
    #    from the original content. This handles cases where the entire response
    #    is meant to be JSON but is wrapped.
    
    cleaned_content = content.strip()
    
    # Remove ```json ... ``` or ``` ... ``` wrappers if they enclose the whole content
    if cleaned_content.startswith("```json") and cleaned_content.endswith("```"):
        cleaned_content = cleaned_content[len("```json") : -len("```")].strip()
    elif cleaned_content.startswith("```") and cleaned_content.endswith("```"):
        # If it's just ``` ... ```, remove them if the inner content looks like JSON
        temp_inner = cleaned_content[len("```") : -len("```")].strip()
        if temp_inner.startswith("{") or temp_inner.startswith("["):
            cleaned_content = temp_inner
        # else, it might be a non-JSON code block, so we probably shouldn't strip the ```
        # and expect the parser to fail if it was expecting JSON.
        # However, if potential_json_str was populated from regex and failed,
        # cleaned_content is still the original content here. This part might need refinement
        # based on how often LLMs return non-JSON in ``` when JSON is expected.
        # For now, if regex found something, use that attempt; otherwise, use original cleaned_content.
        
    if potential_json_str and (not cleaned_content.startswith("{") and not cleaned_content.startswith("[")):
        # This means regex found something, but it wasn't valid, and outer ``` stripping didn't apply
        # or also resulted in non-JSON. This case is ambiguous.
        # Let's prioritize the result of ``` stripping if it looks like JSON.
        if cleaned_content.startswith("{") or cleaned_content.startswith("["):
             return cleaned_content
        # If regex found something that looked like JSON but wasn't, and ``` stripping didn't help,
        # it's better to return the original stripped content and let the parser fail.
        # However, for this specific function, we want the *most JSON-like* part.
        # If no valid JSON was found, return the most "stripped" version assuming it was meant to be JSON.
        return cleaned_content if (cleaned_content.startswith("{") or cleaned_content.startswith("[")) else potential_json_str

    # If after all this, cleaned_content is still not starting with { or [,
    # and potential_json_str (from regex) didn't validate, then it's unlikely to be the JSON we want.
    # Return the most processed version that looks like JSON.
    if cleaned_content.startswith("{") or cleaned_content.startswith("["):
        return cleaned_content
    elif potential_json_str: # from regex, even if it didn't validate alone
        return potential_json_str

    return content.strip() # Fallback to just stripped original content

class ToolDefinition(TypedDict):
    name: str
    description: str
    input_type: str
    input_schema_definition: Optional[Any]
    tool_executor: Callable[..., Coroutine[Any, Any, str]]
    tool_instance: Optional[BaseTool]

class ReactStepOutput(TypedDict):
    """Structured output for a single ReAct planning step."""
    status: str
    tool: Optional[str]
    tool_input: Optional[str]
    answer: Optional[str]
    thought: Optional[str]
    raw_response: str
    message_content_for_history: Optional[str]

async def run_react_planning_step(
    input_question: str,
    agent_scratchpad: str,
    system_message_content: str,
    llm: BaseLanguageModel,
    tool_definitions: List[ToolDefinition],
    node_name: str = "ReAct Planner",
    print_prompt: bool = False,
) -> ReactStepOutput:
    """
    Performs one step of ReAct planning using a STRING scratchpad.
    """
    globals.logger.info(f"[{node_name}] Running ReAct planning step...")
    default_error_output = ReactStepOutput(
        status='error', tool=None, tool_input=None, answer=None, thought=None,
        raw_response="Planner failed unexpectedly." # Removed message_content_for_history
    )
    try:
        react_instructions_template = create_custom_react_prompt_template(tool_definitions)
        prompt_str_template = system_message_content + "\n\n" + react_instructions_template
        custom_react_prompt = ChatPromptTemplate.from_template(prompt_str_template)
        chain: Runnable = custom_react_prompt | llm

        invoke_input = {
            "input": input_question,
            "agent_scratchpad": agent_scratchpad
        }

        globals.logger.debug(f"[{node_name}] Invoking LLM with scratchpad:\n{agent_scratchpad}")
        
        response_chunks = []
        async for chunk in chain.astream(invoke_input):
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
            
        response_text = llm_response.content if hasattr(llm_response, 'content') else str(llm_response)
        globals.logger.info(f"[{node_name}] LLM Raw Planning Response:\n{response_text}")
        if print_prompt:
            print_output(invoke_input, "green")
            #print_chat_prompt(ChatPromptTemplate(messages=messages), node_config)
            #else: print_output(f"Cannot print prompt for {node_name}, 'messages' not defined.", "yellow")


        thought_blocks = re.findall(r"^\s*Thought:\s*(.*?)(?=(?:\n\s*(?:Action:|Final Answer:|$)))", response_text, re.DOTALL | re.MULTILINE)
        thought_text = thought_blocks[-1].strip() if thought_blocks else None

        action_match = re.search(r"^\s*Action:\s*([\w.-]+)", response_text, re.MULTILINE | re.IGNORECASE)
        input_string_from_llm = ""

        action_input_line_match = re.search(r"^\s*Action Input:\s*(.*?)(?:\nObservation:|\Z)", response_text, re.DOTALL | re.MULTILINE | re.IGNORECASE)
        if action_input_line_match:
            raw_input_line = action_input_line_match.group(1).strip()

            # Try to extract JSON content, or clean and fix the raw input
            json_match = re.match(r"^```json\s*(\{.*?\})\s*```$", raw_input_line, re.DOTALL) or re.match(r"^(\{.*?\})\s*$", raw_input_line, re.DOTALL)
            if json_match: 
                input_string_from_llm = json_match.group(1)
                globals.logger.debug(f"[{node_name}] Extracted JSON Action Input: {input_string_from_llm}")
            else:
                # If it looks like it might be JSON but didn't match the regex patterns, try to clean and fix it
                if '{' in raw_input_line and '}' in raw_input_line:
                    input_string_from_llm = clean_and_fix_json(raw_input_line)
                    globals.logger.debug(f"[{node_name}] Cleaned and fixed JSON Action Input: {input_string_from_llm}")
                else:
                    input_string_from_llm = raw_input_line
                    globals.logger.debug(f"[{node_name}] Extracted String Action Input: {input_string_from_llm}")

        if action_match:
            tool_name = action_match.group(1).strip()
            globals.logger.info(f"[{node_name}] LLM planned Action: {tool_name}")

            return ReactStepOutput(
                status='action', tool=tool_name, tool_input=input_string_from_llm,
                answer=None, thought=thought_text, raw_response=response_text,
            )
        elif "Final Answer:" in response_text:
            final_answer_text = response_text.split("Final Answer:")[-1].strip()
            globals.logger.info(f"[{node_name}] LLM provided Final Answer.")
            return ReactStepOutput(
                status='final_answer', tool=None, tool_input=None, answer=final_answer_text,
                thought=thought_text, raw_response=response_text,
            )
        else:
            error_message = f"LLM response did not contain 'Action:' or 'Final Answer:'."
            globals.logger.error(f"[{node_name}] Parsing Error: {error_message}")
            return ReactStepOutput(
                status='error', tool=None, tool_input=None, answer=None, thought=thought_text,
                raw_response=response_text,
            )

    except Exception as planning_error:
        error_message = f"Critical error during ReAct planning step: {type(planning_error).__name__}: {planning_error}"
        console.print(f"[{node_name}] {error_message}", style="red")
        console.print(f"Traceback:\n{traceback.format_exc()}", style="red")
        output = default_error_output.copy()
        output['raw_response'] = f"Error: {error_message}\n{traceback.format_exc()}"
        return output

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
        field_type = Any
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
    elif isinstance(output, str):
        new_state[output_field_name] = output
    elif output is None:
        globals.logger.warning("Received None output for state update.")
    
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
    Includes the {agent_scratchpad} placeholder for text-based history.
    """
    tool_strings = []
    for tool_def in tools_definitions:
        input_desc = ""
        input_type = tool_def.get('input_type', 'STRING')
        schema_def = tool_def.get('input_schema_definition')

        if input_type == 'JSON_SCHEMA':
            schema_desc_str = "[Schema not provided or invalid]"
            properties = {}
            required_list = []
            if schema_def:
                try:
                    schema_json_props = None
                    if isinstance(schema_def, type) and issubclass(schema_def, BaseModel):
                        schema_json_props = schema_def.model_json_schema()
                    elif isinstance(schema_def, dict) and "properties" in schema_def:
                        schema_json_props = schema_def

                    if schema_json_props:
                        properties = schema_json_props.get("properties", {})
                        required_list = schema_json_props.get("required", [])

                    if properties:
                        prop_details_list = []
                        for name, details in properties.items():
                            prop_type = details.get('type', 'unknown')
                            prop_desc = details.get('description', '')
                            prop_enum = details.get('enum')
                            prop_default = details.get('default')

                            is_required = name in required_list
                            req_marker = " (required)" if is_required else ""

                            detail_str = f"'{name}' ({prop_type}{req_marker})"
                            if prop_desc:
                                detail_str += f": {prop_desc}"
                            if prop_enum:
                                # Ensure enum values are correctly quoted if strings
                                enum_strs = [f'"{v}"' if isinstance(v, str) else str(v) for v in prop_enum]
                                detail_str += f" (must be one of: [{', '.join(enum_strs)}])" # List allowed values
                            if prop_default is not None:
                                 detail_str += f" (default: {json.dumps(prop_default)})" # Show default

                            # Escape curly braces within the description details for f-string formatting
                            prop_details_list.append(detail_str.replace("{", "{{").replace("}", "}}"))

                        schema_desc_str = "; ".join(prop_details_list)
                    else:
                         schema_desc_str = "[No properties found in schema]"
                except Exception as e:
                    schema_desc_str = f"[Error extracting schema details]"

            input_desc = f"Input Type: Requires a JSON object string with properties: {schema_desc_str}. IMPORTANT: Generate a valid JSON string containing required properties and only essential optional properties based on the user request."

        elif input_type == 'STRING':
             input_desc = "Input Type: Plain String"

             if schema_def and isinstance(schema_def, str):
                  # Escape curly braces in the example string
                  escaped_example = schema_def.replace("{", "{{").replace("}", "}}")
                  input_desc += f". Expected format/example: {escaped_example}"
        else:
             input_desc = f"Input Type: {input_type}"
             if schema_def and isinstance(schema_def, str):
                 # Escape curly braces
                 input_desc += f". Tool expects: {schema_def.replace('{','{{').replace('}','}}')}"

        tool_name = tool_def.get('name', 'UnnamedTool')
        tool_description = tool_def.get('description', 'No description.')
        # Escape braces in tool name and description just in case, though less likely needed
        safe_tool_name = tool_name.replace("{", "{{").replace("}", "}}")
        safe_tool_description = tool_description.replace("{", "{{").replace("}", "}}")
        tool_strings.append(f"- {safe_tool_name}: {safe_tool_description} {input_desc}")

    formatted_tools = "\n".join(tool_strings) if tool_strings else "No tools available."
    tool_names = ", ".join([tool_def['name'] for tool_def in tools_definitions]) if tools_definitions else "N/A"
    safe_tool_names = tool_names.replace("{", "{{").replace("}", "}}") # Escape braces in tool names list

    template = f"""Answer the following questions as best you can. You have access to the following tools:
        {formatted_tools}

        Use the following format STRICTLY:

        Question: the input question you must answer
        Thought: Analyze the question and available tools. Determine the single best Action to take. Identify the essential arguments required by that Action's schema based on the Question and the tool description (especially paying attention to property descriptions, required fields, and allowed values like enums). Use sensible defaults for optional arguments unless the Question specifies otherwise.
        Action: the action to take, must be one of [{safe_tool_names}]
        Action Input: Provide the exact input required for the selected Action. If Input Type requires a JSON object string, generate a *single, valid JSON object string* containing ONLY the essential properties identified in your Thought process, matching the required types and allowed values (enums) mentioned in the tool description. If Input Type is STRING, provide the plain string. Do NOT add explanations before or after the Action Input line. IMPORTANT: After writing the Action Input line, STOP generating immediately. Wait for the Observation.
        Observation: the result of the action
        ... (this Thought/Action/Action Input/Observation can repeat N times)
        Thought: I now know the final answer based on my thoughts and observations.
        Final Answer: the final answer to the original input question. It must be the extraction of the content of the last Observation, without format modification, unless requested in the prompt.

        Begin!

        Question: {{input}}
        {{agent_scratchpad}}"""

    return template

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
                 formatted_or_original_text = await _format_final_output_with_llm( final_result_text_from_react, parser, llm, node_name )
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

def format_react_step_for_scratchpad(
    thought: Optional[str],
    action: Optional[str],
    action_input: Optional[str],
    observation: str # Observation is always required after an action
    ) -> str:
    """Formats a completed Thought-Action-Input-Observation step for the scratchpad string."""
    # Ensure consistent spacing and newlines, similar to standard ReAct examples
    scratchpad_entry = "\n" # Start with a newline for separation
    if thought:
        # Ensure thought doesn't already start with "Thought:" from LLM output
        thought = re.sub(r"^\s*Thought:\s*", "", thought).strip()
        scratchpad_entry += f"Thought: {thought}\n"
    if action:
         scratchpad_entry += f"Action: {action}\n"
    if action_input is not None: # Can be empty string for actions without input
         scratchpad_entry += f"Action Input: {action_input}\n"
    # Observation comes from the tool execution result or denial
    scratchpad_entry += f"Observation: {observation}\n"
    return scratchpad_entry

async def _format_final_output_with_llm(
    final_text: str,
    parser: PydanticOutputParser,
    llm: BaseLanguageModel,
    node_name: str
) -> Union[BaseModel, str, Dict]: # Allow Dict if parser outputs dict
    globals.logger.info(f"Attempting to format final result into JSON schema for {node_name}...")
    format_instructions = ""
    schema_valid_for_format = False
    try:
        if hasattr(parser.pydantic_object, 'model_json_schema'): 
            format_instructions = parser.get_format_instructions()
            schema_valid_for_format = True
        else: 
            console.print(f"Error: Pydantic V2 model lacks .model_json_schema() for {node_name}", style="red")
    except Exception as e: 
        console.print(f"[{node_name}] Unexpected error getting format instructions: {e}", style="red")
    
    if not schema_valid_for_format: 
        console.print(f"Warning: Schema invalid or unavailable for formatting. Using raw ReAct result for {node_name}.", style="yellow")
        return final_text
    formatting_prompt = ( f"Analyze the following text, which is the final answer from a reasoning process. Your goal is to extract the single, most salient value or piece of information requested by the desired output schema (e.g., if the schema asks for 'execution_result', extract the primary outcome like a number, status, or summary; if it asks for 'weather_summary', extract that). Then, format ONLY this extracted value into a JSON object conforming strictly to the schema provided below. Respond ONLY with the JSON object, without any markdown formatting like ```json or explanations.\n\nFinal Answer Text:\n```\n{final_text}\n```\n\nRequired JSON Schema (extract the relevant value to fit this):\n{format_instructions}" )
    formatting_messages = [HumanMessage(content=formatting_prompt)]
    max_format_retries = 2
    format_retry_count = 0
    parsed_formatted_output = None
    while format_retry_count < max_format_retries:
        try:
            globals.logger.info(f"Attempt {format_retry_count + 1}/{max_format_retries} for JSON formatting...")
            response_chunks = []
            async for chunk in llm.astream(formatting_messages):
                response_chunks.append(chunk)
            
            # Merge the chunks
            if response_chunks:
                # Create a complete response by concatenating all chunk contents
                full_content = ""
                for chunk in response_chunks:
                    if hasattr(chunk, 'content'):
                        full_content += chunk.content
                
                # Use the last chunk as a template for the response object
                formatting_response = response_chunks[-1]
                if hasattr(formatting_response, 'content'):
                    formatting_response.content = full_content
            else:
                formatting_response = None
                
            cleaned_content = clean_and_fix_json(formatting_response.content)
            if not cleaned_content: raise OutputParserException("Received empty formatted response.")
            parsed_formatted_output = parser.parse(cleaned_content)
            globals.logger.info("Successfully extracted and formatted final result to JSON.")
            return parsed_formatted_output
        except (OutputParserException, ValidationError, json.JSONDecodeError) as parse_error:
            format_retry_count += 1
            error_detail = f"{type(parse_error).__name__}: {parse_error}"
            console.print(f"Warning: Formatting LLM call failed parsing/validation (Attempt {format_retry_count}/{max_format_retries}): {error_detail}", style="yellow")
            if format_retry_count >= max_format_retries: 
                console.print(f"Failed to format final result to JSON after retries. Storing raw text result.", style="red")
                return final_text
            feedback = f"Formatting failed: {error_detail}. Extract the single key result value from the text and provide ONLY the valid JSON object matching the schema."
            formatting_messages = [formatting_messages[0], HumanMessage(content=feedback)]
        except Exception as format_error: 
            console.print(f"Unexpected error during final result formatting LLM call: {format_error}", style="red")
            return final_text
    return final_text

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
                role = "Unknown"
                content = str(message)
                color = "red"
                
                if isinstance(message, SystemMessage): 
                    role = "System"
                    content = message.content
                    color = "magenta"
                elif isinstance(message, HumanMessage): 
                    role = "Human"
                    content = message.content
                    color = "yellow"
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
