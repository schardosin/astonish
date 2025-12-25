---
title: Introduction
description: Learn what Astonish is and how it can help you build production AI agents 
---

# Welcome to Astonish

**Astonish** helps you build production AI agents in minutes, not months. Design visually, run anywhere—no servers required.

## Our Vision

**Agent flows should be designed, not coded.**

We believe the future of AI automation is declarative. You should focus on *what* your agent does—the business logic, the steps, the outcomes—not *how* to wire up providers, handle errors, or manage retries.

| You Focus On | Astonish Handles |
|-------------|------------------|
| Designing the flow | Provider connections & authentication |
| Choosing which tools to use | Error detection & intelligent retries |
| Defining success criteria | State management across steps |
| Business logic | Parallel execution & performance |

## Key Features

### 🎯 Single Binary, Zero Infrastructure

No web servers. No cloud subscriptions. Astonish is a single executable that runs anywhere—your laptop, a Raspberry Pi, in a container, or a CI/CD pipeline.

```bash
# Add it to your cron
0 9 * * * /usr/local/bin/astonish agents run daily_report >> /var/log/report.log

# Run in any script
./astonish agents run code_reviewer -p repo="./my-project"
```

### 📄 YAML as Source of Truth

Your agent logic lives in simple YAML files. Version control them. Review them in PRs. Move them between environments. No platform lock-in.

```yaml
# This IS your agent. Copy it, share it, version it.
nodes:
  - name: analyze
    type: llm
    prompt: "Analyze {input}"
flow:
  - from: START
    to: analyze
```

### 🖥️ Design Visually, Run Anywhere

Use **Astonish Studio** to design flows visually, then run the exact same YAML from the command line. No "export" step. No format conversion.

## What's Next?

- [Install Astonish](/getting-started/installation/) to get started
- [Quick Start](/getting-started/quick-start/) to build your first agent
- [Concepts](/concepts/flows/) to understand how flows work
