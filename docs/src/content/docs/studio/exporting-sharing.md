---
title: Exporting & Sharing
description: Save, export, and share your Astonish flows
sidebar:
  order: 7
---

# Exporting & Sharing

Learn how to save, backup, and share your flows with others.

## Where Flows Are Saved

All flows are automatically saved as YAML files. To find your flows directory:

```bash
astonish config directory
```

This is both your **local** storage and your **source of truth**.

## Viewing the YAML

To see the raw YAML in Studio:

1. Click **View Source** in the top bar
2. A drawer opens showing the full file

![YAML Drawer](/astonish/images/studio-flow_view_source.webp)
*The YAML drawer showing raw flow content*

You can:
- **Copy** the YAML
- **Edit** directly (caution!)
- **Close** to return to canvas

## Copying a Flow

### Method 1: Copy YAML

1. Open the flow in Studio
2. Click **View Source** to view raw content
3. Select all and copy
4. Paste into a new file or share directly

### Method 2: Copy the File

```bash
# Find your flows directory
astonish config directory

# Copy a flow (use the path from above)
cp <flows-directory>/flows/my_flow.yaml ~/Desktop/
```

## Sharing Flows

### Share via File

Send the YAML file directly:
- Email attachment
- Slack/Teams message
- Cloud storage link

Recipients can import flows using `astonish flows import <file>.yaml`.

### Share via Git

Version control your flows by creating a personal or team repository.

### Create a Tap

For sharing collections of flows and MCP servers, create a **Tap** — a GitHub repository with a specific structure:

```
your-repo/
├── flows/           # Your YAML flow files
│   ├── my_flow.yaml
│   └── another_flow.yaml
├── manifest.yaml    # Describes flows and MCP servers
└── README.md
```

**Naming convention:**
- If you name your repo `astonish-flows`, users can tap with just your username: `astonish tap add your-username`
- For other names, users specify the full path: `astonish tap add your-username/repo-name`

**Example `manifest.yaml`:**

```yaml
name: My Astonish Flows
author: your-username
description: Collection of useful AI flows

flows:
  my_flow:
    description: Does something useful
    tags: [utility, ai]

mcps:
  tavily:
    description: Enables real-time web search
    command: npx
    args:
      - -y
      - tavily-mcp@0.1.2
    env:
      TAVILY_API_KEY: ""
    tags: [web-search]
```

See **[Share Your Flows](/using-the-app/share-flows/)** for detailed instructions.

## Importing Flows

### From YAML File

```bash
astonish flows import /path/to/shared_flow.yaml
```

The flow appears immediately in Studio's sidebar.

### From a Tap

```bash
# Add the tap
astonish tap add owner/repo-name

# Install a flow
astonish flows store install owner/flow-name
```

## Backup Strategy

Protect your work:

### Manual Backup

```bash
# Get your flows directory path
astonish config directory

# Backup all flows (use path from above)
cp -r <flows-directory> ~/Dropbox/astonish-backup/

# Or zip them
zip -r flows-backup.zip <flows-directory>
```

### Git Backup

```bash
cd <flows-directory>  # from: astonish config directory
git init
git add .
git commit -m "Backup $(date)"
```

### Sync with Cloud

Link the agents folder to cloud storage:

```bash
# Move to cloud-synced folder
mv <flows-directory> ~/Dropbox/astonish-agents

# Create symlink (paths vary by OS)
ln -s ~/Dropbox/astonish-agents <original-flows-directory>
```

## Next Steps

- **[Share Your Flows](/using-the-app/share-flows/)** — Create your own tap
- **[Manage Tap Repositories](/using-the-app/manage-taps/)** — Browse community flows
