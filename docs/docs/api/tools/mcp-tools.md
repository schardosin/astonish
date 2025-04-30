---
sidebar_position: 2
---

# MCP Tools

MCP (Model Context Protocol) tools extend the capabilities of Astonish by connecting to external services. This page explains how to use and create MCP tools.

## Overview

MCP tools are provided by external servers that implement the Model Context Protocol. These servers can provide additional functionality beyond the built-in tools, such as web search, API access, or specialized processing.

## Using MCP Tools

### Configuration

To use MCP tools, you need to configure the MCP server in the MCP configuration file:

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

### Example Configuration

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

### Using MCP Tools in Agents

Once you've configured an MCP server, you can use its tools in your agents:

```yaml
- name: search_web
  type: llm
  prompt: |
    Search the web for information about: {search_query}
  output_model:
    search_results: str
  tools: true
  tools_selection:
    - tavily_search  # An MCP tool for web search
```

## Creating MCP Servers

You can create your own MCP servers to provide custom tools for Astonish. MCP servers are implemented using the Model Context Protocol SDK.

### Prerequisites

- Node.js (version 14 or higher)
- Basic knowledge of JavaScript/TypeScript

### Installation

```bash
npm install @modelcontextprotocol/sdk
```

### Basic Server Structure

Here's a basic structure for an MCP server:

```typescript
#!/usr/bin/env node
import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from '@modelcontextprotocol/sdk/types.js';

class MyMCPServer {
  private server: Server;

  constructor() {
    this.server = new Server(
      {
        name: 'my-mcp-server',
        version: '0.1.0',
      },
      {
        capabilities: {
          tools: {},
        },
      }
    );

    this.setupToolHandlers();
    
    // Error handling
    this.server.onerror = (error) => console.error('[MCP Error]', error);
    process.on('SIGINT', async () => {
      await this.server.close();
      process.exit(0);
    });
  }

  private setupToolHandlers() {
    this.server.setRequestHandler(ListToolsRequestSchema, async () => ({
      tools: [
        {
          name: 'my_tool',
          description: 'A custom tool that does something useful',
          inputSchema: {
            type: 'object',
            properties: {
              param1: {
                type: 'string',
                description: 'Parameter 1',
              },
              param2: {
                type: 'number',
                description: 'Parameter 2',
              },
            },
            required: ['param1'],
          },
        },
      ],
    }));

    this.server.setRequestHandler(CallToolRequestSchema, async (request) => {
      if (request.params.name !== 'my_tool') {
        return {
          content: [
            {
              type: 'text',
              text: `Unknown tool: ${request.params.name}`,
            },
          ],
          isError: true,
        };
      }

      try {
        const param1 = request.params.arguments.param1;
        const param2 = request.params.arguments.param2 || 0;

        // Implement your tool logic here
        const result = `Processed ${param1} with value ${param2}`;

        return {
          content: [
            {
              type: 'text',
              text: result,
            },
          ],
        };
      } catch (error) {
        return {
          content: [
            {
              type: 'text',
              text: `Error: ${error.message}`,
            },
          ],
          isError: true,
        };
      }
    });
  }

  async run() {
    const transport = new StdioServerTransport();
    await this.server.connect(transport);
    console.error('MCP server running on stdio');
  }
}

const server = new MyMCPServer();
server.run().catch(console.error);
```

### Building and Running

1. Save the code to a file (e.g., `my-mcp-server.ts`)
2. Compile it with TypeScript:

```bash
tsc my-mcp-server.ts
```

3. Make the output file executable:

```bash
chmod +x my-mcp-server.js
```

4. Configure it in the MCP configuration file:

```json
{
  "mcpServers": {
    "my-server": {
      "command": "node",
      "args": ["/path/to/my-mcp-server.js"],
      "env": {
        "API_KEY": "your-api-key"
      }
    }
  }
}
```

## Example: Weather MCP Server

Here's a more complete example of an MCP server that provides weather information:

