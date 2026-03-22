---
title: "Config File Reference"
description: "Complete reference for config.yaml"
---

The main configuration file is located at `~/.config/astonish/config.yaml`.

**Useful commands:**

- `astonish config directory` — print the config directory path
- `astonish config edit` — open the config file in your editor
- `astonish config show` — display the current configuration

## general

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `default_provider` | string | | Default provider instance name |
| `default_model` | string | | Default model name |
| `web_search_tool` | string | | Configured web search tool |
| `web_extract_tool` | string | | Configured web extract tool |
| `context_length` | int | | Override context window size (tokens) |
| `timezone` | string | | IANA timezone (e.g., `"America/New_York"`) |

## chat

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `system_prompt` | string | | Custom system prompt |
| `max_tool_calls` | int | | Max tool calls per turn |
| `max_tools` | int | | Max tools available to agent |
| `auto_approve` | bool | `false` | Auto-approve tool executions |
| `workspace_dir` | string | | Working directory |
| `flow_save_dir` | string | | Directory for saved flows |

## sessions

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `storage` | string | `"file"` | `"memory"` or `"file"` |
| `base_dir` | string | `~/.config/astonish/sessions/` | Session storage directory |
| `compaction.enabled` | bool | `true` | Enable auto-compaction |
| `compaction.threshold` | float | `0.8` | Context fraction triggering compaction |
| `compaction.preserve_recent` | int | `4` | Recent messages to keep intact |

## memory

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `enabled` | bool | `true` | Enable memory system |
| `memory_dir` | string | `~/.config/astonish/memory/` | Memory files directory |
| `vector_dir` | string | `~/.config/astonish/memory/vectors/` | Vector store directory |
| `embedding.provider` | string | `"auto"` | `auto`, `local`, `openai`, `ollama`, `openai-compat` |
| `embedding.model` | string | | Embedding model name |
| `embedding.base_url` | string | | Embedding API URL |
| `embedding.api_key` | string | | Embedding API key |
| `chunking.max_chars` | int | `1600` | Max characters per chunk |
| `chunking.overlap` | int | `320` | Overlap between chunks |
| `search.max_results` | int | `6` | Max search results |
| `search.min_score` | float | `0.35` | Min similarity score |
| `sync.watch` | bool | `true` | File watcher enabled |
| `sync.debounce_ms` | int | `1500` | Debounce for file changes |

## daemon

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `port` | int | `9393` | HTTP port |
| `log_dir` | string | `~/.config/astonish/logs/` | Log directory |
| `auth.disabled` | bool | `false` | Disable Studio auth |
| `auth.session_ttl_days` | int | `90` | Auth session TTL |

## channels

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `enabled` | bool | `false` | Master channel switch |
| `telegram.enabled` | bool | `false` | Enable Telegram |
| `telegram.bot_token` | string | | Telegram bot token |
| `telegram.allow_from` | string[] | | Allowed Telegram user IDs |
| `email.enabled` | bool | `false` | Enable email channel |
| `email.provider` | string | `"imap"` | `imap` or `gmail` |
| `email.imap_server` | string | | IMAP server:port |
| `email.smtp_server` | string | | SMTP server:port |
| `email.address` | string | | Agent email address |
| `email.username` | string | | Login username |
| `email.password` | string | | Login password (credential store) |
| `email.poll_interval` | int | `30` | Seconds between checks |
| `email.allow_from` | string[] | | Allowed sender emails |
| `email.folder` | string | `"INBOX"` | IMAP folder |
| `email.mark_read` | bool | `true` | Mark processed as read |
| `email.max_body_chars` | int | `50000` | Max email body chars |

## browser

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `headless` | bool | `false` | Headless mode |
| `viewport_width` | int | `1280` | Viewport width |
| `viewport_height` | int | `720` | Viewport height |
| `chrome_path` | string | | Custom Chrome binary |
| `user_data_dir` | string | `~/.config/astonish/browser/` | Profile directory |
| `navigation_timeout` | int | `30` | Page load timeout (seconds) |
| `proxy` | string | | HTTP/SOCKS proxy |
| `remote_cdp_url` | string | | External CDP WebSocket URL |
| `fingerprint_seed` | string | | CloakBrowser fingerprint seed |
| `fingerprint_platform` | string | | CloakBrowser OS platform |
| `handoff_bind_address` | string | `"127.0.0.1"` | Handoff binding address |
| `handoff_port` | int | `9222` | CDP handoff port |

## sub_agents

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `enabled` | bool | `true` | Enable delegation |
| `max_depth` | int | `2` | Max nesting depth |
| `max_concurrent` | int | `5` | Max parallel sub-agents |
| `task_timeout_sec` | int | `300` | Per-task timeout |

## skills

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `enabled` | bool | `true` | Enable skills |
| `user_dir` | string | `~/.config/astonish/skills/` | Custom skills directory |
| `extra_dirs` | string[] | | Additional skill directories |
| `allowlist` | string[] | | Restrict loaded skills |

## scheduler

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `enabled` | bool | `true` | Enable scheduler |

## agent_identity

| Key | Type | Description |
|-----|------|-------------|
| `name` | string | Display name |
| `username` | string | Base username |
| `email` | string | Agent email |
| `bio` | string | Short description |
| `website` | string | URL for profiles |
| `locale` | string | Language/locale (e.g., `"en-US"`) |
| `timezone` | string | IANA timezone |

## opencode

| Key | Type | Description |
|-----|------|-------------|
| `model` | string | Override model for OpenCode delegate |
