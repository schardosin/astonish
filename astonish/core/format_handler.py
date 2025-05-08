"""
Format handler module for Astonish.
This module contains functions for handling format violations and tool execution.
"""
import json
import asyncio
import traceback
from typing import Dict, Any
from pydantic import BaseModel
from astonish.core.utils import print_output, console

async def execute_tool(
    node_name: str,
    tool_name: str,
    input_string: str,
    tool_registry: Dict[str, Any]
) -> Dict[str, Any]:
    """
    Execute a tool with the corrected input.
    
    Args:
        node_name: The name of the node
        tool_name: The name of the tool to execute
        input_string: The input string for the tool
        tool_registry: The tool registry
    Returns:
        A dictionary with the result of the tool execution
    """
    if tool_name in tool_registry:
        tool_definition = tool_registry[tool_name]
        tool_input_type = tool_definition['input_type']
        tool_schema_def = tool_definition['input_schema_definition']
        tool_executor = tool_definition['tool_executor']
        
        try:
            # Process the input based on the tool type
            if tool_input_type == 'STRING':
                tool_args_for_execution = input_string
            elif tool_input_type == 'JSON_SCHEMA':
                if not tool_schema_def:
                    raise ValueError(f"No schema definition found for JSON tool '{tool_name}'")
                
                cleaned_json_string = input_string.removeprefix("```json").removesuffix("```").strip()
                parsed_args = {} if not cleaned_json_string else json.loads(cleaned_json_string)
                
                if isinstance(tool_schema_def, type) and issubclass(tool_schema_def, BaseModel):
                    validated_args = tool_schema_def(**parsed_args)
                    tool_args_for_execution = validated_args.model_dump(mode='json')
                else:
                    tool_args_for_execution = parsed_args
            else:
                raise ValueError(f"Unsupported tool input_type: '{tool_input_type}'")
            
            print_output(f"Executing tool '{tool_name}'")
            executor_is_async = asyncio.iscoroutinefunction(tool_executor)
            if executor_is_async:
                tool_result = await tool_executor(tool_args_for_execution)
            else:
                tool_result = await asyncio.to_thread(tool_executor, tool_args_for_execution)
            
            observation = str(tool_result)
            return {"output": observation}
        except Exception as exec_error:
            error_message = f"Error executing tool '{tool_name}': {exec_error}"
            console.print(f"[{node_name}] {error_message}", style="red")
            console.print(f"Traceback:\n{traceback.format_exc()}", style="red")
            return {"output": f"Error: Tool execution failed - {exec_error}"}
    else:
        error_message = f"Error: Unknown tool: {tool_name}"
        console.print(f"[{node_name}] {error_message}", style="red")
        return {"output": error_message}
