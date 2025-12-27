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
├── manifest.yaml       # Index of your content
├── flows/              # Your flow YAML files
│   ├── summarizer.yaml
│   └── analyzer.yaml
└── mcp/                # Optional MCP configs
    └── custom-tool.json
```

## Step 1: Create the Repository

Create a new GitHub repository:
- Recommended name: `astonish-flows` (for easy discovery)
- Or any name you prefer

## Step 2: Add Your Flows

Copy your flow files to the repository:

```bash
# Clone your new repo
git clone https://github.com/your-username/astonish-flows.git
cd astonish-flows

# Create directories
mkdir -p flows mcp

# Copy flows from your local Astonish
cp ~/.astonish/agents/summarizer.yaml flows/
cp ~/.astonish/agents/analyzer.yaml flows/
```

## Step 3: Create the Manifest

Create `manifest.yaml` at the repository root:

```yaml
name: your-username-flows
description: A collection of useful AI flows
author: your-username
version: 1.0.0

flows:
  - name: summarizer
    file: flows/summarizer.yaml
    description: Summarizes long text into key points
    tags:
      - text
      - summary
  
  - name: analyzer
    file: flows/analyzer.yaml
    description: Analyzes data and provides insights
    tags:
      - analysis
      - data

# Optional: Include MCP server configurations
mcp_servers:
  - name: custom-tool
    file: mcp/custom-tool.json
    description: Custom MCP server for specific use case
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
version: 1.0.0
repository: https://github.com/you/repo
```

### Flow Entry

```yaml
flows:
  - name: flow-name          # Required
    file: path/to/flow.yaml  # Required
    description: ...         # Recommended
    tags: [tag1, tag2]       # Optional
    version: 1.0.0           # Optional
```

### MCP Server Entry

```yaml
mcp_servers:
  - name: server-name         # Required
    file: path/to/config.json # Required
    description: ...          # Recommended
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
astonish flows run flows/summarizer.yaml
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
astonish flows store update
```

## Next Steps

- **[Manage Taps](/using-the-app/manage-taps/)** — Browse and install taps
- **[Troubleshooting](/using-the-app/troubleshooting/)** — Debug common issues
