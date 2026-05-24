# Testing Architecture

This document defines the three-tier testing structure for Astonish.

`docs/TESTING.md` is the comprehensive reference for the unit / SSE-scenario / backend-integration / prompt-contract layers (vocabulary: "Layer 0–3"). This document is authoritative for the **build-tag taxonomy** (`unit` / `integration` / `e2e`) and for the e2e harness, K8s sandbox infra, and inspector mode. The two are complementary: the layers describe **what kind of bug a test catches**; the build tags describe **what infrastructure a test needs to run**.

## Test Types

| Type | Tag / Mechanism | External Deps | Run When | Command |
|------|----------------|---------------|----------|---------|
| **Unit** | No build tag | None | Every commit / PR | `make test-unit` |
| **Integration** | `//go:build integration` | Postgres, K8s | CI with infra | `make test-integration` |
| **E2E** | `//go:build e2e` | Postgres, live LLM, K8s | Post-release / nightly | `make test-e2e` |

---

## Choosing the Right Test Type

When adding a test for a new feature or bug fix, pick the **cheapest** layer that can actually demonstrate the bug. The cheaper the layer, the faster the feedback loop and the lower the maintenance cost. Promoting a test to a more expensive layer is justified only when the cheaper layer cannot exercise the failure path.

### Boundary rule

> **If it can be tested without a real LLM + auth + DB combo, it should not be an E2E test.**

This is the generalization of the rule in `tests/scenarios/README.md`: *"if it can be tested with mocked fetch, it's a unit test, not an E2E scenario."* The same logic applies in reverse — if a real database query is what's broken, a Go unit test with a mocked DB is not enough; promote to integration.

### Decision tree

Match the **failure shape** (what code path the bug lives on) to the test home:

| Failure shape | Test home | Build tag |
|---|---|---|
| Pure function, parser, formatter, helper | `pkg/<area>/*_test.go` | none |
| HTTP handler with mocked LLM, no DB | `pkg/api/integration_*_test.go` (despite the name, no tag — see footnote at end of doc) | none |
| Real Postgres / pgvector / SQL semantics / migrations | `pkg/store/pgstore/*_integration_test.go` | `integration` |
| Real K8s sandbox primitives (PVCs, exec) | `pkg/sandbox/*_integration_test.go` | `integration` |
| Cross-tier user journey (auth + provider + persisted session + cascade) | `tests/e2e/<feature>/*_test.go` | `e2e` |
| Frontend rendering of an SSE event sequence | `web/src/test/scenarios/*.test.tsx` + JSON fixture | none (Vitest) |
| Frontend backend integration (real `ChatRunner` + MockLLM, no HTTP) | `pkg/api/integration_scenarios_test.go` | none |
| System prompt regex contract (frontend/backend depend on a string) | `pkg/agent/system_prompt_test.go` (golden + structural) | none |
| `StudioChat` rendering invariant (e.g. report-fence gate) | Prompt contract test **plus** SSE scenario test — see `AGENTS.md` "Inline Report Rendering Contract" | none |

### Worked example: hyphen-vs-underscore tool name normalization (CHAT-070)

The cascade fix in `pkg/api/tool_discovery.go:432` had to handle MCP tool names supplied by config in hyphen form (`tavily-search`) when the live tool registry uses underscore form (`tavily_search`). The bug had three plausible test homes:

1. **Unit test** on `isWebSearchConfiguredWith` directly — cheapest, fastest, but tests only the helper in isolation.
2. **Backend integration test** with MockLLM — possible, but the bug is about static config parsing, not LLM behavior; MockLLM adds no signal.
3. **E2E test** that walks the full platform→org→team cascade and asserts `tavily_search` shows up in `GET /api/tools` — most expensive, but exercises every code path that consumes the helper.

**Decision: E2E only (CHAT-070).** The same `tool_discovery` boundary is used by the chat agent, the system prompt builder, and `/api/tools`. An E2E test that walks one of these paths transitively protects the others. A unit test would be cheaper but adds maintenance for the same bug class without catching anything the E2E doesn't.

