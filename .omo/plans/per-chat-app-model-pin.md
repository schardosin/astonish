# per-chat-app-model-pin - Work Plan

## TL;DR (For humans)

**What you'll get:** Every chat and every saved app remembers which AI model it was last using, so switching mid-conversation or reopening later just works. A picker in the chat header and next to each app lets you change the model in one click; the choice sticks until you clear it. A new per-user default sits between team settings and the per-chat pin, so power users can set their own preferred model once instead of re-passing CLI flags every time.

**Why this approach:** The `SwappableLLM` wrapper the codebase already uses was literally built for this — hot-swap without rebuilding the agent. Rather than adding a new resolver that fetches sessions from the DB, we keep the existing 3-tier cascade untouched and layer two thin overlays (user default + per-chat/app pin) on top at the ~9 places that build a request config (grep-verified at plan time; executor re-verifies at Todo 8). Storage goes on the ent schemas as first-class nullable fields, not buried in JSON metadata, so pins are queryable and migratable.

**What it will NOT do:** It won't change how distilled flows pick their model (each flow keeps its own choice). It won't add per-channel model overrides (a Slack thread uses the chat's pin, no separate Slack pin). It won't do a live in-turn swap in the CLI console (persist-only there; Studio still swaps live).

**Effort:** Medium
**Risk:** Medium — resolver has ~9 production callers (grep-confirmed), ent schema changes need coordinated migrations across two scopes × two DB backends, and the missing-credential fallback path must be uniform across Studio, `--resume`, and channels.
**Decisions to sanity-check:**
  - Adding the user-default layer now (vs. shipping without it and adding later).
  - Pin-by-default on CLI flags (breaking behavior change for scripted callers who pass `-m`; `--no-pin` is the escape hatch).
  - Missing-credential = warn banner + cascade fallback (never hard-fail a session because of credential rotation).

Your next move: approve to trigger execution (or request the optional high-accuracy review first via `momus` + `oracle`). Full execution detail follows below.

---

> TL;DR (machine): Medium effort / Medium risk. Adds per-user default + per-session + per-app model pin via nullable ent fields + resolver overlays + SwappableLLM.Swap. Studio picker, CLI --no-pin / --clear-model / model subcommand, warn-banner fallback on missing creds. Tests: unit (resolve overlays), integration (pin+resume+missing-cred), Vitest (picker), e2e scenario. Docs: agent-engine + cli/chat + one line in chat-rendering-pipeline.

## Scope

### Must have
- **Schema:** nullable `provider_name` + `model_name` fields on `ent/personal/schema/session.go`, `ent/team/schema/session.go`, `ent/personal/schema/app.go`, `ent/team/schema/app.go`. New `PersonalSettings` ent schema (personal scope) with `DefaultProvider`, `DefaultModel`. Ent regen + Atlas migrations for SQLite (personal + team) and Postgres (team). Migration commit atomically with schema.
- **Resolver overlays** in `pkg/provider/resolve.go`:
  - `ApplyUserDefault(cfg *config.AppConfig, us *store.PersonalSettings) *config.AppConfig`
  - `ApplyProviderOverride(cfg *config.AppConfig, provider, model string) *config.AppConfig`
  Empty-string pin = inherit; non-empty = pinned. No signature change to `ResolveEffectiveConfig`.
- **Call-site updates** at all **9 production** `ResolveEffectiveConfig` callers to chain the overlays where a session/app/user is in scope. Grep-verified list (2026-07-09): `pkg/api/chat_handlers.go:1094`, `pkg/api/request_helpers.go:238,277`, `pkg/channels/manager.go:646,970`, `pkg/daemon/run.go:2048`, plus test files (`pkg/provider/resolve_test.go`, `pkg/api/integration_multitenant_test.go`) which need updated fixtures — NOT overlay chains. The executor MUST run `grep -RIn "ResolveEffectiveConfig(" pkg/ --include="*.go"` at the start of Todo 8 and use the actual count then; the count 9 in this plan is a spot-check baseline, not authoritative.
- **App pin overlay applies at `pkg/api/ai_chat_handler.go`** (the backend proxy that services `useAppAI`), NOT at any `pkg/apps/*` `ResolveEffectiveConfig` site — grep confirms `pkg/apps/` has **zero** `ResolveEffectiveConfig` callers. `ai_chat_handler.go` already resolves per-request cfg; add `provider.ApplyProviderOverride(cfg, appPin.Provider, appPin.Model)` there.
- **Runtime hot-swap (Studio):** re-use existing `SwappableLLM.Swap()` (`pkg/provider/swappable_llm.go:30`). Handler `PATCH /api/sessions/{id}/model` persists the pin, builds a new `model.LLM` via `provider.GetProvider`, calls `Swap`, and emits SSE `model_changed`. Sub-agents inherit automatically (verified at `pkg/launcher/chat_factory.go:1338`).
- **New API surface:**
  - `PATCH /api/sessions/{id}/model` — body `{provider, model}` (empty strings clear the pin).
  - `PATCH /api/apps/{slug}/model` — same shape, personal + team apps.
  - `GET /api/sessions/{id}/model-status` — returns `{effectiveProvider, effectiveModel, pinnedProvider, pinnedModel, credentialsAvailable}`.
  - `GET /api/user-settings/default-model` + `PATCH` same — personal-scope PersonalSettings CRUD.
  - `GET /api/apps/{slug}` extended to return `pinnedProvider`, `pinnedModel`.
- **CLI:**
  - `astonish chat -p X -m Y` pins by default onto the new session.
  - `astonish chat -p X -m Y --no-pin` restores today's ephemeral behavior.
  - `astonish chat --resume <id> -m Y` overrides for this run ONLY (no pin rewrite).
  - `astonish chat --clear-model` clears the pin on the resumed session and falls back to cascade.
  - `astonish chat model <provider>:<model>` new subcommand to change/clear the last session's pin (empty string = clear).
- **Studio UI:**
  - Model picker in `web/src/components/StudioChat.tsx` header, sourced from the same provider list the settings page uses (`pkg/api/provider_settings_handlers.go` → `GET /api/settings/providers`).
  - Per-app model picker in `web/src/components/AppsPanel.tsx`.
  - Non-blocking yellow banner in the header when the pinned provider has no credential on the current team; button "Pick another model" opens the picker.
- **useAppAI honors the app pin:** `web/src/hooks/useAppAI.ts` sends `appSlug` (already does); backend `pkg/api/ai_chat_handler.go` (the proxy that serves `useAppAI`) reads the app pin via `router.AppPin(ctx, slug)` and applies `provider.ApplyProviderOverride(cfg, pin.Provider, pin.Model)` after `ResolveEffectiveConfig`. Do NOT try to change `pkg/apps/*` — grep confirms it has no `ResolveEffectiveConfig` callers.
- **Missing-credential fallback:** at every call site, after applying overrides, check the resolved provider against the credential store; if absent, log/emit a warning, fall back to the cascade result (do NOT clear the pin). Uniform across Studio open, `--resume`, and channel-delivered messages.
- **Docs:** `docs/architecture/agent-engine.md` cascade section updated with the two new innermost layers; `docs/website/docs/cli/chat.md` flag reference updated with pin semantics; `docs/architecture/chat-rendering-pipeline.md` one-line addition of the new `model_changed` SSE control event.
- **Tests:** unit for overlays; integration for end-to-end pin + resume + missing-cred; Vitest for picker; one e2e scenario for pin+resume.

### Must NOT have (guardrails, anti-slop, scope boundaries)
- **NO `ResolveForSession(ctx, sessionID)` inside `pkg/provider`.** Rejected in DECISION-7 — it creates a resolver→store→session loop and couples the provider package to the DB. Callers already hold the session; they apply the overlay themselves.
- **NO storing pins inside the existing `metadata` JSON field.** Explicit ent fields per DECISION-2.
- **NO in-turn CLI console live swap.** `/model` in CLI persists and takes effect on the next user turn. Only Studio does true live swap.
- **NO changes to distilled/imported flow provider resolution.** Flows keep their own `provider:`/`model:` block. Out of scope per issue.
- **NO per-channel pin override.** Slack/Telegram/Email inherit the session pin. No new per-channel field.
- **NO hard-fail on missing credential.** Always warn + fall back to cascade. Never brick a session.
- **NO changes to `useAppData` proxies.** Only `useAppAI` reads the pin.
- **NO sub-agent-level pin override.** Sub-agents inherit the main-thread `SwappableLLM` — verified — no additional plumbing.
- **NO widening of the resolver signature.** `ResolveEffectiveConfig(ctx, platform, org, team) *AppConfig` stays. All new inputs enter via chained overlay calls.
- **NO cross-org/team migration of pinned models.** If an app is published from personal → team, the pin does NOT carry (out of scope; explicit clear in the publish flow).
- **NO changes to the provider factory or provider list endpoints.** Picker reads what's already there.
- **NO new lint category exceptions.** `.golangci.yml` policy unchanged (bug-finders only).
- **NO commits skipping the pre-commit hook.** Ent schema + Atlas migration MUST go in the same commit; the hook validates hash integrity.

## Verification strategy
> Zero human intervention - all verification is agent-executed.
- Test decision: **TDD for the resolver overlays** (write `resolve_test.go` cases first, then implement); **tests-after** for the API handlers, CLI plumbing, and Studio UI (existing code has no test-first culture there and would create low-value churn). Frameworks: Go stdlib `testing` + table-driven tests (unit, integration), Vitest + Testing Library (Studio), the existing e2e scenario runner under `tests/e2e` + `tests/scenarios/*.yaml`.
- Evidence root: `.omo/evidence/task-<N>-per-chat-app-model-pin.<ext>` (log for `go test`, `.txt` for `npm test`, `.json` for scenario results).
- Every todo runs BOTH a happy-path and a failure-path scenario; failure paths include missing credentials, malformed pin values, and cascade fallback correctness.
- Migration integrity is verified by running `atlas migrate hash --env <env>` for each affected env (the pre-commit hook does this, but the todo also runs it explicitly).
- Regression coverage: existing `pkg/provider/resolve_test.go`, `pkg/api/integration_multitenant_test.go`, and any other cascade-touching test MUST still pass unmodified except for expected additions.

## Execution strategy
### Parallel execution waves
> Target 5-8 todos per wave. Fewer than 3 (except the final) means you under-split.

- **Wave 1 (foundation, TDD)**: Todos 1, 2, 3, 4, 5 — resolver overlay tests + implementation, PersonalSettings store contract, ent schema changes for both scopes. All independent files.
- **Wave 2 (persistence + wiring)**: Todos 6, 7, 8, 9 — migrations, entstore wiring, call-site chains, ChatFactoryConfig plumbing. Depends on Wave 1.
- **Wave 3 (API surface)**: Todos 10, 11, 12, 13 — session model handler, app model handler, user-settings handler, model-status endpoint. Depends on Wave 2.
- **Wave 4 (CLI + channels)**: Todos 14, 15, 16 — CLI flag semantics, `chat model` subcommand, channel path integration. Depends on Wave 3.
- **Wave 5 (UI)**: Todos 17, 18, 19, 20 — Studio header picker, Apps tab picker, warning banner, `useAppAI` wiring. Depends on Wave 3 (API endpoints must exist).
- **Wave 6 (docs + e2e)**: Todos 21, 22, 23 — docs, e2e scenario, final regression sweep. Depends on Waves 4 + 5.
- **Final wave**: F1–F4 (see bottom).

### Dependency matrix
| Todo | Depends on | Blocks | Can parallelize with |
| --- | --- | --- | --- |
| 1  Overlay tests (TDD)                        | —          | 2       | 3, 4, 5              |
| 2  Overlay implementation                     | 1          | 8, 9    | 3, 4, 5              |
| 3  PersonalSettings ent schema + store contract | —        | 6, 7, 12| 1, 2, 4, 5           |
| 4  Session ent schema fields (personal+team)  | —          | 6, 7    | 1, 2, 3, 5           |
| 5  App ent schema fields (personal+team)      | —          | 6, 7    | 1, 2, 3, 4           |
| 6  Ent regen + Atlas migrations               | 3, 4, 5    | 7, 8    | —                    |
| 7  entstore router wiring (PersonalSettings + session/app pin getters/setters) | 3, 4, 5, 6 | 10, 11, 12 | 8, 9   |
| 8  Call-site overlay chain (~9 prod callers)  | 2, 6       | 10      | 7, 9                 |
| 9  ChatFactoryConfig / SwappableLLM.Swap plumbing | 2, 6   | 10      | 7, 8                 |
| 10 PATCH /api/sessions/{id}/model + SSE model_changed | 7, 8, 9 | 17    | 11, 12, 13           |
| 11 PATCH /api/apps/{slug}/model + GET app pin exposure | 7, 8 | 18   | 10, 12, 13           |
| 12 GET/PATCH /api/user-settings/default-model | 7, 8       | 19      | 10, 11, 13           |
| 13 GET /api/sessions/{id}/model-status (credential check) | 7, 8, 9 | 20 | 10, 11, 12         |
| 14 CLI flag semantics (pin-by-default, --no-pin, --clear-model, --resume behavior) | 8, 9 | 22 | 15, 16 |
| 15 `astonish chat model <slug>` subcommand    | 8, 9       | 22      | 14, 16               |
| 16 Channels path (pin honored on inbound messages, missing-cred fallback) | 8, 9 | 22 | 14, 15 |
| 17 Studio header model picker                 | 10, 13     | 22, 23  | 18, 19, 20           |
| 18 Apps tab per-app picker                    | 11         | 22, 23  | 17, 19, 20           |
| 19 User-default picker in Settings            | 12         | 22, 23  | 17, 18, 20           |
| 20 useAppAI reads app pin + fallback banner   | 11, 13     | 22, 23  | 17, 18, 19           |
| 21 Docs updates                               | 14, 17     | —       | 22, 23               |
| 22 Integration test: pin + resume + missing-cred end-to-end | 14, 15, 16, 17, 18, 20 | 23 | 21 |
| 23 e2e scenario: full user flow (pin, refresh, missing-cred banner) | 22 | — | 21 |

## Todos
> Implementation + Test = ONE todo. Never separate.
<!-- APPEND TASK BATCHES BELOW THIS LINE WITH edit/apply_patch - never rewrite the headers above. -->

- [x] 1. `pkg/provider/resolve_test.go`: add TDD test cases for `ApplyUserDefault` + `ApplyProviderOverride` before implementation exists
  What to do: Add ~10 table-driven cases covering: (a) empty overlays leave cfg unchanged; (b) UserDefault overrides team default; (c) ProviderOverride overrides both; (d) empty-string in override = inherit (no-op); (e) providers map from lower layers survives (additive); (f) nil PersonalSettings safely no-ops; (g) idempotent (applying twice = once); (h) chained order Platform→Org→Team→UserDefault→Override matches the intended cascade; (i) partial override (provider set, model empty) inherits model from cascade; (j) full clear (both empty) restores cascade result. Must-NOT: do NOT modify `ResolveEffectiveConfig`; do NOT depend on the store package (fake `store.PersonalSettings` struct inline is fine — pkg/store/settings.go already exports one).
  Parallelization: Wave 1 | Blocked by: — | Blocks: 2
  References (executor has NO interview context - be exhaustive):
    - pkg/provider/resolve.go:24-95 — cascade shape + `applyProviderLayer` merging semantics (defaults override only if non-empty; providers additive)
    - pkg/provider/resolve_test.go — existing table pattern; mimic style
    - pkg/store/settings.go:141-163 — PlatformSettingsStore / OrgSettingsStore / SettingsStore / TeamSettings shape; PersonalSettings will be a new sibling
    - pkg/config/config.go — `config.AppConfig`, `config.ProviderConfig` shapes referenced by overlays
  Acceptance criteria (agent-executable):
    - `go test ./pkg/provider -run "TestApplyUserDefault|TestApplyProviderOverride" -v` — all subtests FAIL with "undefined: ApplyUserDefault" / "undefined: ApplyProviderOverride" (RED state proves TDD).
    - Existing tests still pass: `go test ./pkg/provider -run "TestResolveEffectiveConfig" -v`
  QA scenarios:
    - Happy: `go test -v ./pkg/provider -run TestApplyUserDefault 2>&1 | tee .omo/evidence/task-1-per-chat-app-model-pin.log` — expect compile errors naming undefined helpers.
    - Failure: `go test -v ./pkg/provider -run TestResolveEffectiveConfig 2>&1 | tee -a .omo/evidence/task-1-per-chat-app-model-pin.log` — expect PASS (no regression).
    Evidence: `.omo/evidence/task-1-per-chat-app-model-pin.log`
  Commit: Y | `test(provider): red-state overlay tests for user-default and per-session pin`

- [x] 2. `pkg/provider/resolve.go`: implement `ApplyUserDefault` + `ApplyProviderOverride` to turn Todo 1 GREEN
  What to do: Add two small pure functions next to `applyProviderLayer`:
  ```go
  func ApplyUserDefault(cfg *config.AppConfig, us *store.PersonalSettings) *config.AppConfig
  func ApplyProviderOverride(cfg *config.AppConfig, provider, model string) *config.AppConfig
  ```
  Both mutate-in-place semantics (return same pointer) mirroring `applyTeamProviderLayer`. Nil/empty inputs are no-ops. `ApplyProviderOverride` sets `cfg.General.DefaultProvider` and `cfg.General.DefaultModel` only if non-empty. Must-NOT: do NOT change `ResolveEffectiveConfig`'s signature or behavior; do NOT touch the additive `Providers` map (overrides are default-only, not provider-config-only).
  Parallelization: Wave 1 | Blocked by: 1 | Blocks: 8, 9
  References:
    - pkg/provider/resolve.go:62-95 — pattern to mirror (`applyProviderLayer`, `applyTeamProviderLayer`)
    - pkg/store/settings.go:120-150 — existing settings shapes; new `PersonalSettings` type added in Todo 3
    - `.omo/evidence/task-1-per-chat-app-model-pin.log` — test names to satisfy
  Acceptance criteria:
    - `go test ./pkg/provider -v -run "TestApplyUserDefault|TestApplyProviderOverride|TestResolveEffectiveConfig"` — all PASS.
    - `go vet ./pkg/provider/... && golangci-lint run ./pkg/provider/...` — clean.
  QA scenarios:
    - Happy: `go test -v ./pkg/provider 2>&1 | tee .omo/evidence/task-2-per-chat-app-model-pin.log` — all green.
    - Failure: temporarily pass empty override to a case that expects a pinned value, assert cascade result survives (this case IS one of the table entries from Todo 1).
    Evidence: `.omo/evidence/task-2-per-chat-app-model-pin.log`
  Commit: Y | `feat(provider): add ApplyUserDefault and ApplyProviderOverride overlays`

- [x] 3. `pkg/store/settings.go` + `pkg/store/entstore/personal_settings.go` (new): PersonalSettings store contract and implementation
  What to do: Add `type PersonalSettings struct { DefaultProvider, DefaultModel string }` and `type PersonalSettingsStore interface { Get(ctx) (*PersonalSettings, error); Save(ctx, *PersonalSettings) error }` to `pkg/store/settings.go`. Add the implementation in `pkg/store/entstore/personal_settings.go` mirroring the shape of `pkg/store/entstore/personal_sessions.go`. Must-NOT: do NOT add to org or team scopes (per DECISION-1 it's a user-level default; personal scope only); do NOT reuse team `SettingsStore` — semantics differ.
  Parallelization: Wave 1 | Blocked by: — | Blocks: 6, 7, 12
  References:
    - pkg/store/settings.go:141-163 — pattern for interface + type
    - pkg/store/entstore/personal_sessions.go — implementation shape to mirror (Get/Save/error handling)
    - pkg/store/entstore/tenant_router.go — where to plumb the new store on the router
    - ent/AGENTS.md — schema is edited BY HAND under `ent/personal/schema/`; everything else regenerated
  Acceptance criteria:
    - `go build ./pkg/store/...` succeeds.
    - `PersonalSettingsStore` interface exists; a stub implementation returning `(&PersonalSettings{}, nil)` on Get compiles.
    - Ent schema `ent/personal/schema/personal_settings.go` created with `default_provider` + `default_model` string fields (empty defaults), no ID field beyond the ent default (single-row-per-user pattern; use `field.UUID("user_id", uuid.UUID{})` with a unique index).
  QA scenarios:
    - Happy: `go build ./... 2>&1 | tee .omo/evidence/task-3-per-chat-app-model-pin.log` — clean build.
    - Failure: attempt Save with nil settings pointer → expect returned error, not panic.
    Evidence: `.omo/evidence/task-3-per-chat-app-model-pin.log`
  Commit: Y | `feat(store): PersonalSettings store contract + entstore impl`

- [x] 4. `ent/personal/schema/session.go` + `ent/team/schema/session.go`: add nullable `provider_name` + `model_name` fields
  What to do: In both files, add inside `Fields()`:
  ```go
  field.String("provider_name").Optional().Nillable(),
  field.String("model_name").Optional().Nillable(),
  ```
  Position immediately before `metadata`. Must-NOT: do NOT store inside `metadata` JSON; do NOT add indexes (pins are read alongside the session, no need for lookup-by-model); do NOT touch team `Session`'s edges/annotations.
  Parallelization: Wave 1 | Blocked by: — | Blocks: 6, 7
  References:
    - ent/personal/schema/session.go:21-66 — location and style
    - ent/team/schema/session.go:21-70 — mirror (same fields; team session already has `metadata` optional JSON on line 46)
    - ent/AGENTS.md — hand-editable files; regen required for the rest
  Acceptance criteria:
    - Schema files edited; `go generate ./ent/personal/... && go generate ./ent/team/...` succeeds (Todo 6 runs this).
    - Diff review: both files show exactly two new field entries, nothing else changed.
  QA scenarios:
    - Happy: `git diff ent/personal/schema/session.go ent/team/schema/session.go > .omo/evidence/task-4-per-chat-app-model-pin.diff` — diff is minimal + only the two field lines per file.
    - Failure: run `go generate ./ent/personal/...` after editing → expect success; if it fails, ent DSL is malformed.
    Evidence: `.omo/evidence/task-4-per-chat-app-model-pin.diff` + `.omo/evidence/task-4-per-chat-app-model-pin.log`
  Commit: N (batched with 5, 6 into one atomic schema+migration commit per pre-commit hook policy)

- [x] 5. `ent/personal/schema/app.go` + `ent/team/schema/app.go`: add nullable `provider_name` + `model_name` fields
  What to do: Same two fields as Todo 4, inserted before `created_at` in both files. Must-NOT: do NOT touch the unique `slug` index; do NOT change published_by semantics.
  Parallelization: Wave 1 | Blocked by: — | Blocks: 6, 7
  References:
    - ent/personal/schema/app.go — location
    - ent/team/schema/app.go:20-70 — mirror
    - ent/AGENTS.md
  Acceptance criteria:
    - Schema files edited; `go generate ./ent/personal/... && go generate ./ent/team/...` succeeds (Todo 6).
  QA scenarios:
    - Happy: `git diff ent/personal/schema/app.go ent/team/schema/app.go > .omo/evidence/task-5-per-chat-app-model-pin.diff`
    - Failure: as Todo 4.
    Evidence: `.omo/evidence/task-5-per-chat-app-model-pin.diff`
  Commit: N (batched with 4, 6)

- [x] 6. Ent regeneration + Atlas migrations (personal SQLite, team SQLite, team Postgres)
  What to do: Run `go generate ./ent/personal/... ./ent/team/...`. Verify no diff outside the intended fields (session + app in both scopes, plus new personal_settings). Generate migrations:
  - `make migrate-diff` (or the equivalent per-env `atlas migrate diff --env personal_lite ...` for personal SQLite; `... --env team_lite ...` and `--env team_pg ...` for team). Migration file names: `<timestamp>_add_provider_model_pin.sql` under `pkg/store/personal/migrations/`, `pkg/store/team/migrations/`. Then `atlas migrate hash --env <env>` for each env to update `atlas.sum`.
  Must-NOT: do NOT hand-edit anything under `ent/personal/`, `ent/team/` other than the four `schema/*.go` files touched in 3, 4, 5; do NOT skip `--env team_pg` (Postgres path is production-critical).
  Parallelization: Wave 2 | Blocked by: 3, 4, 5 | Blocks: 7, 8
  References:
    - ent/AGENTS.md — regen flow, migration commit rule
    - Makefile — `migrate-diff` target
    - pkg/store/personal/migrations/, pkg/store/team/migrations/ — existing migration file naming pattern
    - .githooks/pre-commit — Atlas integrity check
  Acceptance criteria:
    - `go build ./...` succeeds after regen.
    - Migration files exist for all three envs (personal_lite, team_lite, team_pg).
    - `atlas migrate hash --env personal_lite` and same for team_lite / team_pg — exits 0.
    - Pre-commit hook simulation: `bash .githooks/pre-commit` passes.
  QA scenarios:
    - Happy: run migration against a scratch SQLite DB (`atlas migrate apply --env personal_lite --url "sqlite://.omo/evidence/task-6-scratch.db"`) → expect success; then re-run → expect "no changes".
    - Failure: attempt to build without regenerating → expect ent client errors referencing the new fields (proves regen is needed).
    Evidence: `.omo/evidence/task-6-per-chat-app-model-pin.log` (build + migrate output)
  Commit: Y | `feat(ent,migrate): add provider_name/model_name pin fields to sessions, apps, personal_settings`

- [x] 7. `pkg/store/entstore/tenant_router.go` + related: wire PersonalSettingsStore + session/app pin getters/setters onto the router
  What to do: Extend `TenantRouter` (or its equivalent surface — see `pkg/store/entstore/AGENTS.md` for the router contract) so that request-scoped code can call:
  - `router.PersonalSettings(ctx) store.PersonalSettingsStore`
  - `router.SessionPin(ctx, sessionID) (*store.SessionPin, error)`
  - `router.SetSessionPin(ctx, sessionID, provider, model string) error`
  - `router.AppPin(ctx, appSlug) (*store.AppPin, error)`
  - `router.SetAppPin(ctx, appSlug, provider, model string) error`
  `SessionPin`/`AppPin` are small `struct{ Provider, Model string }` types. Empty strings = clear. Sessions in personal or team scope are routed to the right ent client. Must-NOT: do NOT bypass the router by opening a raw ent client; do NOT introduce a new interface in `pkg/provider`; do NOT couple `pkg/provider` to `pkg/store/entstore`.
  Parallelization: Wave 2 | Blocked by: 3, 4, 5, 6 | Blocks: 10, 11, 12
  References:
    - pkg/store/entstore/AGENTS.md — router contract, six enforcement points
    - pkg/store/entstore/personal_sessions.go, pkg/store/entstore/team_sessions.go — shape for session store methods
    - pkg/store/entstore/tenant_router.go — where to add the new accessors
  Acceptance criteria:
    - `go test ./pkg/store/entstore/... -v` — existing tests pass.
    - New unit test in `pkg/store/entstore/personal_settings_test.go` covers Get/Save round-trip on a scratch SQLite DB.
    - New unit test `pkg/store/entstore/session_pin_test.go` covers set → get → clear (empty string) → get returns empty on both personal and team session stores.
  QA scenarios:
    - Happy: `go test -v ./pkg/store/entstore 2>&1 | tee .omo/evidence/task-7-per-chat-app-model-pin.log`
    - Failure: `router.SetSessionPin(ctx, "nonexistent-id", "openai", "gpt-4o")` — expect a wrapped error like "session not found", not a silent no-op.
    Evidence: `.omo/evidence/task-7-per-chat-app-model-pin.log`
  Commit: Y | `feat(entstore): expose PersonalSettings + session/app pin accessors on tenant router`

- [x] 8. `pkg/provider` overlay chain applied at all production `ResolveEffectiveConfig` call sites (grep-verified count at execution time; baseline is 9)
  What to do: FIRST run `grep -RIn "ResolveEffectiveConfig(" pkg/ --include="*.go" | grep -v _test.go` and record the exact count + file list in the evidence log. Then for every caller, immediately after `ResolveEffectiveConfig(...)`, chain:
  ```go
  cfg = provider.ApplyUserDefault(cfg, userSettings) // only where user is in scope
  cfg = provider.ApplyProviderOverride(cfg, pinnedProvider, pinnedModel)
  ```
  Grep-baseline list from 2026-07-09 (verify against your fresh grep — the plan is NOT the source of truth for this list):
  - `pkg/api/chat_handlers.go:1094` — StudioChatHandler
  - `pkg/api/request_helpers.go:238, 277` — helper called by other handlers
  - `pkg/channels/manager.go:646, 970` — inbound channel routing
  - `pkg/daemon/run.go:2048` — daemon bootstrap
  Test files (`pkg/provider/resolve_test.go`, `pkg/api/integration_multitenant_test.go`) get updated fixtures — pass empty overrides so existing assertions still pass — but do NOT chain the overlay production-style in tests.
  For channels + daemon paths where "session" is inbound-message-scoped, use the session pin from the routed session ID; for channel-startup paths where no session exists, apply only UserDefault.
  **Missing-credential behavior (read path):** if the pinned provider has no credential on the current team, log a `slog.Warn(...)` and let the cascade result stand in-memory — DO NOT clear or reset the persisted pin. The pin stays; only the runtime cfg falls back. (Pin clearing is user-driven only, per Must-NOT-Have.)
  Must-NOT: do NOT introduce a new resolver function; do NOT modify `pkg/apps/*` (no `ResolveEffectiveConfig` callers exist there — the app pin overlay lives at `pkg/api/ai_chat_handler.go`, handled in Todo 20's backend wiring); do NOT clear the persisted pin on missing credential.
  Parallelization: Wave 2 | Blocked by: 2, 6 | Blocks: 10
  References:
    - Grep-verified list above (re-verify at execution)
    - pkg/provider/resolve.go — new overlay helpers from Todo 2
    - pkg/api/chat_handlers.go — sessionID is already in scope at StudioChatHandler
    - pkg/api/ai_chat_handler.go — app pin overlay lands HERE (not in `pkg/apps/`)
  Acceptance criteria:
    - `go build ./...` clean.
    - `go test ./pkg/api ./pkg/channels ./pkg/daemon -v` — all pass.
    - `grep -RIn "ResolveEffectiveConfig(" pkg/ --include="*.go" | grep -v _test.go | wc -l` returns N (record N in evidence); each such line is followed within 5 lines by `ApplyProviderOverride` and (where user is in scope) `ApplyUserDefault`. Run `grep -A5 "ResolveEffectiveConfig(" pkg/**/*.go | grep -c "ApplyProviderOverride"` — expect ≥ N (once per prod caller, minus test files).
  QA scenarios:
    - Happy: unit test in `pkg/api/chat_handlers_test.go` — chat handler + pinned session returns cfg with pinned model.
    - Failure: chat handler + session pinned to missing-credential provider → returned cfg falls back to team default, warning logged (assert via `slog` capture in the test), **persisted pin unchanged** (query the DB and assert `provider_name` still set).
    Evidence: `.omo/evidence/task-8-per-chat-app-model-pin.log`
  Commit: Y | `feat(provider): chain user-default and per-session/app overlays at all ResolveEffectiveConfig call sites`

- [x] 9. `pkg/launcher/chat_factory.go` + `pkg/api/chat_handlers.go`: expose Swap on SwappableLLM via a request-scoped handle so PATCH /model can hot-swap
  What to do: `NewWiredChatAgent` already stores `SwappableLLM` on the returned `ChatFactoryResult` (line 67). The Studio chat runner (`pkg/api/chat_runner.go`) already holds this per-session. Extend the per-session state to expose:
  ```go
  func (s *SessionState) SwapLLM(ctx context.Context, provider, model string) error
  ```
  Implementation: call `provider.GetProvider(ctx, provider, model, s.cfg)` to build the new `model.LLM`, then `s.result.SwappableLLM.Swap(newLLM)`. Sub-agents inherit because `subAgentMgr.LLM` is the same wrapper (chat_factory.go:1338 — confirmed in grounding). Must-NOT: do NOT rebuild the ChatAgent; do NOT tear down tools/MCP/sandbox; do NOT swap if the resolved provider has no credential — return an error the handler can surface as a 409 + banner signal.
  Parallelization: Wave 2 | Blocked by: 2, 6 | Blocks: 10
  References:
    - pkg/provider/swappable_llm.go:18-59 — Swap primitive
    - pkg/launcher/chat_factory.go:67 (SwappableLLM in result), 178 (wrapper creation), 1338 (sub-agent capture)
    - pkg/api/chat_runner.go — per-session state
    - pkg/provider/factory.go — GetProvider signature (context + provider + model + cfg)
  Acceptance criteria:
    - New unit test in `pkg/provider/swappable_llm_test.go` (extend existing) — Swap + subsequent GenerateContent uses the new LLM.
    - Integration test in `pkg/api/chat_handlers_test.go` — call `SwapLLM("openai", "gpt-4o-mini")` on an active session, assert next request logs the new model name.
  QA scenarios:
    - Happy: swap succeeds, next request uses new model.
    - Failure: swap to a provider with no credential → returns error containing "credential", session state unchanged.
    Evidence: `.omo/evidence/task-9-per-chat-app-model-pin.log`
  Commit: Y | `feat(chat): expose SwapLLM on session state for live model switching`

- [x] 10. `pkg/api/chat_handlers.go`: add `PATCH /api/sessions/{id}/model` + SSE `model_changed` event
  What to do: New handler `PatchSessionModelHandler`. Body: `{"provider": "...", "model": "..."}` (both empty = clear). Steps:
  1. `TenantMiddleware` already gates access.
  2. Validate: provider must exist in the resolved cfg (or empty for clear).
  3. Persist via `router.SetSessionPin(ctx, sessionID, provider, model)`.
  4. If there's an active session in `chat_runner`, call `SessionState.SwapLLM(...)`.
  5. On success, if runner is active, emit SSE `data: {"type":"model_changed","provider":"...","model":"..."}` on the session's existing stream.
  6. Response: 200 + `{effectiveProvider, effectiveModel, pinnedProvider, pinnedModel, credentialsAvailable}`.
  Handle missing credentials: persist the pin, do NOT swap, return 200 with `credentialsAvailable: false` + warning message. Frontend shows the banner.
  Register on the API mux next to existing session routes.
  Must-NOT: do NOT emit `model_changed` if runner isn't active (harmless but noisy); do NOT bypass `TenantMiddleware`; do NOT allow cross-tenant session ID (router already enforces).
  Parallelization: Wave 3 | Blocked by: 7, 8, 9 | Blocks: 17
  References:
    - pkg/api/chat_handlers.go — existing session handlers as shape reference
    - pkg/api/chat_runner.go — where to look up active session state
    - docs/architecture/chat-rendering-pipeline.md — SSE event conventions (do NOT loosen anything; ONLY add a new control event)
    - pkg/api/AGENTS.md — tenant middleware contract
  Acceptance criteria:
    - `go test ./pkg/api -v -run TestPatchSessionModel` — passes for happy, missing-cred, and cross-tenant rejection cases.
    - `curl -X PATCH /api/sessions/<id>/model -d '{"provider":"openai","model":"gpt-4o"}'` (via test harness) returns 200 with the expected body.
    - SSE `model_changed` event appears in the active stream within 1s.
  QA scenarios:
    - Happy: pin + swap + subsequent chat message uses new model (assert via runner log or LLM name).
    - Failure: pin to provider with no credential — 200 + `credentialsAvailable: false` + no `model_changed` event.
    Evidence: `.omo/evidence/task-10-per-chat-app-model-pin.log`
  Commit: Y | `feat(api): PATCH /api/sessions/{id}/model with live SwappableLLM swap`

- [x] 11. `pkg/api/apps_handlers.go`: add `PATCH /api/apps/{slug}/model` + expose pin on `GET /api/apps/{slug}`
  What to do: Handler `PatchAppModelHandler` — same shape as Todo 10 but persists via `router.SetAppPin(ctx, slug, provider, model)`. `GET /api/apps/{slug}` response gains `pinnedProvider`, `pinnedModel`, `effectiveProvider`, `effectiveModel` fields. No live swap (apps rebuild their config per call).
  Must-NOT: do NOT add per-session app pin (out of scope); do NOT change `useAppData` behavior; do NOT allow overriding a team-published app's pin from a personal-scope caller (route uses the app's own scope).
  Parallelization: Wave 3 | Blocked by: 7, 8 | Blocks: 18
  References:
    - pkg/api/apps_handlers.go — existing GET/list handlers
    - pkg/apps/EffectiveAppConfigFromContext — where the pin is applied
    - pkg/api/AGENTS.md
  Acceptance criteria:
    - `go test ./pkg/api -v -run TestPatchAppModel|TestGetAppExposesPin` — passes.
  QA scenarios:
    - Happy: PATCH app pin → next `useAppAI` call proxied through backend picks up the new model.
    - Failure: PATCH with unknown slug → 404; PATCH cross-tenant → 403.
    Evidence: `.omo/evidence/task-11-per-chat-app-model-pin.log`
  Commit: Y | `feat(api): per-app model pin (PATCH /apps/{slug}/model, GET exposes pin)`

- [x] 12. `pkg/api/user_settings_handlers.go` (new): GET/PATCH `/api/user-settings/default-model`
  What to do: Two handlers backed by `router.PersonalSettings(ctx)`. GET returns `{defaultProvider, defaultModel}`; PATCH persists. Empty strings clear.
  Must-NOT: do NOT create org/team versions; do NOT allow one user to read another's settings (personal scope + tenant middleware enforce this).
  Parallelization: Wave 3 | Blocked by: 7, 8 | Blocks: 19
  References:
    - pkg/api/settings_handlers.go — pattern for GET/PATCH settings
    - pkg/store/entstore/personal_settings.go from Todo 3
  Acceptance criteria:
    - `go test ./pkg/api -v -run TestUserSettingsDefaultModel` — passes.
  QA scenarios:
    - Happy: PATCH → GET returns updated value.
    - Failure: PATCH with malformed body → 400.
    Evidence: `.omo/evidence/task-12-per-chat-app-model-pin.log`
  Commit: Y | `feat(api): user-scoped default model (GET/PATCH /api/user-settings/default-model)`

- [x] 13. `pkg/api/chat_handlers.go`: `GET /api/sessions/{id}/model-status` (credential check + effective cfg)
  What to do: Returns `{pinnedProvider, pinnedModel, effectiveProvider, effectiveModel, credentialsAvailable, availableProviders: [...]}`. `availableProviders` is the list of providers with a credential on the current team (from existing `provider_settings_handlers.go` helper). Frontend uses this to render the picker + banner.
  Must-NOT: do NOT expose credential values; do NOT bypass tenant middleware.
  Parallelization: Wave 3 | Blocked by: 7, 8, 9 | Blocks: 20
  References:
    - pkg/api/provider_settings_handlers.go — provider list source
    - pkg/credentials/store.go — credential lookup
  Acceptance criteria:
    - `go test ./pkg/api -v -run TestSessionModelStatus` — passes.
  QA scenarios:
    - Happy: pinned + credentialed → `credentialsAvailable: true`, effective = pinned.
    - Failure: pinned + no credential → `credentialsAvailable: false`, effective = team default.
    Evidence: `.omo/evidence/task-13-per-chat-app-model-pin.log`
  Commit: Y | `feat(api): GET /api/sessions/{id}/model-status with credential check`

- [x] 14. `cmd/astonish/chat.go`: pin-by-default semantics + `--no-pin` + `--clear-model` + `--resume` behavior
  What to do:
  - New session (no `--resume`): if `-p` or `-m` provided, persist the pin on the created session AFTER session creation returns an ID, UNLESS `--no-pin` was passed.
  - Resumed session (`--resume <id>`): `-p`/`-m` override the effective cfg for THIS INVOCATION ONLY. Do NOT call `SetSessionPin` unless the user also passed a new `--pin` flag (edge case; keep AC #3 literal).
  - `--clear-model`: on resumed session, calls `SetSessionPin(ctx, id, "", "")` before starting the runner.
  Update `--help` text (line 114-126 of the current file) to document the new flags. Confirm no `--dry-run` flag exists today (`grep -n "dry-run" cmd/astonish/chat.go` → none) — do NOT invent one for testing; use the `cmd/astonish/chat_test.go` harness (see `chat_test.go` next to the existing chat.go — if none, create it with `_test.go` scaffolding mirroring `cmd/astonish/*_test.go` neighbors) which invokes the flag-parsing and session-pin call paths directly via exported helpers or `NewChatCommand()` cobra construction, and asserts on state (pin written / not written / cleared) instead of stdout strings.
  Must-NOT: do NOT persist the pin on `--resume` unless explicit; do NOT print secrets in the confirmation line; do NOT introduce a hidden env var; do NOT invent `--dry-run` in production code just to make tests easier.
  Parallelization: Wave 4 | Blocked by: 8, 9 | Blocks: 22
  References:
    - cmd/astonish/chat.go:39-126 — current flag block + help
    - cmd/astonish/AGENTS.md — CLI dispatch rules, `mustBeRemote`/`mustNotBeRemote`
    - pkg/launcher/chat_console.go — where new-session ID is returned from
    - existing `cmd/astonish/*_test.go` — for the test scaffolding pattern (if any exist)
  Acceptance criteria:
    - Test `TestChatFlags_PinByDefault_NewSession` (in `cmd/astonish/chat_test.go`): invoke the flag-parsing + factory-build path with `-p openai -m gpt-4o`, assert `SetSessionPin(id, "openai", "gpt-4o")` was called.
    - Test `TestChatFlags_NoPin`: same inputs + `--no-pin`, assert `SetSessionPin` NOT called.
    - Test `TestChatFlags_ResumeOverride`: `--resume <id> -m gpt-4o-mini`, assert LLM built with "gpt-4o-mini" AND `SetSessionPin` NOT called.
    - Test `TestChatFlags_ClearModel`: `--clear-model --resume <id>`, assert `SetSessionPin(id, "", "")` was called.
    - Test `TestChatFlags_ClearModelWithoutResume`: `--clear-model` alone → returns error "requires --resume".
    Use a fake `SessionPinStore` interface (existing `entstore` router or a mock) to observe calls. No live LLM calls needed.
  QA scenarios:
    - Happy: all four flag combinations produce the correct persistence effect via the fake store.
    - Failure: `--clear-model` without `--resume` → error message matches "requires --resume" (exact string).
    Evidence: `.omo/evidence/task-14-per-chat-app-model-pin.log`
  Commit: Y | `feat(cli): pin-by-default -p/-m, --no-pin, --clear-model, resume overrides ephemeral`

- [x] 15. `cmd/astonish/chat.go`: new subcommand `astonish chat model <provider>:<model>`
  What to do: Sub-parser under `chat`. Argument format `provider:model` (colon-separated), or empty to clear. Reads the most recent session ID (from local state file `~/.config/astonish/last-session`), calls PATCH `/api/sessions/{id}/model` (platform mode) or `router.SetSessionPin` directly (personal mode). Prints effective cfg.
  Must-NOT: do NOT touch sessions that are not the last one (accept `--session <id>` optional flag if needed for scripting); do NOT invent a new persistence format.
  Parallelization: Wave 4 | Blocked by: 8, 9 | Blocks: 22
  References:
    - cmd/astonish/chat.go — existing subcommand style (if any); if none, mirror `astonish flows` sub-parser style
    - pkg/launcher/chat_console.go — where last-session ID is written
  Acceptance criteria:
    - `astonish chat model openai:gpt-4o` — succeeds, prints new effective.
    - `astonish chat model ""` — clears pin.
    - `astonish chat model invalid` (no colon) → error "expected provider:model".
  QA scenarios:
    - Happy: set + clear round-trip.
    - Failure: invalid format → non-zero exit + clear error.
    Evidence: `.omo/evidence/task-15-per-chat-app-model-pin.log`
  Commit: Y | `feat(cli): astonish chat model <provider>:<model> subcommand`

- [x] 16. `pkg/channels/manager.go`: honor session pin on inbound channel messages + missing-cred fallback
  What to do: In `handleInbound`, after routing to the session, apply the pin via the overlay chain from Todo 8 (which is already done at that call site). Add a channel-specific slog warning if the pin's provider has no credential on the current team; message still gets processed with cascade fallback.
  Must-NOT: do NOT add a channel-level pin override; do NOT drop messages when credentials are missing.
  Parallelization: Wave 4 | Blocked by: 8, 9 | Blocks: 22
  References:
    - pkg/channels/manager.go:handleInbound
    - pkg/channels/AGENTS.md
    - Todo 8 covers the overlay chain at this call site; Todo 16 is the fallback UX + warning.
  Acceptance criteria:
    - `go test ./pkg/channels -v` — passes, including a new test that asserts warning log + cascade fallback.
  QA scenarios:
    - Happy: channel-delivered message on pinned session uses pinned model.
    - Failure: channel-delivered message + missing credential → cascade fallback, warning logged.
    Evidence: `.omo/evidence/task-16-per-chat-app-model-pin.log`
  Commit: Y | `feat(channels): honor session model pin on inbound messages, warn on missing credential`

- [x] 17. `web/src/components/StudioChat.tsx`: model picker in the header + SSE `model_changed` handling
  What to do:
  - Add a small dropdown to the header near the existing token counter / session title. Populated from `GET /api/sessions/{id}/model-status` (Todo 13).
  - On change, PATCH the session model.
  - Subscribe to SSE: on `model_changed` event, update local state (this covers the case where a different client changed the model).
  - Show the currently-active model at all times (fetch on session load, refresh on change).
  Must-NOT: do NOT introduce Redux/Zustand (per project convention); do NOT change the ChatMsg union (docs/architecture/chat-rendering-pipeline.md constraint); do NOT auto-open the picker on session load.
  Parallelization: Wave 5 | Blocked by: 10, 13 | Blocks: 22, 23
  References:
    - web/src/components/StudioChat.tsx:190+ — session state + header area
    - web/src/api/studioChat.ts — where fetchSessionHistory lives; add `patchSessionModel`, `fetchSessionModelStatus`
    - docs/architecture/chat-rendering-pipeline.md — Inline Report Rendering Contract stays untouched; ONLY a new SSE control event
    - web/src/AGENTS.md — TSX/JSX conventions
  Acceptance criteria:
    - Vitest test in `web/src/components/StudioChat.test.tsx` (or existing test file for StudioChat) — picker renders, PATCH called on change, `model_changed` event updates state.
    - `cd web && npm run build` succeeds (tsc + Vite).
    - `cd web && npm run lint` clean.
  QA scenarios:
    - Happy: change model → PATCH fires → picker reflects new model.
    - Failure: PATCH returns 200 + `credentialsAvailable: false` → banner appears (Todo 20 renders it, but the state is set here).
    Evidence: `.omo/evidence/task-17-per-chat-app-model-pin.log`
  Commit: Y | `feat(studio): model picker in chat header with live swap and model_changed SSE`

- [x] 18. `web/src/components/AppsPanel.tsx`: per-app model picker
  What to do: In each app row (or the app detail view), add a small picker next to the existing controls. Reads `GET /api/apps/{slug}` (extended in Todo 11); PATCH on change.
  Must-NOT: do NOT change app-list layout more than necessary; do NOT block app launch when pin has no credentials.
  Parallelization: Wave 5 | Blocked by: 11 | Blocks: 22, 23
  References:
    - web/src/components/AppsPanel.tsx (existing per-app cards)
    - web/src/api/apps.ts — add `patchAppModel`
  Acceptance criteria:
    - Vitest in `web/src/components/AppsPanel.test.tsx` — picker renders, PATCH fires.
    - `npm run build` clean.
  QA scenarios:
    - Happy: change app model → PATCH fires → picker shows new value.
    - Failure: PATCH returns 4xx → error toast, picker reverts.
    Evidence: `.omo/evidence/task-18-per-chat-app-model-pin.log`
  Commit: Y | `feat(studio): per-app model picker in Apps tab`

- [x] 19. `web/src/components/settings/*`: user-default picker in Settings page
  What to do: New card in the Settings page → "Default Model" — populated from `GET /api/user-settings/default-model`. PATCH on change. Sits between Team defaults and Advanced.
  Must-NOT: do NOT reuse the team-defaults component (different semantics); do NOT show it in personal-mode-only builds if that's out of scope for the layout (leave it visible — falls back gracefully).
  Parallelization: Wave 5 | Blocked by: 12 | Blocks: 22, 23
  References:
    - web/src/components/settings/ — existing settings card patterns
    - web/src/api/settings.ts — add `fetchUserDefaultModel`, `patchUserDefaultModel`
  Acceptance criteria:
    - Vitest for the new component; `npm run build` + `npm run lint` clean.
  QA scenarios:
    - Happy: set + save + reload → value persists.
    - Failure: PATCH 4xx → error inline.
    Evidence: `.omo/evidence/task-19-per-chat-app-model-pin.log`
  Commit: Y | `feat(studio): user default model picker in Settings`

- [x] 20. `web/src/hooks/useAppAI.ts` + fallback banner: app pin is honored, banner surfaces when creds missing
  What to do:
  - `useAppAI` already sends `appSlug` in its request; ensure the backend proxy (`pkg/api/ai_chat_handler.go`) picks up the app pin via the overlay chain (this is already done by Todo 8 + 11).
  - Add a fallback banner component reused in `StudioChat.tsx` header AND in the App preview: when the session/app has a pin but backend reports `credentialsAvailable: false`, show a yellow banner "Model '<pinned>' unavailable — using '<effective>' instead. Pick another model."
  - Clicking "Pick another" opens the same picker from Todo 17/18.
  Must-NOT: do NOT block chat/app use; do NOT surface the banner when there's no pin at all.
  Parallelization: Wave 5 | Blocked by: 11, 13 | Blocks: 22, 23
  References:
    - web/src/hooks/useAppAI.ts
    - pkg/api/ai_chat_handler.go (backend path — already receives the overlay from Todo 8)
  Acceptance criteria:
    - Vitest for the banner: mount with `credentialsAvailable: false` + pin set → banner appears; `credentialsAvailable: true` OR no pin → banner absent.
    - `npm run build` + `npm run lint` clean.
  QA scenarios:
    - Happy: no banner when creds present; banner + one-click open picker when absent.
    - Failure: PATCH-from-banner path (via picker) closes banner on success.
    Evidence: `.omo/evidence/task-20-per-chat-app-model-pin.log`
  Commit: Y | `feat(studio): missing-credential banner + useAppAI honors app pin`

- [x] 21. Docs: agent-engine.md cascade section, cli/chat.md flag reference, chat-rendering-pipeline.md new SSE event
  What to do:
  - `docs/architecture/agent-engine.md`: update the "Provider Resolution" section to show the new 5-tier cascade (Platform → Org → Team → UserDefault → Chat/App override) and reference `ApplyUserDefault` / `ApplyProviderOverride`.
  - `docs/website/docs/cli/chat.md`: document `-p`, `-m` pin-by-default behavior, `--no-pin`, `--clear-model`, `astonish chat model <provider>:<model>`, and the `--resume` interaction.
  - `docs/architecture/chat-rendering-pipeline.md`: **one line** in the SSE event list adding `model_changed`. Do NOT touch the Inline Report Rendering Contract section.
  Must-NOT: do NOT rewrite unrelated sections; do NOT add screenshots; do NOT introduce emojis (project convention: only when explicitly asked).
  Parallelization: Wave 6 | Blocked by: 14, 17 | Blocks: —
  References:
    - docs/architecture/agent-engine.md
    - docs/website/docs/cli/chat.md
    - docs/architecture/chat-rendering-pipeline.md
    - docs/architecture/AGENTS.md — invariant registry (verify no contract wording is loosened)
  Acceptance criteria:
    - `grep -RIn "UserDefault\|--no-pin\|--clear-model\|model_changed" docs/` returns matches in all three files.
    - `cd docs/website && npm run build` (if the docs site uses this target) — no broken links.
  QA scenarios:
    - Happy: doc build clean; new terms grep-findable.
    - Failure: intentionally break a link → build fails (proves the check runs).
    Evidence: `.omo/evidence/task-21-per-chat-app-model-pin.log`
  Commit: Y | `docs: per-chat/app model pin + user default in cascade`

- [x] 22. Integration test: end-to-end pin + resume + missing-credential fallback (both SQLite and Postgres)
  What to do: New file `pkg/api/integration_pin_test.go` (or extend `integration_multitenant_test.go`). Cases:
  - Create session, PATCH pin to a credentialed provider, close session, reopen → effective model matches pin.
  - PATCH pin, revoke credential, reopen → effective model = team default, `credentialsAvailable: false`.
  - PATCH pin, PATCH clear (empty strings), reopen → effective model = team default, no banner.
  - Create app, PATCH app pin, reload app → effective model matches pin.
  - CLI: `astonish chat --resume <id> -m X` — does NOT rewrite pin; next open uses original pin.
  - User default: no session pin + user default set → user default wins over team.
  Must-NOT: do NOT skip Postgres (the team scope is production-critical); do NOT rely on external LLM calls (use the provider factory's mock or a stub).
  Parallelization: Wave 6 | Blocked by: 14, 15, 16, 17, 18, 20 | Blocks: 23
  References:
    - pkg/api/integration_multitenant_test.go — existing patterns for platform + org + team seed
    - tests/AGENTS.md — DSN + provider key requirements
    - Makefile.integration
  Acceptance criteria:
    - `ASTONISH_TEST_DSN=... make test-integration` — new tests pass.
    - `make test-integration` on SQLite (no DSN) — same tests pass in personal scope.
  QA scenarios:
    - Happy: all six cases green.
    - Failure: intentionally break the overlay (revert Todo 2) → tests turn red (proves they cover the behavior).
    Evidence: `.omo/evidence/task-22-per-chat-app-model-pin.log`
  Commit: Y | `test(integration): end-to-end pin, resume, missing-credential across scopes`

- [x] 23. e2e scenario: user flow — pick model, refresh, resume, missing-cred banner
  What to do: New scenario under `tests/scenarios/pin_model.yaml` + Node reporter under `tests/scenarios/pin_model.mjs`. Steps:
  1. Boot studio via e2eboot harness.
  2. Create a session, chat one message.
  3. Change model via header picker (PATCH).
  4. Reload the page, assert picker shows the pinned model.
  5. Revoke the pinned provider's credential.
  6. Reload, assert banner appears + effective model = team default.
  7. Click "Pick another" from banner, select a credentialed model, assert banner dismissed.
  Must-NOT: do NOT bake in provider-specific API keys — use the mock provider path; do NOT reuse the existing `chat_core` scenario (this is a new one).
  Parallelization: Wave 6 | Blocked by: 22 | Blocks: —
  References:
    - tests/AGENTS.md — scenario authoring
    - tests/scenarios/ — existing YAML + MJS pairs
    - tools/e2e-inspector — for post-run state inspection during dev
  Acceptance criteria:
    - `make test-e2e-sqlite` (or `make test-e2e`) with `SCENARIOS=pin_model` — scenario passes.
    - Existing scenarios still pass (no regression).
  QA scenarios:
    - Happy: all 7 steps green.
    - Failure: revoke credential + reload — assert banner text matches exactly (this proves Todo 20's banner wiring).
    Evidence: `.omo/evidence/task-23-per-chat-app-model-pin.log`
  Commit: Y | `test(e2e): pin model, refresh, missing-credential banner scenario`

## Final verification wave
> Runs in parallel after ALL todos. ALL must APPROVE. Surface results and wait for the user's explicit okay before declaring complete.

- [x] F1. Plan compliance audit (`momus` subagent, read-only)
  Verify every acceptance criterion in Todos 1-23 has evidence under `.omo/evidence/task-<N>-per-chat-app-model-pin.*`. Cross-check that Must-NOTs were actually respected (no `ResolveForSession`, no `metadata` JSON storage, no widened resolver signature, no live CLI mid-turn swap, no per-channel pin field, no hard-fail on missing credential). Report: PASS / FAIL with pinpoint file:line for any deviation.

- [x] F2. Code quality review (`oracle` subagent, read-only)
  Focus on: (a) overlay purity (no side effects beyond mutating `cfg.General`); (b) SwappableLLM.Swap thread-safety in the new PATCH handler; (c) tenant middleware coverage on all four new API surfaces; (d) migration correctness on both SQLite and Postgres. Report: any hidden coupling or race condition + remediation.

- [x] F3. Real manual QA (agent-executed happy path via `explore` running the app)
  Run: `make studio-dev`, open the app, sign in, open a chat, change the model via picker, send a message, verify the model in the runtime log matches the pin, refresh the page, verify picker still shows the pin, disable the credential, verify banner. Capture screenshots into `.omo/evidence/task-F3-per-chat-app-model-pin.png`. Report: pass/fail with observed behavior deltas.

- [x] F4. Scope fidelity check (`momus` or `explore`, read-only)
  Diff of what was added vs. Scope IN / Scope OUT lists in this plan. Report any scope creep (e.g., an accidentally-added per-channel field, a widened resolver signature, a touched `useAppData`) with pinpoint file:line.

## Commit strategy

Commits are grouped by concern (not by todo), one atomic commit per group:

1. `test(provider): red-state overlay tests for user-default and per-session pin` (Todo 1)
2. `feat(provider): add ApplyUserDefault and ApplyProviderOverride overlays` (Todo 2)
3. `feat(store): PersonalSettings store contract + entstore impl` (Todo 3)
4. `feat(ent,migrate): add provider_name/model_name pin fields to sessions, apps, personal_settings` (Todos 4+5+6 atomically, per pre-commit hook)
5. `feat(entstore): expose PersonalSettings + session/app pin accessors on tenant router` (Todo 7)
6. `feat(provider): chain user-default and per-session/app overlays at all ResolveEffectiveConfig call sites` (Todo 8)
7. `feat(chat): expose SwapLLM on session state for live model switching` (Todo 9)
8. `feat(api): PATCH /api/sessions/{id}/model with live SwappableLLM swap` (Todo 10)
9. `feat(api): per-app model pin (PATCH /apps/{slug}/model, GET exposes pin)` (Todo 11)
10. `feat(api): user-scoped default model (GET/PATCH /api/user-settings/default-model)` (Todo 12)
11. `feat(api): GET /api/sessions/{id}/model-status with credential check` (Todo 13)
12. `feat(cli): pin-by-default -p/-m, --no-pin, --clear-model, resume overrides ephemeral` (Todo 14)
13. `feat(cli): astonish chat model <provider>:<model> subcommand` (Todo 15)
14. `feat(channels): honor session model pin on inbound messages, warn on missing credential` (Todo 16)
15. `feat(studio): model picker in chat header with live swap and model_changed SSE` (Todo 17)
16. `feat(studio): per-app model picker in Apps tab` (Todo 18)
17. `feat(studio): user default model picker in Settings` (Todo 19)
18. `feat(studio): missing-credential banner + useAppAI honors app pin` (Todo 20)
19. `docs: per-chat/app model pin + user default in cascade` (Todo 21)
20. `test(integration): end-to-end pin, resume, missing-credential across scopes` (Todo 22)
21. `test(e2e): pin model, refresh, missing-credential banner scenario` (Todo 23)

Pre-commit hook runs `golangci-lint` and Atlas migration integrity for the schema commit. No commit uses `--no-verify`.

## Success criteria

The feature is DONE when ALL of the following hold:

1. **Studio:** picking a model in the chat header persists; refreshing the page (or reopening the chat) shows the same model; the next agent turn uses it.
2. **Studio Apps:** picking a model in the Apps tab per-app persists; the app's `useAppAI` calls use it.
3. **CLI new session:** `astonish chat -p X -m Y` (without `--no-pin`) persists the choice onto the new session; a follow-up `astonish chat --resume <id>` (no flags) picks up the same model.
4. **CLI resume:** `astonish chat --resume <id> -m X` overrides for THIS run only and does NOT rewrite the pin.
5. **CLI clear:** `astonish chat --clear-model --resume <id>` and `astonish chat model ""` both restore cascade behavior.
6. **Missing-credential fallback:** revoking the credential for a pinned provider surfaces a warn banner in Studio and a stderr warning in CLI; the session still opens with the cascade default; the pin is NOT auto-cleared.
7. **Regression:** sessions/apps with no pin behave identically to pre-change; `pkg/provider/resolve_test.go`, `pkg/api/integration_multitenant_test.go`, and existing e2e scenarios pass unmodified.
8. **Migrations:** `atlas migrate hash --env personal_lite && ... team_lite && ... team_pg` all exit 0; pre-commit hook passes; migrations run cleanly against a fresh SQLite and a fresh Postgres.
9. **Docs:** `docs/architecture/agent-engine.md`, `docs/website/docs/cli/chat.md`, and `docs/architecture/chat-rendering-pipeline.md` reflect the new cascade + flags + SSE event.
10. **Final wave:** F1, F2, F3, F4 all report PASS. Any FAIL blocks completion.

Total todos: 23 (+ 4 final verification). Estimated effort: Medium. Risk driver: ~9 resolver call sites need consistent overlay + fallback handling (grep-verified at Todo 8) — F1 exists to catch drift.
