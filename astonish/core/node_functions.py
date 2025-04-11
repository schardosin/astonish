import json
import os
import warnings
import re # For parsing ReAct text
import asyncio # Ensure asyncio is imported
import traceback # For printing full tracebacks
from importlib import resources
from typing import TypedDict, Union, Optional, get_args, get_origin, Dict, Any, List, Callable, Coroutine, Type

# --- LangChain Core Imports ---
from langchain_core.messages import SystemMessage, HumanMessage, BaseMessage
from langchain_core.prompts import ChatPromptTemplate, BasePromptTemplate, PromptTemplate
from langchain_core.tools import BaseTool
from langchain_core.runnables import Runnable # For type hinting chain
from langchain_core.language_models.base import BaseLanguageModel # For type hinting LLM

# --- LangChain Community/Other Imports ---
from langchain import hub
from langchain.output_parsers import PydanticOutputParser
from langchain.schema import OutputParserException

# --- Pydantic Imports ---
try:
    from pydantic.v1 import Field, ValidationError, create_model, BaseModel
    PYDANTIC_V2 = False
except ImportError:
    try:
        from pydantic import Field, ValidationError, create_model, BaseModel
        PYDANTIC_V2 = True
    except ImportError:
        raise ImportError("Pydantic not found. Please install pydantic.")

# --- LangSmith Warning Handling ---
# from langsmith.client import LangSmithMissingAPIKeyWarning
# Setup env vars or ignore warnings as needed (see previous responses)
# os.environ["LANGCHAIN_TRACING_V2"] = "false"
# os.environ["LANGCHAIN_API_KEY"] = ""
# os.environ["LANGCHAIN_ENDPOINT"] = ""
# warnings.filterwarnings("ignore", category=LangSmithMissingAPIKeyWarning)

# --- Colorama for printing ---
from colorama import Fore, Style

# --- Local Project Imports ---
try:
    from astonish.tools.internal_tools import tools as internal_tools_list
    if not isinstance(internal_tools_list, list): internal_tools_list = []
except ImportError: internal_tools_list = []

from astonish.core.llm_manager import LLMManager
import astonish.globals as globals
from astonish.core.utils import format_prompt, print_ai, print_output, print_dict

# --- Type Definitions ---
class ToolDefinition(TypedDict):
    name: str
    description: str
    input_type: str
    input_schema_definition: Optional[Any]
    tool_executor: Callable[..., Coroutine[Any, Any, str]]
    tool_instance: Optional[BaseTool]

# --- Updated Helper Function ---

def create_output_model(output_model_config: Dict[str, str]) -> Optional[Type[BaseModel]]:
    """
    Creates a Pydantic model dynamically, handling lowercase type names from YAML
    and complex/generic types using eval.
    """
    if not output_model_config or not isinstance(output_model_config, dict):
        return None # No model needed if no config

    fields = {}
    # Lowercase keys map YAML strings to Python types for direct lookup
    type_lookup = {
        "str": str,
        "int": int,
        "float": float,
        "bool": bool,
        "any": Any,
        "list": List, # Map lowercase 'list' to typing.List
        "dict": Dict, # Map lowercase 'dict' to typing.Dict
        # Add other simple lowercase types used in YAML if needed
    }
    # Context for eval when dealing with generics like List[str], Optional[int]
    # Uses standard Python type names as expected by eval
    eval_context = {
         "Union": Union, "Optional": Optional, "List": List, "Dict": Dict,
         "str": str, "int": int, "float": float, "bool": bool, "Any": Any,
         "BaseModel": BaseModel
         # Add any custom Pydantic models or types used in generics here
         }

    for field_name, field_type_str in output_model_config.items():
        field_type = Any # Default to Any on error
        try:
            # Normalize the input type string from YAML
            normalized_type_str = field_type_str.strip()
            normalized_type_lower = normalized_type_str.lower()

            # 1. Try direct lookup for simple types (list, str, int...) using lowercase key
            if normalized_type_lower in type_lookup:
                field_type = type_lookup[normalized_type_lower]

            # 2. If not found directly, assume it might be a complex generic (List[str], Optional[int], etc.) or custom type and use eval
            elif any(c in normalized_type_str for c in ['[', '|', 'Optional', 'Union']): # Check for indicators of complex types
                try:
                     # Eval needs the context with standard Python type names (List, str, etc.)
                     # It evaluates the original string (e.g., "List[str]")
                     field_type = eval(normalized_type_str, globals(), eval_context)
                except NameError:
                     print(f"{Fore.YELLOW}Warning: Eval failed to find type components within '{normalized_type_str}'. Defaulting field '{field_name}' to Any.{Style.RESET_ALL}")
                     field_type = Any
                except Exception as e_eval:
                     print(f"{Fore.RED}Error evaluating complex type '{normalized_type_str}': {e_eval}{Style.RESET_ALL}")
                     field_type = Any
            else:
                # If it wasn't in direct lookup and doesn't look complex, treat as unknown
                 print(f"{Fore.YELLOW}Warning: Unknown or non-generic type '{normalized_type_str}' for field '{field_name}', defaulting to Any.{Style.RESET_ALL}")
                 field_type = Any


            # --- Post-processing for Optional/Union ---
            # This should run *after* field_type is determined by lookup or eval
            if field_type is not Any: # Avoid processing Any
                origin = get_origin(field_type)
                args = get_args(field_type)
                # Check specifically for Union containing NoneType
                if origin is Union and type(None) in args:
                    non_none_args = tuple(arg for arg in args if arg is not type(None))
                    if len(non_none_args) == 1: field_type = Optional[non_none_args[0]]
                    elif len(non_none_args) > 1: field_type = Optional[Union[non_none_args]]
                    else: field_type = Optional[type(None)] # Only None was in Union

            # Assign the determined (and potentially Optional/Union adjusted) type
            fields[field_name] = (field_type, Field(description=f"{field_name} field"))

        except Exception as e:
            # Catch errors during the processing of a single field
            print(f"{Fore.RED}Error processing field '{field_name}' with type string '{field_type_str}': {e}{Style.RESET_ALL}")
            fields[field_name] = (Any, Field(description=f"{field_name} field (processing error)"))

    # --- Create Model ---
    model_name = f"DynamicOutputModel_{abs(hash(json.dumps(output_model_config, sort_keys=True)))}"
    try:
         Model = create_model(model_name, **fields)
         #print(f"Successfully created model '{model_name}' with fields: {fields}") # Debug print
         return Model
    except Exception as e:
         print(f"{Fore.RED}Failed to create Pydantic model '{model_name}': {e}{Style.RESET_ALL}")
         return None