**When the unit test would have been the right call instead:** if the helper had complex branching (e.g. five aliases, regex normalization, locale rules) that the E2E couldn't exhaustively cover with a single fixture, the cost calculus inverts — unit-test the matrix, then add a single E2E to prove the wiring.

### When to add to the YAML scenario catalog

Every test under `//go:build e2e` that represents a **user-facing journey** (not an infrastructure smoke check) MUST have a matching entry in `tests/scenarios/<view>.yaml` with a stable ID, and the test function MUST carry a `// COVERS: <ID>` comment ≤5 lines above its `func Test...` declaration. The `make scenario-coverage` reconciler enforces this by greppping for `COVERS:` and cross-checking against the catalog. The ≤5-line rule is enforced by `tests/scenarios/parse-run.mjs:60`.

Tests that exercise only platform plumbing (e.g. `TestE2E_StandardMCPInstall_PlatformEncryptionEnvelope`) do not need a YAML entry — the catalog is for user journeys.

---

## Running Tests

### Unit Tests

Fast, deterministic, no external dependencies. Uses mocks for LLM, DB, and network.

```bash
make test-unit
```

This runs:
- `go test ./...` — all Go tests without build tags (includes handler integration tests that use MockLLM)
- `cd web && npm test -- --run` — all frontend vitest tests (component, API, util, SSE scenario tests)

**Prerequisites:** Go 1.24+, Node.js 20+

### Integration Tests

Tests that need real infrastructure (Postgres, K8s) but still mock AI providers.

```bash
make test-integration
```

This runs:
- `go test -tags=integration -count=1 -timeout=10m ./...`

**Required env vars:**
- `ASTONISH_TEST_DSN` — Postgres admin connection string (e.g. `postgres://user:pass@localhost:5432/testdb`)
- For K8s sandbox tests: `kubectl` access to a cluster with sandbox namespace

### E2E Tests

Full end-to-end tests that bootstrap a complete Astonish platform: fresh database, real `StudioServer`, real auth flow, real LLM provider (via platform settings). Every E2E test uses the same framework (`tests/e2eboot`).

```bash
make test-e2e
```

This runs:
- `go test -tags=e2e -count=1 -p 1 -timeout=15m ./tests/e2e/...`

The `-p 1` flag serializes package execution. This prevents Postgres role creation
races (`astonish_app` is a shared role that `BootstrapPlatform` creates/alters).

**Required env vars:**
- `ASTONISH_TEST_DSN` — Postgres admin connection string
- Provider API key (at least one of: `BIFROST_API_KEY`, `OPENAI_API_KEY`, `GOOGLE_API_KEY`, `ANTHROPIC_API_KEY`)
- For sandbox tests: `kubectl` access to a cluster + provisioned e2e infra (see below)
- `ASTONISH_E2E_CONTROL_PLANE_NAMESPACE` (default `astonish`) — control-plane namespace expected by `e2eboot`
- `ASTONISH_E2E_SANDBOX_NAMESPACE` (default `astonish-sandbox`) — sandbox namespace expected by `e2eboot`

Tests that don't exercise sandboxes (e.g. `flow_assistant`) don't need kubectl — but the platform config still includes sandbox settings for consistency.

### E2E K8s Sandbox Infrastructure

Sandbox-aware E2E tests (any test that exercises `write_file`, `edit_file`, `shell_command`, or any other tool routed through the sandbox runtime) require an isolated set of K8s namespaces, PVCs, and a seeded `@base` layer. **The e2e suite MUST own this infrastructure exclusively** — never reuse a live install (e.g. `astonishdev`, production), or you destroy both isolation and the test's value.

The repo ships everything needed:

