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
model: gemini-2.0-flash  # or gpt-4o, claude-3-5-sonnet, etc.

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
` + "```" + `
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

### 4. Output Node
Display messages to user. Use user_message array with strings and state variable names.
Note: LLM responses are shown automatically, so output nodes are mainly for formatting/labeling.
` + "```yaml" + `
- name: show_result
  type: output
  user_message:
    - "Answer:"
    - answer
` + "```" + `

### 5. Update State Node
Modify state variables.
` + "```yaml" + `
- name: set_defaults
  type: update_state
  updates:
    processed: "true"
    counter: "1"
` + "```" + `

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
`

// GetFlowSchema returns the schema as a string for AI context
func GetFlowSchema() string {
	return FlowSchema
}

