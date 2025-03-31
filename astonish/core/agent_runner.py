import yaml
import json
import sys
import os
import astonish.globals as globals
from astonish.core.llm_manager import LLMManager
from typing import TypedDict, Callable, Any, Union, Optional, List, Dict, get_args, get_origin
from pprint import pprint
from langgraph.graph import StateGraph, END
from langchain_core.messages import SystemMessage, HumanMessage
from colorama import Fore, Style, init as colorama_init
from langgraph.checkpoint.sqlite import SqliteSaver
from langchain.output_parsers import PydanticOutputParser
from langchain.prompts import ChatPromptTemplate
from pydantic import BaseModel, Field, ValidationError, create_model
from langchain.schema import OutputParserException
from langchain.globals import set_debug
from mcp import ClientSession
from langchain_mcp_adapters.tools import load_mcp_tools
from langchain_mcp_adapters.client import MultiServerMCPClient
from langgraph.prebuilt import create_react_agent
from langgraph.checkpoint.sqlite.aio import AsyncSqliteSaver

# Constants
GRAPH_OUTPUT_PATH = 'graph_output.png'

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

def load_agents(path):
    with open('astonish/agents/' + path + '.yaml', 'r') as file:
        return yaml.safe_load(file)

async def initialize_mcp_tools():
    with open('mcp_config.json', 'r') as config_file:
        mcp_config = json.load(config_file)
    
    # async with MultiServerMCPClient(mcp_config['mcpServers']) as mcp_client:
    # async with MultiServerMCPClient(
    # {
    #     "tavily-search": {
    #         "command": "/home/schardosin/.local/bin/uv",
    #             "args": [
    #             "--directory",
    #             "/home/schardosin/projects/test_mcp/mcp/mcp-server-tavily",
    #             "run",
    #             "tavily-search"
    #         ],
    #         "env": {
    #             "TAVILY_API_KEY": "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",
    #             "PYTHONIOENCODING": "utf-8"
    #         },
    #         "transport": "stdio",
    #     }
    # }
    # ) as mcp_client:
        # tools = mcp_client.get_tools()
        # return tools

    mcp_client = MultiServerMCPClient(
        {
            "tavily-search": {
                "command": "/home/schardosin/.local/bin/uv",
                    "args": [
                    "--directory",
                    "/home/schardosin/projects/test_mcp/mcp/mcp-server-tavily",
                    "run",
                    "tavily-search"
                ],
                "env": {
                    "TAVILY_API_KEY": "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",
                    "PYTHONIOENCODING": "utf-8"
                },
                "transport": "stdio",
            }
        }
    )
    await mcp_client.__aenter__()
    tools = mcp_client.get_tools()
    return tools

def format_prompt(prompt: str, state: dict, node_config: dict):
    state_dict = dict(state)
    state_dict['state'] = state
    format_dict = {**state_dict, **node_config}
    return prompt.format(**format_dict)

def create_node_function(node_config, mcp_tools):
    if node_config['type'] == 'input':
        return create_input_node_function(node_config)
    elif node_config['type'] == 'llm':
        if node_config.get('tools') is True:
            return create_node_tool_function(node_config, mcp_tools)
        else:
            return create_llm_node_function(node_config, mcp_tools)

def create_input_node_function(node_config):
    def node_function(state: dict):
        if not (node_config.get('is_initial', False) and state.get('user_request') is not None):
            print_ai(node_config['prompt'])
        user_input = input(f"{Fore.YELLOW}You: {Style.RESET_ALL}")
        new_state = state.copy()
        new_state['user_request'] = user_input
        return new_state
    return node_function

