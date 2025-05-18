"""
JSON utilities for Astonish.
This module contains functions for handling JSON data.
"""
import json
import json5

def clean_and_fix_json(content: str) -> str:
    """
    Clean and fix JSON-like content to make it parseable as standard JSON,
    without using regular expressions for extraction.
    
    Attempts to use json5 for lenient parsing. If successful, it returns a
    standardized JSON string (re-serialized by json.dumps). 
    If json5 fails, it applies common markdown cleaning and attempts parsing again.
    If all parsing attempts (that would lead to re-serialization) fail, 
    it returns the markdown-cleaned string as a best-effort.

    Args:
        content: The content string to clean and fix.
        
    Returns:
        A string that is valid standard JSON if parsing and re-serialization 
        were successful. Otherwise, it returns the markdown-cleaned version of 
        the input string as a best-effort. Returns an empty string if the 
        input content is empty or not a string.
    """
    if not content or not isinstance(content, str):
        return ""

    original_content_stripped = content.strip()

    # Attempt 1: Try json5 on the original stripped content directly
    try:
        parsed_data = json5.loads(original_content_stripped, allow_duplicate_keys=True)
        return json.dumps(parsed_data) 
    except Exception:
        # json5 failed on the raw stripped content, proceed to markdown cleaning.
        pass

    # Attempt 2: Clean common markdown wrappers from the original_content_stripped
    # and then try json5 again.
    markdown_cleaned_content = original_content_stripped 
    
    # Handle ```json ... ```
    if markdown_cleaned_content.startswith("```json") and markdown_cleaned_content.endswith("```"):
        markdown_cleaned_content = markdown_cleaned_content[len("```json"):-len("```")].strip()
    # Handle general ``` ... ``` only if the content inside looks like JSON
    elif markdown_cleaned_content.startswith("```") and markdown_cleaned_content.endswith("```"):
        temp_inner = markdown_cleaned_content[len("```"):-len("```")].strip()
        if temp_inner.startswith("{") or temp_inner.startswith("["):
            markdown_cleaned_content = temp_inner

    if markdown_cleaned_content != original_content_stripped:
        try:
            parsed_data_after_markdown = json5.loads(markdown_cleaned_content, allow_duplicate_keys=True)
            return json.dumps(parsed_data_after_markdown) # Standardize output
        except Exception:
            pass
    
    return markdown_cleaned_content