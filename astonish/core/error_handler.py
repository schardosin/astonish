import json
import traceback
from typing import Dict, Any, Optional, Union, Type
from pydantic import ValidationError
from langchain.schema import OutputParserException
import astonish.globals as globals
from astonish.core.utils import print_ai, print_section, print_output, console

def create_error_feedback(error: Exception, node_name: str) -> str:
    """
    Generate specific feedback for the LLM based on the error type.
    
    Args:
        error: The exception that occurred
        node_name: The name of the node where the error occurred
        
    Returns:
        A string with specific feedback to help the LLM correct its response
    """
    if isinstance(error, OutputParserException):
        return (
            f"Your previous response couldn't be parsed correctly. Please ensure your response "
            f"is in the exact format requested. Error details: {error}"
        )
    
    elif isinstance(error, ValidationError):
        # Extract field-specific validation errors
        error_details = []
        for err in error.errors():
            loc = ".".join(str(l) for l in err["loc"])
            error_details.append(f"Field '{loc}': {err['msg']}")
        
        return (
            f"Your response failed validation. Please fix the following issues:\n" + 
            "\n".join(error_details)
        )
    
    elif isinstance(error, json.JSONDecodeError):
        return (
            f"Your response is not valid JSON. Error at position {error.pos}: {error.msg}. "
            f"Please provide a properly formatted JSON response."
        )
    
    elif isinstance(error, str) and "format violation" in error.lower():
        return (
            f"Your response did not follow the required format. You must provide either an Action or a Final Answer. "
            f"Please follow the format strictly:\n\n"
            f"Thought: your reasoning\n"
            f"Action: the tool to use\n"
            f"Action Input: the input for the tool\n\n"
            f"OR\n\n"
            f"Thought: your final reasoning\n"
            f"Final Answer: your final answer"
        )
    
    else:
        return (
            f"An error occurred processing your response: {type(error).__name__}: {error}. "
            f"Please try again with a simpler, correctly formatted response."
        )

async def handle_llm_error(
    node_name: str, 
    error_type: str,
    error_details: Any,
    response_text: str, 
    system_message: str, 
    human_message: str, 
    llm: Any, 
    state: Dict[str, Any], 
    max_retries: int = 2
) -> Dict[str, Any]:
    """
    Handle LLM errors by retrying with feedback.
    
    Args:
        node_name: The name of the node where the error occurred
        error_type: The type of error (e.g., "format_violation", "json_processing", etc.)
        error_details: Details about the error (e.g., exception object, error message)
        response_text: The LLM response that caused the error
        system_message: The system message for the LLM
        human_message: The human message for the LLM
        llm: The LLM to use for retries
        state: The current state dictionary
        max_retries: Maximum number of retries
        
    Returns:
        A dictionary with the result of the retry or an error state
    """
    from langchain_core.messages import SystemMessage, HumanMessage
    import re
    
    # Get retry count key based on error type
    retry_count_key = f"_{error_type}_retry_count"
    
    # Get current retry count
    current_retry = state.get(retry_count_key, 0)
    
    if current_retry >= max_retries:
        # We've exhausted retries, create an error state
        if error_type == "format_violation":
            error_message = f"The AI model did not follow the required format for {node_name} after {max_retries} attempts."
            error_type_name = "FormatViolationError"
            user_message = (
                f"I apologize, but I was unable to process the '{node_name}' step correctly. "
                f"The AI model did not follow the required format after {max_retries} attempts. "
                f"This is likely due to the complexity of the task or limitations of the current model. "
                f"You could try again with a different model or simplify the task."
            )
        elif error_type == "json_processing":
            error_message = f"JSON processing error in {node_name} after {max_retries} attempts: {error_details}"
            error_type_name = "JSONProcessingError"
            user_message = (
                f"I apologize, but I was unable to process the input correctly after {max_retries} attempts. "
                f"There was an issue with the JSON format that the AI model generated. "
                f"This sometimes happens when the model struggles with structured data formats. "
                f"You could try again, or if this persists, try using a different model that might handle JSON better."
            )
        else:
            error_message = f"Error in {node_name} after {max_retries} attempts: {error_details}"
            error_type_name = "LLMError"
            user_message = (
                f"I apologize, but I was unable to process the '{node_name}' step correctly after {max_retries} attempts. "
                f"The AI model encountered difficulties completing this task. "
                f"You might want to try again, or consider using a different model that might be better suited for this type of task."
            )
        
        # Log the error to the logger instead of printing it
        globals.logger.error(f"[{node_name}] {error_message}")
        
        return {
            "output": f"Error: {error_message}",
            "_error": {
                'node': node_name,
                'message': error_message,
                'type': error_type_name,
                'user_message': user_message,
                'recoverable': False
            }
        }
    
    # Increment retry count
    current_retry += 1
    console(f"Attempting to fix {error_type} issue (Retry {current_retry}/{max_retries})...", style="yellow")
    
    # Create feedback for the LLM based on error type
    if error_type == "format_violation":
        feedback = create_error_feedback("format violation", node_name)
    elif error_type == "json_processing":
        feedback = (
            f"There was an error processing your input: {error_details}\n\n"
            f"Please provide a valid JSON object with the required parameters. Common issues include:\n"
            f"- Extra quotes around the JSON object\n"
            f"- Missing or incorrect field names\n"
            f"- Incorrect data types\n"
        )
    else:
        feedback = f"There was an error: {error_details}. Please try again with a different approach."
    
    feedback += f"\n\nLet's try again with the original question: {human_message}"
    
    try:
        print_output("Sending format correction feedback to LLM...")
        
        # Create a new prompt with the feedback
        messages = [
            SystemMessage(content=system_message),
            HumanMessage(content=human_message),
            HumanMessage(content=feedback)
        ]
        
        # Call the LLM again
        new_response = await llm.ainvoke(messages)
        new_response_text = new_response.content if hasattr(new_response, 'content') else str(new_response)
        
        # Check if the new response has Action or Final Answer
        if re.search(r"^\s*Action:\s*([\w.-]+)", new_response_text, re.MULTILINE | re.IGNORECASE):
            print_output(f"Format correction successful! LLM now provides an Action.")
            # Return a special flag to indicate that this response needs to be processed for tool execution
            return {
                "output": new_response_text, 
                retry_count_key: current_retry,
                "_needs_tool_execution": True  # Special flag to indicate this needs tool execution
            }
        elif "Final Answer:" in new_response_text:
            print_output(f"Format correction successful! LLM now provides a Final Answer.")
            final_answer = new_response_text.split("Final Answer:")[-1].strip()
            return {"output": final_answer, retry_count_key: current_retry}
        else:
            # Still no proper format, update state and try again recursively
            print_output(f"Format correction failed. LLM still doesn't follow the format.")
            state[retry_count_key] = current_retry
            return await handle_llm_error(
                node_name, error_type, error_details, new_response_text, 
                system_message, human_message, llm, state, max_retries
            )
    except Exception as retry_error:
        console.print(f"Error during format correction retry: {retry_error}", style="red")
        
        # Create an error state
        error_message = f"Error during format correction retry: {retry_error}"
        return {
            "output": f"Error: {error_message}",
            "_error": {
                'node': node_name,
                'message': error_message,
                'type': 'RetryError',
                'user_message': f"I apologize, but I encountered an error while trying to correct the format for '{node_name}'.",
                'recoverable': False
            }
        }

