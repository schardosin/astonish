---
sidebar_position: 1
---

# Setup Command

The `setup` command is used to configure Astonish, particularly for setting up AI providers.

## Usage

```bash
astonish setup [OPTIONS] [TYPE]
```

## Options

| Option | Description |
|--------|-------------|
| `-h`, `--help` | Show help message and exit |
| `-v`, `--verbose` | Enable verbose output |
| `--version` | Show version information and exit |

## Types

| Type | Description |
|------|-------------|
| `provider` | Configure a specific AI provider |

If no type is specified, the command defaults to provider setup.

## Examples

### Basic Setup

```bash
astonish setup
```

This will start the interactive setup process, allowing you to select and configure an AI provider.

### Provider Setup

```bash
astonish setup provider
```

This explicitly specifies that you want to configure an AI provider.

## Interactive Process

When you run the setup command, Astonish will:

1. Display a list of available AI providers
2. Prompt you to select a provider from the list
3. Guide you through the configuration process for the selected provider
4. Save the configuration for future use

## Supported Providers

Astonish supports multiple AI providers, including:

- Anthropic
- LM Studio
- Ollama
- Openrouter
- SAP AI Core

Each provider may have different configuration requirements, which will be presented during the setup process.

## Implementation Details

The setup command is implemented in the `setup()` function in `main.py`. It uses the `AIProviderFactory` to get the list of registered providers and then guides the user through the configuration process.
