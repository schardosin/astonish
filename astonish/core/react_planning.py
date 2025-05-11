"""
ReAct planning module for Astonish.
This module contains functions for ReAct planning and execution.
"""
import re
import json
import asyncio
import traceback
from typing import TypedDict, Union, Optional, Dict, Any, List, Callable, Coroutine
from langchain_core.messages import SystemMessage, HumanMessage, BaseMessage
from langchain_core.tools import BaseTool
from langchain_core.language_models.base import BaseLanguageModel
from langchain_core.prompts import ChatPromptTemplate
import astonish.globals as globals
from astonish.core.utils import print_output, console
from astonish.core.json_utils import clean_and_fix_json
from astonish.core.prompt_templates import create_custom_react_prompt_template

class ToolDefinition(TypedDict):
    """
    Type definition for a tool in the ReAct planning process.
    """
    name: str
    description: str
    input_type: str
    input_schema_definition: Optional[Any]
    tool_executor: Callable[..., Coroutine[Any, Any, str]]
    tool_instance: Optional[BaseTool]

class ReactStepOutput(TypedDict):
    """
    Structured output for a single ReAct planning step.
    """
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
    
    Args:
        input_question: The input question to answer
        agent_scratchpad: The current agent scratchpad
        system_message_content: The system message content
        llm: The language model to use
        tool_definitions: The list of tool definitions
        node_name: The name of the node
        print_prompt: Whether to print the prompt
        
    Returns:
        A ReactStepOutput object with the result of the planning step
    """
    globals.logger.info(f"[{node_name}] Running ReAct planning step...")
    default_error_output = ReactStepOutput(
        status='error', tool=None, tool_input=None, answer=None, thought=None,
        raw_response="Planner failed unexpectedly.", message_content_for_history=None
    )
    try:
        react_instructions_template = create_custom_react_prompt_template(tool_definitions)
        prompt_str_template = system_message_content + "\n\n" + react_instructions_template
        custom_react_prompt = ChatPromptTemplate.from_template(prompt_str_template)
        chain = custom_react_prompt | llm

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
                message_content_for_history=None
            )
        elif "Final Answer:" in response_text:
            final_answer_text = response_text.split("Final Answer:")[-1].strip()
            globals.logger.info(f"[{node_name}] LLM provided Final Answer.")
            return ReactStepOutput(
                status='final_answer', tool=None, tool_input=None, answer=final_answer_text,
                thought=thought_text, raw_response=response_text, message_content_for_history=None
            )
        else:
            error_message = f"LLM response did not contain 'Action:' or 'Final Answer:'."
            globals.logger.error(f"[{node_name}] Parsing Error: {error_message}")
            return ReactStepOutput(
                status='error', tool=None, tool_input=None, answer=None, thought=thought_text,
                raw_response=response_text, message_content_for_history=None
            )

    except Exception as planning_error:
        error_message = f"Critical error during ReAct planning step: {type(planning_error).__name__}: {planning_error}"
        console.print(f"[{node_name}] {error_message}", style="red")
        console.print(f"Traceback:\n{traceback.format_exc()}", style="red")
        output = default_error_output.copy()
        output['raw_response'] = f"Error: {error_message}\n{traceback.format_exc()}"
        return output

def format_react_step_for_scratchpad(
    thought: Optional[str],
    action: Optional[str],
    action_input: Optional[str],
    observation: str # Observation is always required after an action
    ) -> str:
    """
    Formats a completed Thought-Action-Input-Observation step for the scratchpad string.
    
    Args:
        thought: The thought text
        action: The action name
        action_input: The action input
        observation: The observation text
        
    Returns:
        A formatted string for the scratchpad
    """
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
