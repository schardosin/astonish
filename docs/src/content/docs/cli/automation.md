---
title: Automation
description: Run Astonish flows in scripts, cron jobs, and CI/CD pipelines
sidebar:
  order: 5
---

# Automation

Astonish's single-binary design makes it perfect for automation.

## Shell Scripts

### Basic Script

```bash
#!/bin/bash
# daily_report.sh

# Run the daily report flow
astonish flows run daily_report \
  -p date="$(date +%Y-%m-%d)" \
  >> /var/log/daily_report.log 2>&1

# Check exit code
if [ $? -eq 0 ]; then
  echo "Report generated successfully"
else
  echo "Report generation failed" >&2
  exit 1
fi
```

Make executable:
```bash
chmod +x daily_report.sh
```

### With Error Handling

```bash
#!/bin/bash
set -e  # Exit on error

log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

log "Starting automated analysis..."

if ! astonish flows run analyzer -p input="$1"; then
  log "ERROR: Analysis failed"
  # Send alert, retry, etc.
  exit 1
fi

log "Analysis complete"
```

## Cron Jobs

Schedule flows to run automatically.

### Setup

```bash
# Open crontab editor
crontab -e
```

### Examples

```cron
# Daily report at 9 AM
0 9 * * * /usr/local/bin/astonish flows run daily_report >> /var/log/cron.log 2>&1

# Every 6 hours
0 */6 * * * astonish flows run sync_data -p source="api"

# Monday at 8 AM
0 8 * * 1 astonish flows run weekly_summary

# Every minute (for testing)
* * * * * astonish flows run health_check >> /tmp/health.log 2>&1
```

### Cron Tips

1. **Use absolute paths** for `astonish`
2. **Redirect output** to capture logs
3. **Set environment** if needed

```cron
# With environment
PATH=/usr/local/bin:/usr/bin
OPENROUTER_API_KEY=your-key

0 9 * * * astonish flows run report
```

## CI/CD Integration

### GitHub Actions

```yaml
# .github/workflows/analyze.yml
name: Code Analysis

on:
  pull_request:
    branches: [main]

jobs:
  analyze:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Install Astonish
        run: |
          curl -fsSL https://github.com/schardosin/astonish/releases/latest/download/astonish-linux-amd64 -o astonish
          chmod +x astonish
          sudo mv astonish /usr/local/bin/
      
      - name: Run Analysis
        env:
          OPENROUTER_API_KEY: ${{ secrets.OPENROUTER_API_KEY }}
        run: |
          astonish flows run code_reviewer \
            -p repo="${{ github.repository }}" \
            -p pr="${{ github.event.pull_request.number }}"
```

### GitLab CI

```yaml
# .gitlab-ci.yml
analyze:
  stage: test
  script:
    - curl -fsSL https://github.com/schardosin/astonish/releases/latest/download/astonish-linux-amd64 -o astonish
    - chmod +x astonish
    - ./astonish flows run validator -p branch="$CI_COMMIT_REF_NAME"
  variables:
    OPENROUTER_API_KEY: $OPENROUTER_API_KEY
```

### Jenkins

```groovy
pipeline {
  agent any
  environment {
    OPENROUTER_API_KEY = credentials('openrouter-api-key')
  }
  stages {
    stage('Analyze') {
      steps {
        sh '''
          astonish flows run analyzer \
            -p build="${BUILD_NUMBER}" \
            -p commit="${GIT_COMMIT}"
        '''
      }
    }
  }
}
```

## Docker

### Run in Container

```dockerfile
FROM ubuntu:22.04

# Install Astonish
RUN apt-get update && apt-get install -y curl \
  && curl -fsSL https://github.com/schardosin/astonish/releases/latest/download/astonish-linux-amd64 -o /usr/local/bin/astonish \
  && chmod +x /usr/local/bin/astonish

# Copy flows
COPY flows/ /root/.astonish/agents/

# Default command
CMD ["astonish", "flows", "run", "main"]
```

Build and run:
```bash
docker build -t my-agent .
docker run -e OPENROUTER_API_KEY=$OPENROUTER_API_KEY my-agent
```

## Webhook Triggers

Use Astonish with webhooks:

```bash
#!/bin/bash
# webhook_handler.sh

# Parse incoming webhook JSON
payload=$(cat)
event_type=$(echo "$payload" | jq -r '.event')

case "$event_type" in
  "push")
    astonish flows run on_push -p payload="$payload"
    ;;
  "issue")
    astonish flows run on_issue -p payload="$payload"
    ;;
esac
```

## Monitoring

### Log Output

```bash
# Append to log file
astonish flows run task >> /var/log/astonish.log 2>&1

# With timestamps
astonish flows run task 2>&1 | while read line; do
  echo "[$(date)] $line"
done >> /var/log/astonish.log
```

### Health Checks

```bash
#!/bin/bash
# health_check.sh

if ! astonish --version > /dev/null 2>&1; then
  echo "CRITICAL: Astonish not available"
  exit 2
fi

if ! astonish flows list > /dev/null 2>&1; then
  echo "WARNING: Cannot list flows"
  exit 1
fi

echo "OK: Astonish healthy"
exit 0
```

## Best Practices

1. **Always capture logs** for debugging
2. **Use exit codes** for error handling
3. **Set timeouts** for long-running flows
4. **Secure API keys** with environment variables or secrets managers
5. **Test locally** before scheduling

## Next Steps

- **[Configure Providers](/using-the-app/configure-providers/)** — Set up API keys
- **[Troubleshooting](/using-the-app/troubleshooting/)** — Debug common issues
