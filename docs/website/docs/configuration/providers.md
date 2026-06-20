# AI Providers

Astonish supports 15+ AI providers out of the box. You can configure multiple providers simultaneously and switch between them at runtime.

## Supported Providers

| Provider | Models | Notes |
|----------|--------|-------|
| Anthropic | Claude 4, Sonnet, Haiku | Default recommended |
| OpenAI | GPT-4o, GPT-4.1, o3 | Function calling support |
| Google Gemini | Gemini 2.5 Pro/Flash | Multimodal |
| Groq | Llama, Mixtral | Fast inference |
| OpenRouter | Multi-provider proxy | Access many models |
| xAI | Grok | Real-time knowledge |
| Ollama | Local models | No API key required |
| LM Studio | Local models | OpenAI-compatible |
| SAP AI Core | Enterprise models | SAP ecosystem |
| LiteLLM | Proxy gateway | Unified interface |
| DeepSeek | DeepSeek V3/R1 | Reasoning models |
| Azure OpenAI | GPT-4o, GPT-4.1 | Enterprise Azure |
| AWS Bedrock | Claude, Titan | AWS-hosted |
| Together AI | Open-source models | Fine-tuned variants |
| Fireworks AI | Open-source models | Fast inference |

## Configuration

Each provider entry requires a `name` and typically an `api_key`:

```yaml
providers:
  - name: anthropic
    api_key: "${ANTHROPIC_API_KEY}"
    default: true
    model: "claude-sonnet-4-20250514"

  - name: openai
    api_key: "${OPENAI_API_KEY}"
    model: "gpt-4o"
```

### Local Providers

For Ollama or LM Studio, specify the base URL instead of an API key:

```yaml
providers:
  - name: ollama
    base_url: "http://localhost:11434"
    model: "llama3.1:70b"

  - name: lm-studio
    base_url: "http://localhost:1234/v1"
    model: "local-model"
```

### Multiple Instances

You can register the same provider type multiple times with different configs:

```yaml
providers:
  - name: openai
    label: "openai-fast"
    api_key: "${OPENAI_API_KEY}"
    model: "gpt-4o-mini"

  - name: openai
    label: "openai-reasoning"
    api_key: "${OPENAI_API_KEY}"
    model: "o3"
```

Switch at runtime with `/model openai-reasoning` or via the Studio model selector.

## Cloud Deployment Cascading

In cloud deployments, provider configuration cascades through four levels:

```
Platform → Org → Team → Personal
```

Administrators can enforce allowed providers and models:

```yaml
# Org-level: restrict to approved providers
org:
  enforce_providers: true
  providers:
    - name: sap-ai-core
      base_url: "https://api.ai.sap.com"
      resource_group: "engineering"
      default: true
    - name: anthropic
      api_key: "${ORG_ANTHROPIC_KEY}"
```

When `enforce_providers: true`, users cannot add personal providers—only select from those defined above them in the cascade.

## Provider-Specific Options

Some providers accept additional configuration:

```yaml
providers:
  - name: azure-openai
    api_key: "${AZURE_KEY}"
    base_url: "https://myinstance.openai.azure.com"
    api_version: "2024-06-01"
    deployment: "gpt-4o"

  - name: aws-bedrock
    region: "us-east-1"
    access_key: "${AWS_ACCESS_KEY}"
    secret_key: "${AWS_SECRET_KEY}"
```

See [Config Reference](./config-reference.md) for the full configuration file structure.
