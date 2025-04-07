import yaml
import json
import os
import appdirs
import astonish.globals as globals
from importlib import resources

import yaml
import json
import astonish.globals as globals
from importlib import resources
from astonish.tools.internal_tools import tools
from astonish.core.llm_manager import LLMManager
from typing import TypedDict, Union, Optional, get_args, get_origin
from langgraph.graph import StateGraph, END
from langchain_core.messages import SystemMessage, HumanMessage
from colorama import Fore, Style, init as colorama_init
from langchain.output_parsers import PydanticOutputParser
from langchain.prompts import ChatPromptTemplate
from pydantic import Field, ValidationError, create_model
from langchain.schema import OutputParserException
from langchain.globals import set_debug
from langgraph.prebuilt import create_react_agent
from langgraph.checkpoint.sqlite.aio import AsyncSqliteSaver
from langgraph.checkpoint.memory import MemorySaver

def setup_colorama():
    colorama_init(autoreset=True)

def print_ai(message):
    print(f"{Fore.GREEN}AI: {Style.RESET_ALL}{message}")

def print_user_prompt(message):
    #print(f"{Fore.YELLOW}You: {Style.RESET_ALL}{message}", end="")
    print(f"{Fore.YELLOW}{message}{Style.RESET_ALL}", end="")

def print_section(title):
    print(f"\n{Fore.BLUE}{Style.BRIGHT}{'=' * 40}")
    print(f"{title.center(40)}")
    print(f"{'=' * 40}{Style.RESET_ALL}\n")

def print_output(output):
    print(f"{Fore.CYAN}{output}{Style.RESET_ALL}")

def print_dict(dictionary, key_color=Fore.MAGENTA, value_color=Fore.CYAN):
    for key, value in dictionary.items():
        print(f"{key_color}{key}: {Style.RESET_ALL}{value_color}{value}{Style.RESET_ALL}")

def load_agents(agent_name):
    # Try to load from astonish.agents first
    try:
        with resources.path('astonish.agents', f"{agent_name}.yaml") as agent_path:
            with open(agent_path, 'r') as file:
                return yaml.safe_load(file)
    except FileNotFoundError:
        # If not found, try to load from config_path/agents
        config_dir = appdirs.user_config_dir("astonish")
        config_agents_path = os.path.join(config_dir, "agents", f"{agent_name}.yaml")
        if os.path.exists(config_agents_path):
            with open(config_agents_path, 'r') as file:
                return yaml.safe_load(file)
        else:
            raise FileNotFoundError(f"Agent {agent_name} not found in astonish.agents or {config_agents_path}")

def format_prompt(prompt: str, state: dict, node_config: dict):
    state_dict = dict(state)
    state_dict['state'] = state
    format_dict = {**state_dict, **node_config}
    return prompt.format(**format_dict)

from colorama import Fore, Style

from colorama import Fore, Style

def request_tool_execution(tool):
    """
    Prompt the user for approval before executing a tool command.
    Accepts only 'yes', 'no', 'y', or 'n' as valid inputs (case-insensitive).
    Keeps prompting until a valid response is received.

    Parameters:
    - tool (dict): Dictionary containing tool execution details.

    Returns:
    - bool: True if the user approves, False otherwise.
    """
    try:
        tool_name = tool['name']
        args = tool['args']

        prompt_message = f"\nTool Execution Request:\n"
        prompt_message += f"Tool Name: {tool_name}\n"
        prompt_message += "Arguments:\n"
        
        for key, value in args.items():
            prompt_message += f"  {key}: {value}\n"
        
        prompt_message += "Do you approve this execution? (yes/no): "

        while True:
            user_input = input(f"{Fore.YELLOW}{prompt_message}{Style.RESET_ALL}").strip().lower()
            if user_input in ['yes', 'y']:
                return True
            elif user_input in ['no', 'n']:
                return False
            else:
                print(f"{Fore.RED}Invalid input. Please enter 'yes' or 'no'.{Style.RESET_ALL}")

    except KeyError as e:
        print(f"{Fore.RED}Error: Missing required field in tool object: {e}{Style.RESET_ALL}")

    return False

def create_node_function(node_config, mcp_client):
    if node_config['type'] == 'input':
        return create_input_node_function(node_config)
    elif node_config['type'] == 'llm':
        return create_llm_node_function(node_config, mcp_client, node_config.get('tools'))

def create_input_node_function(node_config):
    def node_function(state: dict):
        if not (node_config.get('is_initial', False) and state.get('user_request') is not None):
            formatted_prompt = format_prompt(node_config['prompt'], state, node_config)

            print_ai(formatted_prompt)

        user_input = input(f"{Fore.YELLOW}You: {Style.RESET_ALL}")
        new_state = state.copy()
        
        # Get the field name from the output_model
        output_field = next(iter(node_config['output_model']))
        new_state[output_field] = user_input
        print_user_messages(new_state, node_config)
        
        return new_state
    return node_function

