# Astonish AI Companion

Astonish is a powerful, low-code AI companion tool that allows you to create and run agentic workflows, manage AI providers, and extend capabilities through the Model Context Protocol (MCP). It provides a flexible, YAML-based framework for configuring and executing AI-powered tasks without requiring extensive coding knowledge.

## Features

- Create and run customizable agentic workflows using a low-code, YAML-based approach
- Create new agents using an AI-powered agent creator
- Configure and manage multiple AI providers
- Extend capabilities through Model Context Protocol (MCP) support
- Integrate and use various tools within workflows, including embedded and custom tools
- Extensible architecture for adding new providers and tools
- Logging and configuration management

## Key Concepts

### Low-Code Agent Creation

Astonish embraces a low-code philosophy, allowing users to create complex AI agents using YAML configuration files. This approach democratizes AI agent creation, making it accessible to both developers and non-developers alike.

### Model Context Protocol (MCP) Support

Astonish leverages the Model Context Protocol (MCP) to extend its capabilities. MCP allows for seamless integration of additional tools and resources, enhancing the power and flexibility of your AI agents.

### Embedded Tools

Astonish comes with several powerful tools embedded out-of-the-box:

- `read_file`: Read the contents of files
- `write_file`: Write or modify file contents
- `shell_command`: Execute shell commands

These tools provide a solid foundation for creating versatile agents capable of interacting with the file system and executing system commands.

## Installation

To install Astonish, follow these steps:

1. Clone the repository:

   ```
   git clone https://github.com/yourusername/astonish.git
   cd astonish
   ```

2. Build and install the package:

   ```
   make install
   ```

   This command will build the package as a wheel and install it.

3. For development purposes, you can install in editable mode:

   ```
   make installdev
   ```

4. Set up the configuration:
   ```
   astonish setup
   ```

## Usage

### Setup

To configure Astonish providers, use the `setup` command:

```
astonish setup
```

### Creating New Agents

One of the key features of Astonish is the ability to create new agents using an AI-powered agent creator:

```
astonish agents run agents_creator
```

This command starts an interactive session where the AI will guide you through the process of creating a new agent using YAML configuration. Once the process is complete, your new agent will be ready for use.

### Running Agents

To run an agentic workflow:

```
astonish agents run <task_name>
```

This works for both pre-defined agents and agents you've created using the `agents_creator`.

To view the flow of an agentic workflow:

```
astonish agents flow <task_name>
```

### Managing Tools

To list available tools (including MCP-enabled tools):

```
astonish tools list
```

To edit the MCP (Model Context Protocol) configuration:

```
astonish tools edit
```

## Configuration

Astonish uses configuration files stored in the user's config directory:

- `config.ini`: General configuration
- `mcp_config.json`: MCP server configuration

These files are automatically created and managed by the application.

## Example YAML Configuration

Here's an example of an agent configuration in YAML that demonstrates tool usage:

```yaml
description: An agent that reads a file, extracts key information, and summarizes it for the user
nodes:
  - name: get_file_path
    type: input
    prompt: |
      Please enter the path to the file you want to analyze:
    output_model:
      file_path: str

  - name: read_file_content
    type: llm
    system: |
      You are a file reading assistant.
    prompt: |
      Read the contents of the file at path: {file_path}
    output_model:
      file_content: str
    tools: true
    tools_selection:
      - read_file

  - name: extract_key_info
    type: llm
    system: |
      You are an AI assistant specialized in extracting and summarizing key information from text.
    prompt: |
      Analyze the following file content and extract the core information:

      {file_content}

      Provide a concise summary of the key points.
    output_model:
      summary: str

  - name: present_summary
    type: llm
    system: |
      You are a helpful AI assistant presenting information to users.
    prompt: |
      Present the following summary to the user in a clear and engaging manner:

      {summary}
    output_model:
      final_response: str
    user_message:
      - final_response

flow:
  - from: START
    to: get_file_path
  - from: get_file_path
    to: read_file_content
  - from: read_file_content
    to: extract_key_info
  - from: extract_key_info
    to: present_summary
  - from: present_summary
    to: END
```

This agent demonstrates the following capabilities:

1. Gets a file path from the user
2. Uses the `read_file` tool to read the content of the specified file
3. Extracts and summarizes key information from the file content
4. Presents the summarized information to the user

The flow defines the sequence of operations, starting with user input for the file path, followed by file reading, information extraction, summary presentation, and ending the process.

## Project Structure

- `astonish/`: Main package directory
  - `main.py`: Entry point of the application
  - `globals.py`: Global variables and configuration
  - `core/`: Core functionality
    - `agent_runner.py`: Executes agentic workflows
    - `graph_builder.py`: Builds and runs workflow graphs
  - `factory/`: Factory classes for creating providers
  - `providers/`: AI provider implementations
  - `tools/`: Tool implementations (including embedded and MCP-enabled tools)
  - `agents/`: Predefined and user-created agent configurations (YAML files)

## Contributing

Contributions to Astonish are welcome! Please follow these steps to contribute:

1. Fork the repository
2. Create a new branch for your feature or bug fix
3. Make your changes and commit them with a clear commit message
4. Push your changes to your fork
5. Create a pull request with a description of your changes

Please ensure your code adheres to the project's coding standards and include tests for new features.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
