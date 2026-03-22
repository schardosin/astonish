---
title: Troubleshooting
description: Common issues and solutions
sidebar:
  order: 3
---

Common issues and how to resolve them.

---

## Installation

**"Command not found" after install**

Ensure the install directory is in your `PATH`. For Homebrew: `/opt/homebrew/bin`. For the install script: `~/.local/bin` or `/usr/local/bin`.

**Version mismatch**

Run `astonish --version` to verify. Update with `brew upgrade astonish` or re-run the install script.

---

## Provider and Model Issues

**"API key invalid"**

Run `astonish setup` to reconfigure. API keys are stored in the credential store -- check with `astonish credential list`.

**Model not responding**

Verify provider connectivity. Try a different model with `astonish chat -m <model>`.

**"Context length exceeded"**

The conversation is too long. Use `/compact` to compress history, or start a new session with `/new`.

---

## Chat

**Session not resuming**

Check the session ID with `astonish sessions list`. Use prefix matching -- you don't need the full ID.

**Tools not being approved**

Check if `auto_approve` is set in config or use the `--auto-approve` flag.

**Slow responses**

Try a faster provider/model. Check `astonish daemon logs` for errors.

---

## Memory

**"Memory not finding relevant results"**

Check `astonish memory status` to verify indexing. Run `astonish memory reindex` to rebuild the index. Verify the similarity threshold in config (`memory.search.min_score`).

**"Memory empty"**

Check `astonish memory list` for indexed files. Verify the `memory.memory_dir` path in config.

---

## Browser

**"Browser not launching"**

Ensure Chrome/Chromium is available. Astonish auto-downloads via Rod, but firewalls may block this. Set `browser.chrome_path` to a local binary.

**"Page loads but snapshot is empty"**

The page may be JavaScript-heavy. Increase `browser.navigation_timeout`. Try `browser_wait_for` with `state: "networkidle"` before taking a snapshot.

**"Stealth detection"**

Use `astonish config browser` to configure CloakBrowser or a remote anti-detect browser via `browser.remote_cdp_url`.

---

## Daemon

**"Daemon won't start"**

Check `astonish daemon logs` for errors. Verify the port is not in use. Try `astonish daemon run` for foreground debugging.

**"Studio not accessible"**

Verify the daemon is running (`astonish daemon status`). Check the port (default: 9393). Ensure the firewall allows the port.

---

## Channels

**"Telegram bot not responding"**

Verify `channels.enabled: true` and `telegram.enabled: true`. Check that the bot token is valid. Verify your user ID is in `allow_from`. Check daemon logs.

**"Email channel not processing"**

Verify IMAP/SMTP config. For Gmail, use an App Password. Check the `allow_from` list. Verify the daemon is running.

---

## Fleet

**"Fleet session stalled"**

Check the session trace in Studio for stuck agents. Agents may be waiting for a response from an unreachable target. Check credentials and external service access.

---

## General

| Task | Command |
|------|---------|
| Config location | `astonish config directory` |
| View full config | `astonish config show` |
| Debug mode | `astonish chat --debug` |
| Live log following | `astonish daemon logs -f` |
