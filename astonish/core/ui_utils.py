"""
UI utilities for Astonish.
This module contains functions for handling UI/UX elements.
"""
import json
from typing import Dict, Any
from langchain_core.prompts import BasePromptTemplate
from langchain_core.messages import SystemMessage, HumanMessage
from astonish.core.utils import format_prompt, print_ai, console, print_output
from collections.abc import Iterable

def print_user_messages(state: Dict[str, Any], node_config: Dict[str, Any]):
    """
    Prints messages defined in node_config, substituting state variables.
    
    Args:
        state: The current state dictionary
        node_config: The node configuration dictionary
    """
    user_message_fields = node_config.get('user_message', [])
    if not isinstance(user_message_fields, list):
         print(f"Warning: 'user_message' in node config is not a list.", style="yellow")
         return
    for field_or_template in user_message_fields:
        if isinstance(field_or_template, str):
            if field_or_template in state and state[field_or_template] is not None:
                value = state.get(field_or_template)
                if isinstance(value, Iterable) and not isinstance(value, str):
                    print_ai('\n'.join(str(item) for item in value))
                else:
                    print_ai(str(value))
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
    """
    Prints formatted chat prompt messages if enabled in config.
    
    Args:
        chat_prompt: The chat prompt template
        node_config: The node configuration dictionary
    """
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
    """
    Prints the current state dictionary if enabled in config.
    
    Args:
        state: The current state dictionary
        node_config: The node configuration dictionary
    """
    print_state_flag = node_config.get('print_state', False)
    node_name = node_config.get('name', 'Unknown Node')
    if print_state_flag:
        print_output(f"Current State after {node_name}:")
        try:
            state_str = json.dumps(state, indent=2, default=lambda o: f"<non-serializable: {type(o).__name__}>")
            console.print(f"{state_str}", style="green")
        except Exception as e: print(f"Could not serialize state to JSON. Error: {e}. Raw state:\n{state}", style="red")
        print("-" * 20)