# update_state (Use version from Response #19 - handles dict['output'])
def update_state(state: Dict[str, Any], output: Union[BaseModel, str, Dict, None], node_config: Dict[str, Any]) -> Dict[str, Any]:
    new_state = state.copy()
    output_field_name = next(iter(node_config.get('output_model', {})), 'agent_final_answer')
    if isinstance(output, BaseModel):
        if PYDANTIC_V2: new_state.update(output.model_dump(exclude_unset=True))
        else: new_state.update(output.dict(exclude_unset=True)) # type: ignore
    elif isinstance(output, dict) and "output" in output:
         new_state[output_field_name] = output.get("output", "")
    elif isinstance(output, str):
        new_state[output_field_name] = output
    elif output is None: print(f"{Fore.YELLOW}Warning: Received None output for state update.{Style.RESET_ALL}")
    limit_counter_field = node_config.get('limit_counter_field'); limit = node_config.get('limit')
    if limit_counter_field and limit:
        counter = new_state.get(limit_counter_field, 0) + 1
        if counter > limit: counter = 1
        new_state[limit_counter_field] = counter
    return new_state

# create_custom_react_prompt_template (Use version from Response #19 - escapes braces)
def create_custom_react_prompt_template(tools_definitions: List[ToolDefinition]) -> str:
    """
    Generates a ReAct prompt string including tool input requirements,
    using a textual description of schema properties instead of escaped JSON.
    Removes the problematic literal JSON example from instructions.
    """
    tool_strings = []
    for tool_def in tools_definitions:
        input_desc = ""; input_type = tool_def.get('input_type', 'STRING'); schema_def = tool_def.get('input_schema_definition')

        if input_type == 'JSON_SCHEMA':
            schema_desc_str = "[Schema not provided or invalid]"
            properties = {}
            if schema_def:
                try:
                    # Extract properties dict
                    if isinstance(schema_def, type) and issubclass(schema_def, BaseModel):
                        schema_json = schema_def.model_json_schema() if PYDANTIC_V2 else schema_def.schema() # type: ignore
                        properties = schema_json.get("properties", {})
                    elif isinstance(schema_def, dict): properties = schema_def.get("properties", {})

                    # Create textual list of properties (escaped for f-string)
                    if properties:
                        prop_list = [f"{name} ({details.get('type', 'unknown')})" for name, details in properties.items()]
                        schema_desc_str = ", ".join(prop_list).replace("{", "{{").replace("}", "}}") # Escape braces here too just in case
                    else: schema_desc_str = "[No properties found]"
                except Exception as e: schema_desc_str = f"[Error extracting schema: {e}]"
            input_desc = f"Input Type: Requires a JSON object string with properties: {schema_desc_str}. IMPORTANT: Generate a valid JSON string containing these properties as keys."

        elif input_type == 'STRING':
             input_desc = "Input Type: Plain String"
             if schema_def and isinstance(schema_def, str):
                  escaped_example = schema_def.replace("{", "{{").replace("}", "}}")
                  input_desc += f". Expected format/example: {escaped_example}"
        else: # Other types
             input_desc = f"Input Type: {input_type}"
             if schema_def and isinstance(schema_def, str):
                 input_desc += f". Tool expects: {schema_def.replace('{','{{').replace('}','}}')}"

        tool_name = tool_def.get('name', 'UnnamedTool'); tool_description = tool_def.get('description', 'No description.')
        tool_strings.append(f"- {tool_name}: {tool_description} {input_desc}")

    formatted_tools = "\n".join(tool_strings) if tool_strings else "No tools available."
    tool_names = ", ".join([tool_def['name'] for tool_def in tools_definitions]) if tools_definitions else "N/A"

    # Template using f-string requires escaping literal braces {{ }}
    # *** REMOVED the literal JSON example from Action Input instruction ***
    template = f"""Answer the following questions as best you can. You have access to the following tools:
{formatted_tools}

Use the following format STRICTLY:

Question: the input question you must answer
Thought: you should always think about what to do, analyzing the question and available tools.
Action: the action to take, must be one of [{tool_names}]
Action Input: Provide the exact input required for the selected Action. If Input Type requires a JSON object string, generate a *single, valid JSON object string* containing the required properties based on the tool's description. If Input Type is STRING, provide the plain string. Do NOT add explanations before or after the Action Input line. IMPORTANT: After writing the Action Input line, STOP generating immediately. Wait for the Observation.
Observation: the result of the action
... (this Thought/Action/Action Input/Observation can repeat N times)
Thought: I now know the final answer based on my thoughts and observations.
Final Answer: the final answer to the original input question

Begin!

Question: {{input}}
Thought:{{agent_scratchpad}}""" # Ensure agent_scratchpad variable is handled correctly in invoke call

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
        formatted_prompt = format_prompt(node_config['prompt'], state, node_config)
        print_ai(formatted_prompt)
        user_input = input(f"{Fore.YELLOW}You: {Style.RESET_ALL}")
        new_state = state.copy()
        output_field = next(iter(node_config.get('output_model', {'user_input': 'str'})), 'user_input')
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
    node_name_for_debug = node_config.get('name', 'Unnamed LLM Node')
    output_model_config = node_config.get('output_model', {})
    OutputModel = create_output_model(output_model_config)
    parser = PydanticOutputParser(pydantic_object=OutputModel) if OutputModel else None
    #print(f"[{node_name_for_debug}] OutputModel created: {OutputModel is not None}. Parser created: {parser is not None}")

    tool_registry: Dict[str, ToolDefinition] = {}

    async def node_function(state: dict) -> dict:
        node_name = node_config.get('name', 'Unnamed LLM Node')
        print_output(f"Processing {node_name}")
        # --- Limit Counter Logic ---
        limit_counter_field = node_config.get('limit_counter_field'); limit = node_config.get('limit')
        current_counter = state.get(limit_counter_field, 0)
        if limit_counter_field and limit:
             next_counter = current_counter + 1; log_counter = next_counter if next_counter <= limit else 1
             print_output(f"Processing {node_name} (Cycle {log_counter}/{limit})")
             # Counter updated later in new_state via update_state

        # --- Prepare LLM Call Inputs ---
        try:
            system_message_content = format_prompt(node_config.get('system',''), state, node_config)
            human_message_content = format_prompt(node_config['prompt'], state, node_config)
            llm = LLMManager.get_llm()
        except Exception as e: print(f"{Fore.RED}Error preparing node {node_name}: {e}{Style.RESET_ALL}"); return state

        # Variable to hold the final result before state update
        final_output_for_state: Union[BaseModel, str, Dict, None] = None
        new_state = state.copy() # Work on a copy

        # --- Inner function defining the core ReAct logic ---
        async def run_react_logic_with_tools(
            active_mcp_client: Optional[Any], # The active client from 'async with' or None
            node_config: Dict[str, Any], # Pass node_config for context
            system_message_content: str, # Pass prepared messages
            human_message_content: str,
            llm: BaseLanguageModel, # Pass the LLM instance
            tool_registry: Dict[str, ToolDefinition], # Pass the registry to be populated/used
            internal_tools_list: List[Any] # Pass the static internal tools list
            ) -> Union[Dict, str, None]:
            """
            Handles tool fetching, prompting, LLM call, parsing, conditional input processing,
            validation, and execution for a single ReAct step. Includes stricter validation.

            Returns:
                A dictionary like {"output": "result_text"} on success/final answer/error observation,
                or None if a critical setup error occurred.
            """
            node_name = node_config.get('name', 'Unnamed ReAct Node')
            # Output specific to this function run - defaults to None or error if setup fails
            processed_output_react: Union[Dict, str, None] = {"output": f"Error: ReAct logic failed to produce output for {node_name}"}

            try: # Wrap major setup steps (tool fetching, prompt gen)
                # --- Fetch/Filter/Register Tools ---
                tool_registry.clear() # Clear registry for this run
                filtered_tool_defs: List[ToolDefinition] = []
                all_fetched_tools: List[Any] = []

                # Fetch external tools IF active_mcp_client is provided
                if active_mcp_client:
                    try:
                        globals.logger.info(f"[{node_name}] Fetching external tools via MCP client...")
                        # Use sync call as per user feedback
                        external_tools_data = active_mcp_client.get_tools() or []
                        if isinstance(external_tools_data, list):
                            all_fetched_tools.extend(external_tools_data)
                            globals.logger.info(f"[{node_name}] Fetched {len(external_tools_data)} external tools.")
                        else:
                            print(f"{Fore.YELLOW}[{node_name}] Warning: mcp_client.get_tools() did not return a list.{Style.RESET_ALL}")
                    except Exception as e:
                        print(f"{Fore.RED}[{node_name}] Warning: MCP client error getting tools: {e}{Style.RESET_ALL}")

                # Add internal tools
                if isinstance(internal_tools_list, list):
                    all_fetched_tools.extend(internal_tools_list)

                # Filter and Build Registry (Robust - assumes tool_obj has .name, .description etc)
                tool_selection = node_config.get('tools_selection')
                processed_tool_names = set()
                for tool_obj in all_fetched_tools:
                    try:
                        # ... (Same tool processing/filtering/registry population logic as in Response #25) ...
                        tool_name = getattr(tool_obj, 'name', None)
                        if not tool_name or not isinstance(tool_name, str) or tool_name in processed_tool_names: continue
                        if tool_selection and isinstance(tool_selection, list) and tool_name not in tool_selection: continue
                        input_schema = getattr(tool_obj, 'args_schema', None)
                        input_type = getattr(tool_obj, 'input_type', 'JSON_SCHEMA' if input_schema else 'STRING') # Infer type
                        executor = getattr(tool_obj, 'arun', getattr(tool_obj, '_arun', getattr(tool_obj, 'run', getattr(tool_obj, '_run', None))))
                        is_async_executor = asyncio.iscoroutinefunction(executor) # Check if the chosen executor is async
                        if not callable(executor): continue
                        tool_def: ToolDefinition = {"name": tool_name, "description": getattr(tool_obj, 'description', 'No description available.'), "input_type": input_type, "input_schema_definition": input_schema, "tool_executor": executor, "tool_instance": tool_obj}
                        filtered_tool_defs.append(tool_def); tool_registry[tool_name] = tool_def; processed_tool_names.add(tool_name)
                    except Exception as e: print(f"{Fore.RED}[{node_name}] Error processing tool definition for {getattr(tool_obj, 'name', 'unknown')}: {e}{Style.RESET_ALL}")

                if not filtered_tool_defs: print(f"{Fore.YELLOW}Warning: No valid tools available for ReAct node {node_name}. Agent may only reason.{Style.RESET_ALL}")

                # --- Generate Custom ReAct Prompt ---
                # Assumes create_custom_react_prompt_template includes JSON examples and brace escaping
                custom_prompt_template_str = create_custom_react_prompt_template(filtered_tool_defs)
                react_system_message = system_message_content + "\n\n" + custom_prompt_template_str
                custom_react_prompt = ChatPromptTemplate.from_messages([ ("system", react_system_message), ("human", "{input}"),])

                # --- Execute Single ReAct Step ---
                globals.logger.info(f"[{node_name}] Invoking LLM for custom ReAct step...")
                chain: Runnable = custom_react_prompt | llm
                # Provide input, include scratchpad if needed (using empty for single step now)
                invoke_input = {"input": human_message_content}
                if "agent_scratchpad" in custom_react_prompt.input_variables: invoke_input["agent_scratchpad"] = ""

                # Make the LLM call
                llm_response = await chain.ainvoke(invoke_input)
                response_text = llm_response.content if hasattr(llm_response, 'content') else str(llm_response)
                globals.logger.info(f"[{node_name}] LLM Raw Response:\n{Style.DIM}{response_text}{Style.RESET_ALL}")

                # --- Parse Action/Input ---
                action_match = re.search(r"^\s*Action:\s*([\w.-]+)", response_text, re.MULTILINE | re.IGNORECASE)
                # More robust Action Input parsing
                input_string_from_llm = ""
                action_input_line_match = re.search(r"^\s*Action Input:\s*(.*)", response_text, re.MULTILINE | re.IGNORECASE)
                if action_input_line_match:
                    raw_input_line = action_input_line_match.group(1).strip()
                    json_match = re.match(r"^(\{.*?\})\s*$", raw_input_line, re.DOTALL)
                    if json_match: input_string_from_llm = json_match.group(1)
                    else:
                        input_string_from_llm = raw_input_line.split('\n')[0].strip()
                        if raw_input_line != input_string_from_llm: print(f"{Fore.YELLOW}[{node_name}] Warning: Truncated Action Input. Using: '{input_string_from_llm}'{Style.RESET_ALL}")

                if action_match: # Process Tool Call if Action is found
                    tool_name = action_match.group(1).strip()
                    globals.logger.info(f"[{node_name}] LLM selected Action: {tool_name}")
                    globals.logger.info(f"[{node_name}] LLM provided Action Input string (parsed): '{input_string_from_llm}'")

                    if tool_name in tool_registry:
                        tool_definition = tool_registry[tool_name]
                        tool_input_type = tool_definition['input_type']; tool_schema_def = tool_definition['input_schema_definition']
                        tool_executor = tool_definition['tool_executor']; tool_args_for_execution: Any = None; observation: str = ""

                        try:
                            # --- Conditional Input Processing & **VALIDATION** ---
                            if tool_input_type == 'STRING':
                                globals.logger.info(f"[{node_name}] Processing as STRING input.")
                                tool_args_for_execution = input_string_from_llm
                                # Optional: Add validation based on schema_def if it's regex etc.

                            elif tool_input_type == 'JSON_SCHEMA':
                                globals.logger.info(f"[{node_name}] Processing as JSON_SCHEMA input.")
                                if not tool_schema_def: raise ValueError(f"No schema definition found for JSON tool '{tool_name}'")

                                cleaned_json_string = input_string_from_llm.removeprefix("```json").removesuffix("```").strip()

                                # Step 1: Robust JSON Parsing
                                parsed_args = None
                                if not cleaned_json_string: parsed_args = {} # Treat empty as empty JSON object
                                else: parsed_args = json.loads(cleaned_json_string) # Raises JSONDecodeError
                                globals.logger.info(f"[{node_name}] JSON string parsed successfully.")

                                # Step 2: **Strict** Validate structure against Pydantic schema
                                if isinstance(tool_schema_def, type) and issubclass(tool_schema_def, BaseModel):
                                    validated_args = tool_schema_def(**parsed_args) # Raises ValidationError
                                    tool_args_for_execution = validated_args.model_dump(mode='json') if PYDANTIC_V2 else validated_args.dict() # type: ignore
                                    globals.logger.info(f"[{node_name}] JSON Validation successful (Pydantic).")
                                # Add elif for validating against JSON schema dict if needed
                                else:
                                    globals.logger.info(f"[{node_name}] JSON Validation skipped (schema not Pydantic model or validation not implemented).")
                                    tool_args_for_execution = parsed_args # Use parsed dict directly (less safe)

                            # Add elif blocks for 'CSV_STRING', 'DIRECT_PASS', etc. as needed

                            else: raise ValueError(f"Unsupported tool input_type: '{tool_input_type}'")

                            # --- Execute Tool (ONLY if parsing/validation succeeded) ---
                            globals.logger.info(f"[{node_name}] Executing tool '{tool_name}'...")
                            executor_is_async = asyncio.iscoroutinefunction(tool_executor)

                            if executor_is_async: tool_result = await tool_executor(tool_args_for_execution)
                            else: tool_result = await asyncio.to_thread(tool_executor, tool_args_for_execution)

                            observation = str(tool_result); globals.logger.info(f"[{node_name}] Tool Observation: {observation}")
                            processed_output_react = {"output": observation} # Wrap observation

                        except (json.JSONDecodeError, ValidationError, ValueError) as proc_error: # Catch parsing/validation errors explicitly
                            error_message = f"Error processing input for tool '{tool_name}': {proc_error}"
                            print(f"{Fore.RED}[{node_name}] {error_message}{Style.RESET_ALL}")
                            # No traceback for these expected errors
                            processed_output_react = {"output": f"Error: Input processing failed - {proc_error}"} # Return error observation
                        except Exception as exec_error: # Catch unexpected execution errors
                            error_message = f"Error executing tool '{tool_name}': {exec_error}"
                            print(f"{Fore.RED}[{node_name}] {error_message}{Style.RESET_ALL}")
                            print(f"{Fore.RED}Traceback:\n{traceback.format_exc()}{Style.RESET_ALL}") # Show traceback
                            processed_output_react = {"output": f"Error: Tool execution failed - {exec_error}"} # Return error observation

                    else: # Tool name parsed but not found in registry
                        error_message = f"Error: LLM selected unknown Action: {tool_name}"
                        print(f"{Fore.RED}[{node_name}] {error_message}{Style.RESET_ALL}")
                        processed_output_react = {"output": error_message}

                # --- Handle Final Answer or No Action ---
                elif "Final Answer:" in response_text:
                    final_answer = response_text.split("Final Answer:")[-1].strip()
                    print_output(f"[{node_name}] LLM provided Final Answer: {final_answer}")
                    processed_output_react = {"output": final_answer} # Wrap final answer
                else:
                    # LLM didn't follow expected ReAct format
                    warning_message = f"Warning: LLM response for {node_name} did not provide Action or Final Answer. Using raw response."
                    print(f"{Fore.YELLOW}{warning_message}{Style.RESET_ALL}")
                    processed_output_react = {"output": response_text} # Use raw output as fallback

            # --- Catch errors during the overall setup/LLM call within run_react_logic_with_tools ---
            except Exception as react_logic_error:
                print(f"{Fore.RED}[{node_name}] Critical error during ReAct step setup or LLM call: {react_logic_error}{Style.RESET_ALL}")
                print(f"{Fore.RED}Traceback:\n{traceback.format_exc()}{Style.RESET_ALL}")
                processed_output_react = {"output": f"Error during ReAct step: {react_logic_error}"} # Ensure dict format

            # Ensure function always returns a dictionary with 'output' key or None
            if isinstance(processed_output_react, dict) and "output" in processed_output_react: return processed_output_react
            elif isinstance(processed_output_react, str): return {"output": processed_output_react} # Should not happen ideally
            else: return {"output": f"Error: ReAct logic failed to produce valid output for {node_name}"} # Default error dict


        # --- Main Logic Branching for Tool Execution Context ---
        react_step_result: Union[Dict, str, None] = None # Store result from ReAct logic if used
        if use_tools:
            # If MCP client exists, run the core logic within its context
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
                      print(f"{Fore.RED}Error within MCP client context execution: {e}{Style.RESET_ALL}")
                      print(f"{Fore.RED}Traceback:\n{traceback.format_exc()}{Style.RESET_ALL}")
                      if react_step_result is None: react_step_result = {"output": f"Error during MCP context: {e}"}

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
                     print(f"{Fore.RED}Error during internal tool ReAct execution: {e}{Style.RESET_ALL}")
                     print(f"{Fore.RED}Traceback:\n{traceback.format_exc()}{Style.RESET_ALL}")
                     if react_step_result is None: react_step_result = {"output": f"Error during internal tool operation: {e}"}

            # --- Final Formatting Step for ReAct Path (if parser exists) ---
            final_result_text = None
            # Check if react_step_result is a dict with 'output' key and it's a string
            if isinstance(react_step_result, dict) and isinstance(react_step_result.get("output"), str):
                final_result_text = react_step_result["output"]
            elif isinstance(react_step_result, str): # Handle case where result is already error string
                final_result_text = react_step_result

            # Default to the direct result from ReAct logic (could be dict or error string)
            final_output_for_state = react_step_result

            # Attempt formatting only if parser exists, we have text, and it's not already an error
            if parser and final_result_text and isinstance(final_result_text, str) and not final_result_text.lower().startswith("error:"):
                globals.logger.info(f"Attempting to extract result from ReAct text and format into JSON schema for {node_name}...")
                format_instructions = ""; schema_valid_for_format = False
                try: # Try getting format instructions
                    # ...(logic to get format_instructions based on Pydantic V1/V2)...
                    # Set schema_valid_for_format = True if successful
                    if PYDANTIC_V2:
                         if hasattr(parser.pydantic_object, 'model_json_schema'): format_instructions = parser.get_format_instructions(); schema_valid_for_format = True
                         else: print(f"{Fore.RED}Error: Pydantic V2 model lacks .model_json_schema() for {node_name}{Style.RESET_ALL}")
                    else: # V1
                         if hasattr(parser.pydantic_object, 'schema'):
                             v1_schema = parser.pydantic_object.schema(); format_instructions = f"JSON schema:\n```json\n{json.dumps(v1_schema, indent=2)}\n```"; schema_valid_for_format = True
                         else: print(f"{Fore.RED}Error: Pydantic V1 model lacks .schema() for {node_name}{Style.RESET_ALL}")

                except Exception as e: print(f"{Fore.RED}[{node_name}] Unexpected error getting format instructions: {e}{Style.RESET_ALL}")

                # Proceed with formatting call only if instructions were obtained
                if schema_valid_for_format:
                    # *** ENHANCED PROMPT FOR EXTRACTION + FORMATTING ***
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
                    # *** END ENHANCED PROMPT ***

                    formatting_messages = [HumanMessage(content=formatting_prompt)]
                    max_format_retries = 2; format_retry_count = 0; parsed_formatted_output = None

                    # ...(Rest of the retry loop for LLM call and parsing remains the same)...
                    # [This loop now uses the enhanced prompt]
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
                            print(f"{Fore.YELLOW}Warning: Formatting LLM call failed parsing/validation (Attempt {format_retry_count}/{max_format_retries}): {error_detail}{Style.RESET_ALL}")
                            if format_retry_count >= max_format_retries:
                                 print(f"{Fore.RED}Failed to format ReAct result to JSON after retries. Storing raw text result.{Style.RESET_ALL}")
                                 parsed_formatted_output = final_result_text # Fallback to raw text
                                 break
                            feedback = f"Formatting failed: {error_detail}. Extract the single key result value from the observation text and provide ONLY the valid JSON object matching the schema."
                            formatting_messages = [formatting_messages[0], HumanMessage(content=feedback)] # Add feedback for retry
                        except Exception as format_error:
                             print(f"{Fore.RED}Unexpected error during result formatting LLM call: {format_error}{Style.RESET_ALL}")
                             parsed_formatted_output = final_result_text # Fallback
                             break
                    # Update the final output ONLY if formatting was attempted
                    final_output_for_state = parsed_formatted_output
                # *** End of 'if schema_valid_for_format:' ***
                else: # Schema wasn't valid for formatting
                     print(f"{Fore.YELLOW}Warning: Schema invalid or unavailable for formatting. Using raw ReAct result for {node_name}.{Style.RESET_ALL}")
                     # final_output_for_state remains the original react_step_result

            # *** Corrected `else` block alignment ***
            else: # No parser OR no text result OR result was error - skip formatting attempt
                print_output(f"Skipping formatting for {node_name} (No parser, no text result, or result was error).")
                # final_output_for_state remains the original react_step_result

        # --- Execution Path: No Tools (Direct LLM Call) ---
        else: # use_tools is False
            globals.logger.info(f"Using Direct LLM Call for {node_name}")
            # ...(Direct call logic exactly as implemented in Response #23)...
            # [This logic should correctly set final_output_for_state at the end]
            # [Make sure the final assignment to final_output_for_state happens correctly within this block]
            if not parser: print(f"{Fore.YELLOW}Warning: No parser for direct call node {node_name}. Expecting raw text.{Style.RESET_ALL}")
            prompt_to_llm = human_message_content; format_instructions = ""; schema_valid_for_format_direct = False
            if parser:
                try: # Try getting instructions
                    if PYDANTIC_V2:
                         if hasattr(parser.pydantic_object, 'model_json_schema'): format_instructions = parser.get_format_instructions(); schema_valid_for_format_direct = True
                         else: print(f"{Fore.RED}Error: Pydantic V2 model lacks .model_json_schema() for {node_name}{Style.RESET_ALL}")
                    else: # V1
                         if hasattr(parser.pydantic_object, 'schema'):
                             v1_schema = parser.pydantic_object.schema(); format_instructions = f"JSON schema:\n```json\n{json.dumps(v1_schema, indent=2)}\n```"; schema_valid_for_format_direct = True
                         else: print(f"{Fore.RED}Error: Pydantic V1 model lacks .schema() for {node_name}{Style.RESET_ALL}")

                    if schema_valid_for_format_direct: # Only add instructions if valid
                         prompt_to_llm += f"\n\nIMPORTANT: Respond ONLY with a JSON object conforming to the schema below. Do not include ```json ``` markers or any text outside the JSON object itself.\nSCHEMA:\n{format_instructions}"
                    else:
                         # If instructions failed, proceed without them? Or raise? Let's proceed without for now.
                         print(f"{Fore.YELLOW}Warning: Cannot get format instructions for {node_name}. Asking for text.{Style.RESET_ALL}")
                         # Do not add JSON instructions to prompt_to_llm

                except Exception as e: print(f"{Fore.RED}[{node_name}] Unexpected error getting format instructions: {e}{Style.RESET_ALL}") # Log error but continue?

            messages: List[BaseMessage] = [SystemMessage(content=system_message_content), HumanMessage(content=prompt_to_llm)]
            max_retries = node_config.get('max_retries', 3); retry_count = 0
            llm_response_content: Optional[str] = None; direct_call_parsed_output: Union[BaseModel, str, None] = None

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
                      retry_count += 1; error_detail = f"{type(e).__name__}: {e}"
                      print(f"{Fore.RED}Output parsing/validation failed (Attempt {retry_count}/{max_retries}): {error_detail}{Style.RESET_ALL}")
                      if retry_count >= max_retries: final_output_for_state = f"Error: Failed after {max_retries} attempts. {error_detail}"; break
                      feedback = f"Your previous response failed ({error_detail}). Please strictly follow the format instructions."
                      messages = messages[:2] + [HumanMessage(content=feedback)]
                 except Exception as e: print(f"{Fore.RED}Unexpected LLM call error: {e}{Style.RESET_ALL}"); final_output_for_state = f"Error: {e}"; break

            if direct_call_parsed_output is not None: final_output_for_state = direct_call_parsed_output
            elif final_output_for_state is None: final_output_for_state = f"Error: Max retries reached. Last response maybe: {llm_response_content}"


        # --- Update State & Print ---
        if final_output_for_state is None:
             print(f"{Fore.RED}Error: No output was processed or error captured for node {node_name}. Returning original state.{Style.RESET_ALL}")
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

