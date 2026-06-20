# Daemon & Scheduler

The daemon runs background services (channel listeners, scheduled flows), and the scheduler manages recurring tasks.

## Daemon

The daemon is a long-running background process that powers channels, scheduled flows, and other services that need to operate without an active terminal session.

### Commands

```bash
# Install daemon as a system service
astonish daemon install

# Start the daemon
astonish daemon start

# Stop the daemon
astonish daemon stop

# Check daemon status
astonish daemon status

# View daemon logs
astonish daemon logs

# View logs and follow new entries
astonish daemon logs --follow
```

### Status Output

```
Daemon: running (PID 12345)
Uptime: 3d 7h 22m
Channels:
  telegram: connected (polling)
  email: connected (last poll: 12s ago)
  slack: connected (socket mode)
Scheduler: 4 active jobs
```

### Configuration

The daemon reads its configuration from the standard Astonish config file. Restart the daemon after config changes:

```bash
astonish daemon stop && astonish daemon start
```

## Scheduler

The scheduler executes flows on a cron schedule. It runs within the daemon process.

### Commands

```bash
# Add a scheduled flow
astonish scheduler add --flow daily-report --cron "0 9 * * *"

# Add with a name
astonish scheduler add --name "Morning Report" --flow daily-report --cron "0 9 * * *"

# List scheduled jobs
astonish scheduler list

# Remove a scheduled job
astonish scheduler remove <job-id>

# Remove by name
astonish scheduler remove --name "Morning Report"
```

### Cron Syntax

Standard five-field cron expressions are supported:

```
┌───────── minute (0-59)
│ ┌─────── hour (0-23)
│ │ ┌───── day of month (1-31)
│ │ │ ┌─── month (1-12)
│ │ │ │ ┌─ day of week (0-6, Sun=0)
│ │ │ │ │
* * * * *
```

### Examples

```bash
# Every weekday at 9am
astonish scheduler add --flow standup --cron "0 9 * * 1-5"

# Every hour
astonish scheduler add --flow health-check --cron "0 * * * *"

# First day of each month
astonish scheduler add --flow monthly-report --cron "0 0 1 * *"
```
