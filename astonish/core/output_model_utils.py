"""
Output model utilities for Astonish.
This module contains functions for creating and handling output models.
"""
import json
from typing import Any, Dict, List, Optional, Type, Union, get_args, get_origin
from pydantic import Field, BaseModel, create_model, ValidationError
from langchain.output_parsers import PydanticOutputParser
from astonish.core.utils import console
from astonish.core.json_utils import clean_and_fix_json
import astonish.globals as globals
import ast

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

async def _format_final_output(
    final_text: str,
    parser: PydanticOutputParser,
    node_name: str
) -> Union[BaseModel, str]:
    """
    Programmatically populates a Pydantic model, assumed to have a single field,
    with the given final_text. Performs basic type coercion.
    For list types, splits multi-line text into list items.
    No LLM calls are made. The function name is kept as per user request.
    """
    globals.logger.info(
        f"[{node_name}] Attempting programmatic population for single-field output model "
        f"using _format_final_output."
    )

    try:
        # Ensure it's a Pydantic model and get its fields
        pydantic_model_class: Type[BaseModel] = parser.pydantic_object # type: ignore
        if not hasattr(pydantic_model_class, 'model_fields'):
            error_msg = (
                f"[{node_name}] parser.pydantic_object ('{pydantic_model_class.__name__}') "
                f"is not a Pydantic V2 model or lacks model_fields."
            )
            globals.logger.error(error_msg)
            return final_text # Return raw text as it's a fundamental model issue

        model_fields = pydantic_model_class.model_fields
        if len(model_fields) != 1:
            error_msg = (
                f"[{node_name}] This function expects the output_model ('{pydantic_model_class.__name__}') "
                f"to have exactly 1 field for programmatic population, "
                f"but found {len(model_fields)}. Configuration error or model definition mismatch."
            )
            globals.logger.error(error_msg)
            return final_text # Return raw text due to model structure mismatch

        # Exactly one field, proceed with programmatic assignment
        field_name = list(model_fields.keys())[0]
        field_info = model_fields[field_name]
        
        coerced_value: Any
        target_type_annotation = field_info.annotation

        actual_target_type = target_type_annotation
        is_optional = False
        origin_type = getattr(actual_target_type, '__origin__', None) # Get origin for generics like List[str]

        if origin_type is Union: # Check if it's Union (which includes Optional)
            union_args = get_args(target_type_annotation)
            non_none_args = [arg for arg in union_args if arg is not type(None)]
            if type(None) in union_args:
                is_optional = True
            
            if len(non_none_args) == 1:
                actual_target_type = non_none_args[0]
                origin_type = getattr(actual_target_type, '__origin__', None) # Update origin for the unwrapped type
            elif len(non_none_args) > 1:
                globals.logger.info(
                    f"[{node_name}] Field '{field_name}' is a complex Union {target_type_annotation}. "
                    f"Pydantic will attempt coercion from string."
                )
                actual_target_type = Any # Fallback to Any
                origin_type = Union # Indicate it's still a Union for logic below if needed

        globals.logger.info(
            f"[{node_name}] Target field: '{field_name}', Type: {actual_target_type}, Optional: {is_optional}, Origin: {origin_type}"
        )

        # Define type checks based on actual_target_type and origin_type
        is_list_target_type = (actual_target_type is list or origin_type is list or origin_type is List or
                               (isinstance(actual_target_type, type) and issubclass(actual_target_type, list)))
        is_dict_target_type = (actual_target_type is dict or origin_type is dict or origin_type is Dict or
                               (isinstance(actual_target_type, type) and issubclass(actual_target_type, dict)))

        if final_text is None:
            if is_optional:
                coerced_value = None
            else:
                raise ValueError(f"Received None for non-optional field '{field_name}'.")
        elif actual_target_type is str:
            coerced_value = final_text
        elif actual_target_type is int:
            # Use .strip() as in original code for int, float, bool
            processed_final_text = final_text.strip()
            if not processed_final_text and is_optional: # Handle empty string for optional int
                 coerced_value = None
            elif not processed_final_text and not is_optional:
                 raise ValueError(f"Cannot convert empty string to int for non-optional field '{field_name}'.")
            else:
                 coerced_value = int(processed_final_text)
        elif actual_target_type is float:
            processed_final_text = final_text.strip()
            if not processed_final_text and is_optional: # Handle empty string for optional float
                 coerced_value = None
            elif not processed_final_text and not is_optional:
                 raise ValueError(f"Cannot convert empty string to float for non-optional field '{field_name}'.")
            else:
                 coerced_value = float(processed_final_text)
        elif actual_target_type is bool:
            val = final_text.strip().lower()
            if not val and is_optional: # Handle empty string for optional bool
                coerced_value = None
            elif not val and not is_optional:
                raise ValueError(f"Cannot convert empty string to bool for non-optional field '{field_name}'.")
            elif val in ["true", "1", "yes", "y", "on"]:
                coerced_value = True
            elif val in ["false", "0", "no", "n", "off"]:
                coerced_value = False
            else:
                raise ValueError(f"Cannot coerce '{final_text}' to bool for field '{field_name}'.")
        
        elif is_list_target_type: # NEW: Specific handling for list types
            # final_text is guaranteed not to be None here.
            parsed_successfully = False
            temp_coerced_value = None

            stripped_final_text = final_text.strip() # Strip leading/trailing whitespace

            # Attempt 1: Parse as a Python literal (e.g., "['a', 'b']" or "('a', 'b')")
            if stripped_final_text.startswith(('[', '(')) and stripped_final_text.endswith((']', ')')):
                try:
                    evaluated_data = ast.literal_eval(stripped_final_text)
                    if isinstance(evaluated_data, (list, tuple)):
                        temp_coerced_value = list(evaluated_data) # Ensure it's a list
                        parsed_successfully = True
                        globals.logger.info(
                            f"[{node_name}] Coerced text to list for field '{field_name}' using ast.literal_eval. "
                            f"Preview: {str(temp_coerced_value)[:100]}"
                        )
                except (ValueError, SyntaxError, TypeError) as e_ast: # TypeError for None if somehow passed
                    globals.logger.debug(
                        f"[{node_name}] ast.literal_eval failed for field '{field_name}' "
                        f"with text '{stripped_final_text[:100]}...'. Error: {e_ast}. Trying other methods."
                    )

            # Attempt 2: If not parsed as Python literal, try as JSON array (e.g., "[\"a\", \"b\"]")
            if not parsed_successfully and stripped_final_text.startswith('[') and stripped_final_text.endswith(']'):
                try:
                    # clean_and_fix_json might be helpful if the string is almost JSON but slightly malformed
                    # However, ast.literal_eval is often stricter with Python syntax.
                    # For JSON parsing, clean_and_fix_json is more relevant.
                    cleaned_text_for_json = clean_and_fix_json(final_text) # Use original final_text for cleaner
                    if cleaned_text_for_json: # Ensure not empty string after cleaning
                        evaluated_data = json.loads(cleaned_text_for_json, strict=False)
                        if isinstance(evaluated_data, list):
                            temp_coerced_value = evaluated_data
                            parsed_successfully = True
                            globals.logger.info(
                                f"[{node_name}] Coerced text to list for field '{field_name}' using json.loads. "
                                f"Preview: {str(temp_coerced_value)[:100]}"
                            )
                        else:
                            globals.logger.debug(
                                f"[{node_name}] json.loads on '{cleaned_text_for_json[:100]}...' "
                                f"did not yield a list for field '{field_name}'."
                            )
                    else:
                        globals.logger.debug(f"[{node_name}] Text for JSON parsing became empty after cleaning for field '{field_name}'.")

                except json.JSONDecodeError as e_json:
                    globals.logger.debug(
                        f"[{node_name}] json.loads failed for field '{field_name}' "
                        f"with text '{final_text[:100]}...'. Error: {e_json}. Falling back to splitlines."
                    )
                except Exception as e_json_other: # Catch other unexpected errors during JSON processing
                    globals.logger.error(
                        f"[{node_name}] Unexpected error during JSON list parsing for '{final_text[:100]}...': {e_json_other}"
                    )


            # Fallback: If not parsed as a literal list (Python or JSON), assume multi-line string
            if not parsed_successfully:
                if final_text == "" and is_optional:
                    # For an optional list field, an empty input string could mean None
                    temp_coerced_value = None
                    globals.logger.info(f"[{node_name}] Setting optional list field '{field_name}' to None due to empty input string.")
                elif final_text == "":
                    # For a non-optional list field, an empty input string results in an empty list
                    temp_coerced_value = []
                    globals.logger.info(f"[{node_name}] Coerced empty string to empty list for field '{field_name}' using splitlines logic.")
                else:
                    # Original splitlines logic for multi-line text
                    temp_coerced_value = [line.strip() for line in final_text.splitlines() if line.strip()]
                    globals.logger.info(
                        f"[{node_name}] Coerced multi-line text to list for field '{field_name}' using splitlines. "
                        f"Preview: {str(temp_coerced_value)[:100]}"
                    )
            
            coerced_value = temp_coerced_value
            globals.logger.info(f"[{node_name}] Coerced multi-line text to list for field '{field_name}'. Preview: {str(coerced_value)[:100]}")

        elif is_dict_target_type: # Existing logic for dict types
            try:
                # Using 'final_text' as input to clean_and_fix_json, consistent with original combined block
                cleaned_final_text = clean_and_fix_json(final_text) 
                if cleaned_final_text is None: # Check if cleaning resulted in None
                     raise ValueError(
                         f"Input text for JSON parsing for field '{field_name}' became None after cleaning."
                     )
                if not cleaned_final_text.strip() and is_optional: # Handle empty string for optional dict after cleaning
                    coerced_value = None
                elif not cleaned_final_text.strip() and not is_optional:
                    raise ValueError(f"Cannot parse empty string as dict for non-optional field '{field_name}'.")
                else:
                    parsed_json = json.loads(cleaned_final_text, strict=False)
                    if not isinstance(parsed_json, dict): # Ensure the parsed result is a dict
                        raise ValueError(
                            f"Expected dict for field '{field_name}', but JSON parsing yielded {type(parsed_json).__name__} "
                            f"from text: '{cleaned_final_text[:100]}'"
                        )
                    coerced_value = parsed_json
            except json.JSONDecodeError as e:
                raise ValueError(
                    f"Failed to parse JSON for dict field '{field_name}' from text: '{final_text[:100]}...'. Error: {e}"
                )
        
        elif actual_target_type is Any or origin_type is Union: # Check origin_type for complex unions not fully resolved
             globals.logger.info(
                 f"[{node_name}] Field '{field_name}' type is Any or complex Union. "
                 f"Assigning raw text; Pydantic will handle coercion."
             )
             coerced_value = final_text # Use original final_text as per existing Any/else logic
        else:
            globals.logger.info(
                f"[{node_name}] Attempting direct assignment for field '{field_name}' of type "
                f"{actual_target_type}, Pydantic will handle coercion from string."
            )
            coerced_value = final_text # Use original final_text
        
        parsed_output = pydantic_model_class(**{field_name: coerced_value})
        globals.logger.info(
            f"[{node_name}] Successfully created Pydantic model instance programmatically: "
            f"{pydantic_model_class.__name__}({field_name}=repr('{str(coerced_value)}'[:50]))"
        )
        return parsed_output

    except (TypeError, ValueError, ValidationError) as e:
        error_detail = f"{type(e).__name__}: {e}"
        model_name_for_log = parser.pydantic_object.__name__ if hasattr(parser.pydantic_object, '__name__') else 'UnknownModel'
        globals.logger.error(
            f"[{node_name}] Programmatic population/coercion failed for model "
            f"{model_name_for_log}: {error_detail}. Input text: '{str(final_text)[:200]}'"
        )
        return final_text

    except Exception as e: # Catch-all for any other unexpected errors
        error_detail = f"Unexpected {type(e).__name__}: {e}"
        globals.logger.error(
            f"[{node_name}] Unexpected error during programmatic population: {error_detail}. "
            f"Input text: '{str(final_text)[:200]}'"
        )
        return final_text

