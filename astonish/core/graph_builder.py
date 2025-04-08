from typing import TypedDict
from langgraph.graph import StateGraph, END
from astonish.core.node_functions import create_node_function
import astonish.globals as globals
from astonish.core.utils import load_agents

def build_graph(node_config, mcp_client, checkpointer):
    # Create a dictionary to store all unique fields from output_models
    all_fields = {}
    for node in node_config['nodes']:
        if 'output_model' in node:
            all_fields.update(node['output_model'])
        if 'limit_counter_field' in node:
            all_fields[node['limit_counter_field']] = 'int'

    # Create TypedDict with all unique fields
    builder = StateGraph(TypedDict('AgentState', {name: eval(type_) for name, type_ in all_fields.items()}))

    for node in node_config['nodes']:
        builder.add_node(node['name'], create_node_function(node, mcp_client))

    for edge in node_config['flow']:
        if edge['from'] == 'START':
            builder.set_entry_point(edge['to'])
        elif 'edges' in edge:
            conditions = {}
            for sub_edge in edge['edges']:
                source_node_config = next(node for node in node_config['nodes'] if node['name'] == edge['from'])
                condition = create_condition_function(sub_edge['condition'], source_node_config)
                conditions[END if sub_edge['to'] == 'END' else sub_edge['to']] = condition

            default_state = END
            combined_condition_function = create_combined_condition_function(conditions, default_state)
            all_possible_transitions = {state: state for state in conditions.keys()}
            all_possible_transitions[default_state] = default_state

            builder.add_conditional_edges(edge['from'], combined_condition_function, all_possible_transitions)
        else:
            builder.add_edge(edge['from'], END if edge['to'] == 'END' else edge['to'])

    return builder.compile(checkpointer=checkpointer)

def safe_eval_condition(condition: str, state: dict, node_config: dict) -> bool:
    try:
        lambda_body = condition.split(":", 1)[1].strip()
        func = eval(f"lambda x, config: {lambda_body}")
        return func(state, node_config)
    except Exception as e:
        globals.logger.error(f"Error evaluating condition: {e}")
        return False

def create_condition_function(condition: str, node_config: dict):
    return lambda state: safe_eval_condition(condition, state, node_config)

def create_combined_condition_function(conditions, default_state):
    def combined_condition_function(*args, **kwargs):
        for state, condition in conditions.items():
            if condition(*args, **kwargs):
                return state
        return default_state
    return combined_condition_function

async def run_graph(graph, initial_state, thread):
    await graph.ainvoke(initial_state, thread)

def print_flow(agent):
    config = load_agents(agent)
    graph = build_graph(config, None, None)
    graph_obj = graph.get_graph()
    graph_obj.print_ascii()
