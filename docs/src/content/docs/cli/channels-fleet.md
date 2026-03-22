---
title: "channels & fleet"
description: "Communication channels and multi-agent team management"
---

## astonish channels

Manage communication channels (Telegram, Email).

### Subcommands

| Subcommand | Description |
|------------|-------------|
| `status` | Show status of all channels |
| `setup <channel>` | Interactive channel setup |
| `disable <channel>` | Disable a channel |

Available channels: `telegram`, `email`

### Examples

```
astonish channels status                # Check all channel status
astonish channels setup telegram        # Interactive Telegram setup
astonish channels disable email         # Disable email channel
```

Channels require the daemon to be running. Set up channels with `astonish channels setup <channel>` or through **Studio Settings > Channels**.

## astonish fleet

Manage fleet plans and autonomous agent teams.

### Subcommands

| Subcommand | Aliases | Description |
|------------|---------|-------------|
| `list` | `ls` | List all fleet plans |
| `show <key>` | | Show plan details |
| `activate <key>` | | Activate (start polling) |
| `deactivate <key>` | | Deactivate (stop polling) |
| `status <key>` | | Show activation status |
| `delete <key>` | `rm` | Delete a plan |
| `templates` | | List available fleet templates |

Plan keys support prefix matching and case-insensitive name matching. Plans are created through the Studio UI or the `/fleet-plan` slash command.

### Examples

```
astonish fleet list                     # List all plans
astonish fleet show my-plan             # View plan details
astonish fleet activate my-plan         # Start polling for work
astonish fleet status my-plan           # Check activation status
astonish fleet templates                # List available templates
```
