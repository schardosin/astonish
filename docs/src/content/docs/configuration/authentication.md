---
title: "Authentication"
description: "Secure Studio access with authentication"
---

Studio has built-in authentication enabled by default when running via the daemon.

## First-Time Setup

When you open Studio for the first time, you will be prompted to create a password. This password protects all subsequent access to the Studio interface.

## Logging In

On subsequent visits, enter your password to log in. Auth sessions last 90 days by default.

## Configuration

```yaml
daemon:
  auth:
    disabled: false          # Set to true to disable auth
    session_ttl_days: 90     # Session duration in days
```

## Disabling Authentication

Set `daemon.auth.disabled: true` in your config file. This is only recommended for trusted networks or local-only access.

## What Is Protected

The auth system protects all Studio endpoints, including:

- Chat API
- Settings
- Fleet management
- All other Studio routes

## Running Without the Daemon

When running `astonish studio` directly (without the daemon), auth behavior depends on the `daemon.auth` section in `config.yaml`. The standalone mode is intended for quick local access — for production use, run Studio through the daemon.
