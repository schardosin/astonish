# pkg/drill — AGENTS.md

Deterministic test/drill suite runner. Drills exercise tools, run assertions (including LLM-based semantic assertions), capture artifacts, and optionally triage failures with an AI triage agent.

## Scope
- `runner.go` — `SuiteRunner`, `ToolExecutor`, `LLMProvider` (tests only; no setup/ready_check/teardown).
- `run_instructions.go` — Studio chat prep text from suite_config (template, git, start script).
- `triage.go` — `TriageAgent`.
- `artifacts.go` — `ArtifactManager`.
- `report.go` — `SuiteReport`, `TestReport`, `StepResult`.

## Key ideas
- The **runner** is deterministic given the same inputs — flaky external dependencies belong behind an interface (`LLMProvider`, `ToolExecutor`) that can be mocked in unit tests.
- **`run_drill` is thin**: inject credentials, then execute drills. It does **not** switch templates, git-pull, start services, ready_check, or teardown. Studio Run pastes `GenerateRunInstructions`; the agent prep’s the sandbox, then calls `run_drill`. Fleet assumes the stack is already live and calls `run_drill` only.
- Suite `setup` / `ready_check` / `workspace` / `branch` / `template` are **instruction sources** for agents, not SuiteRunner side effects.
- **LLM-based assertions** call the injected `LLMProvider`. Keep them opt-in per step; the default should be strict/programmatic assertion.
- The **triage agent** is invoked on failure to produce a human-readable diagnosis. It is a helper, not a substitute for the failing test signal.
- Artifacts (logs, screenshots, outputs) go through `ArtifactManager` — do not write files directly from step handlers.
- **Browser vs shell networking**: shell and browser tools both run in the sandbox when sandboxed. Prefer `http://localhost:<port>` in drills; browser navigation rewrites loopback hostnames to `127.0.0.1` (Chromium IPv6-first vs IPv4-only listeners). Do not hard-code container bridge IPs. `{{CONTAINER_IP}}` remains supported for older drills.
- **Start scripts**: agents run `start-services.sh` during prep (Studio instructions / fleet work). Scripts must detach **restart supervisors** (`setsid`+`nohup`+`while true` restart + PID files), poll until stable, and exit 0 — not `&`+`wait` or one-shot detach. Prefer `npx vite` over `npm run dev`. Always execute a newly written start script once before `save_sandbox_template` (use `overwrite: true` to update an existing named template). Do not put secrets in `bootstrap_files`.

## When editing
1. Adding a new assertion type? Extend the runner's assertion registry rather than special-casing it in step handlers.
2. Changing `SuiteReport`/`TestReport` shape? Coordinate with any UI consumer (Studio shows drill results).
3. Adding a new step kind? Update both the schema loader and the runner dispatch in the same commit.
4. Changing Studio prep? Update `GenerateRunInstructions` and `web/src/utils/generateRunInstructions.ts` together.
