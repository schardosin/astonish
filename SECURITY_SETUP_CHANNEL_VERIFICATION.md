# Security setup, channel and MCP continuation

## Status

The approved remediation remains incomplete. Continuity is complete; urgent-independent-fixes is running; sandbox-and-artifact-boundaries is blocked at D-05; tools/apps/SQL, fleet, operations and final closure remain pending. No phase completion or infrastructure authorization was inferred. HEAD: `a66f59d86e92596828273ed12b6a3f9db8e28c78`.

## Implementation and evidence

- Retained prior SQLite setup repairs: fresh directories, active organization, General-team administrator membership and checked/non-overwriting key preparation. Real HTTP setup succeeds for none/builtin authentication; builtin server recreation accepts fresh password login with correct org/team authority.
- Tightened first-run acceptance to require absent config/data directories. The new assertion failed in both modes before removing harness pre-creation, then passed. This repairs test fidelity, not another production defect.
- Reproduced raw filesystem/SQLite details in setup errors. Replaced SQLite setup errors with fixed operation-level messages. New `pkg/api/platform_setup_errors_test.go` covers seven injected storage failures, two request failures and structurally checks other error branches. Team-membership failure is structurally checked, not directly injected. Sanitization does not imply provisioning rollback.
- Retained atomic email re-verification through optional `store.UserChannelProofStore.VerifyAndEnable`, preserving existing Update/Verify semantics. Real HTTP/JWT temporary-SQLite tests confirm insert/verify/enable failures leave rows unchanged, proofs cannot replay, and fresh proof recovers successfully.
- Added deterministic stale-owner lookup coverage for SQLite and the proof-capable mock: write-time owner mismatch rejects without mutation, replay cannot persist, current-owner fresh proof succeeds. No concurrent ownership-transfer claim.
- Temporary-SQLite memory HTTP tests cover destination insert/update failures without source/duplicate/destination mutation, then successful recovery. They do not prove rollback after later deletion failures.
- Added six independent-role MCP acceptance cases: team-admin default/team success, org/platform denial, sibling-team denial, platform-admin platform success. Authorized platform secret replacement/removal is allowed; unrelated secrets remain protected. These cases are compiled, not live run.
- Strict manifest now requires **147 entries**, with **171 parser tests passing**. Synthetic parser events are not live server evidence.

## Verification observed

| Command/check | Result |
|---|---|
| Recovered `env -u KUBECONFIG CI=1 make test`, process 2bd66b75 | Exit 0; normal Go suites, 109 frontend files / 828 tests |
| New absent-directory assertion before fixture correction | Exit 1, both auth modes |
| First-run real HTTP SQLite after fixture correction, then repeated three times | Exit 0 |
| `npm run typecheck && npm run build` | Exit 0; bundles emitted, nonfatal Vite warnings |
| `go test ./pkg/api ./pkg/store/... -count=1` after setup-error repair | Exit 0 |
| Focused setup/channel/memory API race suite after repair | Exit 0 |
| `go test -tags=e2e ./tests/e2e/chat_auth -run '^TestE2E_SecurityUrgentFirstRunSetup$' -count=1 -timeout=180s` after repair | Exit 0 |
| `make lint` after repair | Exit 0, zero issues |
| `go build -o /tmp/astonish-security-verified .` after repair | Exit 0 |
| Final parser tests and tagged chat_auth compilation after MCP additions | Exit 0; 171 parser cases, Go explicitly ran no tests |
| `git diff --check` | Exit 0 |

The normal full suite preceded the final setup-error and MCP test additions; subsequent scoped checks cover those edits. Final production-check process was `39dae30c`, exit 0. No full infrastructure matrix or browser/TUI acceptance ran.

## Remaining exact work

Continue urgent-independent-fixes: distinct org-admin settings/MCP positives; independent team-admin nonempty provider mutation; platform-admin full-config write success; broader MCP write/legacy/inline-install/discovery matrices, no-start-effect assertions and absent-global-key variants. Platform-provider, SQLite first-run and local real-route memory/channel failures are already implemented—do not recreate them from stale checkpoints.

Positive PostgreSQL first-run, key write/sync/close fault injection, a new-process daemon restart/encryption continuity and later-stage memory deletion-failure behavior remain unverified. Both-storage infrastructure acceptance still requires operator approval of actual disposable database, cluster/context, namespace pair/PVCs and provider prerequisites. D-01/D-03/D-04/D-05, SQLite authorizer/dependency and fleet-schema gates remain open. Artifact containment is not confined live-reader completion. Production incident response, rotation and MTA proof remain separately authorized obligations.

Team app/data/DDL collaboration, personal maintenance/private state and non-sharing of personal credentials remain unchanged. No commit, release, deployment, production rotation or mail change.

## Changed paths and preservation

This continuation adds/extends `pkg/api/platform_setup_errors_test.go`, `pkg/api/platform_setup_handlers.go`, `pkg/api/user_channel_handlers_test.go`, `tests/e2eboot/platform_core.go`, `tests/e2e/chat_auth/chat_auth_scoping_test.go`, `tests/e2e/chat_auth/chat_mcp_standard_install_test.go`, `tests/scenarios/{parse-run.mjs,parse-run.test.mjs,chat.yaml}`, `docs/architecture/{configuration,channels,testing}.md`, and the progress/report records. Prior production/store/provider/settings and test changes are retained. No hand-edited generated files or dependency changes. Unrelated slides files remain untouched.
