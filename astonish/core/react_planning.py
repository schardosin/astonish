"""
ReAct planning module for Astonish.
This module contains functions for ReAct planning and execution.
"""
import re
import json
import traceback
from typing import TypedDict, Optional, Any, List, Callable, Coroutine
from langchain_core.tools import BaseTool
from langchain_core.language_models.base import BaseLanguageModel
from langchain_core.prompts import ChatPromptTemplate
import astonish.globals as globals
from astonish.core.utils import print_output, console
from astonish.core.json_utils import clean_and_fix_json
from astonish.core.prompt_templates import create_custom_react_prompt_template, create_first_run_react_prompt_template
from astonish.core.utils import remove_think_tags

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
        # Determine if this is the first run by checking if the scratchpad is empty
        is_first_run = not agent_scratchpad.strip()
        
        # Use the appropriate template based on whether it's the first run or not
        if is_first_run:
            globals.logger.info(f"[{node_name}] Using first-run template (no Observation or Final Answer)")
            react_instructions_template = create_first_run_react_prompt_template(tool_definitions)
        else:
            globals.logger.info(f"[{node_name}] Using subsequent-run template (with Observation and Final Answer)")
            react_instructions_template = create_custom_react_prompt_template(tool_definitions)
        prompt_str_template = system_message_content + "\n\n" + react_instructions_template
        custom_react_prompt = ChatPromptTemplate.from_template(prompt_str_template)
        chain = custom_react_prompt | llm

        invoke_input = {
            "input": input_question,
            "agent_scratchpad": agent_scratchpad
        }

        globals.logger.debug(f"[{node_name}] Invoking LLM with the prompt:\n{custom_react_prompt.format(**invoke_input)}")
        
        raw_llm_response = await chain.ainvoke(invoke_input);
        raw_llm_response_text = raw_llm_response.content

        globals.logger.info(f"[{node_name}] LLM Raw Planning Response (with potential tags):\n{raw_llm_response_text}")
        if print_prompt:
            print_output(f"Input to LLM for {node_name}:\n{invoke_input}", "green")

        cleaned_response_text = remove_think_tags(raw_llm_response_text)
        globals.logger.info(f"[{node_name}] LLM Cleaned Planning Response (tags removed):\n{cleaned_response_text}")

        thought_blocks = re.findall(r"^\s*Thought:\s*(.*?)(?=(?:\n\s*(?:Action:|Final Answer:|$)))", cleaned_response_text, re.DOTALL | re.MULTILINE)
        thought_text = thought_blocks[-1].strip() if thought_blocks else None

        action_match = re.search(r"^\s*Action:\s*([\w.-]+)", cleaned_response_text, re.MULTILINE | re.IGNORECASE)
        input_string_from_llm = ""

        action_input_line_match = re.search(r"^\s*Action Input:\s*(.*?)(?:\nObservation:|\Z)", cleaned_response_text, re.DOTALL | re.MULTILINE | re.IGNORECASE)
        if action_input_line_match:
            raw_input_line = action_input_line_match.group(1).strip()
            json_match = re.match(r"^```json\s*(\{.*?\})\s*```$", raw_input_line, re.DOTALL) or re.match(r"^(\{.*?\})\s*$", raw_input_line, re.DOTALL)
            if json_match: 
                input_string_from_llm = json_match.group(1)
                globals.logger.debug(f"[{node_name}] Extracted JSON Action Input: {input_string_from_llm}")
            else:
                if '{' in raw_input_line and '}' in raw_input_line:
                    input_string_from_llm = clean_and_fix_json(raw_input_line)
                    globals.logger.debug(f"[{node_name}] Cleaned and fixed JSON Action Input: {input_string_from_llm}")
                else:
                    input_string_from_llm = raw_input_line
                    globals.logger.debug(f"[{node_name}] Extracted String Action Input: {input_string_from_llm}")

        if "Final Answer:" in cleaned_response_text:
            # Use cleaned_response_text for splitting
            final_answer_text = cleaned_response_text.split("Final Answer:")[-1].strip()
            final_answer_json = json.loads(final_answer_text, strict=False)
            content = final_answer_json['result']

            globals.logger.info(f"[{node_name}] LLM provided Final Answer.")
            return ReactStepOutput(
                status='final_answer', tool=None, tool_input=None, answer=content,
                thought=thought_text, raw_response=raw_llm_response_text, # Store original raw response
                message_content_for_history=None # Or construct from cleaned_response_text if needed
            )
        elif action_match:
            tool_name = action_match.group(1).strip()
            globals.logger.info(f"[{node_name}] LLM planned Action: {tool_name}")
            return ReactStepOutput(
                status='action', tool=tool_name, tool_input=input_string_from_llm,
                answer=None, thought=thought_text, raw_response=raw_llm_response_text, # Store original raw response
                message_content_for_history=None # Or construct from cleaned_response_text if needed
            )
        else:
            error_message = f"LLM response did not contain 'Action:' or 'Final Answer:' after cleaning tags."
            globals.logger.error(f"[{node_name}] Parsing Error: {error_message}. Cleaned response: '{cleaned_response_text}'")
            return ReactStepOutput(
                status='error', tool=None, tool_input=None, answer=None, thought=thought_text,
                raw_response=raw_llm_response_text, # Store original raw response
                message_content_for_history=None
            )

    except Exception as planning_error:
        error_message = f"Critical error during ReAct planning step: {type(planning_error).__name__}: {planning_error}"
        console.print(f"[{node_name}] {error_message}", style="red")
        globals.logger.error(f"Traceback:\n{traceback.format_exc()}")
        output = default_error_output.copy()
        output['raw_response'] = f"Error: {error_message}" 
        return output

def format_react_step_for_scratchpad(
    thought: Optional[str],
    action: Optional[str],
    action_input: Optional[str],
    observation: str
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
    scratchpad_entry = "\n" 
    if thought:
        # Thought should already be clean from parsing `cleaned_response_text`
        thought_clean = re.sub(r"^\s*Thought:\s*", "", thought).strip()
        scratchpad_entry += f"Thought: {thought_clean}\n"
    if action:
         scratchpad_entry += f"Action: {action}\n"
    if action_input is not None: 
         scratchpad_entry += f"Action Input: {action_input}\n"
    scratchpad_entry += f"Observation: {observation}\n"
    return scratchpad_entry
