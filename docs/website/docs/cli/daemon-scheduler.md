# Daemon & Scheduler

The daemon runs background services (Studio UI, channel listeners, scheduled flows), and the scheduler manages recurring tasks.

## Daemon

The daemon is a long-running background process that serves Studio at `http://localhost:9393` and powers channels, scheduled flows, and other services.

### Commands

```bash
# Install daemon as a system service
astonish daemon install [--port 9393]

# Uninstall the system service
astonish daemon uninstall

# Start the daemon
astonish daemon start

# Stop the daemon
astonish daemon stop

# Restart the daemon
astonish daemon restart

# Check daemon status
astonish daemon status

# Run in foreground (for debugging)
astonish daemon run [--port 9393]

# View daemon logs
astonish daemon logs [-f] [-n 50]
```

### Log Flags

| Flag | Description |
|------|-------------|
| `-f` | Follow log output (tail) |
| `-n` | Number of lines to show (default: 50) |

## Scheduler

The scheduler executes flows on recurring schedules. It runs within the daemon process.

::: info
Jobs are created through chat — ask the AI to schedule a task and it will create the job for you. The CLI is for managing existing jobs.
:::

### Commands

```bash
# List all scheduled jobs
astonish scheduler list

# Show scheduler status
astonish scheduler status

# Enable a disabled job
astonish scheduler enable <name>

# Disable a job (pauses execution)
astonish scheduler disable <name>

# Trigger immediate execution of a job
astonish scheduler run <name>

# Remove a scheduled job
astonish scheduler remove <name>
```

The `<name>` argument can be a full job ID, a partial ID prefix (must be unambiguous), or a job name (case-insensitive).

### Aliases

- `scheduler list` → `scheduler ls`
- `scheduler remove` → `scheduler rm`

### Examples

```bash
# List all jobs
astonish scheduler ls

# Trigger a job manually
astonish scheduler run daily-report

# Disable a job temporarily
astonish scheduler disable health-check

# Re-enable it
astonish scheduler enable health-check

# Remove a job permanently
astonish scheduler rm old-job
```
