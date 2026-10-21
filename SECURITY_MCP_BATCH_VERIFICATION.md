# Security remediation — MCP batch and remaining gates

## Status

The seven-phase program remains incomplete. `continuity-and-acceptance` is complete; `urgent-independent-fixes` has additional implemented and locally tested fixes but still lacks mandatory live acceptance. `sandbox-and-artifact-boundaries` remains blocked at D-05. Tools/apps/SQL, fleet, operations, and final closure remain pending. No release, deployment, production key rotation, mail-server change, dependency change, or generated-code change was performed in this batch.

## Changes delivered in this batch

- Fixed platform standard-server uninstall clearing team web-tool settings instead of platform settings. Matching platform references are cleared while team settings, replacements and unrelated settings remain unchanged. Missing platform settings never fall back to team settings. Existing best-effort settings-save behavior remains; this is not transactional uninstall.
- Fixed missing-row panics in platform MCP toggle and refresh. Authorized missing rows return 404 before mutation/discovery.
- Strengthened settings/provider/full-config regression coverage with genuinely independent org-admin, team-admin and platform-admin identities, masked readback, persisted writes and unchanged unrelated scopes.
- Expanded the temporary-SQLite registered-route HTTP/JWT MCP test to 223 leaf cases. The original 56 remain; 167 additions cover modern CRUD, inline install, legacy settings, existing-row refresh selection, independent org-admin standard-server operations, and rejected/no-effect requests.
- Strict acceptance requires 380 entries per matrix leg, with 408 parser tests. Missing each of the 167 additional entries failed its parser regression before registration and passes after registration.
- Updated MCP architecture, testing documentation and scenario catalog. Preserved the broader inventory and all policy gates.

Changed paths for this batch: `pkg/api/{standard_servers_handler.go,standard_servers_handler_test.go,mcp_platform_handlers.go,mcp_handlers_test.go,settings_handlers_test.go,provider_settings_handlers_test.go,integration_redaction_test.go}`, `tests/e2e/chat_auth/chat_mcp_standard_install_test.go`, `tests/scenarios/{parse-run.mjs,parse-run.test.mjs,chat.yaml}`, `docs/architecture/{mcp.md,testing.md}`, and the progress/report records. Earlier working-tree changes remain untouched; unrelated slides work remains untouched.

## Regression and review evidence

Platform settings cleanup failed in all five platform cases before the fix; narrower-scope cases passed. All passed afterward. Missing-row handler tests failed in eight scope/operation cases before nil guards and passed afterward. Independent source/test review found no further defect in these narrow fixes.

The HTTP fixture's new logging observer initially deadlocked: wrapping slog's default handler after SetDefault recursively entered the standard logger mutex. Timeout stack `c4bb6a81` proved that test-only failure. It now uses an independent text handler and restores the prior logger/writer/flags; local tests pass. The missing-row HTTP cases exercise both legacy and platform route families, not only similarly named legacy handlers.

## Verification observed

- Recovered process `5980a622`: normal Go/frontend tests, API/store race tests and local HTTP repetitions succeeded, but the combined command exited 2 on a trailing blank line in the progress Markdown. That whitespace error was repaired; it was not a test failure.
- Recovered process `f8894f11`: exit 0, prior parser checks, tagged compile, preflight unit tests, lint, Go build and CLI version smoke.
- Process `7a01fa83`: exit 0 for `env -u KUBECONFIG CI=1 make test`, API/store race tests, `make lint` (0 issues), build to `/tmp/astonish-security-verified`, CLI `--version`, and diff check. This includes the production fixes and independent-principal handler tests; the final 167 HTTP-only additions came later.
- Process `f7780bee`: exit 0, combined local setup/new-process/MCP/memory selection repeated three times, earlier 241-test parser version, diff check.
- Process `5690293d`: exit 0, combined local sequence with race detection, tagged all-package compile-only check, preflight validation unit tests.
- Independent final matrix review: expanded 223-case HTTP test plus setup/new-process/memory sequence passed three repetitions (19.142s), then race detection (109.319s), no race warnings. Logs: `/tmp/review-final-mcp-matrix-count3.log`, `/tmp/review-final-mcp-matrix-race1.log`.
- Final manifest registration: all 408 parser tests passed; tagged chat_auth compile-only check and diff check passed.
- Fresh `make test-security-preflight SECURITY_TRACK=urgent`: exit 2, rejected before cleanup because explicit disposable-environment acknowledgement is missing. No acknowledgement was supplied on the operator's behalf.

## Evidence limits

Local HTTP tests use actual loopback HTTP, production route registration, JWT validation, persisted SQLite tenant membership and stores. Tokens are test-issued; this MCP fixture does not prove login. Unsupported test sandbox configuration stops discovery before external access; these tests prove scope selection and no-effects behavior, not successful MCP execution. SQLite tests are not PostgreSQL coverage even when selected under a PostgreSQL-labeled matrix leg. New-process setup uses test-binary subprocesses, not a deployed daemon.

The async observer sees a terminal log before a deferred in-flight map deletion, not a strict goroutine join. Immediate same-name reuse could theoretically encounter deduplication; expanded tests reject duplicate observations rather than call them success. Instantaneous no-log assertions alone cannot exclude delayed work; production authorization returns before scheduling and handler tests complement these checks.

No browser/TUI, live sandbox artifact reader, both-storage runtime matrix, PostgreSQL first-run success, production migration, receiving-MTA proof or incident rotation was exercised. Platform artifact disabling remains containment, not restored capability.

## Inputs required to continue to full completion

1. Operator approval of the actual disposable PostgreSQL target, Kubernetes cluster/context, namespace pair (`astonishe2e` / `astonishe2e-sandbox`) and eligible layers/uppers PVCs, plus provider/runtime prerequisites. Names alone are not approval. Full urgent command remains `make test-security-preflight SECURITY_TRACK=urgent && make test-security SECURITY_TRACK=urgent`.
2. D-01 session-memory visibility; D-03 fleet execution principal and active-edit/reapproval semantics; D-04 explicit credential-use/consent/destination grants; D-05 supported runtime with trusted reader/capture/publisher topology and feasibility evidence. D-02 personal privacy/team collaboration is preserved and does not approve these choices.
3. SQLite authorizer/dependency feasibility and any required fleet-schema approval before architectural implementation. No regex or search_path substitute for engine isolation.
4. Separately authorized production incident response/rotation and receiving-MTA evidence. Reviewer severities/links for TOOL-02..07 remain unspecified; optional PR594-03 remains deferred rather than an urgent blocker.

The execution runtime previously rejected downstream phase entry as serial-blocked. Passing local tests does not justify bypassing that dependency or marking urgent/overall completion. No announce_completion call is appropriate until every recorded phase verification passes.
