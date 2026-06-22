# AI Providers

Astonish supports 12 AI providers out of the box. Providers are managed through Studio Settings and stored in the database, with API keys secured in the encrypted credential store.

## Supported Providers

| Provider | Type Key | Notes |
|----------|----------|-------|
| Anthropic | `anthropic` | Default recommended |
| OpenAI | `openai` | Function calling support |
| Google GenAI | `gemini` | Multimodal |
| Groq | `groq` | Fast inference |
| OpenRouter | `openrouter` | Multi-provider proxy |
| xAI | `xai` | Grok models |
| Ollama | `ollama` | Local models, no API key required |
| LM Studio | `lm_studio` | Local models, OpenAI-compatible |
| OpenAI Compatible | `openai_compat` | Any OpenAI-compatible endpoint |
| SAP AI Core | `sap_ai_core` | Enterprise SAP ecosystem |
| LiteLLM | `litellm` | Unified proxy gateway |
| Poe | `poe` | Poe platform models |

Additional services (Azure OpenAI, AWS Bedrock, DeepSeek, Together AI, Fireworks AI) can be configured using the `openai_compat` type with a custom base URL, or through a LiteLLM proxy.

## Managing Providers

### Studio Settings (Recommended)

The primary way to manage providers is through **Studio Settings → Providers**. This stores configurations in the database using the 3-tier cascade system.

## 3-Tier Cascade Resolution

Astonish resolves provider configuration using a **3-tier inheritance system** where the closest level to the user takes priority:

```
Platform (base) → Organization (overrides platform) → Team (overrides everything)
```

| Tier | Set By | Scope | Priority |
|------|--------|-------|----------|
| **Platform** | Platform admin | All organizations and teams | Lowest (base defaults) |
| **Organization** | Org admin | All teams within the org | Overrides platform |
| **Team** | Team admin | Only that team's members | Highest (wins always) |

### How Resolution Works

When a user sends a message, Astonish resolves which provider and model to use by applying layers in order:

1. **Start with Platform settings** — These are the base defaults available to everyone. If the platform admin configured Anthropic as the default provider with `claude-sonnet-4-20250514`, that's the starting point.

2. **Apply Organization settings** — If the org admin has set different defaults (e.g., a different model or added extra providers), those override the platform values. Any field left empty at the org level inherits from platform.

3. **Apply Team settings** — If the team admin has configured providers or a different default model, those take final priority. Again, only non-empty values override — anything not set at the team level inherits from the org (or platform).

### Inheritance Rules

- **Default provider/model**: The closest non-empty value to the user wins. If the team sets a default model, it's used regardless of what org or platform say. If the team doesn't set one, the org value is checked, then platform.
- **Provider configs are additive**: Providers defined at any level are merged together. A team can access providers configured at platform level without re-declaring them.
- **Same-name providers override**: If a provider named `openai` exists at both platform and team level, the team's configuration (API key, base URL, etc.) takes precedence.

### Example Scenario

```
Platform admin configures:
  → Default provider: anthropic
  → Default model: claude-sonnet-4-20250514
  → Providers: anthropic, openai

Org admin configures:
  → (nothing — inherits everything from platform)

Team "backend-eng" admin configures:
  → Default model: claude-sonnet-4-20250514
  → Providers: ollama (local models)

Result for "backend-eng" members:
  → Default provider: anthropic (inherited from platform)
  → Default model: claude-sonnet-4-20250514 (team override)
  → Available providers: anthropic, openai (platform) + ollama (team)
```

### Why This Matters

- **Platform admins** set organization-wide defaults and approved providers
- **Org admins** can customize for their organization without affecting others
- **Team admins** can fine-tune for their team's specific needs (faster models for dev, stronger models for code review, local models for air-gapped work)
- **No duplication needed** — teams inherit everything from above and only override what they need

API keys are stored securely — at the platform level they go into a separate encrypted secrets table, at the team level they're stored in the team's database record.

### Setup Wizard

For initial configuration, the setup wizard walks you through provider setup interactively:

```bash
astonish setup
```

This saves the provider to both the platform database and the local credential store.

## Provider Configuration Fields

Each provider is a map of key-value pairs. Common fields:

| Field | Description |
|-------|-------------|
| `type` | Provider type (required if instance name doesn't match a known type) |
| `api_key` | API key for authentication |
| `base_url` | Custom endpoint URL |
| `client_id` | OAuth2 client ID (SAP AI Core) |
| `client_secret` | OAuth2 client secret (SAP AI Core) |
| `auth_url` | OAuth2 token endpoint (SAP AI Core) |
| `resource_group` | Resource group (SAP AI Core) |

### Type Resolution

When the instance name matches a known provider type (e.g., `anthropic`, `openai`, `ollama`), the type is inferred automatically. For custom instance names, specify the `type` explicitly.

## Provider Examples

### Cloud Providers

```
Instance: anthropic
Fields:
  api_key: sk-ant-...
```

```
Instance: openai
Fields:
  api_key: sk-...
```

### Local Providers

```
Instance: ollama
Fields:
  base_url: http://localhost:11434
```

```
Instance: lm-studio
Fields:
  type: lm_studio
  base_url: http://localhost:1234/v1
```

### OpenAI-Compatible Endpoints

Any OpenAI-compatible API can be used with the `openai_compat` type:

```
Instance: deepseek
Fields:
  type: openai_compat
  api_key: ...
  base_url: https://api.deepseek.com/v1
```

```
Instance: azure-openai
Fields:
  type: openai_compat
  api_key: ...
  base_url: https://myinstance.openai.azure.com
```

### SAP AI Core

SAP AI Core requires OAuth2 client credentials:

```
Instance: sap-ai-core
Fields:
  type: sap_ai_core
  client_id: ...
  client_secret: ...
  auth_url: https://auth.example.com/oauth/token
  base_url: https://api.ai.sap.com
  resource_group: engineering
```

## Default Provider and Model

The default provider and model are configured through **Studio Settings → Providers**. These cascade through the same 3-tier system (Platform → Org → Team), with the closest tier taking priority.

The active provider and model are displayed as a read-only chip in the Studio top bar. To change which model is used, update the default in Settings — the change applies to all subsequent messages.

## Environment Variable Fallback

At runtime, if a provider's API key is not found in the database or credential store, the system falls back to environment variables:

| Provider | Environment Variable |
|----------|---------------------|
| Anthropic | `ANTHROPIC_API_KEY` |
| OpenAI | `OPENAI_API_KEY` |
| Google | `GOOGLE_API_KEY` |
| Groq | `GROQ_API_KEY` |
| xAI | `XAI_API_KEY` |

See [Config Reference](./config-reference.md) for the full configuration architecture.
