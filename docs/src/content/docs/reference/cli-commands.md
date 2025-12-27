---
title: CLI Commands
description: Complete reference for all Astonish CLI commands
sidebar:
  order: 1
---

# CLI Commands

Complete reference for all Astonish CLI commands.

## Global Options

```bash
astonish [command] [subcommand] [flags]
```

| Flag | Description |
|------|-------------|
| `-h, --help` | Show help |
| `-v, --version` | Show version |

---

## astonish flows

Design and run AI flows.

### flows run

Execute a flow.

```bash
astonish flows run <name> [flags]
```

| Flag | Description | Default |
|------|-------------|---------|
| `-p key=value` | Pass parameter | — |
| `--model <name>` | Override model | Config |
| `--provider <name>` | Override provider | Config |
| `--debug` | Verbose output | false |
| `--browser` | Web UI mode | false |
| `--port <num>` | Web UI port | 8080 |

Example:
```bash
astonish flows run analyzer -p input="test" --debug
```

### flows import

Import a flow from a local YAML file.

```bash
astonish flows import <file.yaml> [--as <name>]
```

Example:
```bash
astonish flows import my-flow.yaml --as custom_flow
```

### flows remove

Remove an installed flow.

```bash
astonish flows remove <name>
```

### flows list

List available flows.

```bash
astonish flows list
```

### flows show

Display flow structure.

```bash
astonish flows show <name>
```

### flows edit

Open flow in default editor.

```bash
astonish flows edit <name>
```

### flows store

Manage flows from taps.

| Subcommand | Description |
|------------|-------------|
| `list` | List tap flows |
| `search <query>` | Search flows |
| `install <tap/flow>` | Install flow |
| `uninstall <name>` | Remove flow |
| `update` | Refresh manifests |

---

## astonish tools

Manage MCP tools.

### tools list

List available tools.

```bash
astonish tools list [-json]
```

### tools edit

Open MCP config in editor.

```bash
astonish tools edit
```

### tools store

Browse and install MCP servers.

| Subcommand | Description |
|------------|-------------|
| `list` | List available servers |
| `install <name>` | Install server |

---

## astonish tap

Manage extension repositories.

### tap add

Add a tap repository.

```bash
astonish tap add <repo> [--as <alias>]
```

Examples:
```bash
astonish tap add schardosin/astonish-flows
astonish tap add github.enterprise.com/team/flows --as team
```

### tap list

List configured taps.

```bash
astonish tap list
```

### tap remove

Remove a tap.

```bash
astonish tap remove <name>
```

### tap update

Refresh all manifests.

```bash
astonish tap update
```

---

## astonish config

Manage configuration.

### config show

Print current configuration.

```bash
astonish config show
```

### config edit

Open config in editor.

```bash
astonish config edit
```

### config directory

Print config directory path.

```bash
astonish config directory
```

---

## astonish setup

Run interactive provider setup.

```bash
astonish setup
```

Launches a wizard to configure AI providers and API keys.

---

## astonish studio

Launch the visual editor.

```bash
astonish studio [-port <num>]
```

| Flag | Description | Default |
|------|-------------|---------|
| `-port` | Server port | 9393 |

Opens browser to `http://localhost:<port>`.

---

## Environment Variables

### GitHub

| Variable | Description |
|----------|-------------|
| `GITHUB_TOKEN` | GitHub API access |
| `GITHUB_ENTERPRISE_TOKEN` | Enterprise GitHub |

### AI Providers

| Variable | Provider |
|----------|----------|
| `ANTHROPIC_API_KEY` | Anthropic (Claude) |
| `GOOGLE_API_KEY` | Google (Gemini) |
| `GROQ_API_KEY` | Groq |
| `OPENAI_API_KEY` | OpenAI |
| `OPENROUTER_API_KEY` | OpenRouter |
| `POE_API_KEY` | Poe |
| `XAI_API_KEY` | xAI (Grok) |
| `AICORE_CLIENT_ID` | SAP AI Core |
| `AICORE_CLIENT_SECRET` | SAP AI Core |
| `AICORE_AUTH_URL` | SAP AI Core |
| `AICORE_BASE_URL` | SAP AI Core |
| `AICORE_RESOURCE_GROUP` | SAP AI Core (optional) |

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Error |

---

## Configuration Files

| File | Purpose |
|------|---------|
| `config.yaml` | Main configuration |
| `mcp_config.json` | MCP servers |
| `store.json` | Configured taps |

Location: `astonish config directory`
