# Urgent security continuation: memory participant acceptance

## Status

`urgent-independent-fixes` remains running and incomplete. This continuation adds authenticated E2E coverage; it does not claim live remediation acceptance. `sandbox-and-artifact-boundaries` remains blocked at D-05. The four later phases remain pending, including tools/apps/SQL, fleet, operations and final closure.

## Implemented

- Added `TestE2E_SecurityUrgentMemoryParticipants` in `tests/e2e/chat_auth/security_urgent_test.go`, mapped to CHAT-104.
- Five participant paths—source, duplicate, discard, implicitly selected destination and explicitly selected destination—each require a denied and an allowed authenticated request.
- Every case includes a member-owned source before the potentially forbidden participant. Denial requires an unchanged complete team-memory snapshot. Authorized cases require durable deletion/replacement or in-place destination updates and unchanged unrelated rows.
- Registered all 16 new parent/subtest entries in `tests/scenarios/parse-run.mjs`. The urgent manifest now requires 38 entries, rather than 22. Both positive and negative coverage are mandatory.
- Added ten parser regressions and CHAT-104 report mapping, scenario documentation and testing architecture documentation.
- Corrected the trailing blank line in the prior progress checkpoint. No production handler, app policy, runtime boundary or infrastructure configuration changed.

## Verification observed in this continuation

1. Initial channel recheck: 52 parser tests passed; E2E package compiled with no tests run; email-verification race tests passed. The combined command exited 2 solely because `git diff --check` found a new blank line at EOF in `SECURITY_REMEDIATION_PROGRESS.md`.
2. Before registering memory coverage, `node --test tests/scenarios/parse-run.test.mjs` exited 1: 50 passed, 12 failed. Ten missing-case runs were accepted and two reports retained the old required count.
3. After registration, the following command exited 0:

```sh
gofmt -w tests/e2e/chat_auth/security_urgent_test.go && node --test tests/scenarios/parse-run.test.mjs && go test -tags=e2e ./tests/e2e/chat_auth -run '^$' -count=1 && go test -race ./pkg/api -run '^TestConsolidationPreauthorizesAllParticipants$' -count=1 && go test -tags=e2e ./tests/e2eboot -run '^TestSecurityPreflight(Validation|BeforeBootstrap)$' -count=1 && git diff --check
```

Results: 62 parser tests passed; E2E compilation only; existing consolidation preauthorization race tests passed; focused preflight tests passed; whitespace check passed. No new production vulnerability is inferred from these test additions.

## Not verified and remaining work

The new memory and prior channel E2E cases have not executed against either real backend. No live acceptance, DB failure rollback, embedding-call count, SMTP/MTA delivery, injected persistence failure, browser/TUI behavior, multi-replica behavior or runtime confinement is established by these checks. The memory scenario checks authorization atomicity, not transaction atomicity after a persistence error. Existing delete-failure/partial-mutation obligations remain open.

Continue the urgent first-run setup, full role/scope/secret-canary masked updates, same-slug cross-organization session fixtures, scoped MCP side effects, and remaining memory/channel persistence-failure matrix. Live acceptance remains deferred to operator-approved disposable infrastructure; historical exit-2 preflight failure is not superseded. Do not set the infrastructure acknowledgement on the user's behalf.

D-01 memory visibility, D-03 fleet execution authority, D-04 credential consent, D-05 runtime approval, SQLite driver feasibility/dependency approval and any fleet-schema approval remain open. Preserve full ART-01/02 live-reader acceptance in sandbox/final phases and current containment. Personal maintenance remains supported; team apps and their data remain collaborative within the team boundary, without sharing personal credentials or copying personal data.

HEAD observed: `a66f59d86e92596828273ed12b6a3f9db8e28c78`. Earlier edits and unrelated slides files were retained. No commit, release, deployment, production rotation, mail-server change or overall completion was performed.