- `deploy/helm/astonish/values-e2e.yaml` — values file installing only the sandbox slice of the chart (api/worker `replicaCount: 0`) under the `astonishe2e` prefix.
- `make e2e-k8s-up` — provisions namespaces, RBAC, PVCs, and runs the seed Job.
- `make e2e-k8s-down` — uninstalls the helm release and force-deletes both namespaces.

```bash
# One-time setup per cluster (or after `make e2e-k8s-down`)
make e2e-k8s-up

# Run the e2e suite — uses the env vars from .env to find the namespaces
make test-e2e

# Optional cleanup
make e2e-k8s-down
```

Namespaces created:

| Resource | Name |
|----------|------|
| Control plane | `astonishe2e` |
| Sandbox | `astonishe2e-sandbox` |
| Layers PVC | `astonish-layers` (in `astonishe2e-sandbox`) |
| Uppers PVC | `astonish-uppers` (in `astonishe2e-sandbox`) |
| Helm release | `astonishe2e` |

These names MUST NOT collide with any live install. If your dev install uses `astonishdev`/`astonishdev-sandbox` (per `values-dev-proxmox.yaml`) the e2e suite stays disjoint by default.

The seed Job tars the `astonish-sandbox-base:dev` rootfs into `astonish-layers/@base/rootfs`. First `make e2e-k8s-up` is slow (image pull + tar onto NFS); subsequent runs reuse the seeded `@base`.

**Seed verification.** The seed Job uses `helm.sh/hook-delete-policy: hook-succeeded`, so the Job and its pod are garbage-collected immediately after success — there is nothing left to `kubectl wait` on. Instead, `make e2e-k8s-up` invokes `scripts/verify-e2e-seed.sh`, which spins up a short-lived busybox pod, mounts the layers PVC read-only, and asserts that `/@base/rootfs/usr/bin` exists and is non-empty. This is a positive end-to-end check (the bytes are really on disk) rather than a process-level proxy.

**Test-time preflight.** `make test-e2e` will not blindly run against a missing or partial install. Before invoking `go test`, it verifies (Option A — fail loudly):

1. `kubectl` is on `PATH`
2. The control-plane namespace exists (`ASTONISH_E2E_CONTROL_PLANE_NAMESPACE`, default `astonish`)
3. The sandbox namespace exists (`ASTONISH_E2E_SANDBOX_NAMESPACE`, default `astonish-sandbox`)
4. Both PVCs (`astonish-layers`, `astonish-uppers`) exist in the sandbox namespace

On any miss it prints the exact command needed (`make e2e-k8s-up`) and exits non-zero — there are no silent skips and no auto-provision. Provisioning is always an explicit action by the developer or CI.

### Streamed test output

`make test-e2e` consumes Go's `-json` test events through `tests/scenarios/stream.mjs` to render results live, instead of waiting until the suite ends. Each package emits a header (`▶ tests/e2e/<pkg>`) and each test prints `· <name>` when it starts and a colored `PASS`/`FAIL`/`SKIP` line with elapsed seconds when it finishes. A summary table follows at the end with totals and the list of failing tests. The raw `-json` stream is still preserved verbatim at `/tmp/e2e-results.json` for `parse-run.mjs` and post-mortem inspection.

The same streamer is reused by `make test-e2e-inspect`. Exit status is preserved end-to-end — failing tests propagate to make.

### Inspecting test runs in the UI

E2E tests normally run isolated: every test bootstraps a fresh platform DB, fresh per-test org DBs, runs, and tears them down. Useful for CI but unhelpful when a test fails in a way that requires browsing chat sessions, reports, or audit logs.

`make test-e2e-inspect` reuses the same suite but runs it against a long-lived inspector instance:

```bash
make test-e2e-inspect          # boot inspector, run suite, leave inspector running
# … browse http://localhost:9394 (or via ssh -L) …
make test-e2e-inspect-stop     # stop inspector, drop databases, clean tmp files
```

