---
title: "AI Providers"
description: "Configure AI model providers for Astonish"
---

Astonish supports 15+ AI providers. The easiest way to configure a provider is the interactive setup wizard:

```bash
astonish setup
```

You can also add and manage providers through **Studio Settings > Providers**.

## Supported Providers

| Provider | Type Key | Description |
|----------|----------|-------------|
| OpenAI | `openai` | GPT-4o, GPT-4, GPT-3.5, o1, o3 |
| Anthropic | `anthropic` | Claude 4, Claude 3.5 Sonnet |
| Google Gemini | `gemini` | Gemini 2.5 Pro, Flash |
| AWS Bedrock | `bedrock` | Access Anthropic, Meta, etc. via AWS |
| Azure OpenAI | `azure` | OpenAI models via Azure |
| Ollama | `ollama` | Local models (Llama, Mistral, etc.) |
| OpenRouter | `openrouter` | Multi-provider routing |
| Groq | `groq` | Ultra-fast inference |
| DeepSeek | `deepseek` | DeepSeek models |
| Fireworks | `fireworks` | Fast inference platform |
| Cerebras | `cerebras` | Fast inference |
| Together | `together` | Open-source model hosting |
| Mistral | `mistral` | Mistral models |
| xAI | `xai` | Grok models |
| LM Studio | `lm_studio` | Local model server |
| LiteLLM | `litellm` | Universal LLM proxy |
| SAP AI Core | `sap_ai_core` | SAP enterprise AI |
| Poe | `poe` | Poe.com models |
| OpenAI Compatible | `openai_compat` | Any OpenAI-compatible API |

## Manual Configuration

If you prefer to edit the config file directly, providers are defined in `config.yaml`:

```yaml
general:
  default_provider: my-openai
  default_model: gpt-4o

providers:
  my-openai:
    type: openai
    api_key: "sk-..."
    model: gpt-4o

  local-ollama:
    type: ollama
    base_url: "http://localhost:11434"
    model: llama3.1

  my-anthropic:
    type: anthropic
    api_key: "sk-ant-..."
```

## Quick Setup

Run `astonish setup` for an interactive walkthrough that configures your provider and stores credentials securely. You can re-run it at any time to add more providers or change settings.

## Multiple Providers

You can configure several providers and switch between them:

- Use the `--provider` flag on the CLI
- Change the active provider in Studio settings

## API Key Security

API keys are automatically scrubbed from the config file into the encrypted credential store after initial setup. You do not need to keep plaintext keys in the config file.

## OpenAI-Compatible APIs

The `openai_compat` type works with any OpenAI-compatible API endpoint, including vLLM, text-generation-inference, and other local or remote servers.