def create_llm_node_function(node_config, mcp_client, use_tools):
    OutputModel = create_output_model(node_config['output_model'])
    parser = PydanticOutputParser(pydantic_object=OutputModel)

    async def node_function(state: dict):
        if 'limit' in node_config and node_config['limit']:
            counter = state[node_config['limit_counter_field']] + 1
            if counter > node_config['limit']:
                counter = 1

            print_output(f"Processing {node_config['name']} ({counter}/{node_config['limit']})")
        else:
            print_output(f"Processing {node_config['name']}")

        systemMessage = format_prompt(node_config['system'], state, node_config)
        formatted_prompt = format_prompt(node_config['prompt'], state, node_config)
        default_provider = globals.config.get('GENERAL', 'default_provider', fallback='ollama')
        
        human_content = formatted_prompt if default_provider == 'ollama' else f"{formatted_prompt} \n\n IMPORTANT: Respond ONLY with a JSON object that conforms to the following schema. Do not include any preamble, explanation, or extra text outside the JSON object.: {parser.get_format_instructions()} \n\n Do not return nested objects, arrays, or complex structures."
        
        humanMessage = HumanMessage(content=human_content)

        chat_prompt = ChatPromptTemplate.from_messages([
            SystemMessage(content=systemMessage),
            humanMessage
        ])

        llm = LLMManager.get_llm(OutputModel.model_json_schema())

        max_retries = 3
        retry_count = 0
        parsed_output = None

        while retry_count < max_retries:
            try:
                if use_tools:
                    async with mcp_client as client:
                        all_tools = client.get_tools() + tools

                        # Filter tools based on node_config
                        if 'tools_selection' in node_config and node_config['tools_selection']:
                            filtered_tools = [tool for tool in all_tools if tool.name in node_config['tools_selection']]
                        else:
                            filtered_tools = all_tools

                        memory = MemorySaver()
                        agent = create_react_agent(llm, filtered_tools, interrupt_before=["tools"], checkpointer=memory)

                        first_run = True  # Flag to track if it's the first execution
                        config_tools = {"configurable": {"thread_id": "45"}}
                        processed_messages = 0
                        completion_flag = False
                        while True:
                            input_data = {
                                "messages": [
                                    SystemMessage(content=systemMessage),
                                    humanMessage
                                ]
                            } if first_run else None  # Empty input for subsequent runs

                            first_run = False  # Update flag after first execution

                            async for s in agent.astream(input_data, config_tools, stream_mode="values"):
                                if processed_messages > 0:
                                    processed_messages -= 1
                                    continue

                                message = s["messages"][-1]
                                tool_calls = getattr(message, "tool_calls", None)
                                if tool_calls:
                                    approve = request_tool_execution(tool_calls[0])
                                    if approve:
                                        processed_messages += 1
                                        break
                                    else:
                                        continue
                            else:
                                completion_flag = True
                            
                            if completion_flag:
                                parsed_output = parser.parse(message.content)
                                break
                else:
                    agent = create_react_agent(llm, [])
                    agent_output = await agent.ainvoke({
                        "messages": [
                            SystemMessage(content=systemMessage),
                            humanMessage
                        ]
                    })
                    parsed_output = parser.parse(agent_output['messages'][-1].content)
                break
            except OutputParserException as e:
                retry_count += 1
                error_message = str(e)
                feedback_message = f"Your response did not conform to the required JSON schema. The following error was encountered: {error_message}. Please respond ONLY with a JSON object conforming to this schema."
                humanMessage = HumanMessage(content=feedback_message)
                chat_prompt = ChatPromptTemplate.from_messages([
                    SystemMessage(content=systemMessage),
                    humanMessage
                ])

        if parsed_output is None:
            raise ValueError(f"LLM failed to provide valid output after {max_retries} attempts.")

        new_state = update_state(state, parsed_output, node_config)
        print_user_messages(new_state, node_config)
        print_chat_prompt(chat_prompt, node_config)
        print_state(new_state, node_config)

        return new_state

    return node_function

def parse_agent_output(output: str, OutputModel):
    try:
        # Attempt to parse the output as JSON
        parsed_json = json.loads(output)
        return OutputModel(**parsed_json)
    except json.JSONDecodeError:
        # If JSON parsing fails, attempt to extract a JSON object from the text
        import re
        json_match = re.search(r'\{.*\}', output, re.DOTALL)
        if json_match:
            try:
                parsed_json = json.loads(json_match.group())
                return OutputModel(**parsed_json)
            except (json.JSONDecodeError, ValidationError):
                pass
        
        # If all parsing attempts fail, raise an error
        raise ValueError("Failed to parse agent output into the required format")

def create_output_model(output_model_config):
    fields = {}
    for field_name, field_type in output_model_config.items():
        if '|' in field_type:
            types = [eval(t.strip()) for t in field_type.split('|')]
            field_type = Union[tuple(types)]
        else:
            field_type = eval(field_type)

        if get_origin(field_type) is Union and type(None) in get_args(field_type):
            field_type = Optional[get_args(field_type)[0]]

        fields[field_name] = (field_type, Field(description=f"{field_name} field"))

    return create_model('OutputModel', **fields)