`make test-e2e-inspect-stop` is fully idempotent: it kills any running inspector (using the PID file or `pgrep` as fallback), then runs `e2e-inspector --cleanup` which drops every `astonish_e2einspect_*` database and removes `/tmp/astonish-e2e-inspect-*` directories and state files. Safe to run multiple times, on a clean system, or when only orphaned databases remain (e.g. from a process that was killed without going through the make target).

Architecture:

- A separate binary `bin/e2e-inspector` (built from `tools/e2e-inspector`) boots a `StudioServer` on **port 9394** with `InstanceSuffix=e2einspect`. It writes `/tmp/astonish-e2e-inspect.json` (state) and `/tmp/astonish-e2e-inspect.pid` (PID), then blocks on `SIGINT`/`SIGTERM`.
- The make target sets `ASTONISH_E2E_KEEP_ALIVE=1` for the test process. `e2eboot.Bootstrap(t)` detects this, reads the state file, and *attaches* to the running inspector instead of starting a new server.
- **Package-scoped seeding (Plan D)**: instead of one fresh world per test, all tests in the same Go package share one acme+globex pair. `Bootstrap()` uses `runtime.Callers` to derive a stable suffix from the test file path (e.g. `chatauth`, `chatcore`). The first test in the package provisions orgs/users/teams/data via `Seed()`; subsequent tests detect the existing world and reuse it. After a full suite run there are exactly 5 organisations: `default`, `acme-chatauth`, `globex-chatauth`, `acme-chatcore`, `globex-chatcore`.
- In shared mode `provisionOrg` skips its `t.Cleanup(DecommissionOrg)` so all data persists for the developer to browse.
- `TestE2E_SandboxLayerChain` is incompatible with shared seeding (mutates global `sandbox_templates` rows and the `astonish-layers` PVC) and skips itself when `ASTONISH_E2E_KEEP_ALIVE=1`. Run it via `make test-e2e` instead.

#### Logging in to browse data

Two login paths are available:

- **As the bootstrap user** — `e2e@test.local` / `E2ETest2024!`. This user is `platform_role=superadmin` and a member of `default` only. Useful for platform-level admin pages (settings, users, providers).
- **As a seeded test actor** — Alice/Bob/Carol/Dave/Eve. Each package has its own copy with a plus-tagged email and the same password. Use these to see the world from the test's perspective, including chat sessions:

| Email | Password | Org | Team | Role |
|---|---|---|---|---|
| `alice+chatauth@acme.test` | `E2ETestSeed2024!` | acme-chatauth | red | owner / admin |
| `bob+chatauth@acme.test` | `E2ETestSeed2024!` | acme-chatauth | red | member |
| `carol+chatauth@acme.test` | `E2ETestSeed2024!` | acme-chatauth | blue | member |
| `dave+chatauth@globex.test` | `E2ETestSeed2024!` | globex-chatauth | engineering | owner / admin |
| `eve+chatauth@globex.test` | `E2ETestSeed2024!` | globex-chatauth | engineering | member |
| `alice+chatcore@acme.test` | `E2ETestSeed2024!` | acme-chatcore | red | owner / admin |
| … (same shape for chatcore) | | | | |

The shared password is `e2eboot.SeededUserPassword` and is the same for all seeded users in all packages.

The `make test-e2e-inspect` post-run footer prints this table dynamically by listing the seeded users actually present in the platform DB. You can also re-print it any time the inspector is running with:

```bash
bin/e2e-inspector --info
```

`flow_assistant` tests do not call `Seed()`; they exercise only the bootstrap user. `sandbox_layerchain` is skipped under `ASTONISH_E2E_KEEP_ALIVE=1`.

What each login can see in the UI:

- **Bootstrap user**: `default` org only. No test data.
- **Alice (chatauth)**: `acme-chatauth` org. Org-scoped settings, audit log, the team(s) she belongs to, team memories/skills/MCP/credentials, her personal memories/credentials/sessions.
- **Eve (chatauth)**: `globex-chatauth` org and its `engineering` team — the adversarial perspective most boundary tests use to verify isolation.
- Cross-org or cross-package switching is **not** possible — the `/api/orgs/switch` endpoint requires membership, and seeded users are members of exactly one org by design (matches what tests assert).

