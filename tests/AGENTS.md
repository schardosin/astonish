# tests/ — AGENTS.md

Astonish's end-to-end and integration test infrastructure. Read this before adding or modifying any test that touches DB, sandbox, or provider APIs.

## Layout
- `tests/e2e/` — grouped e2e packages (one dir per group):
  - `chat_core/`, `chat_auth/`, `chat_credentials/`
  - `drill/`, `fleet/`, `flows/`, `flow_assistant/`
  - `sandbox_layerchain/`
  - `apps/`
  - …plus additional packages for narrower scenarios.
- `tests/e2eboot/` — the **custom test harness**. Owns DB creation/seed, provider-key gating, sandbox helpers, SSE helpers, HTTP client, inspector state.
- `tests/scenarios/` — YAML scenario catalog (`chat.yaml`, `apps.yaml`, `fleet.yaml`, `drill.yaml`, `flows.yaml`, `sandbox.yaml`) + Node reporter scripts (`stream.mjs`, `parse-run.mjs`, `report.mjs`).
- `tools/e2e-inspector/` — long-lived `StudioServer` binary for post-run inspection (`make test-e2e-inspect`).

## How tests run
- Build tag `e2e`. Makefile targets:
  - `make test-e2e` — Postgres path (requires `ASTONISH_TEST_DSN`).
  - `make test-e2e-sqlite` — SQLite path (no `ASTONISH_TEST_DSN`).
  - `make test-e2e-openshell` — OpenShell backend variant.
  - `make test-e2e-inspect` / `make test-e2e-inspect-stop` — leave an inspector server on `:9394`.
- `go test -tags=e2e -count=1 -p 1 -timeout=…` is the underlying command. Output is piped through `tests/scenarios/stream.mjs` and then `parse-run.mjs` for reporting.

## Environment variables
- **`ASTONISH_TEST_DSN`** (required for `test-e2e` and `test-integration`) — admin-capable Postgres DSN. The harness uses it to create/drop per-run DBs. `.env.integration.example` has a template.
- **Provider API key** (required for **all** e2e paths, including SQLite): at least one of
  - `BIFROST_API_KEY` (preferred),
  - `OPENAI_API_KEY`,
  - `GOOGLE_API_KEY`,
  - `ANTHROPIC_API_KEY`,
  - or provider-specific alternatives (`OPENROUTER_API_KEY`, `POE_API_KEY`, `GROQ_API_KEY`, `XAI_API_KEY`, …).
  `tests/e2eboot/platform_core.go` and `bootstrap_sqlite.go` skip tests with no provider key present.
- **`TAVILY_API_KEY`** — required for MCP-standard-install tests and some `apps` tests (`chat_mcp_standard_install_test.go`, `apps/apps_test.go`).
- Sandbox-dependent tests require **`kubectl`** and **`helm`** on `PATH`, with namespaces + PVCs `astonish-layers` / `astonish-uppers` provisioned. Use `make e2e-k8s-up` / `make e2e-k8s-down` to bring them up/down.
- `docker-compose.e2e.yml` powers `make e2e-env-up` / `make e2e-env-down` for a containerized smoke Astonish (not the full k8s sandbox path).

## e2eboot responsibilities
- `bootstrap.go` / `bootstrap_sqlite.go` — set up DBs (Postgres via `ASTONISH_TEST_DSN`, or SQLite path), seed initial state.
- `platform_core.go` — resolve API keys, initial platform state.
- `seed.go` — DB seed data.
- `client.go` / `http.go` / `sse.go` — HTTP + SSE test clients.
- `sandbox*.go` — sandbox backend selection, port-forwarding, env injection.
- `auth*.go` — auth helpers.
- `embedder*.go` — embedder/memory seeding.
- `inspector_state*.go` — inspector lifecycle state (per-run DB naming, PID/state file locations under `/tmp/astonish-e2e-inspect.*`).

## Scenario catalog and reporters
- `tests/scenarios/*.yaml` — declarative catalog: which packages belong to which group, which env vars each scenario needs, expected fixtures.
- Reporter scripts (`stream.mjs`, `parse-run.mjs`, `report.mjs`) — Node.js. Require `node`/`npm`.

## Rules
1. **New e2e tests go under an existing `tests/e2e/<group>/` package** unless a genuinely new group is warranted. If new, add a `tests/scenarios/<group>.yaml`.
2. **All bootstrap logic belongs in `tests/e2eboot/`** — do not duplicate DB setup or auth in individual test files.
3. **Provider keys**: if a test needs a specific provider, add the skip clause in the harness, not in individual tests.
4. **Do not commit real API keys** — use `.env.integration.example` as the template.
5. **`ASTONISH_TEST_DSN` needs privileges to drop/create DBs** — the inspector target relies on this. Do not point it at a production DB.

## References
- `docs/architecture/testing-chat-scenarios.md` — scenario test infrastructure.
- `docs/TESTING.md` — repo-wide testing overview.
- `.env.integration.example` — env-var template.