def handle_node_failure(state: Dict[str, Any], node_name: str, error: Exception, 
                        max_retries: int) -> Dict[str, Any]:
    """
    Handle the case when a node has failed after all retries.
    
    Args:
        state: The current state dictionary
        node_name: The name of the node that failed
        error: The exception that caused the failure
        max_retries: The maximum number of retries that were attempted
        
    Returns:
        An updated state dictionary with error information
    """
    # Create a user-friendly error message with more detailed guidance
    user_message = (
        f"I apologize, but I was unable to process the '{node_name}' step correctly "
        f"after {max_retries} attempts."
    )
    
    if isinstance(error, OutputParserException):
        user_message += (
            " The AI model is having trouble generating the correct format for this response. "
            "This could be due to the complexity of the required output format. "
            "You might want to try again with a different model that has better structured output capabilities."
        )
    elif isinstance(error, ValidationError):
        user_message += (
            " The AI model generated a response that didn't match the expected structure. "
            "This usually happens when the model omits required fields or provides values in the wrong format. "
            "You could try again, or use a model with better JSON/structured data capabilities."
        )
    elif isinstance(error, json.JSONDecodeError):
        user_message += (
            " The AI model generated invalid JSON data. "
            "This sometimes happens when the model struggles with maintaining proper JSON syntax. "
            "You might want to try again with a different model that has better JSON generation capabilities."
        )
    else:
        user_message += (
            f" An unexpected error occurred: {type(error).__name__}. "
            "This might be due to a temporary issue or a limitation of the current model. "
            "You could try running the agent again, or consider using a different model."
        )
    
    # Log the detailed error for developers
    globals.logger.error(f"Node {node_name} failed after {max_retries} attempts. Last error: {error}")
    if hasattr(error, '__traceback__'):
        globals.logger.debug(f"Traceback: {traceback.format_exc()}")
    
    # Create error information
    error_info = {
        'node': node_name,
        'message': str(error),
        'type': type(error).__name__,
        'user_message': user_message,
        'recoverable': False  # At this point, we've exhausted retries
    }
    
    # Print user-friendly message without a section header
    print_ai(user_message)
    
    # Return a minimal state with just error info to avoid conflicts
    return {
        '_error': error_info,
        '_end': False  # Let the error handler node set this to True
    }

def create_error_handler_node():
    """
    Creates a node function that handles errors and terminates the flow.
    
    Returns:
        A function that can be used as a node in the graph
    """
    def error_handler_node(state: Dict[str, Any]) -> Dict[str, Any]:
        error_info = state.get('_error')
        if not error_info or not isinstance(error_info, dict):
            error_info = {}
            
        node_name = error_info.get('node', 'Unknown node')
        user_message = error_info.get('user_message', 'An unknown error occurred.')
        
        # Print a clean, user-friendly error message without a section header
        print_ai(user_message)
        
        # Return a minimal state with just error info and end signal to avoid conflicts
        return {
            "_error": error_info,
            "_end": True
        }
    
    return error_handler_node

def is_error_state(state: Dict[str, Any]) -> str:
    """
    Check if the state contains an error that should terminate the flow.
    
    Args:
        state: The current state dictionary
        
    Returns:
        The name of the next node to transition to: "_error_handler" if there's an 
        unrecoverable error, or "END" otherwise
    """
    from langgraph.graph import END
    
    try:
        if not state or not isinstance(state, dict):
            return END
            
        if '_error' not in state:
            return END
        
        error_info = state.get('_error')
        if not error_info or not isinstance(error_info, dict):
            return END
        
        # Always route to error handler if there's an error
        # This ensures we don't continue processing after an error
        return "_error_handler"
    except Exception as e:
        # Log the error but don't propagate it - just return END
        globals.logger.error(f"Error in is_error_state: {e}")
        return END
