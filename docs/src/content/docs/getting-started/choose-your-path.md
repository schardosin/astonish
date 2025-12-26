---
title: Choose Your Path
description: Visual learner or CLI power user? Pick your adventure.
sidebar:
  order: 3
---

# Choose Your Path

Astonish offers two equally powerful ways to build AI agents. Both use the same underlying YAML format, so you can switch between them anytime.

## 🎨 Visual Learner → Astonish Studio

**Best for:**
- First-time users exploring the concepts
- Rapid prototyping and experimentation
- Seeing how nodes connect visually
- Teams who prefer low-code tools

**What you'll do:**
1. Launch Studio with `astonish studio`
2. Drag and drop nodes on a canvas
3. Configure nodes through forms
4. Run and test in real-time
5. Save as YAML automatically

![Astonish Studio Interface](/src/assets/placeholder.png)
*The visual flow editor*

**[→ Start with Studio Quickstart](/getting-started/quickstart/studio/)**

---

## ⌨️ CLI Power User → Terminal

**Best for:**
- Developers comfortable with YAML
- Automation and CI/CD pipelines
- Version-controlled workflows
- Headless/server environments

**What you'll do:**
1. Create YAML files directly
2. Run flows with `astonish flows run`
3. Pass parameters via command line
4. Integrate with cron, scripts, CI/CD

```bash
# Run a flow with parameters
astonish flows run my_agent -p input="Hello world"

# Schedule with cron
0 9 * * * astonish flows run daily_report
```

**[→ Start with CLI Quickstart](/getting-started/quickstart/cli/)**

---

## The Best of Both Worlds

You don't have to choose just one:

1. **Start visually** — Use Studio to design and understand flows
2. **Switch to CLI** — Run the same YAML in production scripts
3. **Edit YAML directly** — Fine-tune details when needed
4. **Back to Studio** — Visualize and debug complex flows

The YAML is always the source of truth. Studio reads and writes the same files you'd edit by hand.

```yaml
# This file works in both Studio and CLI
name: hybrid-workflow
nodes:
  - name: process
    type: llm
    prompt: "Process {input}"
flow:
  - from: START
    to: process
  - from: process
    to: END
```

## Quick Comparison

| Feature | Studio | CLI |
|---------|--------|-----|
| Learning curve | Gentle | Steeper |
| Visual debugging | ✅ Yes | Limited |
| Automation friendly | Limited | ✅ Yes |
| CI/CD integration | ❌ No | ✅ Yes |
| Same YAML format | ✅ Yes | ✅ Yes |

## Ready?

Pick your adventure:

- **[Studio Quickstart](/getting-started/quickstart/studio/)** — 5-minute visual walkthrough
- **[CLI Quickstart](/getting-started/quickstart/cli/)** — 5-minute command-line guide
