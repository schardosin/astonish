---
title: Share Your Flows
description: Create a tap repository to share your flows with others
sidebar:
  order: 4
---

# Share Your Flows

Create a tap repository to share your flows and MCP configurations with your team or the community.

## What You'll Create

A tap is a GitHub repository containing:
```
your-flows/
├── manifest.yaml       # Index of flows and MCP servers
├── README.md           # Documentation
└── flows/              # All flow YAML files
    ├── summarizer.yaml
    └── analyzer.yaml
```

The `manifest.yaml` lists your flows and defines MCP servers inline.

## Step 1: Create the Repository

Create a new GitHub repository:
- Recommended name: `astonish-flows` (for easy discovery)
- Or any name you prefer

## Step 2: Add Your Flows

Create a `flows` folder and add your flow files:

```bash
# Clone your new repo
git clone https://github.com/your-username/astonish-flows.git
cd astonish-flows

# Create flows directory
mkdir flows

# Copy flows from your local Astonish
cp ~/.config/astonish/flows/summarizer.yaml flows/
cp ~/.config/astonish/flows/analyzer.yaml flows/
```

## Step 3: Create the Manifest

Create `manifest.yaml` at the repository root:

```yaml
name: your-username Astonish Flows
author: your-username
description: A collection of useful AI flows

flows:
  summarizer:
    description: Summarizes long text into key points
    tags: [text, summary]
  
  analyzer:
    description: Analyzes data and provides insights
    tags: [analysis, data]

# Optional: Include MCP server configurations
mcps:
  tavily:
    description: Enables real-time web search
    command: npx
    args:
      - -y
      - tavily-mcp@0.1.2
    env:
      TAVILY_API_KEY: ""
    tags: [web-search, web-extraction]

  python-sandbox:
    description: Python Sandbox for code execution
    command: uvx
    args:
      - mcp-run-python
      - stdio
    tags: [python, sandbox]
```

## Step 4: Push and Share

```bash
git add .
git commit -m "Add initial flows"
git push
```

Now others can add your tap:

```bash
astonish tap add your-username/astonish-flows
```

Or with a shorter name:

```bash
astonish tap add your-username
# Uses your-username/astonish-flows automatically
```

## Manifest Reference

### Required Fields

```yaml
name: tap-name
description: What this tap provides
```

### Optional Fields

```yaml
author: your-name
```

### Flow Entry

Flows are defined as a map, where the key is the filename (without .yaml):

```yaml
flows:
  flow-name:                  # File: flows/flow-name.yaml
    description: ...          # Recommended
    tags: [tag1, tag2]        # Optional
```

The flow YAML file must be in the `flows/` folder:
```
flows/
└── flow-name.yaml
```

### MCP Server Entry

MCP servers are defined inline in the manifest:

```yaml
mcps:
  server-name:               # Server identifier
    description: ...         # Recommended
    command: npx             # npx, uvx, docker, etc.
    args:                    # Command arguments
      - -y
      - package-name
    env:                     # Environment variables
      API_KEY: ""
    tags: [tag1, tag2]       # Optional
    transport: stdio         # stdio or sse
```

## Best Practices

### Documentation

Add a README.md:

```markdown
# My Astonish Flows

A collection of AI flows for [use case].

## Installation

\`\`\`bash
astonish tap add your-username
astonish flows store install your-username/summarizer
\`\`\`

## Flows

### summarizer
Summarizes long text into key points.

### analyzer
Analyzes data for insights.
```

### Versioning

Tag releases for stability:

```bash
git tag v1.0.0
git push --tags
```

### Testing

Test your flows before sharing:

```bash
# Run locally
astonish flows run summarizer/summarizer.yaml
```

## Private Taps

For internal team use:

1. Use a private GitHub repository
2. Ensure team has access
3. Use personal access tokens:

```bash
export GITHUB_TOKEN="ghp_xxx"
astonish tap add your-org/internal-flows
```

## Updating Your Tap

When you update flows:

1. Commit and push changes
2. Users update with:

```bash
astonish tap update
```

## Next Steps

- **[Manage Taps](/astonish/using-the-app/manage-taps/)** — Browse and install taps
- **[Troubleshooting](/astonish/using-the-app/troubleshooting/)** — Debug common issues
