# PostgreSQL first-run acceptance and local verification

## Status

The security remediation program remains incomplete. This batch completes local PostgreSQL first-run acceptance wiring and strengthens its test safety barriers; it does not establish live PostgreSQL/Kubernetes acceptance or overall remediation closure.

`continuity-and-acceptance` remains complete. `urgent-independent-fixes` is failed/blocked pending live acceptance; `sandbox-and-artifact-boundaries` remains blocked at D-05. An attempt to start `tools-apps-and-sql` was rejected by the execution runtime with `serial_blocked`: an earlier serial phase is not complete. Fleet, operations and final closure remain pending. No completion tool was called.

HEAD: `a66f59d86e92596828273ed12b6a3f9db8e28c78`. Existing working-tree changes and unrelated slides work were preserved. No commit, release, production deployment, credential rotation, MTA change, schema or dependency change was made.

## Implemented and reviewed

- `TestE2E_SecurityPostgresFirstRunSetup` / CHAT-115 performs registered HTTP PostgreSQL setup, first-user registration/login, owner/superadmin/team-admin checks and separate-test-process restart continuity. Configured initialization retries must preserve config and identities. This fixture is implemented but its PostgreSQL-positive path has not run.
- Make selects CHAT-115 only in the PostgreSQL leg. Strict reports require 597 PostgreSQL entries versus 594 SQLite entries; the difference is the first-run parent, setup and restart. Existing local-SQLite scenarios remain local-SQLite evidence even when selected in a PostgreSQL-labeled run.
- First-run requires separate whole-disposable-instance approval and an explicit matching host/port DSN. Database-only approval is insufficient because setup changes cluster-wide roles. No database or role sweep is attempted; disposal remains the operator's obligation after success, failure or partial setup.
- Child processes use an explicit environment allowlist. HTTP shutdown precedes tracked audit draining and store closure. Review added rejection of symlinks in config and evidence paths before setup/reopening. This is private fixture-path hygiene, not the trusted race-resistant sandbox reader required by ART-01/02.
- Eight symlink subprocess cases failed before the guard and passed after it. A forbidden ambient PostgreSQL variable provided a secondary barrier during the failing-before test, preventing PostgreSQL access.
- `TestSecurityPreflightEnvironmentFirstRunAuthority` exercises the actual preflight entrypoint with missing/mismatched authority, an approved synthetic target and SQLite without PostgreSQL authority. Removing the validator callback made the four rejection cases fail; the callback was restored.
- `TestSecurityPreflightMakeFirstRunAuthority` invokes both actual Make targets in a private copy without `.env` files. A restricted Go shim runs the real preflight; Kubernetes and acceptance commands are trapped. Missing approval or mismatched instance must fail before downstream effects.
- Six Node orchestration cases execute the actual Make recipes and real parser with synthetic Go/Kubernetes/git responses: both legs succeed, preflight fails, either Go leg fails, or either parser leg fails. They verify selectors, backend order, reports and failure propagation. Synthetic successful events are not live acceptance evidence.
- Corrected the documented safety command, which previously misspelled `IsolationBeforeEffects` and silently omitted that test. A regression failed before the documentation correction and passed afterward. Synthetic PostgreSQL report metadata now names a command that actually selects CHAT-115.

## Verification observed this turn

| Command / evidence | Result |
|---|---|
| Recovered process `b6a5ee5c`: combined local MCP/full-config/memory/process-continuity race checks | Exit 0, 277.865s; local HTTP/SQLite and subprocess evidence only |
| `env -u KUBECONFIG CI=1 make test` | Exit 0; normal Go tests and 109 frontend files / 828 tests passed; log `/tmp/astonish-security-local-suite.log` |
| `node --test --test-reporter=dot tests/scenarios/parse-run.test.mjs` | Exit 0 in combined verification; 643 test dots |
| `go test -race -tags=e2e ./tests/e2eboot -run '^(TestPostgresFirstRunPreflightValidation\|TestSecurityPreflightValidation\|TestSecurityPreflightBeforeBootstrap\|TestSecurityPreflightEnvironmentFirstRunAuthority\|TestSecurityPreflightMakeFirstRunAuthority)$' -count=1 -timeout=180s` | Exit 0, 9.130s |
| `go test -race -tags=e2e ./tests/e2e/chat_auth -run '^TestPostgresFirstRun(GateBeforeEffects\|IsolationBeforeEffects\|SymlinkIsolationBeforeEffects\|RejectionsLocal)$' -count=1 -timeout=180s` | Exit 0, 12.412s; separate rerun exit 0, 11.942s |
| Focused actual-Make, backend-report and documentation-selector Node tests | Exit 0, 9/9, no skipped tests |
| `go test -tags=e2e ./tests/e2e/... ./tests/e2eboot -run '^$'` | Exit 0; compilation only, explicitly no tests executed |
| `make lint` | Exit 0, zero issues |
| `go build -o /tmp/astonish-security-verified .` and `/tmp/astonish-security-verified --version` | Exit 0; executable reports version dev. This is CLI build/version smoke, not setup/daemon acceptance |
| `git diff --check` | Exit 0 |

`go test -race ./pkg/api ./pkg/store/entstore -count=1 && npm --prefix web run typecheck` completed with exit 0: API 68.662s, store 19.810s, and TypeScript checking passed.

## Remaining gates

No live acceptance command was launched against unapproved targets. The historical exit-2 preflight failure remains unsuperseded. Completion requires explicit operator approval for the actual disposable PostgreSQL instance, ordinary test database, Kubernetes context/namespaces/PVCs and provider prerequisites. No acknowledgement was set on the operator's behalf.

D-01 memory policy, D-03 fleet principal/active edits, D-04 credential consent, D-05 runtime feasibility/approval, SQLite authorizer/dependency feasibility and possible fleet schema approvals remain open. Full ART confined-reader, positive sandbox/browser/terminal flows, SQL/broker isolation, migrations and incident/MTA obligations remain mandatory. Approved collaborative team-app maintenance/data/DDL, private personal apps and non-sharing of personal credentials remain unchanged.

Next permitted phase: resume `urgent-independent-fixes` live acceptance when approved infrastructure is supplied; preserve the full positive/negative contract. Downstream work cannot proceed through the current runtime's serial gate merely because local checks pass.