```typescript
#!/usr/bin/env node
import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import {
  CallToolRequestSchema,
  ErrorCode,
  ListToolsRequestSchema,
  McpError,
} from '@modelcontextprotocol/sdk/types.js';
import axios from 'axios';

const API_KEY = process.env.OPENWEATHER_API_KEY;
if (!API_KEY) {
  throw new Error('OPENWEATHER_API_KEY environment variable is required');
}

class WeatherServer {
  private server: Server;
  private axiosInstance;

  constructor() {
    this.server = new Server(
      {
        name: 'weather-server',
        version: '0.1.0',
      },
      {
        capabilities: {
          tools: {},
        },
      }
    );

    this.axiosInstance = axios.create({
      baseURL: 'http://api.openweathermap.org/data/2.5',
      params: {
        appid: API_KEY,
        units: 'metric',
      },
    });

    this.setupToolHandlers();
    
    // Error handling
    this.server.onerror = (error) => console.error('[MCP Error]', error);
    process.on('SIGINT', async () => {
      await this.server.close();
      process.exit(0);
    });
  }

  private setupToolHandlers() {
    this.server.setRequestHandler(ListToolsRequestSchema, async () => ({
      tools: [
        {
          name: 'get_weather',
          description: 'Get current weather for a city',
          inputSchema: {
            type: 'object',
            properties: {
              city: {
                type: 'string',
                description: 'City name',
              },
            },
            required: ['city'],
          },
        },
        {
          name: 'get_forecast',
          description: 'Get weather forecast for a city',
          inputSchema: {
            type: 'object',
            properties: {
              city: {
                type: 'string',
                description: 'City name',
              },
              days: {
                type: 'number',
                description: 'Number of days (1-5)',
                minimum: 1,
                maximum: 5,
              },
            },
            required: ['city'],
          },
        },
      ],
    }));

    this.server.setRequestHandler(CallToolRequestSchema, async (request) => {
      try {
        if (request.params.name === 'get_weather') {
          const city = request.params.arguments.city;
          
          const response = await this.axiosInstance.get('weather', {
            params: { q: city },
          });

          return {
            content: [
              {
                type: 'text',
                text: JSON.stringify({
                  temperature: response.data.main.temp,
                  conditions: response.data.weather[0].description,
                  humidity: response.data.main.humidity,
                  wind_speed: response.data.wind.speed,
                }, null, 2),
              },
            ],
          };
        } else if (request.params.name === 'get_forecast') {
          const city = request.params.arguments.city;
          const days = Math.min(request.params.arguments.days || 3, 5);
          
          const response = await this.axiosInstance.get('forecast', {
            params: {
              q: city,
              cnt: days * 8,
            },
          });

          return {
            content: [
              {
                type: 'text',
                text: JSON.stringify(response.data.list, null, 2),
              },
            ],
          };
        } else {
          throw new McpError(
            ErrorCode.MethodNotFound,
            `Unknown tool: ${request.params.name}`
          );
        }
      } catch (error) {
        if (axios.isAxiosError(error)) {
          return {
            content: [
              {
                type: 'text',
                text: `Weather API error: ${
                  error.response?.data.message ?? error.message
                }`,
              },
            ],
            isError: true,
          };
        }
        throw error;
      }
    });
  }

  async run() {
    const transport = new StdioServerTransport();
    await this.server.connect(transport);
    console.error('Weather MCP server running on stdio');
  }
}

const server = new WeatherServer();
server.run().catch(console.error);
```

## Best Practices

1. **Error Handling**: Implement robust error handling in your MCP server
2. **Input Validation**: Validate input parameters to prevent errors
3. **Security**: Be careful with API keys and sensitive data
4. **Documentation**: Provide clear descriptions for your tools and parameters
5. **Performance**: Optimize your server for performance, especially for frequently used tools
6. **Testing**: Test your MCP server thoroughly before using it in production
7. **Versioning**: Use semantic versioning for your MCP server

## Troubleshooting

### MCP Server Not Connected

If your MCP server is not connecting:

1. Check that the server is properly configured in the MCP configuration file
2. Verify that the server command and arguments are correct
3. Ensure any required environment variables are set
4. Check the server logs for error messages

### Tool Not Found

If a tool is not found:

1. Check that the tool is properly registered in the MCP server
2. Verify that the tool name is spelled correctly in the agent configuration
3. Check that the MCP server is running and connected

### Tool Execution Failed

If a tool execution fails:

1. Check the error message for specific details
2. Verify that the tool's input parameters are correct
3. Check that any external services the tool depends on are available
4. Check the server logs for error messages

## Related Modules

- [Internal Tools](/docs/api/tools/internal-tools): Built-in tools provided by Astonish
