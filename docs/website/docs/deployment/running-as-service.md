# Running as a Service

Astonish runs as a background daemon managed by your operating system's service manager. The daemon serves Studio at `http://localhost:9393`, handles scheduled flows, channel integrations, and persistent sessions. It starts automatically on boot and restarts on failure.

## Installation

The `daemon install` command registers Astonish with the appropriate service manager for your OS:

```bash
astonish daemon install
```

This creates:
- **macOS**: A launchd plist at `~/Library/LaunchAgents/dev.astonish.daemon.plist`
- **Linux**: A systemd user unit at `~/.config/systemd/user/astonish.service`

## Starting and Stopping

```bash
# Start the daemon
astonish daemon start

# Stop the daemon
astonish daemon stop

# Restart the daemon
astonish daemon restart

# Check status
astonish daemon status
```

## Auto-Start on Boot

The daemon is configured to start automatically on login by default. To disable auto-start:

```bash
astonish daemon install --no-autostart
```

### macOS (launchd)

The plist includes `RunAtLoad: true`. To manually control:

```bash
# Disable auto-start
launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/dev.astonish.daemon.plist

# Re-enable
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/dev.astonish.daemon.plist
```

### Linux (systemd)

The unit is enabled via `systemctl --user`. To manually control:

```bash
# Disable auto-start
systemctl --user disable astonish

# Re-enable
systemctl --user enable astonish
```

## Viewing Logs

```bash
# Tail live logs
astonish daemon logs

# macOS: view via system log
log show --predicate 'subsystem == "dev.astonish.daemon"' --last 1h

# Linux: view via journalctl
journalctl --user -u astonish -f
```

Log files are also written to `~/.local/share/astonish/logs/`.

## Uninstalling

Remove the daemon registration:

```bash
astonish daemon uninstall
```

This stops the service and removes the plist or systemd unit file.

## See Also

- [Deployment Overview](./index.md) — choosing between local and cloud deployment
- [Kubernetes Deployment](./kubernetes.md) — cloud deployments use Kubernetes instead of the daemon
