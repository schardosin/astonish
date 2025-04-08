import astonish.globals as globals
from langchain.globals import set_debug
from langgraph.checkpoint.sqlite.aio import AsyncSqliteSaver
from astonish.core.utils import setup_colorama, load_agents, print_ai
from astonish.core.node_functions import create_node_function, create_output_model
from astonish.core.graph_builder import build_graph, run_graph, print_flow

async def run_agent(agent):
    # Setup
    setup_colorama()
    set_debug(False)

    # Load agents
    config = load_agents(agent)

    # Initialize MCP tools
    mcp_client = await globals.initialize_mcp_tools()

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

    # Build graph
    async with AsyncSqliteSaver.from_conn_string(":memory:") as checkpointer:
        thread = {"configurable": {"thread_id": "1"}}
        graph = build_graph(config, mcp_client, checkpointer)

        await run_graph(graph, initial_state, thread)

        print_ai("Bye! Bye!")