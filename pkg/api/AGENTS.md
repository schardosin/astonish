# pkg/api — AGENTS.md

HTTP + SSE surface for Astonish Studio and the platform API. Handlers here are the boundary between the SPA/remote CLI and the rest of the Go runtime.

## Scope
- REST + SSE handlers under `/api/*` (registered via `api.RegisterRoutes`).
- Chat runner (`chat_runner.go`) — the SSE producer for Studio Chat.
- Tenant middleware — enforces org/team/personal scoping on every authenticated request.
- Image build orchestration handlers (`image_build_handlers.go`).
- Auth/SSO/session endpoints for platform mode.

## Conventions
- Handler signature: `func Name(w http.ResponseWriter, r *http.Request)`.
- JSON: `w.Header().Set("Content-Type", "application/json")` then `json.NewEncoder(w).Encode(...)`. Errors via `http.Error(w, msg, code)`; do not write JSON error bodies with `http.Error`.
- SSE: use the helpers in `chat_runner.go` / `sse_*.go`. Every SSE stream must flush after each event and must terminate with a final `done`/close event.
- Every handler that touches per-tenant data must resolve tenancy via the context provided by `TenantMiddleware` — **never** read org/team IDs directly from query strings or bodies for authorization decisions.
- Never re-authenticate inside a handler; the middleware chain already did it. Just consume `platform.UserFromContext(r.Context())`.

## Non-Negotiable Invariants

### Chat report marker (three-signal gate)
`detectAndEmitReportMarkers` (`chat_runner.go`) and `joinReportMarkers` (`chat_utils.go`) implement the **inline-report contract** described in the root `AGENTS.md`. All three of these must remain true simultaneously for an artifact to be promoted to inline `EmbeddedFileViewer`:

1. The artifact event was emitted in the last turn.
2. `fileType === "Markdown"`.
3. `isReport === true`, set only when the agent emitted an `` ```astonish-report `` fence whose `path:` matches the artifact's path.

**Do not widen the gate.** The gate exists because non-report edits (touching a stray `.md` while doing a task) must render as compact tiles, not full previews. The regression commits `b5310ae` and `ee2d47d` are the historical warning. Guarded by `TestE2E_Chat_PlainWriteFileNotReport` (CHAT-066) and the system-prompt contract test.

Authoritative reference: `docs/architecture/chat-rendering-pipeline.md`, "The Report Pipeline" section.

### Tenant isolation
- Any handler that reads or writes tenant data must go through `pkg/store/entstore` — do not open an ent client directly.
- Do not leak identifiers across tenants in error messages or logs.
- Cross-tenant reads (e.g., admin listing all orgs) require the `platform` scope and go through the platform ent client only.

### Image build handlers
`PlatformImageBuildHandler` and `TeamImageBuildHandler` in `image_build_handlers.go`:
- Always acquire a per-template build lock (`tplStore.AcquireTemplateBuildLock`) before starting Kaniko.
- Base image is always the configured `SandboxImage` — do not use the last-built image as the base.
- Image tag is content-hashed from the Dockerfile body. Do not add non-deterministic inputs (timestamps, hostnames) to the hash.
- Stream build logs as SSE; on success set `LastBuiltImage` + `SandboxImage` + `BuildStatus=succeeded`; on failure set `BuildStatus=failed` with `BuildError`.
- Enforce the 30-minute build timeout — do not remove it without discussion.

## Key files
- `chat_runner.go` — SSE chat driver, report marker detection.
- `chat_utils.go` — projection helpers (including `joinReportMarkers`).
- `image_build_handlers.go` — platform/team image build.
- `tenant_middleware.go` (or similar) — the auth/tenant boundary.
- `platform_auth_*.go`, `sso_*.go` — auth surfaces.
- `*_test.go` and `pkg/api/testdata/` — scenario fixtures for chat SSE.

## Testing
- Chat scenarios: `docs/architecture/testing-chat-scenarios.md`.
- Integration tests use build tag `integration` and need `ASTONISH_TEST_DSN`.
- Prompt contract tests defend the two-step artifact/report contract — keep them green.

## When editing
1. If adding a new SSE event type, update **both** `chat_runner.go` (emit) and the SPA consumer (`web/src/components/StudioChat.tsx`). Document the event in `docs/architecture/chat-rendering-pipeline.md` and add a scenario fixture.
2. If adding a new handler, register it in `RegisterRoutes`, add tenant-middleware coverage, and write a `_test.go` that hits it with both a valid and a cross-tenant request.
3. Never bypass `entstore` even for one-off admin utilities.
