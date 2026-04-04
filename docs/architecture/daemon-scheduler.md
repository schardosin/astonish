# Daemon & Scheduler

## Overview

The Astonish daemon is a background service that orchestrates the entire runtime: configuration loading, credential store, provider setup, agent initialization, channels, scheduler, fleet plan activation, sandbox management, and the Studio HTTP server. It installs as a platform-native service (systemd on Linux, launchd on macOS, Windows service on Windows).

The scheduler provides cron-based job execution with three modes: routine (headless flow execution), adaptive (chat-based instruction execution), and fleet poll (GitHub issue monitoring).

## Key Design Decisions

### Why a Daemon Instead of On-Demand Processes

Astonish needs long-running capabilities:

- **Channels**: Telegram long polling and IMAP email polling require persistent connections.
- **Scheduler**: Cron jobs need a process to fire them.
- **Fleet**: Fleet sessions run for minutes to hours with continuous agent collaboration.
- **Sandbox**: Container lifecycle management, idle watchdog, and orphan cleanup need a coordinator.

A daemon provides all of these with a single process, proper service management (auto-restart on crash), and clean shutdown.

### Why Platform-Native Service Installation

Instead of requiring users to set up systemd units or launchd plists manually, `astonish daemon install` generates the correct service definition for the current platform:

- **Linux**: systemd service unit with auto-restart, journald logging.
- **macOS**: launchd plist with keep-alive.
- **Windows**: Windows service registration.

`astonish daemon start/stop/status` abstract the platform-specific commands.

### Why Hot-Reload for Channels

Channel configuration (Telegram bot token, email IMAP settings, allowlists) can change without restarting the daemon. The daemon watches for config changes and hot-reloads channel adapters, avoiding downtime for the rest of the system.

### Why Three Scheduler Execution Modes

Different use cases need different execution models:

- **Routine**: Runs a predefined flow YAML headlessly. Best for deterministic, repeatable tasks (backups, reports, deployments).
- **Adaptive**: Sends instructions as a chat message to the shared ChatAgent. The agent decides how to handle it, can use any tool, and adapts to the current situation. Best for tasks that need judgment.
- **Fleet poll**: Delegates to the PlanActivator for GitHub Issues monitoring. Not a direct execution mode but a scheduling trigger for fleet activation.

### Why Exponential Backoff on Failures

When a scheduled job fails, the scheduler applies exponential backoff before retrying. This prevents:

- Hammering an unavailable external service every minute.
- Accumulating costs from repeated LLM calls that always fail.
- Flooding logs with identical errors.

## Architecture

### Daemon Startup Sequence

```
astonish daemon start
    |
    v
1. Load configuration (config.yaml)
2. Open credential store (AES-256-GCM encrypted)
3. Run credential migration (plaintext config -> encrypted store)
4. Set provider environment variables from credential store
5. Generate OpenCode managed config (if sandbox enabled)
6. Initialize session store (FileStore)
7. Run device authorization check
8. Initialize memory system (indexer, vector store, embeddings)
9. Create ChatAgent (SystemPromptBuilder, tools, callbacks)
10. Initialize channels (Telegram, email) with hot-reload
11. Initialize scheduler with job registry
12. Initialize fleet PlanActivator with GitHub monitoring
13. Setup sandbox runtime (detect platform, connect to Incus)
14. Prune stale containers from previous runs
15. Start idle watchdog for sandbox containers
16. Start Studio HTTP server
17. Register signal handlers (SIGINT, SIGTERM)
    |
    v
Running: serving API, processing channels, executing schedules
    |
    v
Shutdown signal:
  1. Stop channels
  2. Stop scheduler
  3. Stop fleet sessions
  4. Cleanup sandbox containers (stop, don't destroy)
  5. Close credential store
  6. Close HTTP server
```

### Scheduler Architecture

```
Job Definition (config or API):
  - Name, description
  - Cron expression (standard 5-field or predefined: @hourly, @daily)
  - Mode: routine | adaptive | fleet_poll
  - For routine: flow file path
  - For adaptive: instruction text
  - For fleet_poll: fleet plan reference
    |
    v
Scheduler.Start():
  - Parse cron expressions
  - Create goroutine per job with next-fire calculation
  - Main loop: sleep until next fire, execute, calculate next
    |
    v
Execution:
  - Routine: load flow YAML, run AstonishAgent headlessly
  - Adaptive: send instruction as user message to ChatAgent
  - Fleet poll: trigger PlanActivator.CheckForWork()
    |
    v
Result delivery:
  - Channel-based: results sent to configured channel targets
  - Logging: all results logged to daemon log
  - On failure: exponential backoff for next retry
```

### PID File Management

The daemon writes a PID file on startup and removes it on clean shutdown. This enables:

- `astonish daemon status` to check if the daemon is running.
- Detection of stale daemons (PID file exists but process doesn't).
- Prevention of multiple daemon instances.

### Periodic Cleanup

The daemon runs periodic maintenance:

- **Expired session cleanup**: Removes sessions older than the configured TTL.
- **Orphan container pruning**: Removes sandbox containers whose sessions no longer exist.
- **Interval**: Configurable, default is every few hours.

## Key Files

| File | Purpose |
|---|---|
| `pkg/daemon/daemon.go` | Main daemon Run() orchestration, startup/shutdown sequence |
| `pkg/daemon/service.go` | Platform-native service installation (systemd/launchd/Windows) |
| `pkg/daemon/config_reload.go` | Hot-reload for channel configuration |
| `pkg/scheduler/scheduler.go` | Cron-based job scheduler, execution modes |
| `pkg/scheduler/job.go` | Job definition, cron parsing |
| `pkg/scheduler/executor.go` | Job execution: routine, adaptive, fleet poll |

## Interactions

- **Agent Engine**: The daemon creates and owns the ChatAgent instance. Adaptive scheduler jobs use it.
- **Channels**: Initialized during daemon startup, hot-reloaded on config changes.
- **Fleet**: PlanActivator manages fleet plan lifecycle, GitHub monitoring.
- **Sandbox**: Runtime setup, container pruning, idle watchdog all run within the daemon.
- **API/Studio**: The HTTP server is started as part of the daemon.
- **Sessions**: Session cleanup runs as a periodic daemon task.
- **Credentials**: Credential store is opened at daemon startup, used throughout.
- **Configuration**: Config is loaded once at startup, channel changes trigger hot-reload.
