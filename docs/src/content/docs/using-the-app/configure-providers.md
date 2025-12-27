---
title: Configure Providers
description: Set up AI model providers in Astonish
sidebar:
  order: 1
---

# Configure Providers

Astonish supports multiple AI providers. Configure one or more to power your flows.

## Supported Providers

| Provider | Type | Best For |
|----------|------|----------|
| **OpenRouter** | Cloud | Access to many models with one key |
| **OpenAI** | Cloud | GPT-4, GPT-3.5 |
| **Anthropic** | Cloud | Claude 3 family |
| **Google Gemini** | Cloud | Gemini Pro, Flash |
| **Groq** | Cloud | Fast inference |
| **xAI** | Cloud | Grok models |
| **Ollama** | Local | Self-hosted open models |
| **LM Studio** | Local | Self-hosted with GUI |
| **AWS Bedrock** | Cloud | Enterprise AWS integration |
| **Vertex AI** | Cloud | Google Cloud integration |
| **SAP AI Core** | Cloud | SAP enterprise |
| **Poe** | Cloud | Multiple models via Poe |

## Method 1: Setup Wizard

The easiest way to configure providers:

```bash
astonish setup
```

![Setup Wizard](/astonish/images/placeholder.png)
*Interactive provider selection*

1. Select a provider from the list
2. Enter your API key
3. Choose a default model
4. Done!

## Method 2: Edit Config

Open the configuration file:

```bash
astonish config edit
```

Or edit directly:

```bash
# macOS
code ~/Library/Application\ Support/astonish/config.yaml

# Linux
code ~/.config/astonish/config.yaml
```

### Config Structure

```yaml
general:
  default_provider: openrouter
  default_model: anthropic/claude-3-sonnet

providers:
  openrouter:
    api_key: sk-or-v1-...
  
  openai:
    api_key: sk-...
  
  anthropic:
    api_key: sk-ant-...
  
  ollama:
    # No API key needed for local
```

## Provider Configuration

### OpenRouter (Recommended)

```yaml
providers:
  openrouter:
    api_key: sk-or-v1-xxxxxxxxxxxxxxxxxxxxx
```

Get your API key at [openrouter.ai](https://openrouter.ai/).

Access 100+ models including:
- `anthropic/claude-3-opus`
- `openai/gpt-4-turbo`
- `google/gemini-pro`
- `meta-llama/llama-3-70b`

### OpenAI

```yaml
providers:
  openai:
    api_key: sk-xxxxxxxxxxxxxxxxxxxxx
```

Models:
- `gpt-4o`
- `gpt-4-turbo`
- `gpt-3.5-turbo`

### Anthropic

```yaml
providers:
  anthropic:
    api_key: sk-ant-xxxxxxxxxxxxxxxxxxxxx
```

Models:
- `claude-3-opus`
- `claude-3-sonnet`
- `claude-3-haiku`

### Google Gemini

```yaml
providers:
  google:
    api_key: AIzaxxxxxxxxxxxxxxxxxx
```

Models:
- `gemini-1.5-pro`
- `gemini-1.5-flash`

### Groq

```yaml
providers:
  groq:
    api_key: gsk_xxxxxxxxxxxxxxxxxxxxx
```

Models:
- `llama3-70b-8192`
- `mixtral-8x7b-32768`

### Ollama (Local)

```yaml
providers:
  ollama:
    # No API key required
    # Ollama must be running on localhost:11434
```

Install: [ollama.ai](https://ollama.ai/)

```bash
# Pull a model
ollama pull llama3

# Run Astonish with Ollama
astonish flows run my_flow -provider ollama -model llama3
```

### LM Studio (Local)

```yaml
providers:
  lmstudio:
    # No API key required
    # LM Studio must be running with server enabled
```

Download: [lmstudio.ai](https://lmstudio.ai/)

## Setting Defaults

Configure your preferred provider and model:

```yaml
general:
  default_provider: openrouter
  default_model: anthropic/claude-3-sonnet
```

Override at runtime:

```bash
astonish flows run my_flow -provider openai -model gpt-4
```

## Environment Variables

Some providers support environment variables:

```bash
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export OPENROUTER_API_KEY="sk-or-v1-..."
```

## Verifying Configuration

Check your current config:

```bash
astonish config show
```

Test with a simple flow:

```bash
astonish flows run test_flow
```

## Security

:::warning
Never commit API keys to version control!
:::

Best practices:
1. Use environment variables in CI/CD
2. Add `config.yaml` to `.gitignore`
3. Use secrets management in production

## Next Steps

- **[Add MCP Servers](/using-the-app/add-mcp-servers/)** — Connect external tools
- **[Manage Taps](/using-the-app/manage-taps/)** — Access community flows
