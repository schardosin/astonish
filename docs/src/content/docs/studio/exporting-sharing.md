---
title: Exporting & Sharing
description: Save, export, and share your Astonish flows
sidebar:
  order: 7
---

# Exporting & Sharing

Learn how to save, backup, and share your flows with others.

## Where Flows Are Saved

All flows are stored as YAML files:

```
~/.astonish/agents/<flow-name>.yaml
```

This is both your **local** storage and your **source of truth**.

## Viewing the YAML

To see the raw YAML in Studio:

1. Click **YAML** in the top bar
2. A drawer opens showing the full file

![YAML Drawer](/astonish/images/placeholder.png)
*The YAML drawer showing raw flow content*

You can:
- **Copy** the YAML
- **Edit** directly (caution!)
- **Close** to return to canvas

## Copying a Flow

### Method 1: Copy YAML

1. Open the flow in Studio
2. Click **YAML** to view raw content
3. Select all and copy
4. Paste into a new file or share directly

### Method 2: Copy the File

```bash
# Find your flows
ls ~/.astonish/agents/

# Copy a flow
cp ~/.astonish/agents/my_flow.yaml ~/Desktop/
```

## Sharing Flows

### Share via File

Send the YAML file directly:
- Email attachment
- Slack/Teams message
- Cloud storage link

Recipients can save to their `~/.astonish/agents/` folder.

### Share via Git

Version control your flows:

```bash
# Create a flows repository
mkdir my-flows && cd my-flows
git init

# Copy your flows
cp ~/.astonish/agents/*.yaml .

# Commit and push
git add .
git commit -m "Add my flows"
git push origin main
```

### Create a Tap

For sharing collections of flows, create a **Tap**:

1. Create a GitHub repo with `astonish-flows` suffix
2. Add your YAML files
3. Add a `manifest.yaml`
4. Others can add your tap with `astonish tap add`

See **[Share Your Flows](/using-the-app/share-flows/)** for detailed instructions.

## Importing Flows

### From YAML File

```bash
# Copy to flows directory
cp /path/to/shared_flow.yaml ~/.astonish/agents/
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
# Backup all flows
cp -r ~/.astonish/agents ~/Dropbox/astonish-backup/

# Or zip them
zip -r flows-backup.zip ~/.astonish/agents/
```

### Git Backup

```bash
cd ~/.astonish/agents
git init
git add .
git commit -m "Backup $(date)"
```

### Sync with Cloud

Link the agents folder to cloud storage:

```bash
# Move to cloud-synced folder
mv ~/.astonish/agents ~/Dropbox/astonish-agents

# Create symlink
ln -s ~/Dropbox/astonish-agents ~/.astonish/agents
```

## Next Steps

- **[Share Your Flows](/using-the-app/share-flows/)** — Create your own tap
- **[Manage Tap Repositories](/using-the-app/manage-taps/)** — Browse community flows