Sessions are per-user. When `ASTONISH_E2E_KEEP_ALIVE=1` is set (which `make test-e2e-inspect` does automatically), tests skip their **hygienic** session DELETEs so the data they created remains browsable in the UI. The helper `e2eboot.RetainSessions()` gates this behavior. **Asserted** DELETEs — tests that explicitly verify deletion semantics (e.g. CHAT-026 "another user cannot delete", `TestE2E_Chat_SessionCreateListDelete`) — still run their delete unconditionally because the DELETE response itself is the subject of the assertion. Net effect: log in as Alice and you'll see sessions from `TestE2E_Chat_SendAndReceive`, `TestE2E_Chat_MultiTurnContext`, the artifact tests, etc.; you won't see sessions from tests where deletion is the test.

#### Resource sizing

Postgres pools are deliberately small in inspect mode (`MaxOpenConns=2`, `MinConns=0`). With Plan D the inspector now keeps only ~5 long-lived org pools (down from ~52 in the per-test design), so connection pressure on Postgres is low and `make test-e2e-inspect` finishes much faster on re-runs (~135s vs ~376s) because subsequent tests in a package skip provisioning.

Each `make test-e2e-inspect` always starts with a fresh inspector platform DB (`DropExisting=true`). Re-running individual tests against the running inspector also works: `Seed()` is idempotent in shared mode and detects existing data, so re-runs cost milliseconds rather than seconds.

If you see `FATAL: sorry, too many clients already`, drop any orphaned `astonish_<old-suffix>_*` databases left behind by killed runs (they hold idle connections):

```sql
SELECT datname, count(*) FROM pg_stat_activity GROUP BY datname ORDER BY count(*) DESC;
DROP DATABASE astonish_<old-suffix>_<orgname>;
```

### Using `.env` Files

The Makefile auto-loads `.env` and `.env.local` from the repo root (if present). This avoids re-exporting credentials every shell session.

```bash
cp .env.example .env        # Shared defaults (DSN)
# Edit .env with your values

# Optional: per-developer secrets (API keys)
touch .env.local
echo "BIFROST_API_KEY=your-key-here" >> .env.local
```

**Precedence:** shell env vars > `.env.local` > `.env`

Both files are gitignored. Only `.env.example` is committed as a reference template.

**Caveat:** Make's `include` does not support shell interpolation (`${VAR}`) inside `.env` files.
Each line must be a simple `KEY=VALUE` assignment (no quotes needed for simple strings, but
quotes are tolerated: `KEY="value with spaces"`).

### Scenario Coverage Report

Track which user-facing scenarios have passing tests:

```bash
make scenario-coverage
```

The catalog lives in `tests/scenarios/` (one YAML file per view: `chat.yaml`, `flows.yaml`, `fleet.yaml`, `drill.yaml`, `apps.yaml`). See `tests/scenarios/README.md` for full details on how to add and mark scenarios.
### Docker Test Environment

To bring up an isolated Docker environment for running integration/e2e tests:

```bash
make e2e-env-up       # Start isolated test environment
make e2e-env-down     # Stop test environment
make e2e-env-rebuild  # Rebuild and restart test environment
```

---

## E2E Framework (`tests/e2eboot`)

All E2E tests use the shared `e2eboot.Bootstrap(t)` harness. It provides:

- **Fresh platform DB** per test (suffix derived from `crc32(t.Name())` for parallel safety)
- **Real `StudioServer`** on a random port
- **Real auth** — registers a default user and returns a Bearer token
- **Provider seeded** in platform settings (Bifrost via `BIFROST_API_KEY`)
- **Full config** including sandbox/K8s settings (always wired, tests opt in to using them)
- **Automatic cleanup** — database dropped, server shut down via `t.Cleanup`

