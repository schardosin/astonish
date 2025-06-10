"""
Prompt template utilities for Astonish.
This module contains functions for creating and managing prompt templates.
"""
import json
from typing import List, Dict, Any
from pydantic import BaseModel

def create_first_run_react_prompt_template(tools_definitions: List[Dict[str, Any]]) -> str:
    """
    Generates a ReAct prompt string for the FIRST RUN ONLY, including detailed tool input requirements,
    WITH property descriptions, enums, defaults, and escaping schema braces.
    Includes the {agent_scratchpad} placeholder for text-based history.
    
    This template is specifically designed for the first run and ONLY includes instructions for
    Thought, Action, and Action Input (no Observation or Final Answer).
    """
    tool_strings = []
    for tool_def in tools_definitions:
        input_desc = ""
        input_type = tool_def.get('input_type', 'STRING')
        schema_def = tool_def.get('input_schema_definition')

        if input_type == 'JSON_SCHEMA':
            schema_desc_str = "[Schema not provided or invalid]"
            properties = {}
            required_list = []
            if schema_def:
                try:
                    schema_json_props = None
                    if isinstance(schema_def, type) and issubclass(schema_def, BaseModel):
                        schema_json_props = schema_def.model_json_schema()
                    elif isinstance(schema_def, dict) and "properties" in schema_def:
                        schema_json_props = schema_def

                    if schema_json_props:
                        properties = schema_json_props.get("properties", {})
                        required_list = schema_json_props.get("required", [])

                    if properties:
                        prop_details_list = []
                        for name, details in properties.items():
                            prop_type = details.get('type', 'unknown')
                            prop_desc = details.get('description', '')
                            prop_enum = details.get('enum')
                            prop_default = details.get('default')

                            is_required = name in required_list
                            req_marker = " (required)" if is_required else ""

                            detail_str = f"'{name}' ({prop_type}{req_marker})"
                            if prop_desc:
                                detail_str += f": {prop_desc}"
                            if prop_enum:
                                # Ensure enum values are correctly quoted if strings
                                enum_strs = [f'"{v}"' if isinstance(v, str) else str(v) for v in prop_enum]
                                detail_str += f" (must be one of: [{', '.join(enum_strs)}])" # List allowed values
                            if prop_default is not None:
                                 detail_str += f" (default: {json.dumps(prop_default)})" # Show default

                            # Escape curly braces within the description details for f-string formatting
                            prop_details_list.append(detail_str.replace("{", "{{").replace("}", "}}"))

                        schema_desc_str = "; ".join(prop_details_list)
                    else:
                         schema_desc_str = "[No properties found in schema]"
                except Exception as e:
                    schema_desc_str = f"[Error extracting schema details]"

            input_desc = f"Input Type: Requires a JSON object string with properties: {schema_desc_str}. IMPORTANT: Generate a valid JSON string containing required properties and only essential optional properties based on the user request."

        elif input_type == 'STRING':
             input_desc = "Input Type: Plain String"

             if schema_def and isinstance(schema_def, str):
                  # Escape curly braces in the example string
                  escaped_example = schema_def.replace("{", "{{").replace("}", "}}")
                  input_desc += f". Expected format/example: {escaped_example}"
        else:
             input_desc = f"Input Type: {input_type}"
             if schema_def and isinstance(schema_def, str):
                 # Escape curly braces
                 input_desc += f". Tool expects: {schema_def.replace('{','{{').replace('}','}}')}"

        tool_name = tool_def.get('name', 'UnnamedTool')
        tool_description = tool_def.get('description', 'No description.')
        # Escape braces in tool name and description just in case, though less likely needed
        safe_tool_name = tool_name.replace("{", "{{").replace("}", "}}")
        safe_tool_description = tool_description.replace("{", "{{").replace("}", "}}")
        tool_strings.append(f"- {safe_tool_name}: {safe_tool_description} {input_desc}")

    formatted_tools = "\n".join(tool_strings) if tool_strings else "No tools available."
    tool_names = ", ".join([tool_def['name'] for tool_def in tools_definitions]) if tools_definitions else "N/A"
    safe_tool_names = tool_names.replace("{", "{{").replace("}", "}}") # Escape braces in tool names list

    template = f"""Answer the following questions as best you can. You have access to the following tools:
        {formatted_tools}

        Use the following format STRICTLY for this FIRST RUN ONLY:

        Question: The input question you must answer.
        Thought: Analyze the question and available tools. Do I have the answer, or do I need a tool? If I need a tool, which one is best, and what arguments does it need?
        Action: The action to take, chosen from [{safe_tool_names}].
        Action Input: The input for the selected action (must be a valid JSON object string or a plain string, as required by the tool).

        IMPORTANT: For this FIRST RUN, DO NOT include Observation or Final Answer. The system will execute your chosen Action and provide the Observation in the next run.

        ---

        Here is an example of a proper FIRST RUN response:

        Question: Add a comment 'Needs refactoring' to line 15 of src/main.js in PR #123 for repo 'owner/my-repo'.
        Thought: The user wants to add a review comment. The available tool is `add_pull_request_review_comment_to_pending_review`. I have all the necessary information: owner, repo, pullNumber, path, line, and body. I should use the LINE subjectType.
        Action: add_pull_request_review_comment_to_pending_review
        Action Input: {{{{"owner": "owner", "repo": "my-repo", "pullNumber": 123, "body": "Needs refactoring", "path": "src/main.js", "line": 15, "subjectType": "LINE"}}}}

        ---

        Key Guidelines for FIRST RUN:
        * ONLY provide Thought, Action, and Action Input - DO NOT include Observation or Final Answer
        * Always analyze the question carefully in your Thought step
        * Choose the most appropriate tool from the available options
        * Provide properly formatted Action Input according to the tool's requirements

        CRITICAL FORMATTING RULE: You MUST NOT wrap your 'Thought:', 'Action:', 'Action Input:', or 'Final Answer:' in JSON markdown ```json code blocks.

        Begin!

        Question: {{input}}
        {{agent_scratchpad}}"""

    return template

