---
sidebar_position: 1
---

# Agent Runner

The Agent Runner is responsible for executing agentic flows in Astonish. It loads agent configurations, initializes tools, builds the execution graph, and runs the flow.

## Overview

The `agent_runner.py` module contains the core functionality for running agents. It provides the `run_agent` function, which is the main entry point for executing agentic flows.

## Functions

### run_agent

```python
async def run_agent(agent: str) -> None
```

Runs an agentic flow with the specified name.

#### Parameters

- `agent` (str): The name of the agent to run. This should correspond to a YAML file in one of the agent directories.

#### Returns

- `None`

#### Example

```python
await run_agent("file_summarizer")
```

#### Process

1. Sets up colorama for colored terminal output
2. Loads the agent configuration from the YAML file
3. Initializes MCP tools if available
4. Initializes the state with variables from the output models
5. Builds the execution graph using the agent configuration
6. Runs the graph with the initial state
7. Handles any errors that occur during execution

## Error Handling

The Agent Runner includes robust error handling to ensure that errors during agent execution are properly reported to the user. It handles:

- Missing agent files
- Errors loading agent configurations
- Errors initializing MCP tools
- Errors building the execution graph
- Errors during graph execution

## Dependencies

The Agent Runner depends on several other modules:

- `astonish.globals`: Global variables and configuration
- `langchain.globals`: LangChain configuration
- `langgraph.checkpoint.sqlite.aio.AsyncSqliteSaver`: Checkpoint saving for graph execution
- `astonish.core.utils`: Utility functions for setup, loading agents, and printing
- `astonish.core.graph_builder`: Functions for building and running the execution graph

## Implementation Details

### State Initialization

The Agent Runner initializes the state dictionary with variables from the output models of all nodes in the agent configuration. This ensures that all variables used in the flow are properly initialized.

```python
# Initialize state
initial_state = {}
for node in config['nodes']:
    if 'output_model' in node:
        for field, type_ in node['output_model'].items():
            if field not in initial_state:
                initial_state[field] = None
    
    # Add initialization for limit_counter_field
    if 'limit_counter_field' in node:
        limit_counter_field = node['limit_counter_field']
        if limit_counter_field not in initial_state:
            initial_state[limit_counter_field] = 0  # Initialize to 0
```

### Error Tracking

The Agent Runner adds special fields to the state for tracking errors:

```python
# Add error tracking fields
initial_state['_error'] = None
initial_state['_end'] = False
```

These fields are used by the error handling system to track and report errors during execution.

### Graph Execution

The Agent Runner uses the `AsyncSqliteSaver` to save checkpoints during graph execution. This allows for potential future features like resuming interrupted flows.

```python
async with AsyncSqliteSaver.from_conn_string(":memory:") as checkpointer:
    thread = {"configurable": {"thread_id": "1"}, "recursion_limit": 200}
    
    # Build and run the graph
    graph = build_graph(config, mcp_client, checkpointer)
    final_state = await run_graph(graph, initial_state, thread)
```

## Related Modules

- **Graph Builder**: Builds and runs the execution graph
- **Node Functions**: Defines the functions for different node types
- **Utils**: Utility functions for agent execution
