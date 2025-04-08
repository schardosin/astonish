import json
from importlib import resources
from astonish.tools.internal_tools import tools
from astonish.core.llm_manager import LLMManager
import astonish.globals as globals
from typing import TypedDict, Union, Optional, get_args, get_origin
from langgraph.graph import StateGraph, END
from langchain_core.messages import SystemMessage, HumanMessage
from colorama import Fore, Style
from langchain.output_parsers import PydanticOutputParser
from langchain.prompts import ChatPromptTemplate
from pydantic import Field, ValidationError, create_model
from langchain.schema import OutputParserException
from langgraph.prebuilt import create_react_agent
from langgraph.checkpoint.memory import MemorySaver
from astonish.core.utils import format_prompt, print_ai, print_output, print_dict, request_tool_execution

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