def create_custom_react_prompt_template(tools_definitions: List[Dict[str, Any]]) -> str:
    """
    Generates a ReAct prompt string including detailed tool input requirements,
    WITH property descriptions, enums, defaults, and escaping schema braces.
    Includes the {agent_scratchpad} placeholder for text-based history.
    
    This template is for subsequent runs after the first tool execution, and includes
    the full cycle with Observation and Final Answer.
    """
    tool_strings = []
    for tool_def in tools_definitions:
        input_desc = ""
        input_type = tool_def.get('input_type', 'STRING')
        schema_def = tool_def.get('input_schema_definition')

        if input_type == 'JSON_SCHEMA':
            schema_desc_str = "[Schema not provided or invalid]"
            properties = {}
            required_list = []
            if schema_def:
                try:
                    schema_json_props = None
                    if isinstance(schema_def, type) and issubclass(schema_def, BaseModel):
                        schema_json_props = schema_def.model_json_schema()
                    elif isinstance(schema_def, dict) and "properties" in schema_def:
                        schema_json_props = schema_def

                    if schema_json_props:
                        properties = schema_json_props.get("properties", {})
                        required_list = schema_json_props.get("required", [])

                    if properties:
                        prop_details_list = []
                        for name, details in properties.items():
                            prop_type = details.get('type', 'unknown')
                            prop_desc = details.get('description', '')
                            prop_enum = details.get('enum')
                            prop_default = details.get('default')

                            is_required = name in required_list
                            req_marker = " (required)" if is_required else ""

                            detail_str = f"'{name}' ({prop_type}{req_marker})"
                            if prop_desc:
                                detail_str += f": {prop_desc}"
                            if prop_enum:
                                # Ensure enum values are correctly quoted if strings
                                enum_strs = [f'"{v}"' if isinstance(v, str) else str(v) for v in prop_enum]
                                detail_str += f" (must be one of: [{', '.join(enum_strs)}])" # List allowed values
                            if prop_default is not None:
                                 detail_str += f" (default: {json.dumps(prop_default)})" # Show default

                            # Escape curly braces within the description details for f-string formatting
                            prop_details_list.append(detail_str.replace("{", "{{").replace("}", "}}"))

                        schema_desc_str = "; ".join(prop_details_list)
                    else:
                         schema_desc_str = "[No properties found in schema]"
                except Exception as e:
                    schema_desc_str = f"[Error extracting schema details]"

            input_desc = f"Input Type: Requires a JSON object string with properties: {schema_desc_str}. IMPORTANT: Generate a valid JSON string containing required properties and only essential optional properties based on the user request."

        elif input_type == 'STRING':
             input_desc = "Input Type: Plain String"

             if schema_def and isinstance(schema_def, str):
                  # Escape curly braces in the example string
                  escaped_example = schema_def.replace("{", "{{").replace("}", "}}")
                  input_desc += f". Expected format/example: {escaped_example}"
        else:
             input_desc = f"Input Type: {input_type}"
             if schema_def and isinstance(schema_def, str):
                 # Escape curly braces
                 input_desc += f". Tool expects: {schema_def.replace('{','{{').replace('}','}}')}"

        tool_name = tool_def.get('name', 'UnnamedTool')
        tool_description = tool_def.get('description', 'No description.')
        # Escape braces in tool name and description just in case, though less likely needed
        safe_tool_name = tool_name.replace("{", "{{").replace("}", "}}")
        safe_tool_description = tool_description.replace("{", "{{").replace("}", "}}")
        tool_strings.append(f"- {safe_tool_name}: {safe_tool_description} {input_desc}")

    formatted_tools = "\n".join(tool_strings) if tool_strings else "No tools available."
    tool_names = ", ".join([tool_def['name'] for tool_def in tools_definitions]) if tools_definitions else "N/A"
    safe_tool_names = tool_names.replace("{", "{{").replace("}", "}}") # Escape braces in tool names list

    template = f"""Answer the following questions as best you can. You have access to the following tools:
        {formatted_tools}

        Use the following format STRICTLY:

        Question: The input question you must answer.
        Thought: Analyze the question and available tools. Do I have the answer, or do I need a tool? If I need a tool, which one is best, and what arguments does it need?
        Action: The action to take, chosen from [{safe_tool_names}]. If you have the answer, do NOT use this line.
        Action Input: The input for the selected action (must be a valid JSON object string or a plain string, as required by the tool). If you have the answer, do NOT use this line.
        Observation: The result provided by the tool.

        ... (this Thought/Action/Action Input/Observation cycle can repeat N times)

        Thought: Based on the previous Observation (or the initial Question, if no tools were needed), I now have the complete answer. I will format this answer inside a JSON object, under the key "result".
        Final Answer: A valid JSON object string, like {{{{"result": <your_answer_here>}}}}.

        ---

        Here is an example:

        Question: Add a comment 'Needs refactoring' to line 15 of src/main.js in PR #123 for repo 'owner/my-repo'.
        Thought: The user wants to add a review comment. The available tool is `add_pull_request_review_comment_to_pending_review`. I have all the necessary information: owner, repo, pullNumber, path, line, and body. I should use the LINE subjectType.
        Action: add_pull_request_review_comment_to_pending_review
        Action Input: {{{{"owner": "owner", "repo": "my-repo", "pullNumber": 123, "body": "Needs refactoring", "path": "src/main.js", "line": 15, "subjectType": "LINE"}}}}
        Observation: {{{{"result":"pull request review comment successfully added to pending review"}}}}
        Thought: I have successfully added the comment based on the observation. I can now provide the final answer, placing the observation content into the "result" key.
        Final Answer: {{{{"result":"pull request review comment successfully added to pending review"}}}}

        ---

        Key Guidelines:
        * Always follow the 'Thought:', 'Action:', 'Action Input:', 'Observation:' structure until you reach the final answer.
        * Before using a tool, *always* consider in your 'Thought:' step if you already have enough information from previous steps.
        * Once you have the information needed to answer the 'Question', your *very next step* must be a 'Thought:' explaining this, followed *immediately* by the 'Final Answer:'.
        * The 'Final Answer:' MUST be provided as soon as you have the complete answer, avoiding unnecessary steps. So be very critial in your 'Thought:' step to determine if you have enough information as soon as possible.
        * The 'Final Answer:' MUST be provide with the prefix 'Final Answer:' and the content must be a valid JSON object string, like {{{{"result": <your_answer_here>}}}}.
        * The 'Final Answer:' MUST be provided only in case of successful completion of the task. If you encounter an error or cannot complete the task, do not provide a 'Final Answer:'.

        CRITICAL FORMATTING RULE: You MUST NOT wrap your 'Thought:', 'Action:', 'Action Input:', or 'Final Answer:' in JSON markdown ```json code blocks.

        Begin!

        Question: {{input}}
        {{agent_scratchpad}}"""

    return template
