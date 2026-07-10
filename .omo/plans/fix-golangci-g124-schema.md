# fix-golangci-g124-schema - Work Plan

## TL;DR (For humans)

**What you'll get:** The GitHub Actions lint CI will pass again. One invalid entry in the linter configuration is removed.

**Why this approach:** The gosec rule `G124` referenced in the config doesn't exist in gosec's rule set — the valid range jumps from G123 to G201. Removing it is entirely safe (no real check is re-enabled) and is the correct minimal fix.

**What it will NOT do:** Won't downgrade the linter version, won't add suppression comments, won't touch any other lint rule or Go source file.

**Effort:** Quick
**Risk:** Low — single-line deletion of a no-op config entry
**Decisions to sanity-check:** Deleting the `G124` entry rather than working around it (correct — G124 is non-existent).

Your next move: run `$start-work`. Full execution detail follows below.

---

> TL;DR (machine): Quick / Low risk — delete lines 79-80 of .golangci.yml (non-existent gosec G124 exclude); fixes CI schema validation failure in golangci-lint v2.11.4.

## Scope
### Must have
- Delete lines 79–80 of `.golangci.yml`: the comment `# Secure cookie flag is set dynamically from TLS / X-Forwarded-Proto` and the line `- G124  # Cookie missing Secure attribute (static analysis false positive)`
- Verify `golangci-lint config verify` exits 0 with golangci-lint v2.11.4

### Must NOT have (guardrails, anti-slop, scope boundaries)
- Do NOT downgrade the `version: v2.11.4` in `.github/workflows/lint.yml`
- Do NOT add any `//nolint:gosec` comments in Go source files
- Do NOT modify any other gosec exclude entry
- Do NOT touch any Go source file

## Verification strategy
> Zero human intervention - all verification is agent-executed.
- Test decision: none (config-only change; the linter itself is the QA gate)
- Evidence: .omo/evidence/task-1-fix-golangci-g124-schema.txt

## Execution strategy
### Parallel execution waves
Wave 1 (single todo — atomic config fix + verify):
- T1: Delete G124 entry from `.golangci.yml` and verify with golangci-lint

### Dependency matrix
| Todo | Depends on | Blocks | Can parallelize with |
| --- | --- | --- | --- |
| T1 | — | F1–F4 | nothing |

## Todos
> Implementation + Test = ONE todo. Never separate.
<!-- APPEND TASK BATCHES BELOW THIS LINE WITH edit/apply_patch - never rewrite the headers above. -->
- [x] 1. `.golangci.yml`: delete non-existent G124 gosec exclude — fixes CI schema validation
  What to do: Open `.golangci.yml`. Delete lines 79–80 exactly:
  ```
      # Secure cookie flag is set dynamically from TLS / X-Forwarded-Proto
      - G124  # Cookie missing Secure attribute (static analysis false positive)
  ```
  The file must remain valid YAML. All other gosec excludes (G101, G104, G107, G115, G117, G118, G204, G301, G302, G304, G306, G404, G702, G703, G704, G705, G706) stay untouched.
  Must NOT do: Do not remove any other exclude. Do not touch `.github/workflows/lint.yml`. Do not modify any Go source file.
  Parallelization: Wave 1 | Blocked by: nothing | Blocks: F1–F4
  References (executor has NO interview context - be exhaustive):
    - `.golangci.yml:47-80` — full gosec excludes section; G124 is the last entry at lines 79-80
    - `.github/workflows/lint.yml:40-43` — uses golangci/golangci-lint-action@v9 with version v2.11.4
    - CI error (run 29055068693): `"linters.settings.gosec.excludes.17" does not validate … value must be one of 'G101'..'G123', 'G201'..` — G124 not in enum; index 17 = 18th entry = G124
  Acceptance criteria (agent-executable):
    - `grep -n 'G124' .golangci.yml` returns no matches
    - `golangci-lint config verify` exits 0 (run locally with golangci-lint v2 installed, or via docker: `docker run --rm -v $PWD:/app -w /app golangci/golangci-lint:v2.11.4 golangci-lint config verify`)
    - The number of gosec excludes decreases by 1 (was 17 entries, now 16)
  QA scenarios:
    - Happy path: `golangci-lint config verify` → exit 0, no schema errors → capture stdout to .omo/evidence/task-1-fix-golangci-g124-schema.txt
    - Failure path: if `golangci-lint` not installed locally, use `docker run --rm -v $PWD:/app -w /app golangci/golangci-lint:v2.11.4 golangci-lint config verify` — same expectation
  Commit: Y | fix(lint): remove non-existent gosec G124 from .golangci.yml excludes

## Final verification wave
> Runs in parallel after ALL todos. ALL must APPROVE. Surface results and wait for the user's explicit okay before declaring complete.
- [x] F1. Plan compliance audit — T1 checked, G124 absent (grep exit 1), all 17 other excludes intact
- [x] F2. Code quality review — YAML valid, file ends cleanly at `- G118`, golangci-lint config verify exit 0
- [x] F3. Real manual QA — `golangci-lint config verify` exit 0 confirmed locally (v2.12.2)
- [x] F4. Scope fidelity — only `.golangci.yml` changed (git diff --stat), no nolint comments (grep exit 1), lint.yml untouched

## Commit strategy
Single commit on completion of T1:
```
fix(lint): remove non-existent gosec G124 from .golangci.yml excludes

gosec does not define rule G124 — the valid range jumps from G123
to G201. golangci-lint v2.11.4 introduced strict schema validation
which now rejects the unknown rule ID, breaking CI.

Remove the entry. No gosec check is re-enabled; G124 never existed.
```

## Success criteria
- `golangci-lint config verify` exits 0 with version v2.11.4
- CI run on main (lint workflow) passes golangci-lint job
- All other gosec excludes preserved exactly as before
- No other files modified
