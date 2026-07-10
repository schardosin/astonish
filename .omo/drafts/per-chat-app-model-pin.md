---
slug: per-chat-app-model-pin
status: awaiting-approval
intent: clear
review_required: false
pending-action: write .omo/plans/per-chat-app-model-pin.md
approach: |
  Extend the 3-tier provider cascade (Platform→Org→Team) with two new innermost layers:
    (a) a per-user default in the personal scope, and
    (b) a per-session/per-app pin.
  Store the pins as first-class nullable string fields on Session and App ent schemas
  (both scopes) plus a new PersonalSettings row for the user default. Add a small
  overlay helper `ApplyProviderOverride(cfg, pin) *config.AppConfig` that layers on
  top of the existing `ResolveEffectiveConfig` at each of the ~4 places that build a
  per-request config (chat_handlers, apps EffectiveAppConfigFromContext,
  channels/manager, CLI console). Use the existing SwappableLLM to hot-swap in
  Studio; in the CLI console persist-only (applies next turn). CLI flags pin by
  default; add `--no-pin` and `--clear-model` opt-outs. Missing-credential path:
  warn banner + fall back to cascade, one-click pick-another.
---

# Draft: per-chat-app-model-pin

## Components (topology ledger)
- schema-persistence   | Session + App ent schemas gain nullable provider_name/model_name; new PersonalSettings row for user default. Regen ent, add Atlas migrations for SQLite + Postgres.        | active | pkg/store/entstore/*/migrations, ent/{personal,team}/schema/{session,app}.go
- resolver-overlay     | `ApplyProviderOverride(cfg, pin)` + `ApplyUserDefault(cfg, userSettings)` overlays applied to the result of ResolveEffectiveConfig at call sites; resolver signature unchanged. | active | pkg/provider/resolve.go (new sibling funcs), 13 call sites
- runtime-wiring       | ChatFactoryConfig already carries ProviderName/ModelName; add PinnedProvider/PinnedModel on chat_handlers.buildFactoryCfg path; SwappableLLM.Swap on API model-change; NewWiredChatAgent unchanged. | active | pkg/launcher/chat_factory.go:37-60,178, pkg/api/chat_handlers.go
- studio-api           | New endpoints: PATCH /api/sessions/{id}/model, PATCH /api/apps/{slug}/model, GET /api/sessions/{id}/model-status (credential check). SSE emits `model_changed` event. | active | pkg/api/chat_handlers.go, pkg/api/apps_handlers.go
- studio-ui            | Model picker in StudioChat header + Apps tab per-app; picker sources from provider_settings_handlers list of credentialed providers; warning banner when pin fails credential check. | active | web/src/components/StudioChat.tsx, web/src/components/AppsPanel.tsx, web/src/api/*.ts
- cli                  | `astonish chat -p X -m Y` pins by default; `--no-pin`, `--clear-model`; `astonish chat model <slug>` subcommand; `--resume -m X` does NOT rewrite pin. | active | cmd/astonish/chat.go
- app-runtime          | useAppAI reads app pin from app definition metadata surfaced via existing /api/apps endpoint; falls back to cascade. | active | web/src/hooks/useAppAI.ts, pkg/api/apps_handlers.go
- tests-and-docs       | Unit tests for overlay + missing-cred fallback; integration test for end-to-end pin+resume; e2e Studio scenario; docs updates to agent-engine.md, cli/chat.md. | active | pkg/provider/resolve_test.go, pkg/api/integration_multitenant_test.go, tests/scenarios, docs/*

## Open assumptions (announced defaults)
- assumption: SwappableLLM.Swap already covers sub-agents (they capture the same wrapper at wire time). Verified: pkg/launcher/chat_factory.go:1338 assigns `subAgentMgr.LLM = llm` where `llm` is the SwappableLLM. Reversible: yes, via explicit sub-agent LLM rebuild if the assumption breaks.
- assumption: Empty-string pin (== NULL in DB) means "inherit from cascade" (unset). Non-empty means pinned. This mirrors how existing DefaultProvider/DefaultModel work in each layer. Reversible: yes.
- assumption: The channels path (Slack/Telegram/Email) inherits the same behavior: a chat that originated via a channel and was pinned via /model on Studio uses the pin on the next channel-delivered message. No per-channel pin override. Reversible: extend later if requested.
- assumption: Flow / distilled runs are OUT of scope — flow YAML controls its own provider/model. Confirmed by user in issue text ("out of scope for this issue").
- assumption: Personal-mode (SQLite / single-user) does NOT need a per-user default layer to be meaningful because Platform+Org+Team collapse to the personal config. We add the PersonalSettings storage anyway so the code path is uniform, but in single-user SQLite it will effectively behave like a session-scoped default.

## Findings (cited - path:lines)
- pkg/provider/resolve.go:24-95 — `ResolveEffectiveConfig(ctx, platformSettings, orgSettings, teamSettings)` is the 3-tier cascade. `applyProviderLayer` merges providers additively; defaults override only if non-empty. This shape lets us add overlays without changing the signature.
- pkg/provider/swappable_llm.go:18-59 — `SwappableLLM` already exists with `Swap()`, protected by RWMutex. Its docstring explicitly names ChatManager model-swap as the use case ("support model changes without tearing down the entire chat agent"). This is a pre-built primitive.
- pkg/launcher/chat_factory.go:37-60 — `ChatFactoryConfig{ProviderName string; ModelName string; ...}` already carries the values needed. Line 167-178 constructs the LLM and wraps it in SwappableLLM. Line 1338 passes the same wrapper to SubAgentManager, so a Swap propagates to sub-agents.
- cmd/astonish/chat.go:39-50 — CLI flags `-p/--provider`, `-m/--model`, `-r/--resume` are ephemeral (passed to `SessionID`, `Provider`, `Model` in `RunChatConsole` params). No persistence.
- ent/personal/schema/session.go:21-66 — Session already has metadata JSON field (line 45); we will add `provider_name` and `model_name` as first-class nullable strings alongside it (user-chosen shape).
- ent/team/schema/session.go:21-70 — Same shape for team-scoped sessions. Both schemas are hand-editable per ent/AGENTS.md.
- ent/team/schema/app.go:20-58, ent/personal/schema/app.go — App schema has no metadata field; adding provider_name/model_name is the only path.
- pkg/api/chat_handlers.go — 13 total callers of `ResolveEffectiveConfig` project-wide (per codegraph blast radius). Each will need to call the overlay helper AFTER resolving. The overlay signature stays trivial: `ApplyOverride(cfg *config.AppConfig, pinnedProvider, pinnedModel string)` mutates only DefaultProvider/DefaultModel.
- pkg/provider/resolve_test.go, pkg/api/integration_multitenant_test.go — Existing cascade regression coverage. New tests must extend, not replace.
- ent/AGENTS.md — Schema edits require regeneration of everything under ent/<scope>/ and a migration in the same commit; pre-commit hook enforces Atlas integrity.
- pkg/store/entstore/AGENTS.md exists; per-scope migration folders under pkg/store/*/migrations/*.sql; changes to Session or App must add a migration in each affected scope (personal_sqlite, personal_pg is N/A since personal is SQLite only, team_sqlite, team_pg).
- docs/architecture/chat-rendering-pipeline.md — The header changes; ChatMsg union does NOT change (model swap is a session-scoped state, not a message type), so no SSE contract impact BEYOND a new `model_changed` control event.
- pkg/apps EffectiveAppConfigFromContext — Called by useAppAI proxy; will receive the app-pin overlay.

## Decisions (with rationale)
- **DECISION-1: User-level default layer YES.** Rationale: marginal cost is low because we're already touching the resolver; matches user instinct; makes CLI users first-class citizens on the platform. Cascade becomes Platform→Org→Team→UserDefault→Chat/App override.
- **DECISION-2: First-class ent fields for pins, NOT metadata JSON.** Rationale: queryable (e.g. "which sessions still pin claude-3-opus?"), migratable (rename provider slug requires DDL, not code), self-documenting, matches issue text. Cost is one small migration per scope.
- **DECISION-3: Missing-credential fallback = warn banner + cascade.** Rationale: never brick a session because of credential rotation; user retains one-click "pick another"; consistent semantics at Studio open, `--resume`, and channel-delivered paths.
- **DECISION-4: CLI mid-chat `/model` = persist-only, applies next turn.** Rationale: CLI console has one ADK runner per invocation; live in-turn swap is meaningful complexity and buys little (user typically re-runs after switch anyway). Studio still gets true live-swap via SwappableLLM.
- **DECISION-5: CLI `-p/-m` pin by default, `--no-pin` opt-out.** Rationale: the whole point of this feature is that users forget to re-pass flags; the sticky default matches the mental model. Backward-compat: `--no-pin` restores today's behavior for scripted callers.
- **DECISION-6: AC #3 handled via CLI branching, NOT a schema flag.** Rationale: `astonish chat -m X --resume` = "override for this run only, don't rewrite the pin" is a CLI-level decision (skip the pin-persist step when --resume is set), not a schema concern.
- **DECISION-7: No new `ResolveForSession(ctx, sessionID)` helper.** Rationale: the issue proposed it, but wiring the store into the resolver creates a resolver→store→session loop and increases the coupling of `pkg/provider`. Instead, callers already hold the session/app; they call `ResolveEffectiveConfig(...)` then `ApplyProviderOverride(cfg, pinnedProvider, pinnedModel)` in one place. Same effect, cleaner separation.
- **DECISION-8: TDD approach — tests first for overlay + fallback**, hand-lifted from existing resolve_test.go patterns. Migration correctness verified by pre-commit Atlas hash + a smoke integration test on both SQLite and Postgres. Studio UI: Playwright/Vitest depending on layer.

## Scope IN
- ent schema fields: `session.provider_name`, `session.model_name` (personal + team), `app.provider_name`, `app.model_name` (personal + team) — all nullable strings.
- New PersonalSettings ent schema + store (personal scope only) with `DefaultProvider`, `DefaultModel`.
- Atlas migrations: schema/*.sql + pkg/store/{personal,team}/migrations/*.sql (SQLite; Postgres for team).
- `pkg/provider/resolve.go`: new `ApplyUserDefault(cfg, personalSettings)` + `ApplyProviderOverride(cfg, provider, model string) *config.AppConfig` overlays.
- Call-site updates at 13 `ResolveEffectiveConfig` callers to chain the overlays.
- `pkg/api/chat_handlers.go`: PATCH /api/sessions/{id}/model, GET /api/sessions/{id}/model-status; SSE `model_changed` event; SwappableLLM.Swap on change.
- `pkg/api/apps_handlers.go`: PATCH /api/apps/{slug}/model; expose pin in app definition read.
- `pkg/api/user_settings_handlers.go` (new): GET/PATCH /api/user-settings/default-model.
- `cmd/astonish/chat.go`: pin-by-default; `--no-pin`, `--clear-model`; `astonish chat model <slug>` subcommand; `--resume -m X` ephemeral override.
- `web/src/components/StudioChat.tsx`: header model picker + warning banner.
- `web/src/components/AppsPanel.tsx`: per-app model picker.
- `web/src/hooks/useAppAI.ts`: read app pin.
- Docs: docs/architecture/agent-engine.md (cascade section), docs/website/docs/cli/chat.md (flags + persistence), docs/architecture/chat-rendering-pipeline.md (new `model_changed` event only).
- Tests: unit (`resolve_test.go` extend), integration (`integration_multitenant_test.go` extend + new `TestIntegration_SessionModelPin_*`), Studio Vitest for picker, e2e scenario for pin+resume.

## Scope OUT (Must NOT have)
- Distilled/imported flows keeping the flow's own provider/model — out of scope per issue.
- Per-channel pin override (Slack/Telegram/Email) — inherit session pin, no new override.
- Model routing/multi-model within a single turn — the pin is a static setting.
- New `ResolveForSession` inside the resolver package (rejected in DECISION-7).
- Live mid-turn CLI console swap (rejected in DECISION-4).
- Storing pins inside `metadata` JSON (rejected in DECISION-2).
- Provider/credential CRUD changes — picker only READS what provider_settings_handlers already surfaces.
- Cross-org migration of pinned models (out of scope for the platform-level publish flow).
- `useAppData` proxies — not affected; only `useAppAI` LLM calls read the pin.
- Sub-agent-level pin overrides — sub-agents inherit main-thread LLM (SwappableLLM).

## Open questions
_None remaining. All five owner-decisions resolved via the interview turn._

## High-accuracy review — Round 3 (planner self-review; both subagent runs hung with zero output)

Two subagent-based review rounds were dispatched and cancelled after producing no assistant output:
- Round 1: `momus` `bg_e704b3af` (~16 min, 0 messages), `oracle` `bg_9602be93` (~16 min, 0 messages) — cancelled.
- Round 2: `momus` `bg_4f6ee826` (~11 min, 0 messages), `oracle` `bg_504787bf` (~11 min, 0 messages) — cancelled.

User approved a self-review as fallback. This is NOT the dual-independent-review the skill prescribes for `review_required: true` (which is currently `false` here anyway), but was chosen deliberately over blocking on tooling — flagged transparently rather than faked.

**Verdict: REJECT → fixes applied → APPROVE.** Four blocking issues found and fixed in-place on the plan file; verified fixes below.

**Verification method:**
- Read full plan (637 lines) + full draft.
- Read source: `pkg/provider/resolve.go` (95 lines), `pkg/provider/swappable_llm.go` (63 lines), `pkg/launcher/chat_factory.go:1330-1359`, `ent/personal/schema/session.go:1-60`, `ent/team/schema/session.go:1-70`.
- Grep across repo: `ResolveEffectiveConfig(` → 15 matches / 7 files (9 production sites in 6 files, 6 test-file sites).
- Grep `pkg/apps/` for any `ResolveEffectiveConfig|EffectiveAppConfig|effectiveAppConfig` → zero matches.
- Grep `cmd/astonish/chat.go` for `dry-run` → zero matches.
- Read AGENTS.md invariants: `pkg/launcher/AGENTS.md` + `ent/AGENTS.md` (delivered via system-reminder).

**Blocking issues found + fixes applied:**
1. Plan repeatedly claimed "13 `ResolveEffectiveConfig` callers"; grep shows 9 production sites. Fixed in TL;DR (line 7), Scope IN (line 32), dep matrix (line 101), Todo 8 title (line 258), Todo 8 file list (lines 264-268), Todo 8 AC (line 280), risk-driver line (line 641). Todo 8 now instructs the executor to run grep FIRST and record the current count in evidence.
2. Plan claimed `pkg/apps` has `ResolveEffectiveConfig` callers and named `EffectiveAppConfigFromContext` / `effectiveAppConfig`; neither exists. Fixed in Scope IN (line 50), Todo 8 file list, Must-NOT-Have added ("do NOT modify `pkg/apps/*`"), redirected app-pin overlay to `pkg/api/ai_chat_handler.go` — which is where Todo 20 already correctly pointed.
3. Todo 14 depended on a `--dry-run` CLI flag that doesn't exist. Fixed by rewriting ACs to invoke flag-parsing + factory-build code paths directly through `cmd/astonish/chat_test.go` with a fake `SessionPinStore`; Must-NOT explicitly forbids inventing `--dry-run` in production code.
4. Todo 8 vs Todo 10 contradicted on missing-cred behavior (Todo 8 said "reset override"; Todo 10 said "persist the pin, do NOT swap"). Fixed by rewriting Todo 8's fallback: "log warning and let cascade result stand in-memory; the persisted pin is NOT cleared" — consistent with DECISION-3 and Todo 10.

**Non-blocking suggestions (NOT applied; flagged for the worker's judgment or a follow-up):**
- PersonalSettings storage model ambiguous for personal-mode SQLite where `session.user_id` is often NULL. Executor should decide: singleton row vs uuid.Nil sentinel — record decision in commit message.
- Migration downgrade path never mentioned; project is forward-only per convention but worth a sentence.
- Fleet path (`pkg/fleet/`) never mentioned; sessions spawned by fleet inherit pins by virtue of using the same session store — worth a Scope OUT sentence.
- Todo 22's SQLite behavior of `make test-integration` without a DSN: confirm the harness skips DB-requiring tests gracefully.
- Todo 6's migration filename hint (`<timestamp>_add_provider_model_pin.sql`) — Atlas emits the filename; the executor shouldn't hand-craft it.

**Risks the plan does NOT acknowledge but should (still not applied; for worker awareness):**
- Personal-mode single-user identity for PersonalSettings.
- Distilled flow → session inheritance of the flow's provider as pin.
- Concurrent PATCH races between two Studio tabs.
- Model picker source-of-truth drift (credentialed providers list vs. cascade cfg).

**Trust check on citations (all VERIFIED):**
- `SwappableLLM.Swap()` — confirmed at `pkg/provider/swappable_llm.go:30-34`.
- Sub-agent LLM inheritance — confirmed at `pkg/launcher/chat_factory.go:1338` (`subAgentMgr.LLM = llm`).
- Ent Session schemas viable — confirmed both `ent/personal/schema/session.go` and `ent/team/schema/session.go` have `metadata` JSON but no `provider_name`/`model_name`; hand-editable per `ent/AGENTS.md`.
- Original "13 callers" claim — **WRONG**; corrected to ~9 (grep-verified list in Todo 8).

**Approval status:** Plan file now internally consistent. All four blocking issues resolved by in-place edits. Ready for worker execution.

## Approval gate
status: awaiting-approval
