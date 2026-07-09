# pkg/daemon — AGENTS.md

Long-running platform bootstrap. `daemon.Run` is the wiring boundary: it loads config, opens stores, initializes providers/memory/tools/channels/scheduler/fleet, builds the Studio HTTP server, and manages graceful shutdown.

## Scope
- `run.go` — `Run(RunConfig)`: the top-level bootstrap function. All heavy dependency injection lives here.
- `config_watcher.go` — reloads config live for supported subsystems (`ConfigWatcherOpts`, `channelSnapshot`).
- `multi_tenant_scheduler.go` — `MultiTenantScheduler`, the platform-aware wrapper around `pkg/scheduler`.
- `delivery_resolver.go` — picks the right delivery target (channel, fleet, in-app) for a scheduler job's output.

## Key rules
1. **`run.go` is a wiring file, not a business-logic file.** Keep new business logic in the appropriate `pkg/*` package and wire it in `Run`.
2. **Order matters.** Storage → auth → memory/embedder → tools → channels → scheduler → fleet → Studio. If you add a new subsystem, place it after its dependencies.
3. **Graceful shutdown**: every started goroutine or server must be cancelable via the context and joined before `Run` returns. Do not fire-and-forget.
4. **PreWarm**: `api.GetChatManager().PreWarm(...)` is called in the background after Studio starts — do not block startup on it.

## Entry-point relationship
- CLI: `astonish daemon run` → `cmd/astonish/daemon.go:handleDaemonRun` → `daemon.Run`.
- The daemon can also be installed as a service (`daemon install/start/stop` via launchd/systemd) — see `cmd/astonish/daemon.go`.

## When editing
1. Adding a new dependency? Thread it via `RunConfig` or via the `services` struct, not via globals.
2. Adding a new HTTP route? Put the handler in `pkg/api`, register it in `api.RegisterRoutes`, and pass any needed service into `NewStudioServer` via `WithServices`.
3. Live-reloading a new subsystem? Extend `config_watcher.go` — do not sprinkle file watchers throughout the codebase.
