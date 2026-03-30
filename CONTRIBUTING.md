# Contributing to Astonish

Thank you for your interest in contributing to Astonish! This document provides the information you need to get started.

## Development Setup

### Prerequisites

- **Go 1.25+** (toolchain 1.26+)
- **Node.js 24+** with npm
- **Make** (for build automation)
- **golangci-lint** (for Go linting)

### Getting Started

```bash
# Clone the repository
git clone https://github.com/schardosin/astonish.git
cd astonish

# Build everything (UI + Go binary)
make build-all

# Or build components separately:
make build-ui   # React frontend only
make build       # Go binary only
```

### Running Locally

```bash
# Run the CLI
go run .

# Run Studio (production mode — serves built UI)
make studio

# Run Studio (dev mode — live UI reload on http://localhost:5173)
make studio-dev
```

## Project Structure

See [AGENTS.md](AGENTS.md) for a detailed breakdown. Key directories:

| Directory | Description |
|-----------|-------------|
| `cmd/astonish/` | CLI entry points |
| `pkg/agent/` | Agent execution logic |
| `pkg/api/` | HTTP handlers (REST API) |
| `pkg/config/` | YAML config loading |
| `pkg/launcher/` | Console/Studio launchers |
| `pkg/mcp/` | MCP server management |
| `pkg/provider/` | AI provider integrations |
| `pkg/tools/` | Built-in tools |
| `pkg/ui/` | TUI components |
| `web/src/` | React frontend |

## Testing

```bash
# Run all Go tests
go test ./...

# Run a specific package
go test ./pkg/tools

# Run a specific test
go test ./pkg/tools -run TestFileTree

# Verbose output
go test -v ./pkg/tools -run TestFileTree

# With race detector
go test -race ./pkg/tools

# Lint the Go code
golangci-lint run

# Lint the React frontend
cd web && npm run lint
```

### Testing Guidelines

- Write tests for non-trivial functions
- Focus on core business logic (agent execution, tools, config)
- Use table-driven tests where appropriate
- Test files: `*_test.go` in the same package
- Use `os.MkdirTemp` with `t.Cleanup` for temporary files

## Code Style

### Go

- **Imports**: stdlib, then external, then internal — separated by blank lines
- **Naming**: `PascalCase` exports, `camelCase` private, lowercase packages
- **Tags**: `yaml` and `json` with `omitempty` for optional fields
- **Errors**: return as last value, check immediately, wrap with `fmt.Errorf`
- **Comments**: avoid unless complex or non-obvious
- **Linting**: `golangci-lint run` (runs automatically via pre-commit hook)

### React/JSX

- Functional components with hooks, one per file
- Tailwind CSS v4 for styling
- No TypeScript — JSX only
- ESLint config in `web/eslint.config.js`

## Pull Request Process

1. Create a feature branch from `main`
2. Make your changes with clear, focused commits
3. Ensure all tests pass: `go test ./...`
4. Ensure lint is clean: `golangci-lint run` and `cd web && npm run lint`
5. Write a descriptive PR title and summary
6. Request review

### Before Submitting

```bash
# Full validation
make build-all
go test ./...
golangci-lint run
cd web && npm run lint
```

## Adding New Components

### New Tool

1. Create a file in `pkg/tools/` (e.g., `my_tool.go`)
2. Implement `Run(ctx tool.Context, args any) (map[string]any, error)`
3. Implement `Declaration() *genai.FunctionDeclaration` for the function schema
4. Register the tool in the agent setup
5. Write tests in `my_tool_test.go`

### New Provider

1. Create a package in `pkg/provider/` (e.g., `pkg/provider/mycloud/`)
2. Implement the `model.LLM` interface
3. Register in `pkg/provider/registry.go`
4. Add config support in `pkg/config/`

### New Channel

1. Create a package in `pkg/channels/` (e.g., `pkg/channels/slack/`)
2. Implement message handling and the channel interface
3. Add config support and register in the channel manager

## Reporting Issues

Please open an issue on GitHub with:
- A clear description of the problem or feature request
- Steps to reproduce (for bugs)
- Expected vs actual behavior
- Go version, OS, and relevant config (with secrets redacted)
