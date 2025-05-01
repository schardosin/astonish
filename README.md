<div align="center">
  <img src="https://raw.githubusercontent.com/schardosin/astonish/main/images/astonish-logo-only.svg" width="300" height="300" alt="Astonish Logo">
  
  # Astonish AI Companion
  
  *Empowering AI-driven workflows with low-code simplicity*
  
  [![Astonish Introduction](https://img.youtube.com/vi/83360OXEqcA/0.jpg)](https://www.youtube.com/watch?v=83360OXEqcA)

  [![Documentation](https://img.shields.io/badge/docs-website-blue)](https://schardosin.github.io/astonish/)
  [![PyPI version](https://img.shields.io/pypi/v/astonish.svg)](https://pypi.org/project/astonish/)
  [![Build Package](https://github.com/schardosin/astonish/actions/workflows/build.yml/badge.svg)](https://github.com/schardosin/astonish/actions/workflows/build.yml)
  [![Publish to PyPI](https://github.com/schardosin/astonish/actions/workflows/publish.yml/badge.svg)](https://github.com/schardosin/astonish/actions/workflows/publish.yml)

</div>

---

Astonish is a low-code AI companion that empowers you to create and run sophisticated agentic workflows with unprecedented ease. By leveraging a flexible, YAML-based framework, Astonish allows you to configure and execute AI-powered tasks without extensive coding knowledge, democratizing the world of AI agent creation.

## Unleash the Power of AI in Your Workflow

Imagine having the ability to seamlessly integrate your favorite command-line tools—be it git, jq, cat, or any other—into an AI-driven workflow. Astonish turns this vision into reality, enabling you to extract information, process data, and perform actions with the combined power of AI and your trusted tools.

Here's how Astonish revolutionizes your workflow:

1. **AI-Powered Flow Creation**: Simply run `astonish agents run agents_creator` and describe your problem. Astonish's AI will craft a custom flow tailored to your needs, leveraging your existing tools.

2. **Flexible Refinement**: Fine-tune your AI-generated flow by editing the YAML file, optimizing prompts, and adjusting the workflow to perfectly fit your requirements.

3. **Endless Expandability**: Need additional capabilities like web searches, webpage content extraction, or PDF reading? Easily connect to thousands of available MCP (Model Context Protocol) servers to extend your toolset on demand.

4. **Personalized AI Assistance**: Astonish adapts to your unique workflow, combining the familiarity of your go-to tools with the innovation of AI, creating a powerhouse of productivity.

The possibilities are truly endless. Whether you're a developer streamlining your coding process, a data analyst automating complex data operations, or a content creator enhancing your research workflow, Astonish empowers you to achieve more with less effort.

Embrace the future of workflow automation with Astonish, where your tools meet AI, and your productivity knows no bounds.

## Key Features

- **Low-Code Magic**: Create and run customizable agentic workflows using an intuitive, YAML-based approach
- **AI-Powered Agent Creator**: Design new agents effortlessly with our intelligent agent creation assistant
- **Flexible AI Provider Management**: Configure and manage multiple AI providers seamlessly
- **Extensible Capabilities**: Leverage the Model Context Protocol (MCP) to expand your toolkit
- **Rich Tool Integration**: Incorporate various tools within your workflows, including embedded and custom options

## Supported AI Providers

| Provider         | Status       | Free API Plan               |
|------------------|--------------|-----------------------------|
| Anthropic        | Supported    | No                          |
| Google AI        | Supported    | Yes                         |
| Groq             | Supported    | Yes                         |
| LM Studio        | Supported    | Local                       |
| Ollama           | Supported    | Local                       |
| OpenAI           | Supported    | No                          |
| Openrouter       | Supported    | Yes                         |
| SAP AI Core      | Supported    | Yes (via SAP BTP Free Tier) |
| AWS Bedrock      | Coming soon  | No                          |
| X AI             | Coming soon  | No                          |

## Agents Creator: Your AI Architect

At the heart of Astonish lies the Agents Creator. This intelligent assistant guides you through the process of designing and implementing new AI agents:

1. **Interactive Design**: Engage in a dynamic conversation to capture the essence of your desired agent
2. **Automatic Flow Creation**: Watch as the Agents Creator designs an optimal agentic flow based on your requirements
3. **YAML Generation**: Receive a complete, ready-to-use YAML configuration for your new agent
4. **Instant Deployment**: Your new agent is immediately available for use within the Astonish ecosystem

With the Agents Creator, you're not just using AI – you're using AI that creates AI, unlocking a new realm of possibilities.

## Key Concepts

### Low-Code Revolution

Astonish champions a low-code philosophy, enabling users of all technical backgrounds to create complex AI agents using simple YAML configuration files and integrating MCP servers. This approach breaks down barriers, making advanced AI agent creation accessible to developers and non-developers alike.

### AI-Powered Agent Creation

The Agents Creator feature represents a paradigm shift in how AI agents are designed and implemented. By leveraging AI to create AI, Astonish offers an unprecedented level of assistance and automation in the agent creation process.

### Model Context Protocol (MCP) Support

Astonish leverages the Model Context Protocol (MCP) to extend its capabilities. MCP allows for seamless integration of additional tools and resources, enhancing the power and flexibility of your AI agents.

### Embedded Tools

Astonish comes with several tools embedded out-of-the-box:

- `read_file`: Read the contents of files
- `write_file`: Write or modify file contents
- `shell_command`: Execute shell commands

These tools provide a solid foundation for creating versatile agents capable of interacting with the file system and executing system commands.

## Installation

You can install Astonish using pip or from source code.

### Install with pip (Recommended)

To install Astonish using pip, run the following command:

```
pip install astonish
```

### Install from source code

To install Astonish from source code, follow these steps:

1. Clone the repository:

   ```
   git clone https://github.com/schardosin/astonish.git
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

To list all available agents:

```
astonish agents list
```

To edit a specific agent:

```
astonish agents edit <agent_name>
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
  - `agents/`: Predefined agents configurations (YAML files)

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
