#!/usr/bin/env python3
import asyncio
import sys
from astonish.core.agent_runner import run_agent

async def main():
    # Example of running an agent with parameters
    # Replace 'essay' with the name of your agent
    # Replace the parameters with the ones your agent expects
    agent_name = 'essay'
    
    # Parameters are keyed by node name
    parameters = {
        # For the 'user_interaction' node in the essay agent
        'user_interaction': 'Write an essay about artificial intelligence'
    }
    
    print(f"Running agent '{agent_name}' with parameters: {parameters}")
    await run_agent(agent_name, parameters)

def print_usage():
    print("Usage: python test_params.py [agent_name] [param1=value1] [param2=value2] ...")
    print("Example: python test_params.py essay user_interaction=\"Write an essay about AI\" continue_loop=no")
    print("\nIf no arguments are provided, the script will run with default parameters.")

if __name__ == "__main__":
    if len(sys.argv) > 1:
        if sys.argv[1] in ['-h', '--help']:
            print_usage()
            sys.exit(0)
            
        agent_name = sys.argv[1]
        parameters = {}
        
        # Parse command line parameters (param=value format)
        for arg in sys.argv[2:]:
            if '=' in arg:
                key, value = arg.split('=', 1)
                parameters[key.strip()] = value.strip()
            else:
                print(f"Warning: Ignoring malformed parameter: {arg} (missing '=')")
        
        print(f"Running agent '{agent_name}' with parameters: {parameters}")
        asyncio.run(run_agent(agent_name, parameters))
    else:
        # Run with default parameters
        asyncio.run(main())