### Usage

```go
//go:build e2e

package my_feature

import (
    "testing"
    "time"

    "github.com/schardosin/astonish/tests/e2eboot"
)

func TestE2E_MyFeature(t *testing.T) {
    h := e2eboot.Bootstrap(t)

    // Authenticated POST
    resp := h.Post(t, "/api/studio/some-endpoint", map[string]string{"key": "value"})
    defer resp.Body.Close()

    // SSE endpoint (reads full stream)
    events := h.SSE(t, "/api/ai/chat", reqBody, 90*time.Second)
    completeEvent := e2eboot.FindEvent(events, "complete")

    // Sandbox pod assertions
    sessionID, podName := h.ChatAndWaitForPod(t, "Run echo hello")
    e2eboot.AssertCommandPresent(t, podName, "node", "should have node")
    h.CleanupSession(t, sessionID, podName)
}
```

### Parallelism

E2E tests currently run sequentially within their package because `Bootstrap` uses
`t.Setenv("XDG_CONFIG_HOME", ...)` to point `config.LoadAppConfig()` at a per-test
config directory. Go's testing framework forbids `t.Setenv` + `t.Parallel()`.

Each test still gets its own isolated DB (per-test suffix), so there is no shared
state between tests. If the config-loading mechanism is refactored to accept an
explicit path (rather than relying on env vars), parallel execution can be enabled.

---

## Adding New Tests