def update_state(state, parsed_output, node_config):
    new_state = state.copy()
    for field in parsed_output.model_fields:
        if getattr(parsed_output, field) is not None:
            new_state[field] = getattr(parsed_output, field)

    if 'limit_counter_field' in node_config and node_config['limit_counter_field']:
        new_state[node_config['limit_counter_field']] = new_state.get(node_config['limit_counter_field'], 0) + 1
        if new_state[node_config['limit_counter_field']] > node_config['limit']:
            new_state[node_config['limit_counter_field']] = 1

    return new_state

def print_user_messages(state, node_config):
    user_message_fields = node_config.get('user_message', [])
    for field in user_message_fields:
        if field in state and state[field]:
            print_ai(f"{state[field]}")
        else:
            print_ai(field)

def print_chat_prompt(chat_prompt, node_config):
    print_prompt = node_config.get('print_prompt', False)
    if print_prompt:
        print(f"{Fore.BLUE}{Style.BRIGHT}ChatPromptTemplate:{Style.RESET_ALL}")
        for i, message in enumerate(chat_prompt.messages, 1):
            if isinstance(message, SystemMessage):
                print(f"  {Fore.MAGENTA}SystemMessage {i}:{Style.RESET_ALL}")
                print(f"    {Fore.CYAN}{message.content}{Style.RESET_ALL}")
            elif isinstance(message, HumanMessage):
                print(f"  {Fore.YELLOW}HumanMessage {i}:{Style.RESET_ALL}")
                print(f"    {Fore.GREEN}{message.content}{Style.RESET_ALL}")
            else:
                print(f"  {Fore.RED}Unknown Message Type {i}:{Style.RESET_ALL}")
                print(f"    {message}")
        print()

def print_state(state, node_config):
    print_state = node_config.get('print_state', False)
    if print_state:
        print_output("Current State: \n")
        print_dict(state, key_color=Fore.YELLOW, value_color=Fore.GREEN)
        print("")

def build_graph(node_config, mcp_client, checkpointer):
    # Create a dictionary to store all unique fields from output_models
    all_fields = {}
    for node in node_config['nodes']:
        if 'output_model' in node:
            all_fields.update(node['output_model'])
        if 'limit_counter_field' in node:
            all_fields[node['limit_counter_field']] = 'int'

    # Create TypedDict with all unique fields
    builder = StateGraph(TypedDict('AgentState', {name: eval(type_) for name, type_ in all_fields.items()}))

    for node in node_config['nodes']:
        builder.add_node(node['name'], create_node_function(node, mcp_client))

    for edge in node_config['flow']:
        if edge['from'] == 'START':
            builder.set_entry_point(edge['to'])
        elif 'edges' in edge:
            conditions = {}
            for sub_edge in edge['edges']:
                source_node_config = next(node for node in node_config['nodes'] if node['name'] == edge['from'])
                condition = create_condition_function(sub_edge['condition'], source_node_config)
                conditions[END if sub_edge['to'] == 'END' else sub_edge['to']] = condition

            default_state = END
            combined_condition_function = create_combined_condition_function(conditions, default_state)
            all_possible_transitions = {state: state for state in conditions.keys()}
            all_possible_transitions[default_state] = default_state

            builder.add_conditional_edges(edge['from'], combined_condition_function, all_possible_transitions)
        else:
            builder.add_edge(edge['from'], END if edge['to'] == 'END' else edge['to'])

    return builder.compile(checkpointer=checkpointer)

def safe_eval_condition(condition: str, state: dict, node_config: dict) -> bool:
    try:
        lambda_body = condition.split(":", 1)[1].strip()
        func = eval(f"lambda x, config: {lambda_body}")
        return func(state, node_config)
    except Exception as e:
        globals.logger.error(f"Error evaluating condition: {e}")
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
    await graph.ainvoke(initial_state, thread)

def print_flow(agent):
    config = load_agents(agent)
    graph = build_graph(config, None, None)
    graph_obj = graph.get_graph()
    graph_obj.print_ascii()

async def run_agent(agent):
    # Setup
    setup_colorama()
    set_debug(False)

    # Load agents
    config = load_agents(agent)

    # Initialize MCP tools
    mcp_client = await globals.initialize_mcp_tools()

    # Initialize state
    initial_state = {}
    for node in config['nodes']:
        if 'output_model' in node:
            for field, type_ in node['output_model'].items():
                if field not in initial_state:
                    initial_state[field] = None
        
        # Add initialization for limit_counter_field
        if 'limit_counter_field' in node:
            limit_counter_field = node['limit_counter_field']
            if limit_counter_field not in initial_state:
                initial_state[limit_counter_field] = 0  # Initialize to 0

    # Build graph
    async with AsyncSqliteSaver.from_conn_string(":memory:") as checkpointer:
        thread = {"configurable": {"thread_id": "1"}}
        graph = build_graph(config, mcp_client, checkpointer)

        await run_graph(graph, initial_state, thread)

        print_ai("Bye! Bye!")
