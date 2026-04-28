# AGENTS.md - Agent Coding Guidelines

This document provides guidance for agentic coding systems working on the Astonish codebase.

## Project Overview

Astonish is a Go-based AI agent framework with a React UI. It uses Google's Agent Development Kit (ADK) and provides both CLI and visual Studio interfaces.

**Stack:**
- Backend: Go 1.24.4, ADK (Google), bubbletea for TUI
- Frontend: React 19.2, Vite 7.2, Tailwind CSS 4.1, JSX (no TypeScript)
- Build System: Makefile

## Build / Lint / Test Commands

### Go Backend
```bash
# Build everything (UI + Go binary)
make build-all

# Build Go binary only
make build

# Build React UI only
make build-ui

# Run Go application
go run .

# Run Astonish Studio
make studio              # Production mode (serves built UI)
make studio-dev          # Dev mode (live UI reload on http://localhost:5173)

# Run all tests
go test ./...
make test

# Run single test
go test ./pkg/tools -run TestFileTree
go test -v ./pkg/tools -run TestFileTree  # Verbose

# Lint
golangci-lint run        # Full lint check
```

### React Frontend (in `web/` directory)
```bash
cd web

# Development server with hot reload
npm run dev              # http://localhost:5173

# Build for production
npm run build

# Lint
npm run lint
```

### Quick Reference
- Single test: `go test ./pkg/path -run TestFunctionName`
- Verbose test: `go test -v ./pkg/path -run TestFunctionName`
- Run specific package: `go test ./pkg/path`
- Run with race detector: `go test -race ./pkg/path`

## Go Code Style

- **Imports**: stdlib → external → internal, with blank lines between groups
- **Naming**: `PascalCase` for exports, `camelCase` for private, lowercase packages
- **Tags**: `yaml` and `json` with `omitempty` for optional fields
- **Errors**: return as last value, check immediately, wrap with `fmt.Errorf` when needed
- **Interfaces**: minimal, defined near use (e.g., `RunnableTool`, `ToolWithDeclaration`)
- **Testing**: `*_test.go` same package, table-driven tests, `os.MkdirTemp` with cleanup
- **Linting**: pre-commit runs `golangci-lint`, focuses on bugs not style
- **Comments**: avoid unless complex/non-obvious

## React/JSX Code Style

- **Components**: Functional with hooks, single per file, `export default`
- **Imports**: External first, local second, named exports preferred
- **Styling**: Tailwind CSS v4 with `var(--variable-name)` for theming
- **State**: React hooks only, no external state libraries, props drilling OK
- **Handlers**: CamelCase, prevent default on forms, cleanup in useEffect
- **Linting**: ESLint in `web/eslint.config.js`, `varsIgnorePattern: '^[A-Z_]'` for components
- **No TypeScript**: JSX only, runtime PropTypes if needed

## File Structure

```
astonish/
├── cmd/astonish/          # CLI entry points
├── pkg/
│   ├── agent/             # Agent execution logic
│   ├── api/               # HTTP handlers (REST API)
│   ├── config/            # YAML config loading
│   ├── launcher/          # Console/Studio launchers
│   ├── mcp/               # MCP server management
│   ├── provider/          # AI provider integrations (OpenAI, Anthropic, etc.)
│   ├── tools/             # Built-in tools (grep, file tree, git diff)
│   └── ui/                # TUI components
├── web/
│   ├── src/
│   │   ├── components/    # React components
│   │   └── api/           # API client functions
│   └── package.json
├── Makefile
├── go.mod
└── main.go
```

## Key Patterns

### Config Loading (Go)
- Config files in `~/.config/astonish/`
- YAML-based with `gopkg.in/yaml.v3`
- Use `LoadAgent()` for flow configs, `LoadAppConfig()` for app settings

### Tool Implementation (Go)
- Tools implement `Run(ctx tool.Context, args any) (map[string]any, error)`
- Declare function schema with `Declaration() *genai.FunctionDeclaration`
- Return maps with string keys and `any` values

### API Handlers (Go)
- Pattern: `func HandlerName(w http.ResponseWriter, r *http.Request)`
- Set `Content-Type: application/json`
- Use `json.NewEncoder(w).Encode(response)`
- Error responses: `http.Error(w, "message", statusCode)`

### MCP Integration
- MCP servers defined in config file
- Tools cached via `pkg/cache/tools_cache.go`
- Use `GetCachedTools()` to retrieve available tools

## Testing Guidelines

- Write tests for non-trivial functions
- Focus on core business logic (agent execution, tools, config)
- Mock external dependencies (API calls, file system)
- Test file names: `*_test.go`
- Run tests before committing (pre-commit hook only lints, doesn't run tests)
- See `docs/architecture/testing-chat-scenarios.md` for the Studio Chat test infrastructure (SSE scenario tests, backend integration tests, prompt contract tests)

## Architecture Documentation

- `docs/architecture/chat-rendering-pipeline.md` — **Authoritative reference** for the Studio Chat rendering pipeline: SSE transport, event types, message-to-component mapping, report/app/artifact pipelines, export pipeline, rules and invariants. **Read this before modifying StudioChat.tsx or adding new SSE event types.**
- `docs/architecture/testing-chat-scenarios.md` — Test infrastructure, scenario catalog, fixture authoring guidelines
- `docs/architecture/generative-ui.md` — App preview (Generative UI) pipeline
- `docs/architecture/api-studio.md` — REST API and SSE streaming documentation

## Before Committing

Always run linting:
```bash
# Go linting (automatic via pre-commit hook)
make build-all

# Manual Go lint
golangci-lint run

# Web linting
cd web && npm run lint
```

Skip lint check only when absolutely necessary:
```bash
git commit --no-verify
```
