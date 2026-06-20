# Installation

Astonish ships as a single binary for macOS and Linux. Choose the installation method that fits your environment.

## Homebrew (macOS and Linux)

```bash
brew install schardosin/astonish/astonish
```

Verify the installation:

```bash
astonish --version
```

## Install Script

For quick installation without Homebrew:

```bash
curl -fsSL https://raw.githubusercontent.com/schardosin/astonish/refs/heads/main/install.sh | sh
```

The script detects your OS and architecture, downloads the appropriate binary, and places it in your PATH.

## Build from Source

Requirements:

- Go 1.24.4 or later
- Node.js 18+ (for building the Studio UI)
- Make

```bash
git clone https://github.com/schardosin/astonish.git
cd astonish
make build-all
```

This builds the React frontend and compiles the Go binary with the UI embedded. The resulting binary is at `./astonish`.

To build only the Go binary without the UI:

```bash
make build
```

## Cloud Deployment Prerequisites

If you plan to deploy Astonish with PostgreSQL (multi-tenant, teams), you need:

- **PostgreSQL 15+** with the `pgvector` extension installed
- A database user with permissions to create databases (Astonish creates one database per organization)

Install pgvector on your PostgreSQL instance:

```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

On managed PostgreSQL services (AWS RDS, GCP Cloud SQL, Azure), pgvector is typically available as a supported extension that can be enabled without manual compilation.

## Verify Installation

After installing, confirm everything works:

```bash
astonish --version
astonish setup       # Interactive configuration wizard
```

The setup wizard walks you through AI provider configuration. See [Quick Start: Local](./quick-start-local.md) or [Quick Start: Cloud](./quick-start-cloud.md) for next steps.

::: tip Kubernetes with OpenShell (Recommended for Production)
For production deployments on Kubernetes with secure agent sandboxing, see the [OpenShell Deployment](../deployment/openshell.md) guide. It provides kernel-level isolation, granular policy enforcement, and full audit trails for autonomous agent execution.
:::