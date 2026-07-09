# pkg/tools — AGENTS.md

Built-in tool implementations. Every tool the LLM can call is either here or supplied dynamically by an MCP server (see `pkg/mcp`).

## The contract
A tool implements:

```go
type RunnableTool interface {
    Run(ctx tool.Context, args any) (map[string]any, error)
}

type ToolWithDeclaration interface {
    RunnableTool
    Declaration() *genai.FunctionDeclaration
}
```

- `Run` returns `map[string]any` (string keys) — Studio Chat and the CLI both consume this shape.
- `Declaration` is the JSON schema exposed to the LLM.

## Sandbox wrapping
Tools that touch the filesystem, network, or shell **must** be executable via `pkg/sandbox.Backend`. Do not spawn processes with `os/exec` directly — the sandbox wrapper adapts the same tool implementation to Incus / K8s / OpenShell / Mock.

## Categories
- File / grep / tree (used heavily by the agent for repo understanding).
- Shell (PTY-backed via the sandbox).
- Web fetch / PDF read.
- Memory (semantic search across personal / team / org tiers).
- Browser (delegates to `pkg/browser`).
- Credentials / secrets (delegates to `pkg/credentials`).
- Sub-agent delegation.
- Skill lookup.

Full list: 58+ built-in tools — see the README.

## When editing
1. Adding a new tool? Implement `ToolWithDeclaration`, register it in the appropriate group, and — if it hits shell/network/fs — verify it runs through the sandbox path.
2. Changing a tool's schema? Coordinate with prompt tests and the tools cache (`pkg/cache/tools_cache.go`).
3. Never bypass credentials/secret scanning — sensitive-looking arguments must flow through `pkg/credentials` scanning where applicable.
