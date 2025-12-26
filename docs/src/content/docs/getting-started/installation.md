---
title: Installation
description: Install Astonish on macOS, Linux, or Windows
sidebar:
  order: 2
---

# Installation

Astonish is distributed as a single binary with no dependencies. Choose your preferred installation method.

## Quick Install (Recommended)

### macOS (Homebrew)

```bash
brew tap schardosin/astonish
brew install astonish
```

### Linux / macOS (Direct Download)

```bash
# Download the latest release for your platform
curl -fsSL https://github.com/schardosin/astonish/releases/latest/download/astonish-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m) -o astonish

# Make it executable
chmod +x astonish

# Move to your PATH
sudo mv astonish /usr/local/bin/
```

### Windows

Download the latest `astonish-windows-amd64.exe` from the [releases page](https://github.com/schardosin/astonish/releases) and add it to your PATH.

## Verify Installation

```bash
astonish --version
```

You should see output like:

```
Astonish v0.x.x
```

## Build from Source

If you prefer to build from source:

```bash
# Clone the repository
git clone https://github.com/schardosin/astonish.git
cd astonish

# Build
go build -o astonish ./cmd/astonish

# Install
sudo mv astonish /usr/local/bin/
```

**Requirements:**
- Go 1.21 or later
- Node.js 18+ (for Studio development only)

## Configuration Directory

Astonish stores its configuration in:

| Platform | Location |
|----------|----------|
| macOS | `~/Library/Application Support/astonish/` |
| Linux | `~/.config/astonish/` |
| Windows | `%APPDATA%\astonish\` |

You can verify this with:

```bash
astonish config directory
```

## Next Steps

Now that Astonish is installed:

1. **[Choose Your Path](/getting-started/choose-your-path/)** — Decide how you want to work
2. **[Studio Quickstart](/getting-started/quickstart/studio/)** — Visual approach
3. **[CLI Quickstart](/getting-started/quickstart/cli/)** — Command-line approach
