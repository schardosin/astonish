---
title: "daemon & scheduler"
description: "Background service and scheduled task management"
---

## astonish daemon

Manage the Astonish background service.

### Subcommands

| Subcommand | Description |
|------------|-------------|
| `install` | Install as system service (launchd on macOS, systemd on Linux) |
| `uninstall` | Remove the system service |
| `start` | Start the daemon |
| `stop` | Stop the daemon |
| `restart` | Restart the daemon |
| `status` | Show daemon status |
| `run` | Run in foreground (for debugging) |
| `logs` | Show daemon logs |

`daemon install` and `daemon run` support `--port` (default: 9393).
`daemon logs` supports `-f` (follow) and `-n` (line count, default: 50).

### Examples

```
astonish daemon install                 # Install as system service
astonish daemon start                   # Start the service (run after install)
astonish daemon status                  # Check if running
astonish daemon logs -f                 # Follow live logs
astonish daemon run --port 8080         # Foreground on custom port
```

## astonish scheduler

Manage scheduled jobs. Jobs are typically created through chat (ask the AI to schedule something) or via the `schedule_job` tool.

### Subcommands

| Subcommand | Aliases | Description |
|------------|---------|-------------|
| `list` | `ls` | List all scheduled jobs |
| `enable <name>` | | Enable a job |
| `disable <name>` | | Disable a job |
| `remove <name>` | `rm` | Remove a job |
| `run <name>` | | Trigger immediate execution |
| `status` | | Show scheduler status |

Job names support partial matching and case-insensitive lookup.

### Examples

```
astonish scheduler list                 # List all jobs
astonish scheduler run "daily-report"   # Trigger now
astonish scheduler disable "daily-report" # Pause a job
```
