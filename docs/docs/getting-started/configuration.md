---
sidebar_position: 2
---

# Configuration

After installing Astonish, you need to configure it to work with an AI provider. This guide will walk you through the configuration process.

## Configuration Files

Astonish uses two main configuration files:

1. **config.ini**: General configuration settings
2. **mcp_config.json**: MCP (Model Context Protocol) server configuration

These files are automatically created and managed by the application in the user's config directory:

- On Linux/macOS: `~/.config/astonish/`
- On Windows: `%APPDATA%\astonish\`

## Setting Up an AI Provider

Astonish supports multiple AI providers. To set up a provider, use the `setup` command:

```bash
astonish setup
```

This will start an interactive setup process:

1. Astonish will display a list of available providers
2. Select a provider by entering its number
3. Follow the provider-specific configuration steps

### Supported Providers

Astonish currently supports the following AI providers:

| Provider | Description |
|----------|-------------|
| Anthropic | Access to Claude models |
| LM Studio | Integration with locally running LM Studio |
| Ollama | Integration with locally running Ollama models |
| Openrouter | Access to various models through Openrouter |
| SAP AI Core | Integration with SAP AI Core |

### Provider-Specific Configuration

Each provider has different configuration requirements:

#### Anthropic

- API Key: Required for authentication
- Model: The Claude model to use (e.g., claude-3-opus-20240229)

#### LM Studio

- Host: The host where LM Studio is running (default: localhost)
- Port: The port LM Studio is listening on (default: 1234)
- Model: The model name (optional)

#### Ollama

- Host: The host where Ollama is running (default: localhost)
- Port: The port Ollama is listening on (default: 11434)
- Model: The model name (e.g., llama2)

#### Openrouter

- API Key: Required for authentication
- Model: The model to use (e.g., openai/gpt-4)

#### SAP AI Core

- API Key: Required for authentication
- URL: The SAP AI Core API URL
- Deployment ID: The deployment ID for the model

## MCP Configuration

MCP (Model Context Protocol) allows Astonish to extend its capabilities by connecting to external tools and resources. To configure MCP servers:

```bash
astonish tools edit
```

This will open the MCP configuration file in your default editor. The configuration is a JSON file with the following structure:

```json
{
  "mcpServers": {
    "server-name": {
      "command": "command-to-run-server",
      "args": ["arg1", "arg2"],
      "env": {
        "ENV_VAR1": "value1",
        "ENV_VAR2": "value2"
      }
    }
  }
}
```

### Example MCP Configuration

Here's an example configuration for a weather MCP server:

```json
{
  "mcpServers": {
    "weather": {
      "command": "node",
      "args": ["/path/to/weather-server/index.js"],
      "env": {
        "OPENWEATHER_API_KEY": "your-api-key"
      }
    }
  }
}
```

## Advanced Configuration

### Environment Variables

Astonish supports the following environment variables:

- `ASTONISH_CONFIG_DIR`: Override the default configuration directory
- `ASTONISH_LOG_LEVEL`: Set the logging level (DEBUG, INFO, WARNING, ERROR)
- `ASTONISH_PROVIDER`: Override the default AI provider

### Logging

Astonish logs information to help with debugging and monitoring. You can control the verbosity of logging using the `--verbose` flag or the `ASTONISH_LOG_LEVEL` environment variable.

## Next Steps

After configuring Astonish, you're ready to:

1. Try the [Quick Start Guide](/docs/getting-started/quick-start) to create your first agent
2. Learn about [Agentic Flows](/docs/concepts/agentic-flows) to understand how Astonish works
3. Explore the [Commands Reference](/docs/commands/setup) for more details on available commands
