# Decisions — per-chat-app-model-pin

## [2026-07-10] Plan-time decisions (all owner-approved)

- DECISION-1: User-level default layer YES. Personal scope only.
- DECISION-2: First-class ent fields (NOT metadata JSON). Queryable + migratable.
- DECISION-3: Missing-credential = warn + cascade fallback. Never hard-fail. Never auto-clear pin.
- DECISION-4: CLI console = persist-only (next turn). Studio = true live swap via SwappableLLM.Swap.
- DECISION-5: -p/-m pin by default; --no-pin opt-out for scripted callers.
- DECISION-6: --resume -m X = ephemeral override for this run only. No pin rewrite.
- DECISION-7: NO ResolveForSession in pkg/provider. Callers hold the session; they apply overlay.
- DECISION-8: TDD for overlays + fallback. Tests-after for API, CLI, UI.
