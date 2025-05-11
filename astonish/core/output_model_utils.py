"""
Output model utilities for Astonish.
This module contains functions for creating and handling output models.
"""
import json
import asyncio
from typing import Dict, Any, Union, Optional, Type, List, get_args, get_origin
from pydantic import Field, BaseModel, create_model, ValidationError
from langchain_core.language_models.base import BaseLanguageModel
from langchain_core.messages import HumanMessage
from langchain.output_parsers import PydanticOutputParser
from langchain.schema import OutputParserException
from astonish.core.utils import console, print_output
from astonish.core.json_utils import clean_and_fix_json

def create_output_model(output_model_config: Dict[str, str]) -> Optional[Type[BaseModel]]:
    """
    Creates a Pydantic model dynamically, handling lowercase type names from YAML
    and complex/generic types using eval.
    """
    if not output_model_config or not isinstance(output_model_config, dict):
        return None

    fields = {}
    type_lookup = {
        "str": str,
        "int": int,
        "float": float,
        "bool": bool,
        "any": Any,
        "list": List,
        "dict": Dict,
    }
    eval_context = {
         "Union": Union, "Optional": Optional, "List": List, "Dict": Dict,
         "str": str, "int": int, "float": float, "bool": bool, "Any": Any,
         "BaseModel": BaseModel
         }

    for field_name, field_type_str in output_model_config.items():
        field_type = Any
        try:
            normalized_type_str = field_type_str.strip()
            normalized_type_lower = normalized_type_str.lower()

            if normalized_type_lower in type_lookup:
                field_type = type_lookup[normalized_type_lower]

            elif any(c in normalized_type_str for c in ['[', '|', 'Optional', 'Union']):
                try:
                     field_type = eval(normalized_type_str, globals(), eval_context)
                except NameError:
                     console.print(f"Warning: Eval failed to find type components within '{normalized_type_str}'. Defaulting field '{field_name}' to Any.", style="yellow")

                     field_type = Any
                except Exception as e_eval:
                     console.print(f"Error evaluating complex type '{normalized_type_str}': {e_eval}", style="red")
                     field_type = Any
            else:
                 console.print(f"Warning: Unknown or non-generic type '{normalized_type_str}' for field '{field_name}', defaulting to Any.", style="yellow")
                 field_type = Any

            if field_type is not Any:
                origin = get_origin(field_type)
                args = get_args(field_type)

                if origin is Union and type(None) in args:
                    non_none_args = tuple(arg for arg in args if arg is not type(None))
                    if len(non_none_args) == 1: field_type = Optional[non_none_args[0]]
                    elif len(non_none_args) > 1: field_type = Optional[Union[non_none_args]]
                    else: field_type = Optional[type(None)]

            fields[field_name] = (field_type, Field(description=f"{field_name} field"))

        except Exception as e:
            console.print(f"Error processing field '{field_name}' with type string '{field_type_str}': {e}", style="red")
            fields[field_name] = (Any, Field(description=f"{field_name} field (processing error)"))

    model_name = f"DynamicOutputModel_{abs(hash(json.dumps(output_model_config, sort_keys=True)))}"
    try:
         Model = create_model(model_name, **fields)
         return Model
    except Exception as e:
         console.print(f"Failed to create Pydantic model '{model_name}': {e}", style="red")
         return None

async def _format_final_output_with_llm(
    final_text: str,
    parser: PydanticOutputParser,
    llm: BaseLanguageModel,
    node_name: str
) -> Union[BaseModel, str, Dict]: # Allow Dict if parser outputs dict
    import astonish.globals as globals
    globals.logger.info(f"Attempting to format final result into JSON schema for {node_name}...")
    format_instructions = ""
    schema_valid_for_format = False
    try:
        if hasattr(parser.pydantic_object, 'model_json_schema'): 
            format_instructions = parser.get_format_instructions()
            schema_valid_for_format = True
        else: 
            console.print(f"Error: Pydantic V2 model lacks .model_json_schema() for {node_name}", style="red")
    except Exception as e: 
        console.print(f"[{node_name}] Unexpected error getting format instructions: {e}", style="red")
    
    if not schema_valid_for_format: 
        console.print(f"Warning: Schema invalid or unavailable for formatting. Using raw ReAct result for {node_name}.", style="yellow")
        return final_text
    formatting_prompt = ( f"Analyze the following text, which is the final answer from a reasoning process. Your goal is to extract the single, most salient value or piece of information requested by the desired output schema (e.g., if the schema asks for 'execution_result', extract the primary outcome like a number, status, or summary; if it asks for 'weather_summary', extract that). Then, format ONLY this extracted value into a JSON object conforming strictly to the schema provided below. Respond ONLY with the JSON object, without any markdown formatting like ```json or explanations.\n\nFinal Answer Text:\n```\n{final_text}\n```\n\nRequired JSON Schema (extract the relevant value to fit this):\n{format_instructions}" )
    formatting_messages = [HumanMessage(content=formatting_prompt)]
    max_format_retries = 2
    format_retry_count = 0
    parsed_formatted_output = None
    while format_retry_count < max_format_retries:
        try:
            globals.logger.info(f"Attempt {format_retry_count + 1}/{max_format_retries} for JSON formatting...")
            response_chunks = []
            async for chunk in llm.astream(formatting_messages):
                response_chunks.append(chunk)
            
            # Merge the chunks
            if response_chunks:
                # Create a complete response by concatenating all chunk contents
                full_content = ""
                for chunk in response_chunks:
                    if hasattr(chunk, 'content'):
                        full_content += chunk.content
                
                # Use the last chunk as a template for the response object
                formatting_response = response_chunks[-1]
                if hasattr(formatting_response, 'content'):
                    formatting_response.content = full_content
            else:
                formatting_response = None
                
            cleaned_content = clean_and_fix_json(formatting_response.content)
            if not cleaned_content: raise OutputParserException("Received empty formatted response.")
            parsed_formatted_output = parser.parse(cleaned_content)
            globals.logger.info("Successfully extracted and formatted final result to JSON.")
            return parsed_formatted_output
        except (OutputParserException, ValidationError, json.JSONDecodeError) as parse_error:
            format_retry_count += 1
            error_detail = f"{type(parse_error).__name__}: {parse_error}"
            console.print(f"Warning: Formatting LLM call failed parsing/validation (Attempt {format_retry_count}/{max_format_retries}): {error_detail}", style="yellow")
            if format_retry_count >= max_format_retries: 
                console.print(f"Failed to format final result to JSON after retries. Storing raw text result.", style="red")
                return final_text
            feedback = f"Formatting failed: {error_detail}. Extract the single key result value from the text and provide ONLY the valid JSON object matching the schema."
            formatting_messages = [formatting_messages[0], HumanMessage(content=feedback)]
        except Exception as format_error: 
            console.print(f"Unexpected error during final result formatting LLM call: {format_error}", style="red")
            return final_text
    return final_text
