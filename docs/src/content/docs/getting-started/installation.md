---
title: Installation
description: Install Astonish on macOS, Linux, or Windows
---

Astonish ships as a single binary with no external dependencies.

## macOS (Homebrew)

```bash
brew install schardosin/tap/astonish
```

## Linux / macOS (curl)

```bash
curl -fsSL https://raw.githubusercontent.com/schardosin/astonish/main/install.sh | bash
```

This downloads the latest release and installs it to your PATH.

## Windows

Download the latest `.exe` from the [GitHub Releases](https://github.com/schardosin/astonish/releases) page and place it in a directory on your PATH.

## Build from Source

Requires **Go 1.24.4+** and **Node.js** (for the web UI).

```bash
git clone https://github.com/schardosin/astonish.git
cd astonish
make build-all
```

This builds the React frontend and compiles the Go binary. The resulting binary will be in the project root.

To build only the Go binary without the web UI:

```bash
make build
```

## Verify Installation

```bash
astonish --version
```

If this prints a version number, you are ready to go. Next step: [Quick Setup](/getting-started/quick-setup/).
