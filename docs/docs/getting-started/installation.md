---
sidebar_position: 1
---

# Installation

This guide will walk you through the process of installing Astonish on your system.

## Prerequisites

Before installing Astonish, ensure you have the following prerequisites:

- **Python**: Version 3.8 or higher
- **pip**: The Python package installer

You can check your Python version by running:

```bash
python --version
```

## Installation Methods

There are two ways to install Astonish:

1. Using pip (recommended)
2. From source code

### Install with pip (Recommended)

The easiest way to install Astonish is using pip:

```bash
pip install astonish
```

This will install Astonish and all its dependencies.

### Install from source code

To install Astonish from source code, follow these steps:

1. Clone the repository:

```bash
git clone https://github.com/schardosin/astonish.git
cd astonish
```

2. Build and install the package:

```bash
make install
```

This command will build the package as a wheel and install it.

3. For development purposes, you can install in editable mode:

```bash
make installdev
```

## Verifying the Installation

To verify that Astonish has been installed correctly, run:

```bash
astonish --version
```

This should display the version information for Astonish.

## Next Steps

After installing Astonish, you should:

1. [Configure Astonish](/docs/getting-started/configuration) by setting up an AI provider
2. Try the [Quick Start Guide](/docs/getting-started/quick-start) to create your first agent

## Troubleshooting

### Common Issues

#### Package Not Found

If you encounter a "Package not found" error when installing with pip, try upgrading pip:

```bash
pip install --upgrade pip
```

#### Permission Errors

If you encounter permission errors when installing, you may need to use `sudo` (on Linux/macOS) or run as administrator (on Windows), or use a virtual environment:

```bash
# Create a virtual environment
python -m venv venv

# Activate the virtual environment
# On Windows:
venv\Scripts\activate
# On Linux/macOS:
source venv/bin/activate

# Install Astonish in the virtual environment
pip install astonish
```

#### Dependency Conflicts

If you encounter dependency conflicts, consider using a virtual environment as described above.

For other issues, please check the [GitHub issues](https://github.com/schardosin/astonish/issues) or create a new issue if your problem is not already reported.