# --- Utility Printing Functions ---
# Assume these are defined correctly elsewhere or add them here
# Ensure print_chat_prompt, print_state handle potential errors gracefully

def print_user_messages(state: Dict[str, Any], node_config: Dict[str, Any]):
    """Prints messages defined in node_config, substituting state variables."""
    user_message_fields = node_config.get('user_message', [])
    if not isinstance(user_message_fields, list):
         print(f"{Fore.YELLOW}Warning: 'user_message' in node config is not a list.{Style.RESET_ALL}")
         return
    for field_or_template in user_message_fields:
        if isinstance(field_or_template, str):
            # *** Add this check ***
            # First, check if the string is DIRECTLY a key in the state with a value
            if field_or_template in state and state[field_or_template] is not None:
                 print_ai(str(state[field_or_template])) # Print the value directly
                 continue # Skip further processing for this item
            # *** End Check ***

            # If not a direct key, treat as template or literal using format_prompt
            try:
                 formatted_msg = format_prompt(field_or_template, state, node_config)
                 # Check if formatting actually did anything or just returned the original
                 # (this depends on format_prompt implementation, maybe just print?)
                 print_ai(formatted_msg)
            except Exception as e:
                 print(f"{Fore.RED}Error formatting user message '{field_or_template}': {e}{Style.RESET_ALL}")
                 print_ai(field_or_template) # Print original on error
        else:
             print(f"{Fore.YELLOW}Warning: Item in 'user_message' is not a string: {field_or_template}{Style.RESET_ALL}")

