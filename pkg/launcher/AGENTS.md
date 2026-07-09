# pkg/launcher — AGENTS.md

Launcher wires runtime components together and exposes two entry surfaces: the interactive CLI console and the Studio HTTP server.

## Scope
- `chat_console.go:RunChatConsole` — interactive CLI loop (bubbletea UI, ADK runner, SSE-style streaming).
- `chat_factory.go:NewWiredChatAgent` — the single place where a fully wired `ChatAgent` is built (LLM, tools, sandbox, memory, tool index, prompt builder, session service, cleanup).
- `studio.go:NewStudioServer` — HTTP server + SPA serving. Registers `/api/*` (via `pkg/api.RegisterRoutes`), platform auth, tenant middleware, CSP, rate-limit; serves the embedded SPA from `web/embed.go` (falls back to `web/dist` on disk when present).
- `web_simple.go:RunSimpleWeb` — minimal dev-only chat web server.

## Key rules
1. **`NewWiredChatAgent` is the wiring choke point.** If you need a new dependency in the agent, add it here rather than piping it through every caller.
2. **Studio SPA assets**: `getWebAssets()` prefers `web/dist` on disk (dev) and falls back to the embedded FS built via `web/embed.go`. Do not add a third code path.
3. **Middleware order in `NewStudioServer`** matters: platform auth → tenant → rate-limit → CSP → SPA/API split. Preserve this order when adding middleware.
4. **`/api/*` vs. SPA**: everything under `/api/` is routed to the API mux; every other path serves the SPA `index.html`. Do not intercept SPA routes at this layer.

## Entry-point relationship
- CLI: `astonish chat` → `cmd/astonish/chat.go:handleChatCommand` → `RunChatConsole` → `NewWiredChatAgent` → `runner.Run`.
- Studio: `astonish daemon run` → `pkg/daemon.Run` → `NewStudioServer`.

## When editing
- Changing the console UI? Keep the ADK runner contract intact — the agent side runs in `pkg/agent`.
- Changing SPA-serving behavior? Update both dev (`web/dist`) and embedded paths, and re-test `make studio-dev` and `make studio`.
