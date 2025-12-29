package api

// FlowSchema contains the schema documentation for AI to understand flow structure
const FlowSchema = `
# Astonish Agent Flow Schema

## Overview
An agent flow is defined in YAML with nodes (processing steps) and flow (edges connecting them).

## Basic Structure
` + "```yaml" + `
name: agent_name
description: What this agent does

nodes:
  - name: node_name
    type: llm|input|tool|output|update_state
    # type-specific fields...

flow:
  - from: START
    to: first_node
  - from: first_node
    to: second_node
  - from: last_node
    to: END
` + "```" + `

## Node Types

### 1. LLM Node (PREFERRED for tool usage)
AI processing with optional tool use.
- output_model: saves result to state for later nodes
- user_message: DISPLAY result to user (use this when user needs to see the response!)
` + "```yaml" + `
# LLM that shows answer to user (most common pattern)
- name: answer_question
  type: llm
  system: "You are a helpful assistant."
  prompt: "{user_question}"
  output_model:
    answer: str
  user_message:
    - answer  # This displays the LLM response to the user!

# LLM with tools
- name: search_and_summarize
  type: llm
  system: "You are a search assistant."
  prompt: "Search for: {query}"
  tools: true
  tools_selection:
    - tavily-search
  output_model:
    search_result: str
  user_message:
    - search_result  # Show the result to user
` + "```" + `

#### Advanced: Raw Tool Output (CONTEXT OPTIMIZATION)
**WARNING:** Only use this when explicitly requested by the user for large data handling.
This bypasses the LLM for tool output, storing it directly in state.
Useful for large datasets processed by subsequent nodes to save context/costs.

**CRITICAL:** do NOT use this unless the user specifically asks for 'raw output', 'direct state storage', or 'optimization for large data'. For normal flows, use ` + "`" + `output_model` + "`" + `.

When using ` + "`" + `raw_tool_output` + "`" + `, it is highly recommended to also use ` + "`" + `output_model` + "`" + ` to export a status field (e.g., ` + "`" + `<node_context>_status` + "`" + `) to confirm the operation's success/failure for subsequent nodes.

` + "```yaml" + `
- name: fetch_large_dataset
  type: llm
  prompt: "Fetch the data"
  tools: true
  tools_selection: [big_data_tool]
  # output_model is NOT used here because we use raw_tool_output
  raw_tool_output:
    my_large_variable: any  # Stores the raw tool result directly in 'my_large_variable'
` + "```" + `

### 2. Input Node
Collect user input. output_model is REQUIRED to store input in state.

**IMPORTANT: options behavior**
- WITHOUT options: User can type ANY text (free form input)
- WITH options: User can ONLY select from the listed options (no free text!)
` + "```yaml" + `
# Free text input - user can type anything
- name: get_question
  type: input
  prompt: "What is your question?"
  output_model:
    question: str

# Choice input - user can ONLY select from these options
- name: ask_continue
  type: input
  prompt: "Continue? (yes/no)"
  output_model:
    choice: str
  options:
    - "yes"
    - "no"

# DYNAMIC options from state variable (from previous node output)
# Use this when a previous LLM node outputs a list and you want user to select from it
- name: list_items
  type: llm
  prompt: "List all available items"
  output_model:
    items: list  # This outputs a list to state
  tools: true
  user_message:
    - items

- name: select_item
  type: input
  prompt: "Select an item from the list above:"
  output_model:
    selected_item: str
  options:
    - items  # Reference the state variable containing the list (NOT {items}!)
` + "```" + `
**CRITICAL for dynamic options:**
- Use ` + "`" + `options: [variable_name]` + "`" + ` to reference a list from state
- Do NOT use ` + "`" + `options: '{variable_name}'` + "`" + ` - this is WRONG!
- Do NOT use ` + "`" + `options: variable_name` + "`" + ` without brackets - this is WRONG!
- The variable must be a list type from a previous node's output_model

**DO NOT use options if you want the user to type free text!**

### 3. Tool Node (RARELY USED)
Execute a tool directly WITHOUT LLM intelligence. Only use when you need deterministic tool execution.
In most cases, prefer LLM node with tools: true instead.
` + "```yaml" + `
- name: run_fixed_command
  type: tool
  tools_selection:
    - shell_command
  args:
    command: "ls -la"
  output_model:
    shell_result: str
` + "```" + `

#### Error Handling with continue_on_error
By default, tool failures stop the flow. Use ` + "`" + `continue_on_error: true` + "`" + ` to capture errors and continue:
` + "```yaml" + `
- name: check_tool_exists
  type: tool
  tools_selection:
    - shell_command
  args:
    command: "yt-dlp --version"
  continue_on_error: true  # If tool fails, capture error instead of stopping
  output_model:
    check_result: str      # Will contain version info OR error message
` + "```" + `
When ` + "`" + `continue_on_error` + "`" + ` is true, the result will include:
- On success: ` + "`" + `{..., "success": true}` + "`" + `
- On failure: ` + "`" + `{"error": "...", "success": false}` + "`" + `

### 4. Output Node
Display messages to user. Use user_message array with strings and state variable names.
Each item is displayed as a separate paragraph.

**Behavior:**
- If item matches a state variable name: its value is displayed
- If item doesn't match any variable: treated as literal text
- Multi-line text is preserved in each item

Note: LLM responses are shown automatically, so output nodes are mainly for formatting/labeling.
` + "```yaml" + `
- name: show_result
  type: output
  user_message:
    - "Here is your answer:"   # Literal text
    - answer                   # State variable (resolved at runtime)
    - "Thank you for using our service!"  # More literal text
` + "```" + `

### 5. Update State Node
Modify state directly without AI. Supports three actions: **append**, **increment**, and **overwrite**.

**Primary use cases:**
- **append**: Building lists over iterations (conversation history, collected items)
- **increment**: Counting loop iterations or tracking numeric progress
- **overwrite**: Copying values between variables (use sparingly - output_model usually handles this)

` + "```yaml" + `
# APPEND: Build a list over iterations (e.g., conversation history)
- name: update_history
  type: update_state
  source_variable: ai_message   # The variable to append
  action: append                # APPEND to the list
  output_model:
    history: list               # Target list that grows each iteration

# INCREMENT: Count loop iterations
- name: increment_counter
  type: update_state
  action: increment
  value: 1                      # Amount to add (default: 1)
  output_model:
    counter: int                # Variable to increment

# INCREMENT with source: Copy and increment
- name: step_counter
  type: update_state
  source_variable: current      # Read current value from here
  action: increment
  value: 1
  output_model:
    total: int                  # Store result here
` + "```" + `

**CRITICAL: Two separate modes - DO NOT MIX!**

Mode 1: **Legacy mode** (uses ` + "`" + `updates` + "`" + `) - for setting initial values:
` + "```yaml" + `
- name: init
  type: update_state
  updates:              # Use updates for initialization
    counter: "0"        # No action field!
    status: "pending"
` + "```" + `

Mode 2: **Action mode** (uses ` + "`" + `action` + "`" + ` + ` + "`" + `value` + "`" + `/` + "`" + `source_variable` + "`" + `) - for append/increment/overwrite:
` + "```yaml" + `
- name: increment
  type: update_state
  action: increment     # Requires value OR source_variable
  value: 1              # No updates field!
  output_model:
    counter: int
` + "```" + `

**WRONG - mixing modes will cause errors:**
` + "```yaml" + `
# ❌ WRONG: action + updates = ERROR!
- name: bad_init
  type: update_state
  action: overwrite
  updates:              # This is IGNORED when action is set!
    counter: "0"
  output_model:
    counter: int
` + "```" + `

**DO NOT use update_state just to copy or overwrite variables** - that's what output_model does automatically.

## Flow Edges

### Simple Edge
` + "```yaml" + `
- from: node_a
  to: node_b
` + "```" + `

### Conditional Edges
` + "```yaml" + `
- from: decision_node
  edges:
    - to: yes_path
      condition: "lambda x: x['decision'] == 'yes'"
    - to: no_path
      condition: "lambda x: x['decision'] == 'no'"
` + "```" + `

### Loop (Back-edge)
IMPORTANT: Loops must point to actual nodes, NEVER to START!
` + "```yaml" + `
- from: ask_continue
  edges:
    - to: get_question  # Loop back to the actual node, NOT to START!
      condition: "lambda x: x['continue'] == 'yes'"
    - to: END
      condition: "lambda x: x['continue'] == 'no'"
` + "```" + `

## State & Templating
- Use {variable_name} to reference state (single braces, no spaces)
- Data flows through nodes via output_model which saves to state
- Access previous node outputs from state keys defined in output_model

## Patterns

### User Confirmation Pattern
Use INPUT node with options for reliable branching:
` + "```yaml" + `
- name: confirm
  type: input
  prompt: "Continue? (yes/no)"
  output_model:
    choice: str
  options:
    - "yes"
    - "no"
` + "```" + `
Then use in conditional edge: ` + "`" + `condition: "lambda x: x['choice'] == 'yes'"` + "`" + `

### Displaying LLM Response
Always add user_message when the user should see the output:
` + "```yaml" + `
- name: process
  type: llm
  prompt: "{user_input}"
  output_model:
    result: str
  user_message:
    - result  # This shows the response to the user
` + "```" + `

## Anti-Patterns (DO NOT DO)

❌ Using LLM to check yes/no conditions - unpredictable output breaks edges
❌ Creating "check_exit" or "check_quit" nodes - use input with options instead  
❌ Missing user_message on LLM nodes - user won't see the response
❌ Using LLM output in conditional edges - conditions may never match

## Rules
1. Flow must start from START and end at END
2. START and END are VIRTUAL nodes - they are NOT actual nodes you define
3. You can ONLY use "from: START" to begin the flow - NEVER use "to: START"
4. Loops must point to actual node names (e.g., "to: get_question"), NEVER to START
5. All node names must be unique
6. Use output_model to pass data between nodes
7. ALWAYS include user_message on LLM nodes when user should see output
8. For branching/loops, use INPUT with options - gives reliable condition values
9. NEVER use LLM output in conditional edges - it's unpredictable
10. Use ` + "`" + `silent: true` + "`" + ` on nodes you want to run without showing execution indicator
`

// GetFlowSchema returns the schema as a string for AI context
func GetFlowSchema() string {
	return FlowSchema
}
