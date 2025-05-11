"""
JSON utilities for Astonish.
This module contains functions for handling JSON data.
"""
import json
import re

def clean_and_fix_json(content: str) -> str:
    """
    Clean and fix JSON content to make it parseable.
    
    Args:
        content: The content to clean and fix
        
    Returns:
        The cleaned and fixed content
    """
    if not content or not isinstance(content, str):
        return ""

    # 1. Attempt to find a JSON object or array using a regex.
    # This is useful if the JSON is embedded within other text.
    # Regex to find content starting with { or [ and ending with } or ] respectively,
    # attempting to match balanced braces/brackets. This is a common heuristic.
    # Note: A perfect regex for balanced structures is complex; this is an approximation.
    json_match = re.search(r"(\{((?:[^{}]*|\{(?:[^{}]*|\{[^{}]*\})*\})*)\}|\[((?:[^\[\]]*|\[(?:[^\[\]]*|\[[^\[\]]*\])*\])*)\])", content)
    
    potential_json_str = ""
    if json_match:
        potential_json_str = json_match.group(0)
        try:
            # Try to parse to see if it's valid JSON
            json.loads(potential_json_str)
            # If it parses, we assume this is our primary JSON content
            return potential_json_str.strip() 
        except json.JSONDecodeError:
            # If parsing fails, it might be that this regex match wasn't the actual JSON,
            # or it's malformed. We'll proceed to other cleaning steps.
            potential_json_str = "" # Reset, as this wasn't clean JSON

    # 2. If no clear JSON structure was extracted via regex,
    #    or if the extracted part was not valid, try cleaning common markdown wrappers
    #    from the original content. This handles cases where the entire response
    #    is meant to be JSON but is wrapped.
    
    cleaned_content = content.strip()
    
    # Remove ```json ... ``` or ``` ... ``` wrappers if they enclose the whole content
    if cleaned_content.startswith("```json") and cleaned_content.endswith("```"):
        cleaned_content = cleaned_content[len("```json") : -len("```")].strip()
    elif cleaned_content.startswith("```") and cleaned_content.endswith("```"):
        # If it's just ``` ... ```, remove them if the inner content looks like JSON
        temp_inner = cleaned_content[len("```") : -len("```")].strip()
        if temp_inner.startswith("{") or temp_inner.startswith("["):
            cleaned_content = temp_inner
        # else, it might be a non-JSON code block, so we probably shouldn't strip the ```
        # and expect the parser to fail if it was expecting JSON.
        # However, if potential_json_str was populated from regex and failed,
        # cleaned_content is still the original content here. This part might need refinement
        # based on how often LLMs return non-JSON in ``` when JSON is expected.
        # For now, if regex found something, use that attempt; otherwise, use original cleaned_content.
        
    if potential_json_str and (not cleaned_content.startswith("{") and not cleaned_content.startswith("[")):
        # This means regex found something, but it wasn't valid, and outer ``` stripping didn't apply
        # or also resulted in non-JSON. This case is ambiguous.
        # Let's prioritize the result of ``` stripping if it looks like JSON.
        if cleaned_content.startswith("{") or cleaned_content.startswith("["):
             return cleaned_content
        # If regex found something that looked like JSON but wasn't, and ``` stripping didn't help,
        # it's better to return the original stripped content and let the parser fail.
        # However, for this specific function, we want the *most JSON-like* part.
        # If no valid JSON was found, return the most "stripped" version assuming it was meant to be JSON.
        return cleaned_content if (cleaned_content.startswith("{") or cleaned_content.startswith("[")) else potential_json_str

    # If after all this, cleaned_content is still not starting with { or [,
    # and potential_json_str (from regex) didn't validate, then it's unlikely to be the JSON we want.
    # Return the most processed version that looks like JSON.
    if cleaned_content.startswith("{") or cleaned_content.startswith("["):
        return cleaned_content
    elif potential_json_str: # from regex, even if it didn't validate alone
        return potential_json_str

    return content.strip() # Fallback to just stripped original content