**Before writing the file, decide which layer.** Walk the [Choosing the Right Test Type](#choosing-the-right-test-type) decision tree above. The cost of putting a test in the wrong layer is real — a test that lives in `tests/e2e/` when it could have been a unit test pays the e2e tax (DB bootstrap, real LLM key, kubectl) on every CI run, forever.

### Adding a unit test

Drop a `*_test.go` file in the relevant Go package (no build tag), or a `*.test.ts` / `*.test.tsx` in the relevant frontend `__tests__/` directory. `make test-unit` picks it up automatically.

### Adding an integration test

Add `//go:build integration` at the top of the file. Use `t.Skip` with a helpful message if a required env var is missing:

```go
//go:build integration

package mypackage

func TestMyIntegration(t *testing.T) {
    dsn := os.Getenv("ASTONISH_TEST_DSN")
    if dsn == "" {
        t.Skip("ASTONISH_TEST_DSN not set; skipping integration test")
    }
    // ...
}
```

### Adding an E2E test

Create a new package under `tests/e2e/<feature>/` and use the harness:

```go
//go:build e2e

package my_feature

import (
    "testing"
    "github.com/schardosin/astonish/tests/e2eboot"
)

// COVERS: CHAT-NNN
func TestE2E_MyFeature(t *testing.T) {
    h := e2eboot.Bootstrap(t)
    // Use h.Post, h.SSE, h.Get, h.Delete, h.ChatAndWaitForPod, etc.
}
```

The test is automatically discovered by `make test-e2e` (runs `./tests/e2e/...`).

**If the test represents a user journey**, also:

1. Add an entry in `tests/scenarios/<view>.yaml` (one of `chat.yaml`, `flows.yaml`, `fleet.yaml`, `drill.yaml`, `apps.yaml`) with a fresh stable ID, `status: covered`, and a `test_refs:` pointer to the test function.
2. Add `// COVERS: <ID>` ≤5 lines above the `func Test...` declaration. The reconciler at `tests/scenarios/parse-run.mjs:60` enforces this distance — comments further away are not associated with the test.
3. Run `make scenario-coverage` to verify the catalog and the COVERS comments agree.

Tests that protect platform plumbing rather than user journeys (e.g. encryption envelope shape, role provisioning) do not need a YAML entry.

---

## File Organization

### Backend (Go)

```
pkg/
├── api/
│   ├── *_test.go                          # Unit tests (MockLLM, httptest)
│   ├── integration_*_test.go              # Handler integration tests (MockLLM, no build tag — these are unit tests by our taxonomy)
│   └── *_integration_test.go              # DB-dependent tests (//go:build integration)
├── store/pgstore/
│   └── *_integration_test.go              # PG-dependent tests (//go:build integration)
└── ...

tests/
├── e2eboot/                               # Shared E2E harness (//go:build e2e)
│   ├── bootstrap.go                       # Bootstrap(t) → *Harness
│   ├── http.go                            # Post/Get/Delete helpers
│   ├── auth.go                            # registerUser, loginUser
│   ├── sandbox.go                         # Pod helpers (wait, assert, cleanup)
│   └── sse.go                             # SSE stream parsing and helpers
└── e2e/
    ├── sandbox_layerchain/
    │   └── layerchain_test.go             # Full sandbox layer-chain pipeline (NOT parallel)
    └── flow_assistant/
        └── flow_assistant_test.go         # Flow AI assistant (parallel)
```

### Frontend (Vitest)

```
web/src/
├── api/__tests__/                         # API client unit tests
├── components/__tests__/                  # Component unit tests
├── hooks/__tests__/                       # Hook unit tests
├── utils/__tests__/                       # Utility unit tests
└── test/
    ├── setup.ts                           # Vitest setup (jest-dom, matchMedia mock)
    ├── fixtures/scenarios/                # SSE scenario JSON fixtures
    └── scenarios/                         # SSE scenario integration tests
```

---

## Key Patterns

### MockLLM (Backend Unit Tests)

The `pkg/api/mock_llm_test.go` file provides a reusable `MockLLM` that implements `model.LLM`. Use the builder functions:

```go
mockLLM := NewMockLLM(
    StreamChunk("Here is "),
    StreamFinal("the answer."),
)
```

Available builders: `TextTurn`, `TextTurnWithUsage`, `ToolCallTurn`, `MultiToolCallTurn`, `StreamChunk`, `StreamFinal`, `ErrorTurn`, `EmptyTurn`.

### Provider Injection (AIChatHandler Unit Tests)

`AIChatHandler` uses `getProviderFn` (a package-level variable) to obtain an LLM client. Unit tests override this to inject MockLLM:

```go
func TestAIChatHandler_LargePayload(t *testing.T) {
    original := getProviderFn
    defer func() { getProviderFn = original }()

    getProviderFn = func(ctx context.Context, ...) (model.LLM, error) {
        return NewMockLLM(TextTurn(largeYAML)), nil
    }
    // ... httptest.NewRequest + httptest.NewRecorder ...
}
```

E2E tests do NOT use this injection — they go through the real platform settings path.

### Frontend SSE Testing

Mock `globalThis.fetch` with a `ReadableStream` that emits controlled chunks:

```typescript
const encoder = new TextEncoder()
const stream = new ReadableStream({
    start(controller) {
        controller.enqueue(encoder.encode('event: chunk\ndata: {"content":"hi"}\n\n'))
        controller.close()
    },
})
globalThis.fetch = vi.fn().mockResolvedValue({ ok: true, body: stream })
```

---

## Naming Conventions

- `*_test.go` — standard unit test (no tag)
- `*_integration_test.go` with `//go:build integration` — needs external infra
- `tests/e2e/<feature>/*_test.go` with `//go:build e2e` — full platform E2E
- `web/src/**/__tests__/*.test.ts` — frontend unit test
- `web/src/test/scenarios/*.test.tsx` — frontend SSE scenario test

---

## Note on Existing `integration_*_test.go` Files

The files `pkg/api/integration_test.go`, `integration_scenarios_test.go`, `integration_expanded_test.go`, and `integration_gaps_test.go` use `MockLLM` and have **no build tag**. Despite their "integration" naming, they run under `make test-unit` (no external deps needed). This naming predates the three-tier taxonomy. They are effectively handler-level unit tests that wire multiple internal components together with mocked boundaries.
