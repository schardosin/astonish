---
title: Installation
description: How to install Astonish on your system
---

# Installation

Astonish is a single binary with no external dependencies. Choose your preferred installation method.

## Homebrew (macOS/Linux)

The easiest way to install Astonish:

```bash
brew install schardosin/astonish/astonish
```

## cURL Script

Download and install with a single command:

```bash
curl -fsSL https://raw.githubusercontent.com/schardosin/astonish/refs/heads/main/install.sh | sh
```

## Pre-built Binaries

Download pre-built binaries from [GitHub Releases](https://github.com/schardosin/astonish/releases):

1. Go to the releases page
2. Download the binary for your OS/architecture
3. Make it executable: `chmod +x astonish`
4. Move to your PATH: `sudo mv astonish /usr/local/bin/`

## Build from Source

For developers who want to build from source:

```bash
git clone https://github.com/schardosin/astonish.git
cd astonish
make build-all  # Builds UI + Go binary
```

:::note
`go install` is not supported as the web UI must be built separately before embedding into the binary.
:::

## Verify Installation

After installation, verify it works:

```bash
astonish --version
```

You should see the version number printed.

## Next Steps

Now that Astonish is installed:

1. [Launch Astonish Studio](/getting-started/quick-start/) to design your first flow
2. Or configure providers via CLI with `astonish setup`
