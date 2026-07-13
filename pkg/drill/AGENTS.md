# pkg/drill — AGENTS.md

Deterministic test/drill suite runner. Drills exercise tools, run assertions (including LLM-based semantic assertions), capture artifacts, and optionally triage failures with an AI triage agent.

## Scope
- `runner.go` — `SuiteRunner`, `ToolExecutor`, `LLMProvider`.
- `triage.go` — `TriageAgent`.
- `artifacts.go` — `ArtifactManager`.
- `report.go` — `SuiteReport`, `TestReport`, `StepResult`.

## Key ideas
- The **runner** is deterministic given the same inputs — flaky external dependencies belong behind an interface (`LLMProvider`, `ToolExecutor`) that can be mocked in unit tests.
- **LLM-based assertions** call the injected `LLMProvider`. Keep them opt-in per step; the default should be strict/programmatic assertion.
- The **triage agent** is invoked on failure to produce a human-readable diagnosis. It is a helper, not a substitute for the failing test signal.
- Artifacts (logs, screenshots, outputs) go through `ArtifactManager` — do not write files directly from step handlers.
- **Browser vs shell networking**: shell and browser tools both run in the sandbox when sandboxed. Prefer `http://localhost:<port>` in drills; browser navigation rewrites loopback hostnames to `127.0.0.1` (Chromium IPv6-first vs IPv4-only listeners). Do not hard-code container bridge IPs. `{{CONTAINER_IP}}` remains supported for older drills.
- **Start scripts**: suite setup invoking `start-services.sh` is forced to `background=true`. The script must end with `wait` (or `exec`) after starting children with `&`; bare `npm run dev &` + exit leaves Vite hung when the PTY closes.

## When editing
1. Adding a new assertion type? Extend the runner's assertion registry rather than special-casing it in step handlers.
2. Changing `SuiteReport`/`TestReport` shape? Coordinate with any UI consumer (Studio shows drill results).
3. Adding a new step kind? Update both the schema loader and the runner dispatch in the same commit.
