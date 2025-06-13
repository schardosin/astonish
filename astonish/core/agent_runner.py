import traceback
import astonish.globals as globals
from langchain.globals import set_debug
from langgraph.checkpoint.sqlite.aio import AsyncSqliteSaver
from astonish.core.utils import load_agents, print_ai, print_section
from astonish.core.graph_builder import build_graph, run_graph
from typing import Dict, Any, Optional
from astonish.core.mcp_manager import MCPManager

async def run_agent(agent, parameters: Optional[Dict[str, Any]] = None):
    """
    Run an agentic flow
    """
    set_debug(False)

    # Load agents
    try:
        config = load_agents(agent)
    except FileNotFoundError as e:
        print_section("Agent Not Found")
        print_ai(f"I couldn't find the agent '{agent}'. Please check the name and try again.")
        return
    except Exception as e:
        print_section("Error Loading Agent")
        print_ai(f"I encountered an error while loading the agent: {str(e)}")
        globals.logger.error(f"Error loading agent: {e}")
        globals.logger.debug(traceback.format_exc())
        return

    # Initialize MCPManager
    try:
        mcp_manager = MCPManager()
        await mcp_manager.startup()
    except Exception as e:
        print_section("Warning")
        print_ai(f"I had trouble initializing MCP tools, but I'll continue without them: {str(e)}")
        globals.logger.warning(f"Error initializing MCP tools: {e}")
        mcp_manager = None

    # Initialize state
    initial_state = {}
    
    # Initialize output model fields
    for node in config['nodes']:
        if 'output_model' in node:
            for field, type_ in node['output_model'].items():
                initial_state[field] = None
        
        # Add initialization for limit_counter_field
        if 'limit_counter_field' in node:
            limit_counter_field = node['limit_counter_field']
            if limit_counter_field not in initial_state:
                initial_state[limit_counter_field] = 0  # Initialize to 0

        if 'raw_tool_output' in node:
            for field, type_ in node['raw_tool_output'].items():
                initial_state[field] = None
    
    # Add error tracking fields
    initial_state['_error'] = None
    initial_state['_end'] = False
    
    # Store parameters in state for node lookup
    if parameters:
        initial_state['_parameters'] = parameters
        globals.logger.info(f"Added parameters to initial state: {parameters}")
    else:
        globals.logger.info("No parameters provided")

    # Build graph
    async with AsyncSqliteSaver.from_conn_string(":memory:") as checkpointer:
        thread = {"configurable": {"thread_id": "1"}, "recursion_limit": 200}
        
        try:
            graph = build_graph(config, mcp_manager, checkpointer)
        except Exception as e:
            print_section("Error Building Graph")
            print_ai(f"I encountered an error while building the agent graph: {str(e)}")
            globals.logger.error(f"Error building graph: {e}")
            globals.logger.debug(traceback.format_exc())
            return

        try:
            # Run the graph and get the final state
            final_state = await run_graph(graph, initial_state, thread)
            
            # Check if we terminated due to an error
            if final_state and isinstance(final_state, dict):
                if '_error' in final_state and final_state['_error'] is not None:
                    error_info = final_state.get('_error')
                    if isinstance(error_info, dict) and not error_info.get('recoverable', True):
                        # Error was already handled and displayed by the error handler node
                        return
            
            # If we get here, the graph completed successfully
            print_ai("See you soon! Bye!")
            
        except Exception as e:
            # This should rarely happen since run_graph already handles exceptions
            print_section("Critical Error")
            print_ai(f"I encountered a critical error while running the agent: {str(e)}")
            print_ai("This is likely a bug in the system. Please report this issue to the developers.")
            globals.logger.error(f"Critical error running graph: {e}")
            globals.logger.debug(traceback.format_exc())
        finally:
            # Ensure the MCP session is properly closed
            if mcp_manager:
                await mcp_manager.shutdown()