def print_chat_prompt(chat_prompt: BasePromptTemplate, node_config: Dict[str, Any]):
    """Prints formatted chat prompt messages if enabled in config."""
    print_prompt_flag = node_config.get('print_prompt', False)
    node_name = node_config.get('name', 'Unknown Node')
    if print_prompt_flag and hasattr(chat_prompt, 'messages'):
        print(f"{Fore.BLUE}{Style.BRIGHT}ChatPrompt for {node_name}:{Style.RESET_ALL}")
        try:
            for i, message in enumerate(chat_prompt.messages, 1):
                role = "Unknown"; content = str(message); color = Fore.RED; style = Style.NORMAL
                if isinstance(message, SystemMessage): role = "System"; content = message.content; color = Fore.MAGENTA
                elif isinstance(message, HumanMessage): role = "Human"; content = message.content; color = Fore.YELLOW
                content_preview = (content[:500] + '...') if len(content) > 503 else content
                print(f"  {color}{role} {i}:{Style.RESET_ALL} {style}{content_preview}{Style.RESET_ALL}")
        except Exception as e: print(f"{Fore.RED}Error printing chat prompt: {e}{Style.RESET_ALL}")
        print("-" * 20)


def print_state(state: Dict[str, Any], node_config: Dict[str, Any]):
    """Prints the current state dictionary if enabled in config."""
    print_state_flag = node_config.get('print_state', False)
    node_name = node_config.get('name', 'Unknown Node')
    if print_state_flag:
        print_output(f"Current State after {node_name}:")
        try:
            state_str = json.dumps(state, indent=2, default=lambda o: f"<non-serializable: {type(o).__name__}>")
            print(f"{Fore.GREEN}{state_str}{Style.RESET_ALL}")
        except Exception as e: print(f"{Fore.RED}Could not serialize state to JSON. Error: {e}. Raw state:{Style.RESET_ALL}\n{state}")
        print("-" * 20)