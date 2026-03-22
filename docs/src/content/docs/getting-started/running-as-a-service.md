---
title: Running as a Service
description: Install Astonish as an always-on background service
---

Running Astonish as a background service is the standard way to operate it. The daemon is the foundation that powers all Astonish interfaces — it serves the Studio web UI, listens on communication channels (Telegram, Email), runs scheduled tasks, and manages fleet sessions.

## Install and Start the Service

```bash
astonish daemon install
astonish daemon start
```

This creates a **launchd** service on macOS or a **systemd** service on Linux, then starts it immediately. The service is configured to start automatically on boot.

## Management Commands

```bash
astonish daemon start      # Start the service
astonish daemon stop       # Stop the service
astonish daemon restart    # Restart the service
astonish daemon status     # Check if the service is running
astonish daemon uninstall  # Remove the service entirely
```

## Viewing Logs

```bash
astonish daemon logs          # Print recent logs
astonish daemon logs -f       # Follow logs in real time
astonish daemon logs -n 100   # Show the last 100 lines
```

## Studio Access

When the daemon is running, Studio is available at [http://localhost:9393](http://localhost:9393) by default. To use a different port:

```bash
astonish daemon install --port 8080
astonish daemon start
```

### Authentication

Studio has built-in authentication enabled by default when running as a daemon. This protects access to your sessions, credentials, and tools. If you are running on a trusted local network and prefer to skip auth, it can be disabled in the configuration file.

## What the Daemon Powers

- **Studio** — The web UI at `http://localhost:9393` for chat, flow design, fleet management, and settings.
- **CLI Chat** — `astonish chat` connects to the daemon for session management and tool execution.
- **Scheduling** — Define recurring tasks with `astonish scheduler`. The daemon executes them on time.
- **Channels** — Connect Telegram and Email with `astonish channels`. The daemon listens for incoming messages and responds.
- **Fleet sessions** — Run multi-agent teams that coordinate on complex tasks.

## Running in Foreground

For debugging, you can run the daemon process in the foreground instead of as a system service:

```bash
astonish daemon run
```

This runs the same daemon code but keeps it attached to your terminal, making it easier to see output and interrupt with Ctrl+C.
