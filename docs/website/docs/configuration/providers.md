# AI Providers

Astonish supports multiple AI providers out of the box. You can configure several providers simultaneously and switch between them at runtime via the Studio model selector.

## Supported Providers

| Provider | Type Key | Notes |
|----------|----------|-------|
| Anthropic | `anthropic` | Default recommended |
| OpenAI | `openai` | Function calling support |
| Google Gemini | `gemini` | Multimodal |
| Groq | `groq` | Fast inference |
| OpenRouter | `openrouter` | Multi-provider proxy |
| xAI | `xai` | Grok models |
| Ollama | `ollama` | Local models, no API key required |
| LM Studio | `lm_studio` | Local models, OpenAI-compatible |
| SAP AI Core | `sap_ai_core` | Enterprise SAP ecosystem |
| LiteLLM | `litellm` | Unified proxy gateway |
| Poe | `poe` | Poe platform models |

Additional providers (Azure OpenAI, AWS Bedrock, DeepSeek, Together AI, Fireworks AI) can be configured using the `openai` type with a custom `base_url`, or through a LiteLLM proxy.

## Configuration

Providers are configured as a map in `~/.config/astonish/config.yaml`. The map key is the instance name, and values are key-value pairs:

```yaml
general:
  default_provider: "anthropic"
  default_model: "claude-sonnet-4-20250514"

providers:
  anthropic:
    api_key: "$<ANTHROPIC_API_KEY>"
  openai:
    api_key: "$<OPENAI_API_KEY>"
```

The `general.default_provider` field specifies which provider to use by default, and `general.default_model` sets the default model.

### Provider Type Resolution

When the instance name matches a known provider type (e.g., `anthropic`, `openai`, `ollama`), the type is inferred automatically. For custom instance names, specify the `type` explicitly:

```yaml
providers:
  my-fast-model:
    type: "openai"
    api_key: "$<OPENAI_API_KEY>"
    base_url: "https://api.openai.com/v1"

  my-reasoning-model:
    type: "openai"
    api_key: "$<OPENAI_API_KEY>"
```

### Local Providers

For Ollama or LM Studio, specify the base URL instead of an API key:

```yaml
providers:
  ollama:
    base_url: "http://localhost:11434"

  lm-studio:
    type: "lm_studio"
    base_url: "http://localhost:1234/v1"
```

### OpenAI-Compatible Endpoints

Any OpenAI-compatible API can be used with the `openai` type and a custom base URL:

```yaml
providers:
  deepseek:
    type: "openai"
    api_key: "$<DEEPSEEK_API_KEY>"
    base_url: "https://api.deepseek.com/v1"

  azure-openai:
    type: "openai"
    api_key: "$<AZURE_KEY>"
    base_url: "https://myinstance.openai.azure.com"
    api_version: "2024-06-01"
```

### SAP AI Core

SAP AI Core requires OAuth2 client credentials:

```yaml
providers:
  sap-ai-core:
    type: "sap_ai_core"
    client_id: "$<SAP_CLIENT_ID>"
    client_secret: "$<SAP_CLIENT_SECRET>"
    auth_url: "https://auth.example.com/oauth/token"
    base_url: "https://api.ai.sap.com"
    resource_group: "engineering"
```

## Switching Providers at Runtime

Use the model selector dropdown in the Studio Chat header to switch between configured providers and models during a conversation. Changes take effect on the next message.

## Cloud Deployment

In cloud deployments, provider configuration can be managed at multiple levels through the platform admin interface:

- **Platform level** — Available to all users across all organizations
- **Org level** — Available to all teams within an organization
- **Team level** — Available to team members
- **Personal** — User's own providers

Administrators configure providers through **Studio Settings → Team Providers** or **Org Providers**.

## Setup Wizard

The easiest way to configure your first provider is through the interactive setup wizard:

```bash
astonish setup
```

This walks you through selecting a provider, entering your API key, and choosing a default model.

See [Config Reference](./config-reference.md) for the full configuration file structure.