def create_llm_node_function(node_config, mcp_tools):
    OutputModel = create_output_model(node_config['output_model'])
    parser = PydanticOutputParser(pydantic_object=OutputModel)

    def node_function(state: dict):
        if 'limit' in node_config and node_config['limit']:
            counter = state[node_config['limit_counter_field']] + 1
            print_output(f"Processing {node_config['name']} ({counter}/{node_config['limit']})")
        else:
            print_output(f"Processing {node_config['name']}")

        systemMessage = format_prompt(node_config['system'], state, node_config)
        formatted_prompt = format_prompt(node_config['prompt'], state, node_config)
        humanMessage = ""
        default_provider = globals.config.get('GENERAL', 'default_provider', fallback='ollama')
        if default_provider == 'ollama':
            humanMessage = HumanMessage(content=f"{formatted_prompt}")
        else:
            humanMessage = HumanMessage(content=f"{formatted_prompt} \n\n IMPORTANT: Respond ONLY with a JSON object that conforms to the following schema. Do not include any preamble, explanation, or extra text outside the JSON object.: {parser.get_format_instructions()} \n\n Do not return nested objects, arrays, or complex structures.")

        chat_prompt = ChatPromptTemplate.from_messages([
            SystemMessage(content=systemMessage),
            humanMessage
        ])

        llm = LLMManager.get_llm(OutputModel.model_json_schema())
        chain = chat_prompt | llm | parser

        parsed_output = None
        max_retries = 3
        retry_count = 0

        while retry_count < max_retries:
            try:
                parsed_output = chain.invoke({})
                break
            except OutputParserException as e:
                # Handle validation error and retry
                retry_count += 1
                error_message = str(e)
                # Recreate the chain with a feedback message
                feedback_message = f"Your response did not conform to the required JSON schema. The following error was encountered: {error_message}. Please respond ONLY with a JSON object conforming to this schema."
                humanMessage = HumanMessage(content=feedback_message)
                chat_prompt = ChatPromptTemplate.from_messages([
                    SystemMessage(content=systemMessage),
                    humanMessage
                ])
                chain = chat_prompt | llm | parser

        if parsed_output is None:
            raise ValueError(f"LLM failed to provide valid output after {max_retries} attempts.")

        new_state = update_state(state, parsed_output, node_config)
        print_user_messages(new_state, node_config)
        print_chat_prompt(chat_prompt, node_config)
        print_state(new_state, node_config)

        return new_state
    return node_function

def create_node_tool_function(node_config, mcp_tools):
    OutputModel = create_output_model(node_config['output_model'])
    
    async def node_function(state: dict):
        print_output(f"Processing {node_config['name']} (with tools)")
        
        systemMessage = format_prompt(node_config['system'], state, node_config)
        formatted_prompt = format_prompt(node_config['prompt'], state, node_config)

        llm = LLMManager.get_llm(OutputModel.model_json_schema())
        agent = create_react_agent(llm, mcp_tools)

        result = agent.invoke({
            "messages": [
                SystemMessage(content=systemMessage),
                HumanMessage(content=formatted_prompt)
            ]
        })

        # Parse the result and update the state
        parsed_output = parse_agent_output(result['messages'][-1].content, OutputModel)
        new_state = update_state(state, parsed_output, node_config)
        
        print_user_messages(new_state, node_config)
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

    return new_state

def print_user_messages(state, node_config):
    user_message_fields = node_config.get('user_message', [])
    for field in user_message_fields:
        if field in state and state[field] is not None:
            print_ai(f"{state[field]}")

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

def build_graph(config, mcp_tools, checkpointer):
    builder = StateGraph(TypedDict('AgentState', {item['name']: eval(item['type']) for item in config['state']}))

    for node in config['nodes']:
        builder.add_node(node['name'], create_node_function(node, mcp_tools))

    for edge in config['flow']:
        if edge['from'] == 'START':
            builder.set_entry_point(edge['to'])
        elif 'edges' in edge:
            conditions = {}
            for sub_edge in edge['edges']:
                source_node_config = next(node for node in config['nodes'] if node['name'] == edge['from'])
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
    iteration = 0
    for output in await graph.ainvoke(initial_state, thread):
        globals.logger.debug(f"\nIteration: {iteration}")
        node_executed = list(output.keys())[0]
        globals.logger.debug(f"Node executed: {node_executed}")
        
        if node_executed == END:
            print("Reached END node. Process completed.")
            break

        state_history = graph.get_state_history(thread)
        globals.logger.debug("State History: ")
        for snapshot in state_history:
            globals.logger.debug(snapshot)
        
        flat_state = {}
        for key, value in output.items():
            if isinstance(value, dict):
                flat_state.update(value)
            else:
                flat_state[key] = value
        globals.logger.debug("Current state")
        globals.logger.debug(flat_state)

async def run_agent(agent):
    # Setup
    setup_colorama()
    set_debug(False)

    # Load agents
    config = load_agents(agent)

    # Initialize MCP tools
    mcp_tools = await initialize_mcp_tools()

    # Initialize state
    initial_state = {item['name']: item.get('default', None) for item in config['state']}

    # Build graph
    async with AsyncSqliteSaver.from_conn_string(":memory:") as checkpointer:
        graph = build_graph(config, mcp_tools, checkpointer)
        #graph.get_graph().draw_png(GRAPH_OUTPUT_PATH)

        while True:
            thread = {"configurable": {"thread_id": "1"}}
            await run_graph(graph, initial_state, thread)
