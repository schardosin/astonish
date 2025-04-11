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

# --- Helper Functions ---
# create_output_model (Use version from Response #19 - robust type handling)
def create_output_model(output_model_config: Dict[str, str]) -> Optional[Type[BaseModel]]:
    """Creates a Pydantic model dynamically if config provided."""
    if not output_model_config or not isinstance(output_model_config, dict): return None
    fields = {}
    basic_types = {"str": str, "int": int, "float": float, "bool": bool, "Any": Any}
    eval_context = {"Union": Union, "Optional": Optional, "List": List, "Dict": Dict, **basic_types, "BaseModel": BaseModel}
    for field_name, field_type_str in output_model_config.items():
        field_type = Any
        try:
            if field_type_str in basic_types: field_type = basic_types[field_type_str]
            elif any(c in field_type_str for c in ['|', '[', '{', 'Optional', 'Union', 'List', 'Dict']):
                 field_type = eval(field_type_str, globals(), eval_context)
            else:
                 field_type = eval_context.get(field_type_str, Any)
                 if field_type is Any: print(f"{Fore.YELLOW}Warning: Unknown type '{field_type_str}' for field '{field_name}', defaulting to Any.{Style.RESET_ALL}")
            origin = get_origin(field_type); args = get_args(field_type)
            if origin is Union and type(None) in args:
                 non_none_args = tuple(arg for arg in args if arg is not type(None))
                 if len(non_none_args) == 1: field_type = Optional[non_none_args[0]]
                 elif len(non_none_args) > 1: field_type = Optional[Union[non_none_args]]
                 else: field_type = Optional[type(None)]
            fields[field_name] = (field_type, Field(description=f"{field_name} field"))
        except NameError as ne:
             print(f"{Fore.RED}Error evaluating type string '{field_type_str}' for field '{field_name}': Name not found ({ne}). Defaulting to Any.{Style.RESET_ALL}")
             fields[field_name] = (Any, Field(description=f"{field_name} field (name error)"))
        except Exception as e:
            print(f"{Fore.RED}Error processing type string '{field_type_str}' for field '{field_name}': {e}{Style.RESET_ALL}")
            fields[field_name] = (Any, Field(description=f"{field_name} field (processing error)"))
    model_name = f"DynamicOutputModel_{abs(hash(json.dumps(output_model_config, sort_keys=True)))}"
    try: return create_model(model_name, **fields)
    except Exception as e: print(f"{Fore.RED}Failed to create Pydantic model '{model_name}': {e}{Style.RESET_ALL}"); return None

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
    tool_strings = []
    for tool_def in tools_definitions:
        input_desc = ""; input_type = tool_def.get('input_type', 'STRING'); schema_def = tool_def.get('input_schema_definition')
        if input_type == 'JSON_SCHEMA':
            schema_str = "[Schema not provided or invalid]"
            if schema_def:
                try:
                    schema_dict = {}
                    if isinstance(schema_def, type) and issubclass(schema_def, BaseModel):
                        schema_json = schema_def.model_json_schema() if PYDANTIC_V2 else schema_def.schema() # type: ignore
                        schema_dict = schema_json.get("properties", {})
                    elif isinstance(schema_def, dict): schema_dict = schema_def.get("properties", {})
                    if schema_dict:
                         schema_dump = json.dumps(schema_dict)
                         escaped_schema_str = schema_dump.replace("{", "{{").replace("}", "}}") # Escape for f-string
                         schema_str = escaped_schema_str
                    else: schema_str = "[No properties found in schema]"
                except Exception as e: schema_str = f"[Error extracting/escaping schema: {e}]"
            input_desc = f"Input Type: JSON object string matching schema properties: {schema_str}"
        elif input_type == 'STRING':
             input_desc = "Input Type: Plain String"
             if schema_def and isinstance(schema_def, str):
                  escaped_example = schema_def.replace("{", "{{").replace("}", "}}")
                  input_desc += f". Expected format/example: {escaped_example}"
        else:
             input_desc = f"Input Type: {input_type}"
             if schema_def and isinstance(schema_def, str): input_desc += f". Tool expects: {schema_def.replace('{','{{').replace('}','}}')}"
        tool_name = tool_def.get('name', 'UnnamedTool'); tool_description = tool_def.get('description', 'No description.')
        tool_strings.append(f"- {tool_name}: {tool_description} {input_desc}")
    formatted_tools = "\n".join(tool_strings) if tool_strings else "No tools available."
    tool_names = ", ".join([tool_def['name'] for tool_def in tools_definitions]) if tools_definitions else "N/A"
    template = f"""Answer the following questions as best you can. You have access to the following tools:
{formatted_tools}
Use the following format STRICTLY:
Question: the input question you must answer
Thought: you should always think about what to do, analyzing the question and available tools.
Action: the action to take, must be one of [{tool_names}]
Action Input: Provide the exact input required for the selected Action, formatting it precisely according to the tool's 'Input Type' description above. For JSON_SCHEMA, provide a valid JSON object string. For STRING, provide the plain string. For others, follow the specific format mentioned.
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
    ensuring MCP client context is active and handling diverse tool inputs.
    Direct call otherwise. Includes debugging for Pydantic issue.
    """
    # --- Common Setup ---
    node_name_for_debug = node_config.get('name', 'Unnamed LLM Node') # Use for logging
    output_model_config = node_config.get('output_model', {})

    # --- Debugging OutputModel Creation ---
    print(f"[{node_name_for_debug}] Raw output_model_config: {output_model_config}") # Debug Print 1
    OutputModel = create_output_model(output_model_config)

    print(f"[{node_name_for_debug}] create_output_model result: {OutputModel}") # Debug Print 2
    if OutputModel:
        print(f"[{node_name_for_debug}] OutputModel type: {type(OutputModel)}") # Debug Print 3
        try:
            # Explicitly check against the currently imported BaseModel
            print(f"[{node_name_for_debug}] Is BaseModel subclass: {issubclass(OutputModel, BaseModel)}") # Debug Print 4
        except TypeError:
             print(f"[{node_name_for_debug}] Is BaseModel subclass check failed (TypeError - not a class?).")
        # Check specifically for the method *before* creating the parser
        print(f"[{node_name_for_debug}] Has '.model_json_schema': {hasattr(OutputModel, 'model_json_schema')}") # Debug Print 5
        print(f"[{node_name_for_debug}] Has '.schema' (Pydantic V1): {hasattr(OutputModel, 'schema')}") # Debug Print 5b
        if hasattr(OutputModel, 'model_json_schema'):
             try:
                  print(f"[{node_name_for_debug}] Calling .model_json_schema() early...")
                  schema_test = OutputModel.model_json_schema() # Try calling it early
                  print(f"[{node_name_for_debug}] Early .model_json_schema() call successful. Result type: {type(schema_test)}")
             except Exception as e_schema:
                  print(f"{Fore.RED}[{node_name_for_debug}] Error calling .model_json_schema() early: {e_schema}{Style.RESET_ALL}")
    # --- End Debugging ---

    parser = PydanticOutputParser(pydantic_object=OutputModel) if OutputModel else None
    print(f"[{node_name_for_debug}] Parser created: {'Yes' if parser else 'No'}") # Debug Print 6


    # Tool Registry - populated inside async function per run if needed
    tool_registry: Dict[str, ToolDefinition] = {}

    async def node_function(state: dict) -> dict:
        node_name = node_config.get('name', 'Unnamed LLM Node') # Get name again inside async func
        print_output(f"Processing {node_name}")
        # --- Limit Counter Logic ---
        limit_counter_field = node_config.get('limit_counter_field')
        limit = node_config.get('limit')
        if limit_counter_field and limit:
             counter = state.get(limit_counter_field, 0) + 1
             state[limit_counter_field] = counter # Mutate state copy for this run's logic
             if counter > limit: print_output(f"Limit {limit} reached for {node_name}. Cycle: {counter}")
             print_output(f"Processing {node_name} (Cycle {counter}/{limit})")

        # --- Prepare LLM Call Inputs ---
        try:
            system_message_content = format_prompt(node_config.get('system',''), state, node_config)
            human_message_content = format_prompt(node_config['prompt'], state, node_config)
            llm = LLMManager.get_llm()
        except Exception as e:
            print(f"{Fore.RED}Error preparing node {node_name}: {e}{Style.RESET_ALL}")
            return state # Return original state on setup error

        processed_output: Union[BaseModel, str, Dict, None] = None
        new_state = state.copy() # Work on a copy for the final return value

        # --- Inner function defining the core ReAct logic ---
        async def run_react_logic_with_tools(active_mcp_client=None):
            """Handles tool fetching, prompting, LLM call, parsing, and execution."""
            nonlocal processed_output # Allow modification of outer scope variable
            tool_registry.clear()
            filtered_tool_defs: List[ToolDefinition] = []
            all_fetched_tools: List[Any] = []

            # Fetch external tools IF active_mcp_client is provided
            if active_mcp_client:
                try:
                    print_output("Fetching external tools via MCP client...")
                    external_tools_data = active_mcp_client.get_tools() or []
                    all_fetched_tools.extend(external_tools_data)
                except Exception as e: print(f"{Fore.RED}Warning: MCP client error getting tools: {e}{Style.RESET_ALL}")

            # Add internal tools
            if isinstance(internal_tools_list, list): all_fetched_tools.extend(internal_tools_list)

            # Filter and Build Registry
            tool_selection = node_config.get('tools_selection')
            processed_tool_names = set()
            for tool_obj in all_fetched_tools:
                 try:
                    tool_name = getattr(tool_obj, 'name', None)
                    if not tool_name or not isinstance(tool_name, str) or tool_name in processed_tool_names: continue
                    if tool_selection and isinstance(tool_selection, list) and tool_name not in tool_selection: continue
                    input_schema = getattr(tool_obj, 'args_schema', None)
                    input_type = getattr(tool_obj, 'input_type', 'JSON_SCHEMA' if input_schema else 'STRING')
                    executor = getattr(tool_obj, 'arun', getattr(tool_obj, '_arun', getattr(tool_obj, 'run', getattr(tool_obj, '_run', None))))
                    if not callable(executor): continue
                    tool_def: ToolDefinition = {
                        "name": tool_name, "description": getattr(tool_obj, 'description', ''),
                        "input_type": input_type, "input_schema_definition": input_schema,
                        "tool_executor": executor, "tool_instance": tool_obj
                    }
                    filtered_tool_defs.append(tool_def); tool_registry[tool_name] = tool_def; processed_tool_names.add(tool_name)
                 except Exception as e: print(f"{Fore.RED}Error processing tool definition for {getattr(tool_obj, 'name', 'unknown')}: {e}{Style.RESET_ALL}")

            if not filtered_tool_defs: print(f"{Fore.YELLOW}Warning: No valid tools available for ReAct node {node_name}.{Style.RESET_ALL}")

            # Generate Custom ReAct Prompt
            custom_prompt_template_str = create_custom_react_prompt_template(filtered_tool_defs)
            react_system_message = system_message_content + "\n\n" + custom_prompt_template_str
            custom_react_prompt = ChatPromptTemplate.from_messages([
                ("system", react_system_message), ("human", "{input}"),
                # Add placeholder if implementing multi-turn loop
                # MessagesPlaceholder(variable_name="agent_scratchpad", optional=True)
            ])

            # Execute Single ReAct Step
            print_output("Invoking LLM for custom ReAct step...")
            chain: Runnable = custom_react_prompt | llm
            invoke_input = {"input": human_message_content, "agent_scratchpad": ""} # Provide required vars
            llm_response = await chain.ainvoke(invoke_input)
            response_text = llm_response.content if hasattr(llm_response, 'content') else str(llm_response)
            print_output(f"LLM Raw Response:\n{Style.DIM}{response_text}{Style.RESET_ALL}")

            # Parse Action/Input
            action_match = re.search(r"^\s*Action:\s*([\w.-]+)", response_text, re.MULTILINE | re.IGNORECASE)
            action_input_match = re.search(r"^\s*Action Input:\s*(.*)", response_text, re.MULTILINE | re.DOTALL | re.IGNORECASE)

            if action_match: # Process if action is found
                tool_name = action_match.group(1).strip()
                input_string_from_llm = action_input_match.group(1).strip() if action_input_match else ""
                print_output(f"LLM selected Action: {tool_name}")
                print_output(f"LLM provided Action Input string: '{input_string_from_llm}'")

                if tool_name in tool_registry:
                    tool_definition = tool_registry[tool_name]
                    tool_input_type = tool_definition['input_type']; tool_schema_def = tool_definition['input_schema_definition']
                    tool_executor = tool_definition['tool_executor']; tool_args_for_execution: Any = None
                    try:
                        # --- Conditional Input Processing ---
                        if tool_input_type == 'STRING':
                            print_output("Processing as STRING input."); tool_args_for_execution = input_string_from_llm
                        elif tool_input_type == 'JSON_SCHEMA':
                            print_output("Processing as JSON_SCHEMA input.")
                            if not tool_schema_def: raise ValueError(f"No schema for tool '{tool_name}'")
                            cleaned_json_string = input_string_from_llm.removeprefix("```json").removesuffix("```").strip()
                            if not cleaned_json_string:
                                if isinstance(tool_schema_def, type) and issubclass(tool_schema_def, BaseModel):
                                     validated_args = tool_schema_def(); parsed_args = validated_args.model_dump(mode='json') if PYDANTIC_V2 else validated_args.dict() # type: ignore
                                else: parsed_args = {}
                            else: parsed_args = json.loads(cleaned_json_string)
                            if isinstance(tool_schema_def, type) and issubclass(tool_schema_def, BaseModel):
                                 validated_args = tool_schema_def(**parsed_args)
                                 tool_args_for_execution = validated_args.model_dump(mode='json') if PYDANTIC_V2 else validated_args.dict() # type: ignore
                                 print_output("JSON Validation successful (Pydantic).")
                            else: print_output("JSON Parsing successful (validation skipped)."); tool_args_for_execution = parsed_args
                        # Add other elif for custom types
                        else: raise ValueError(f"Unsupported tool input_type: '{tool_input_type}'")

                        # --- Execute Tool ---
                        print_output(f"Executing tool '{tool_name}'...")
                        if asyncio.iscoroutinefunction(tool_executor): tool_result = await tool_executor(tool_args_for_execution)
                        else: tool_result = await asyncio.to_thread(tool_executor, tool_args_for_execution)
                        observation = str(tool_result)
                        print_output(f"Tool Observation: {observation}")
                        processed_output = {"output": observation}

                    except (json.JSONDecodeError, ValueError, ValidationError, TypeError, Exception) as exec_error:
                        error_message = f"Error processing/executing tool '{tool_name}': {exec_error}"
                        print(f"{Fore.RED}{error_message}{Style.RESET_ALL}")
                        print(f"{Fore.RED}Traceback:\n{traceback.format_exc()}{Style.RESET_ALL}")
                        processed_output = {"output": f"Error: {exec_error}"} # Pass error observation

                else: # Tool name not found
                    error_message = f"Error: LLM selected unknown Action: {tool_name}"
                    print(f"{Fore.RED}{error_message}{Style.RESET_ALL}")
                    processed_output = {"output": error_message}

            # --- Handle Final Answer or No Action ---
            elif "Final Answer:" in response_text:
                 final_answer = response_text.split("Final Answer:")[-1].strip()
                 print_output(f"LLM provided Final Answer: {final_answer}")
                 processed_output = {"output": final_answer}
            else:
                 warning_message = "Warning: LLM response did not provide Action or Final Answer. Using raw response."
                 print(f"{Fore.YELLOW}{warning_message}{Style.RESET_ALL}")
                 processed_output = {"output": response_text}

        # --- Main Logic Branching for Tool Execution Context ---
        if use_tools:
            # Run the ReAct logic, potentially within MCP client context
            if mcp_client:
                 print_output("Entering MCP client context for tool operations...")
                 try:
                      async with mcp_client as client:
                           await run_react_logic_with_tools(active_mcp_client=client) # Pass active client
                 except Exception as e:
                      print(f"{Fore.RED}Error within MCP client context execution: {e}{Style.RESET_ALL}")
                      print(f"{Fore.RED}Traceback:\n{traceback.format_exc()}{Style.RESET_ALL}")
                      # Ensure processed_output is set to avoid None later
                      if processed_output is None: processed_output = {"output": f"Error during MCP context: {e}"}
            else: # Tools requested, but no MCP client
                 print_output("No MCP client provided, using only internal tools for ReAct...")
                 try:
                      await run_react_logic_with_tools(active_mcp_client=None)
                 except Exception as e:
                     print(f"{Fore.RED}Error during internal tool ReAct execution: {e}{Style.RESET_ALL}")
                     print(f"{Fore.RED}Traceback:\n{traceback.format_exc()}{Style.RESET_ALL}")
                     if processed_output is None: processed_output = {"output": f"Error during internal tool operation: {e}"}


        # --- Execution Path: No Tools (Direct LLM Call) ---
        else: # use_tools is False
            print_output(f"Using Direct LLM Call for {node_name}")

            # --- Add Debugging before using parser ---
            print(f"[{node_name}] Attempting direct call. Parser object: {parser}") # Debug Print 7
            if parser and hasattr(parser, 'pydantic_object'):
                 pyd_obj = parser.pydantic_object
                 # Check if pyd_obj is not None before accessing attributes
                 if pyd_obj is not None:
                      print(f"[{node_name}] Parser's pydantic_object: {pyd_obj}") # Debug Print 8
                      print(f"[{node_name}] Parser's pydantic_object type: {type(pyd_obj)}") # Debug Print 9
                      print(f"[{node_name}] Parser's pydantic_object Has '.model_json_schema': {hasattr(pyd_obj, 'model_json_schema')}") # Debug Print 10
                      print(f"[{node_name}] Parser's pydantic_object Has '.schema' (V1): {hasattr(pyd_obj, 'schema')}") # Debug Print 10b
                 else:
                      print(f"[{node_name}] Parser's pydantic_object is None.") # Debug Print 8b
            elif parser:
                 print(f"[{node_name}] Parser exists but has no 'pydantic_object' attribute?") # Debug Print 7b
            # --- End Debugging ---

            if not parser:
                 print(f"{Fore.YELLOW}Warning: No output_model/parser for direct call node {node_name}. Expecting raw text.{Style.RESET_ALL}")
                 prompt_to_llm = human_message_content
            else: # Proceed only if parser seems valid
                prompt_to_llm = human_message_content
                try:
                     # *** This is the line that fails - get format instructions ***
                     # Check existence of method based on detected Pydantic version
                     if PYDANTIC_V2:
                         if not hasattr(parser.pydantic_object, 'model_json_schema'):
                             raise AttributeError(f"Pydantic V2 detected, but model {type(parser.pydantic_object)} lacks 'model_json_schema' method.")
                         format_instructions = parser.get_format_instructions() # Should now work if checks pass
                     else: # Pydantic V1
                         # Langchain's parser might still try V2 method internally.
                         # A V1 specific parser or manual instructions might be needed if this fails.
                         # Let's try the default and see if Langchain handles V1 compatibility here.
                          if not hasattr(parser.pydantic_object, 'schema'):
                              raise AttributeError(f"Pydantic V1 detected, but model {type(parser.pydantic_object)} lacks 'schema' method.")
                          # Manually create V1 style instructions (example)
                          v1_schema = parser.pydantic_object.schema()
                          format_instructions = f"The output should be formatted as a JSON instance that conforms to the JSON schema below.\n\n```json\n{json.dumps(v1_schema, indent=2)}\n```"
                          # Or try default Langchain parser way and see if it adapts:
                          # format_instructions = parser.get_format_instructions() # Might still raise error if it insists on V2 method

                     prompt_to_llm += f"\n\nIMPORTANT: Respond ONLY with a JSON object conforming to the schema below. Do not include ```json ``` markers or any text outside the JSON object itself.\nSCHEMA:\n{format_instructions}"

                except AttributeError as e:
                     print(f"{Fore.RED}[{node_name}] CRITICAL: Failed get_format_instructions. Pydantic V2 mode={PYDANTIC_V2}. Object type: {type(parser.pydantic_object if parser else None)}. Error: {e}{Style.RESET_ALL}")
                     # Fallback: Cannot ask for JSON if instructions fail. Proceed asking for raw text?
                     # Or re-raise the error. Raising for now.
                     raise e
                except Exception as e:
                     print(f"{Fore.RED}[{node_name}] Unexpected error getting format instructions: {e}{Style.RESET_ALL}")
                     raise e

            # --- Direct LLM Call Loop (with retry logic) ---
            messages: List[BaseMessage] = [SystemMessage(content=system_message_content), HumanMessage(content=prompt_to_llm)]
            max_retries = node_config.get('max_retries', 3); retry_count = 0
            final_parsed_output: Union[BaseModel, str, None] = None; llm_response_content: Optional[str] = None

            while retry_count < max_retries:
                 try:
                      print_output(f"Attempt {retry_count + 1}/{max_retries} for direct LLM call...")
                      llm_response = await llm.ainvoke(messages)
                      llm_response_content = llm_response.content if hasattr(llm_response, 'content') else str(llm_response)

                      if parser: # Only parse if parser exists and format instructions were added
                            cleaned_content = llm_response_content.strip().removeprefix("```json").removesuffix("```").strip()
                            if not cleaned_content: raise OutputParserException("Received empty response.")
                            final_parsed_output = parser.parse(cleaned_content)
                            print_output("Successfully parsed and validated JSON output.")
                      else: # No parser, use raw text
                            final_parsed_output = llm_response_content
                            print_output("Received raw text output (no parser defined).")
                      break # Success!

                 except (OutputParserException, ValidationError, json.JSONDecodeError) as e:
                      retry_count += 1; error_detail = f"{type(e).__name__}: {e}"
                      print(f"{Fore.RED}Output parsing/validation failed (Attempt {retry_count}/{max_retries}): {error_detail}{Style.RESET_ALL}")
                      if retry_count >= max_retries: processed_output = f"Error: Failed to parse/validate LLM output after {max_retries} attempts."; break
                      feedback_message = f"Your previous response failed ({error_detail}). Please strictly follow the format instructions and provide ONLY the required output."
                      messages = messages[:2] + [HumanMessage(content=feedback_message)]
                 except Exception as e:
                       print(f"{Fore.RED}Unexpected error during direct LLM call for node {node_name}: {e}{Style.RESET_ALL}")
                       processed_output = f"Error in Direct LLM Call: {e}"; break

            if final_parsed_output is not None: processed_output = final_parsed_output
            elif processed_output is None and retry_count >= max_retries: processed_output = f"Error: Max retries reached for node {node_name}. Last response maybe: {llm_response_content}"


        # --- Update State & Print ---
        if processed_output is None:
             print(f"{Fore.RED}Error: No output was processed or error captured for node {node_name}. Returning original state.{Style.RESET_ALL}")
             # Keep new_state as the original state copy
        else:
             new_state = update_state(new_state, processed_output, node_config) # Update the copy

        print_user_messages(new_state, node_config)
        # Conditionally print prompt only for direct call path if enabled
        if not use_tools and node_config.get('print_prompt', False):
            # Need to potentially reconstruct the ChatPromptTemplate if messages list was modified
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
            try:
                 # Use the existing robust formatter if available
                 formatted_msg = format_prompt(field_or_template, state, node_config)
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