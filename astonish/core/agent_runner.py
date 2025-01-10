import yaml
import importlib
import sys
import os
import astonish.globals as globals
from astonish.tools.tool_base import ToolBase
from astonish.core.llm_manager import LLMManager
from typing import TypedDict, Callable, Any, Union, Optional, List, Dict, get_args, get_origin
from pprint import pprint
from langgraph.graph import StateGraph, END
from langchain_core.messages import SystemMessage, HumanMessage
from tavily import TavilyClient
from colorama import Fore, Style, init as colorama_init
from langgraph.checkpoint.sqlite import SqliteSaver
from langchain.output_parsers import PydanticOutputParser
from langchain.prompts import ChatPromptTemplate
from pydantic import BaseModel, Field, ValidationError, create_model
from langchain.globals import set_debug
#from IPython.display import Image

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

def load_agents(path):
    with open('astonish/agents/' + path + '.yaml', 'r') as file:
        return yaml.safe_load(file)

def initialize_tools(config):
    tools = {}
    plugin_folder = "astonish.tools"  # Default tools folder

    # Check if the environment variable for additional tool folders is set
    custom_tool_folder = os.getenv('ASTONISH_TOOLS_PATH')  # Updated variable name
    if custom_tool_folder and os.path.isdir(custom_tool_folder):
        # If the folder exists, add it to sys.path for dynamic imports
        if custom_tool_folder not in sys.path:
            sys.path.append(custom_tool_folder)

    # Load tools from the standard plugin folder (astonish.tools) and any additional folders
    for tool_config in config['tools']:
        tool_name = tool_config['name']

        tool_config = dict(globals.config[tool_name]) if tool_name in globals.config else {}

        # Try to import from the default folder first
        try:
            module = importlib.import_module(f"{plugin_folder}.{tool_name}")
            tool_class = getattr(module, 'Tool')
            tools[tool_name] = tool_class(tool_config)
        except (ImportError, AttributeError) as e:
            print(f"Error loading tool from {plugin_folder}.{tool_name}: {e}")
            sys.exit(1)
        except ValueError as e:
            print(f"Configuration error for {tool_name}: {str(e)}")
            sys.exit(1)

        # If tool wasn't found in default folder, try to load from the custom folder
        if custom_tool_folder:
            try:
                # Try importing from the custom folder using its name
                module = importlib.import_module(f"{custom_tool_folder.replace(os.path.sep, '.')}.{tool_name}")
                tool_class = getattr(module, 'Tool')
                tools[tool_name] = tool_class(tool_config)
            except (ImportError, AttributeError) as e:
                print(f"Error loading tool from custom folder {custom_tool_folder}.{tool_name}: {e}")

    return tools


def format_prompt(prompt: str, state: dict, node_config: dict):
    state_dict = dict(state)
    state_dict['state'] = state
    format_dict = {**state_dict, **node_config}
    return prompt.format(**format_dict)

def execute_tools(tool_configs, state: dict, tools):
    print_output("Executing tools...")
    results = {}
    for tool_config in tool_configs:
        tool_name = tool_config['name']
        tool = tools.get(tool_name)
        if tool and isinstance(tool, ToolBase):
            input_field = tool_config.get('input_field')
            output_field = tool_config.get('output_field')
            query = state.get(input_field, '')
            result = tool.execute(query)
            results[output_field] = result
    return results

def create_node_function(node_config, tools):
    if node_config['type'] == 'input':
        return create_input_node_function(node_config)
    elif node_config['type'] == 'llm':
        return create_llm_node_function(node_config, tools)

def create_input_node_function(node_config):
    def node_function(state: dict):
        if not (node_config.get('is_initial', False) and state.get('user_request') is not None):
            print_ai(node_config['prompt'])
        user_input = input(f"{Fore.YELLOW}You: {Style.RESET_ALL}")
        new_state = state.copy()
        new_state['user_request'] = user_input
        return new_state
    return node_function

def create_llm_node_function(node_config, tools):
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
        parsed_output = chain.invoke({})

        new_state = update_state(state, parsed_output, node_config, tools)
        print_user_messages(new_state, node_config)

        return new_state
    return node_function

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

def update_state(state, parsed_output, node_config, tools):
    new_state = state.copy()
    for field in parsed_output.model_fields:
        if getattr(parsed_output, field) is not None:
            new_state[field] = getattr(parsed_output, field)

    if 'tools' in node_config:
        tool_results = execute_tools(node_config['tools'], new_state, tools)
        new_state.update(tool_results)

    if 'limit_counter_field' in node_config and node_config['limit_counter_field']:
        new_state[node_config['limit_counter_field']] = new_state.get(node_config['limit_counter_field'], 0) + 1

    return new_state

def print_user_messages(state, node_config):
    user_message_fields = node_config.get('user_message', [])
    for field in user_message_fields:
        if field in state and state[field] is not None:
            print_ai(f"{state[field]}")

def build_graph(config, tools):
    builder = StateGraph(TypedDict('AgentState', {item['name']: eval(item['type']) for item in config['state']}))

    for node in config['nodes']:
        builder.add_node(node['name'], create_node_function(node, tools))

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

    return builder.compile()

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

def run_graph(graph, initial_state):
    iteration = 0
    for output in graph.stream(initial_state):
        globals.logger.debug(f"\nIteration: {iteration}")
        node_executed = list(output.keys())[0]
        globals.logger.debug(f"Node executed: {node_executed}")
        
        if node_executed == END:
            print("Reached END node. Process completed.")
            break
        
        flat_state = {}
        for key, value in output.items():
            if isinstance(value, dict):
                flat_state.update(value)
            else:
                flat_state[key] = value
        globals.logger.debug("Current state")
        globals.logger.debug(flat_state)

def run_agent(agent):
    # Setup
    setup_colorama()
    set_debug(False)

    # Load agents
    config = load_agents(agent)

    # Initialize tools
    tools = initialize_tools(config)

    # Build graph
    graph = build_graph(config, tools)
    graph.get_graph().draw_png(GRAPH_OUTPUT_PATH)

    # Initialize state
    initial_state = {item['name']: item.get('default', None) for item in config['state']}

    # Run graph
    run_graph(graph, initial_state)