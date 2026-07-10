# Issues / Gotchas — per-chat-app-model-pin

## [2026-07-10] Known gotchas from plan review

- Caller count was wrong in original plan draft (said 13, grep shows 9). Plan corrected. Executor re-verifies at Todo 8.
- pkg/apps has ZERO ResolveEffectiveConfig callers. App overlay goes at pkg/api/ai_chat_handler.go.
- No --dry-run flag. Do NOT invent it. Use test harness with fake store.
- Missing-cred contradiction fixed: Todo 8 read-path = cascade wins in-memory, pin stays. Todo 10 PATCH = persist pin, no swap, credentialsAvailable:false.
- PersonalSettings user_id ambiguity in personal-mode SQLite. Use uuid.Nil sentinel OR singleton row. Record decision in commit msg.
- Ent regen + migration MUST be one atomic commit. Pre-commit hook validates Atlas hash.
- Atlas names migration files. Do NOT hand-craft the timestamp prefix.
- Fleet sessions inherit pins naturally via the same factory. No special fleet code path needed.

## [2026-07-09] Todo 6 — Atlas migration infrastructure does not exist in this repo

- The plan's Todo 6 assumed an Atlas-based migration flow (`schema/`, `pkg/store/{pgstore,sqlitestore}/migrations/`, `atlas.hcl`, `make migrate-diff`). **None of these exist in the current codebase.** `docs/architecture/migrations.md` and `.githooks/pre-commit`'s Atlas hash validation describe an aspirational future state.
- Reality: `pkg/store/entstore/{tenant_router.go, bootstrap.go}` uses **ent's runtime `client.Schema.Create(ctx)` (auto-migration)** at every startup. Schema changes are picked up automatically from the generated ent code — no SQL files, no atlas.sum, no atlas CLI in use.
- Decision (confirmed with user): **Option A** — adapt Todo 6 scope. Skip Atlas steps entirely. Do NOT create migration infrastructure. Only run ent codegen + strip build tag + `go build`.
- Also fixed: `ent/personal/schema/personal_settings.go` — `field.UUID("user_id", uuid.UUID{}).Default(uuid.Nil)` failed ent codegen with `expect type (func() uuid.UUID) for uuid default value`. Wrapped as `Default(func() uuid.UUID { return uuid.Nil })`. DECISION-1's `uuid.Nil` semantics preserved.
- No pre-commit Atlas hash mismatch occurred because zero `.sql` files were touched; hook is a no-op for this Todo (and skips silently when `atlas` CLI is absent, which it is on this machine).
- Rule for future Todos: treat the Atlas-migration language in the plan and in AGENTS.md as documentation-only until the infrastructure is actually built. That build-out is its own plan.

## [2026-07-09] Task 8 — mocks in pkg/api tests are drift-prone

- `pkg/api/app_sharing_handlers_test.go` had a compile-broken `mockTeamDataStore`/`mockPersonalDataStore` because prior Todos (5/6/7) added `SessionPin/SetSessionPin/AppPin/SetAppPin/PersonalSettings` to the store interfaces without updating this mock. This is a general risk: any future interface-adding Todo must sweep the mocks in `pkg/api/` (and any other test that hand-rolls a store mock) in the same commit.
- Fix pattern: return `nil, nil` from Get-shaped methods and `nil` from Set-shaped methods on the mock. Behavior-preserving — the tests don't exercise pin paths yet.
