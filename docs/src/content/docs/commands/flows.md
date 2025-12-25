---
title: astonish flows
description: Manage and share flows from the store
---

# astonish flows

Manage agent flows from the Flow Store.

## Commands

### flows store list

List available flows:

```bash
astonish flows store list
```

Shows flows from the official store and all configured taps.

### flows store install

Install a flow:

```bash
astonish flows store install <flow_name>
```

#### Examples

```bash
# Install from official store
astonish flows store install github_pr_description_generator

# Install from a specific tap
astonish flows store install mytap/custom_flow
```

### flows run

Run an installed flow:

```bash
astonish flows run <flow_name> [flags]
```

This is equivalent to `astonish agents run`.

## Examples

### Browse and Install

```bash
# See what's available
astonish flows store list

# Install one
astonish flows store install code_reviewer

# Run it
astonish flows run code_reviewer
```

### Share Your Flows

To share your flows, create a tap repository. See [Flow Store & Taps](/concepts/taps/) for details.
