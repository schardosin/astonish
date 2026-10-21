# Security remediation execution progress

## Current disposition

The approved remediation program is **incomplete**. Only `handoff-and-decisions` has recorded phase verification. `urgent-boundary-fixes` is failed/blocked on acceptance, and later phases remain pending. The execution runtime rejected starting `tools-apps-sql` with `serial_blocked`: an earlier serial phase is not complete. No overall completion has been recorded.

Changes remain in the working tree. No PR release, deployment, production rotation, mail-infrastructure change, or complete live security closure is established. Unrelated slides files were not edited in this continuation.

## Latest implementation

Platform artifact reads no longer call unrestricted sandbox `PullFile`. Both the HTTP reader and context-aware transfer helper reject scoped/platform execution before backend construction, state queries, resume, or transfer. Authorized transcript-backed content and downloads remain available. Trusted-local host and sandbox reads remain unchanged.

**This is containment, not filesystem confinement.** Live binary or shell-created artifacts absent from the transcript are unavailable (404). Restoring them requires a race-resistant rooted reader with actual backend acceptance. ART-01/ART-02 remain open. Stream-close failures continue to discard partial bytes.

A new regression, `TestPullArtifactPlatformRejectsUnconfinedBackendBeforeEffects`, failed before the production change: both services-only and authenticated-user contexts received canary bytes and made two backend calls. It passes after the guard. `TestArtifactPlatformTranscriptAvailableDuringContainment` checks authorized content/download success and teammate denial. Existing trusted-local read, stopped-session resume, and transfer-integrity tests remain passing.

Architecture documentation in `docs/architecture/sessions.md` explicitly records the temporary loss of live platform artifact transfers and the remaining confinement requirement.

## Verification observed in this continuation

| Check | Result |
|---|---|
| Prior `env -u KUBECONFIG make test` process `36fc5f4a` | Confirmed completed, exit 0 |
| New artifact confinement-containment regression before change | Failed as expected; unrestricted bytes returned |
| `go test ./pkg/api -run 'TestPullArtifact\|TestArtifact' -count=1` | Passed |
| `go test -race ./pkg/api -run 'TestPullArtifact\|TestArtifact' -count=1` | Passed |
| `env -u KUBECONFIG CI=1 make test` after containment change | Passed, exit 0; normal Go and frontend suites, not tagged live security acceptance |
| `git diff --check` | Passed as part of the post-change test command |
| `docker ps` | Existing Incus, LiteLLM and buildkit containers observed; none treated as disposable infrastructure |
| `kubectl config current-context` | Failed: current-context is not set |
| `make test-security SECURITY_TRACK=urgent` | Exit 2 before cleanup: explicit disposable-environment acknowledgement missing |
| `update_plan(tools-apps-sql, running)` | Rejected: earlier serial phase incomplete |

`KUBECONFIG` was unset only for the normal test process, not changed globally. This resolves the inherited-invalid-kubeconfig test contamination; it does not provision or approve Kubernetes infrastructure. Frontend test warnings do not change the successful exit status and are not live browser proof.

## Earlier implemented work retained

Earlier working-tree remediation includes configured-backend setup rejection and explicit bootstrap capability checks, setup-client token submission without browser-storage persistence, copied/redacted settings and MCP responses, exact-target masked updates, MCP writable-scope authorization before effects, session ownership enforcement, memory preauthorization, and channel verification persistence/proof checks. Prior focused tests and race tests were reported and recorded in the session. Those claims are not upgraded to deployed or live-verified status here.

The urgent tagged test file now contains configuration, session, artifact, MCP, memory and channel scenarios. Their presence/compilation is not execution evidence. The complete same-slug cross-org matrix, positive counterparts, side-effect assertions, first-run backend acceptance and browser/TUI checks still require reconciliation and execution.

## Required inputs and unresolved gates

1. **Disposable acceptance environment:** identify an approved Kubernetes context and explicitly dedicated DB, namespaces and PVCs. The approved namespace names are `astonishe2e` and `astonishe2e-sandbox`; names alone do not prove isolation. Supply isolated PostgreSQL configuration and required test runtime/provider prerequisites. Do not bypass preflight or designate existing workloads disposable without authorization.
2. **Artifact implementation:** provide the confined sandbox read boundary and verify live/restored artifact, browser and terminal compatibility. Current disabling cannot satisfy phase acceptance.
3. **Team decisions:** approve intended team-session-memory visibility, shared-app data read/write/DDL rights, fleet execution principal/active-edit approval, credential consent/destination grants, and supported multi-user runtime profile. These are not reviewer decisions.
4. **Architecture gates:** verify SQLite authorizer feasibility and dependency choice, approve/test replacement sandbox isolation, and approve any required durable fleet schema extension before implementation.
5. **Reviewer follow-up:** exact current-main severities and pinned links for TOOL-02 through TOOL-07 remain unspecified; do not guess them.
6. **Operational evidence:** actual trusted-mail-receiver header stripping and approved incident response/rotation remain outstanding. Disposable rehearsals cannot establish production actions.

## Next permitted execution

Resume `urgent-boundary-fixes` to implement confined artifact reads and run acceptance after safe prerequisites are supplied. Preserve the full urgent acceptance command and its positive cases; do not mark it complete from containment or unit tests. Once that phase passes, the runtime permits the next architectural phase. The remaining plan is authoritative and has not been replaced or marked complete.

---

## Continuity checkpoint — approved revised plan, 2026-10-20

**This section supersedes current-status/next-step statements above; preceding content remains historical evidence.** The revised plan is approved. Its current phase is `continuity-and-acceptance`; completion requires the runtime verification below. The original `handoff-and-decisions` pass and original `urgent-boundary-fixes` exit-2 failure are not reset. No historical test is claimed to have rerun unless listed here.

### Checkout and evidence preservation

- HEAD observed: `a66f59d86e92596828273ed12b6a3f9db8e28c78`, `fix(security): consolidate urgent boundary remediation`. Earlier urgent edits are committed, not still uncommitted as earlier reports describe. No new commit/release/deployment is made here.
- On resumption only `tests/scenarios/parse-run.test.mjs` was modified. Unrelated untracked paths: `slides-diag`, `slides-import-fidelity-plan.md`, `slides_improve/`; untouched. `git ls-files --error-unmatch web/src/components/chat/ArtifactDelivery.test.tsx` exited 0: the previously untracked test is now tracked.
- The plan revision reused `/Users/I851355/Library/Application Support/astonish/sessions/code/astonish_code/local_user_f32806e58f42b514/90d09ce8-7908-452b-b403-757c74f32e89.PLAN.md`. Original contents were recovered in planning, but no unchanged original sidecar copy is claimed. The saved inventory/reports, not every unsaved conversation message, are recoverable evidence.
- `SECURITY_REMEDIATION_LATEST_CHECK.md` and `SECURITY_CHANNEL_LINK_CHECK.md` remain untouched. §11 of `SECURITY_REMEDIATION_ANALYSIS.md` adds the full ID-to-phase ledger, policy/provenance supersession and residual migration/deployment obligations.

### Decisions retained

D-02 is approved: simple personal apps remain maintainable with private state; team apps/data are collaborative for all team members, including app-local read/write/DDL and maintenance. APP-02 must not describe authorized team collaboration as a defect. Org/platform publication remains independently authorized. Publishing definitions does not copy private state or grant use of another user's personal secrets.

D-01 memory visibility, D-03 fleet principal/active edits, D-04 credential consent and D-05 runtime remain separate unapproved gates. SQLite authorizer/dependency feasibility and possible fleet schema expansion need their specified approval. Proposed owners are roles, not named assignments. TOOL-01 remains High; TOOL-02..07 severity/links remain unspecified. Knowledge-debug, refresh-session and remote-MCP review candidates remain unconfirmed. Incident response remains mandatory for exposed deployments; PR594-03 remains optional low-priority hardening.

### Changes and tests in this continuation

| Path / symbols | Change |
|---|---|
| `tests/scenarios/parse-run.mjs`: securityManifests, selectionErrors, strict lifecycle/report | Exact urgent package, six categories and three session actor subcases; reject incomplete/invalid events and missing metadata; allowlisted selection-only gate; real command/revision/storage/runtime/count report |
| `tests/scenarios/parse-run.test.mjs` | Negative event/metadata/subcase cases, both backend reports, legal parallel transitions, actual Make invalid-selector rejection; ordinary parsing preserved |
| `Makefile`: test-security, test-security-preflight | Explicit urgent/both/k8s selection; validate both storage profiles; preserve inspector rejection; export executed command and revision; no automatic disposable acknowledgement |
| `tests/e2eboot/security_preflight.go`: requireSecurityPreflight | Guard every TestE2E_Security prefix, not only urgent |
| `tests/e2eboot/security_preflight_test.go`: TestSecurityPreflightBeforeBootstrap, TestE2E_SecurityPreflightProbe | Subprocess proof on PostgreSQL/SQLite with and without acceptance flag; synthetic environment, PATH trap, temporary HOME |
| `docs/architecture/testing.md` | Exact operator prerequisites, reporting limitations and urgent-versus-full-artifact split |
| `SECURITY_REMEDIATION_ANALYSIS.md`, this file | Additive evidence/policy/phase ledger; no historical report erasure |

| Actual command/check | Result this continuation |
|---|---|
| `node --test tests/scenarios/parse-run.test.mjs` before parser implementation | Exit 1: 16 passed, 13 failed; missing subcases/metadata/order were accepted |
| Parser tests after first implementation | Exit 0: 37 passed |
| New prefix bootstrap regression before guard correction | Exit 1: both no-flag probes bypassed the gate; PATH blocked kubectl and synthetic invalid DSN prevented external DB use, but disposable local bootstrap/model download occurred. This is a demonstrated gate failure, not claimed absence of all effects. Temporary test state is cleanup-managed. |
| `node --test tests/scenarios/parse-run.test.mjs && go test -tags=e2e ./tests/e2eboot -run '^TestSecurityPreflight(Validation\|BeforeBootstrap)$' -count=1 && git diff --check` after changes | Exit 0: 46 Node tests and focused Go preflight tests passed; no skipped Node cases |
| Git HEAD/title and tracked artifact test check | Exit 0; results recorded above |

### Remaining phase order and blockers

After runtime verification of continuity, start **both wave-2 phases** together: `urgent-independent-fixes` and `sandbox-and-artifact-boundaries`. Urgent can expand independent regressions without waiting for runtime design. It cannot pass live acceptance until an operator confirms actual disposable cluster/context, admin DB target, eligible namespaces/PVCs and provider setup. Do not set acknowledgement, inspect secrets or initiate destructive cleanup on inferred authorization.

The sandbox phase must stop at D-05 unless an approved concrete runtime/storage/reader/capture/publisher design with feasibility evidence exists. Platform artifact disabling remains containment. Full original ART-01/02 live binary/shell-created/stopped/restored/browser/terminal acceptance moved to sandbox and final closure; it was not deleted. The urgent track's existing manifest is a minimum current coverage contract, not proof that all expanded urgent scenarios are implemented.

Later phases remain pending: `tools-apps-and-sql`, `tenant-fleet-lifecycle`, `hardening-and-incident-rehearsal`, `closure-matrix-and-handoff`. Unimplemented selectors (including `all`) are rejected. Malformed quick-fix-table follow-up, classification/provenance reconciliation and reviewer scoring/link follow-ups remain handoff duties; no unobserved reviewer approval is claimed. No live matrix, production key/MTA action, runtime/SQL/broker acceptance or overall completion is claimed.


## Wave-2 checkpoint — blocked gates, 2026-10-20

**Latest runtime status:** `continuity-and-acceptance` complete (runtime verify passed, including a rerun after harness updates); `urgent-independent-fixes` failed/blocked, with partial test-only implementation; `sandbox-and-artifact-boundaries` failed/blocked at D-05. All four later phases remain pending. No overall completion tool was called.

The wave-2 tasks were dispatched together. Urgent delegation added tests; runtime delegation failed before executing any tools (`duplicate tool: skill_lookup`). Main thread performed the bounded non-destructive runtime readiness check instead, without retrying delegation or modifying runtime production code.

### Additional implemented tests (not live verified)

- `tests/e2e/chat_auth/security_urgent_test.go`: `TestE2E_SecurityUrgentConfiguredInit` requires 409 from both init endpoints for Alice/Bob, unchanged file config/JWT, persisted settings and organization count, and absent rejected SQLite directory. `TestE2E_SecurityUrgentSameTeamOwnerDenied` requires Alice's org-owner role not to grant access to Bob's private session, with no mutation, Bob resume/history and deletion positives. `urgentCompletedChat` requires text/done and rejects error.
- `tests/scenarios/chat.yaml`: CHAT-101/102 entries; COVERS markers added to those tests. Catalog `covered` means implemented, not live passed.
- `tests/scenarios/parse-run.mjs` and `.test.mjs`: register both tests and exact init endpoint/actor subcases; urgent requires 17 entries. Missing-init-actor and missing-owner-direction regressions added; now 48 parser/selector tests.
- `docs/architecture/testing.md`: manifest-count update. No urgent production handlers or sandbox behavior changed in this slice.
- `tests/e2eboot/security_preflight_test.go`: subprocess working directory now `t.TempDir()` as well as temporary HOME/PATH. Correction to earlier cleanup statement: the failing-before probe left synthetic SQLite fallback files in the checkout. Fresh `git status` exposed exactly `tests/e2eboot/dedicated-test-placeholder dbname=astonish_e2e9802d293_platform` and `tests/e2eboot/orgs/`; narrowly scoped `git clean -fd --` of those two test-created paths exited 0. No unrelated files were removed. Future regression probes isolate CWD too.

### Additional observed verification

- Main-thread `node --test tests/scenarios/parse-run.test.mjs` — exit 0, 48/48.
- `go test -tags=e2e ./tests/e2eboot -run '^TestSecurityPreflight(Validation|BeforeBootstrap)$' -count=1` — exit 0 after CWD isolation.
- `go test -tags=e2e ./tests/e2e/chat_auth -run '^$' -count=1` — exit 0, explicitly **no tests to run**. Compilation only, not positive security evidence.
- Delegated focused existing API init/bootstrap authority/serialization tests — exit 0 (`go test ./pkg/api -run '^(TestSecurityUrgentInitConfiguredBeforeBody|TestPlatformSetupRequiresExplicitBootstrapAuthority|TestPlatformSetupExplicitAuthorityReachesValidation|TestPlatformSetupSerializesBothEntryPointsAndRechecksConfig)$' -count=1`). No new production defect or failing-before assertion inferred.
- `git diff --check` — exit 0.
- `kubectl config current-context` — exit 1, current-context not set.
- `docker ps --format '{{.Names}}\t{{.Status}}'` — exit 0; existing `astonish-incus`, `litellm-proxy`, `buildx_buildkit_astonish-builder0` observed. None designated disposable or a verified hostile-isolation runtime.
- `make test-security-preflight SECURITY_TRACK=urgent ASTONISH_E2E_DEDICATED=` — exit 2, expected rejection before cleanup. Explicitly empty acknowledgement prevents any inferred permission; this is not a successful live phase verification. Full `make test-security` was not run.

### Exact next actions

1. Obtain operator confirmation of actual disposable cluster/context, PostgreSQL target, namespace pair and eligible PVCs, plus securely supplied provider/runtime prerequisites. Never paste secrets into the report or set operator acknowledgement on their behalf. Then resume urgent regression expansion and both-storage live acceptance; current additions are not exhaustive (authorized first-run, full masked-update/scope matrices, duplicate-slug cross-org fixture, MCP global-secret side effects, consolidation permutations, channel concurrent/persistence cases still required).
2. Obtain D-05 concrete approved runtime/storage/backend and trusted reader/capture/publisher topology with feasibility evidence for shell/install/browser/capture; then a bounded follow-on implementation design if necessary. Current runtime replacement cannot be improvised. Preserve platform PullFile containment and full ART positive obligations.
3. Preserve D-01/D-03/D-04, SQLite authorizer/dependency and potential fleet schema gates; do not jump to blocked downstream execution or call the plan complete. Production rotation/mail proof and optional PR594-03 remain separately tracked.

Current changed paths: Makefile; SECURITY_REMEDIATION_ANALYSIS.md; SECURITY_REMEDIATION_PROGRESS.md; docs/architecture/testing.md; tests/e2e/chat_auth/security_urgent_test.go; tests/e2eboot/security_preflight.go; tests/e2eboot/security_preflight_test.go; tests/scenarios/chat.yaml; tests/scenarios/parse-run.mjs; tests/scenarios/parse-run.test.mjs. Unrelated slides untracked paths remain untouched. HEAD remains a66f59d86e92596828273ed12b6a3f9db8e28c78. No release, deployment, production rotation or receiving-MTA change performed.


## Urgent implementation continuation — final live verification deferred by user

This checkpoint supersedes the latest implementation/status statements above, not their historical evidence. User requested: “continue, I will verify everything in the end.” Live acceptance is deferred, not waived or passed. Runtime rejected `update_plan(tools-apps-and-sql, running)` with `serial_blocked`; resumed `urgent-independent-fixes` instead. D-01/D-03/D-04/D-05, SQLite feasibility/dependency and fleet-schema gates remain unchanged. No sandbox architecture, app collaboration policy, credential-consent policy or production infrastructure changed.

### Implemented changes and reproduced defects

- `pkg/api/provider_settings_handlers.go`: `mergeProviders` now returns `(map[string]map[string]string, error)` and validates exact nonempty masks in the authorized instance/field. Both platform/org save callers reject invalid placeholders before Save and abort on settings Get errors. `maskProviderSecrets` and effective-provider response masking use the legacy instance name when `type` is omitted, preventing SAP/OAuth secret disclosure.
- `pkg/api/settings_handlers.go`: team patch/replacement masked fields require the exact stored-value mask, not merely any existing nonempty value. Omitted, replacement, deletion and explicit-clear semantics remain distinct.
- `pkg/api/provider_settings_handlers_test.go`: 44-case `TestProviderMaskedUpdateMatrix`; `TestProviderSaveReadFailureDoesNotWrite`; `TestProviderResponseMaskingLegacyTypeAndCopy`. Delegated failing-before command `go test ./pkg/api -run 'TestProvider(MaskedUpdateMatrix|SaveReadFailureDoesNotWrite|ResponseMaskingLegacyTypeAndCopy)$' -count=1` reproduced stale-mask acceptance, writes following read failures and legacy response leaks. Delegated post-change focused command passed. These are CFG-track FixedPendingVerification changes, not live closure.
- `pkg/api/standard_servers_handler.go`: `UninstallStandardServerHandler` returns bounded HTTP 500 immediately on MCP-store Delete failure, before credential cleanup/settings changes/chat reset.
- `pkg/api/standard_servers_handler_test.go`: new scoped install/uninstall, pre-effect rejection, failed Save and failed Delete tests, with isolated platform and encrypted legacy file secrets. Delegated before-fix delete tests failed in team/org/platform; platform failure removed both secrets and reported success. Post-fix targeted tests passed three times. Existing platform uninstall test now isolates the legacy store too. MCP-track FixedPendingVerification; successful install tests deliberately suppress discovery, so they do not prove live sandbox discovery.
- `pkg/api/user_channel_handlers_test.go`: `TestVerifyEmailCode_ConcurrentHandlersSinglePersistence` runs eight simultaneous HTTP handler calls against memory and DB-adapter backends, requires one success and one correctly owned verified/enabled link. `TestVerifyEmailCode_LookupFailureDoesNotPersist` checks bounded lookup errors, zero persistence and replay denial. These passed without changing channel production code; no new channel vulnerability inferred.
- `docs/architecture/configuration.md`, `docs/architecture/mcp.md`, `docs/architecture/channels.md`: document exact behavior and local-test limitations.

### Main-thread verification this continuation

All commands below completed with exit 0:

1. `gofmt -w pkg/api/user_channel_handlers_test.go && go test ./pkg/api -run 'TestVerifyEmailCode_|TestProvider(MaskedUpdateMatrix|SaveReadFailureDoesNotWrite|ResponseMaskingLegacyTypeAndCopy)$|TestSecurityUrgent|TestSavePlatformProviders_|TestFullConfig|TestRestoreBrowserURL|TestRedactBrowserURL|Test(StandardServer|UninstallStandardServer|MCPWrite|MCPInstallRejects)' -count=1 && git diff --check`
2. `go test -race ./pkg/api -run 'TestVerifyEmailCode_(ConcurrentHandlersSinglePersistence|LookupFailureDoesNotPersist)$|TestProvider(MaskedUpdateMatrix|SaveReadFailureDoesNotWrite|ResponseMaskingLegacyTypeAndCopy)$|TestStandardServer(ScopedInstallUninstallPreservesGlobalSecrets|MutationRejectsBeforeEffects|InstallSaveFailurePreservesSecrets|UninstallDeleteFailurePreservesSecrets)$' -count=1`
3. `go test ./pkg/api -count=1` — full API package passed (30.096s).
4. `git diff --check && git diff --stat && git status --short`.

During delegation, MCP checks briefly encountered partially edited provider signatures; the final combined targeted, race and full API runs above passed after both agents finished. No compile error remains in that package at this checkpoint. These are local regressions, not actual PostgreSQL/SQLite live matrix, deployed middleware, browser/TUI, sandbox discovery or service integration evidence. No `make test-security` run or phase completion attempted; the prior exit-2 preflight record remains authoritative. No production keys, real credentials, network cleanup or MTA modifications used.

### Durable next steps

`urgent-independent-fixes` remains partial and live-verification-blocked; `sandbox-and-artifact-boundaries` remains D-05-blocked; tools/apps/SQL, fleet, operations and final closure remain pending. Next implementation is the remaining urgent authenticated E2E expansion: authorized first-run, real route role/scope and masked-update matrix, duplicate-slug cross-org fixtures, MCP global-secret side effects, consolidation permutations and channel persistence/concurrency. Port the new local assertions into real fixtures without relaxing preflight; defer destructive live runs until operator-approved infrastructure is supplied. Do not confuse current local tests with completing these E2E obligations. Runtime still requires earlier verification before advancing downstream phases.

HEAD remains `a66f59d86e92596828273ed12b6a3f9db8e28c78`. Existing ten modified paths retained: Makefile; SECURITY_REMEDIATION_ANALYSIS.md; SECURITY_REMEDIATION_PROGRESS.md; docs/architecture/testing.md; tests/e2e/chat_auth/security_urgent_test.go; tests/e2eboot/security_preflight.go; tests/e2eboot/security_preflight_test.go; tests/scenarios/chat.yaml; tests/scenarios/parse-run.mjs; tests/scenarios/parse-run.test.mjs. Added nine modified paths this turn: the six API production/test paths and three architecture documents listed above. Unrelated untracked `slides-diag`, `slides-import-fidelity-plan.md`, `slides_improve/` remain untouched. Standalone continuation report: `SECURITY_URGENT_CONTINUATION.md`. No commits, releases, deployment, production rotation, policy approvals or overall completion claimed.

## Channel lifecycle E2E continuation — live acceptance still deferred

This additive checkpoint preserves all prior findings, decisions, failed verification and approval gates. `urgent-independent-fixes` was marked running for this implementation slice; it remains incomplete. `sandbox-and-artifact-boundaries` remains blocked at D-05; tools/apps/SQL, fleet, operations and closure remain pending. No attempt was made to bypass the earlier serial verification requirement.

### Changes this turn

- `tests/e2e/chat_auth/security_urgent_test.go`: added `TestE2E_SecurityUrgentChannelProofLifecycle` (CHAT-103). Seeds a real pending disabled link through the existing harness store and asserts its initial state. Named cases require direct verification to return 410 for owner/member without mutation; expired proof to leave state unchanged; eight simultaneous authenticated HTTP requests to produce exactly one 200 and seven 400s, with one existing link verified/enabled, proof consumed and replay unchanged; and a conflicting user's proof to return 409 then replay 400 without changing the original link. Transport, 429 and 5xx errors cannot count as successful denial evidence. Request goroutines return results to the test thread rather than using Fatal. No production handler changed or new channel defect claimed.
- `tests/scenarios/parse-run.mjs`: requires the lifecycle test and its four named subcases, increasing urgent manifest entries from 17 to 22.
- `tests/scenarios/parse-run.test.mjs`: four missing-channel-subcase rejection regressions, updated counts, and CHAT-103 report mapping assertion.
- `tests/scenarios/chat.yaml`: CHAT-103 implemented-coverage entry (not a live-pass claim).
- `docs/architecture/testing.md`: records the 22-entry minimum manifest and the separation from SMTP/injected persistence-failure acceptance.

### Observed checks

- Before manifest registration, `node --test tests/scenarios/parse-run.test.mjs` exited 1: 46 passed, 6 failed. Four missing channel cases were accepted, and two reports retained the old required count.
- After registration, `gofmt -w tests/e2e/chat_auth/security_urgent_test.go && node --test tests/scenarios/parse-run.test.mjs && go test -tags=e2e ./tests/e2e/chat_auth -run '^$' -count=1 && go test -tags=e2e ./tests/e2eboot -run '^TestSecurityPreflight(Validation|BeforeBootstrap)$' -count=1 && git diff --check` exited 0: 52 parser tests passed; E2E package compiled with no tests executed; focused preflight tests passed.
- After adding initial-state and scenario-mapping assertions, `gofmt -w tests/e2e/chat_auth/security_urgent_test.go && node --test tests/scenarios/parse-run.test.mjs && go test -tags=e2e ./tests/e2e/chat_auth -run '^$' -count=1 && go test -race ./pkg/api -run '^TestVerifyEmailCode_' -count=1 && git diff --check` exited 0: 52 parser tests, E2E compilation only, and existing email handler race regressions passed.

The new E2E scenario has NOT executed against either live database backend. It checks persisted outcome, not the exact number of persistence attempts; the earlier local spy tests cover attempt counts. SMTP/MTA, injected persistence failures through real routes, multi-replica delivery and browser/TUI remain unverified. No infrastructure acknowledgement was supplied; no new destructive preflight/live acceptance run occurred. Historical exit-2 live-preflight failure remains unsuperseded.

### Next exact work and preservation

Continue urgent authenticated setup/role/scope/masked-update coverage, duplicate-slug cross-org fixtures, scoped MCP side-effect assertions, consolidation permutations and real-route persistence-failure coverage; then run both-storage acceptance only on operator-approved disposable infrastructure. D-01/D-03/D-04/D-05, SQLite driver feasibility/dependency and fleet-schema gates remain open. Do not remove ART confinement containment or infer credential consent from approved team collaboration. Full pending inventory remains in the approved plan, SECURITY_REMEDIATION_ANALYSIS.md and SECURITY_REMEDIATION_REMAINING.md.

HEAD observed unchanged: `a66f59d86e92596828273ed12b6a3f9db8e28c78`. All prior working-tree modifications were retained. This turn changes only the five test/harness/docs paths above, this progress file, and the standalone `SECURITY_CHANNEL_E2E_CONTINUATION.md` report. Existing untracked continuation/remaining reports and `slides-diag`, `slides-import-fidelity-plan.md`, `slides_improve/` remain untouched. No commit, release, deployment, production rotation, mail-server change or plan completion.

## Memory participant E2E continuation — local checks only

Latest status: `urgent-independent-fixes` running/incomplete, `continuity-and-acceptance` complete, `sandbox-and-artifact-boundaries` failed/blocked at D-05, all four later phases pending. The prior failed live verification remains unsuperseded. No phase completion was attempted.

Added `TestE2E_SecurityUrgentMemoryParticipants` (CHAT-104) in `tests/e2e/chat_auth/security_urgent_test.go`: source, duplicate, discard, implicit destination and explicit destination paths each require denied/no persisted mutation and allowed/persisted result. Every case includes an authorized source before the potentially forbidden participant; full team snapshots also protect unrelated rows. This is authorization atomicity coverage, not DB-error rollback evidence. No production handler changed. `tests/scenarios/parse-run.mjs`, `tests/scenarios/parse-run.test.mjs`, `tests/scenarios/chat.yaml` and `docs/architecture/testing.md` now require/document all ten outcomes (38 required parent/subtest entries).

Observed commands:
- Initial `node --test tests/scenarios/parse-run.test.mjs && go test -tags=e2e ./tests/e2e/chat_auth -run '^$' -count=1 && go test -race ./pkg/api -run '^TestVerifyEmailCode_' -count=1 && git diff --check`: exit 2 only at diff check for this file's trailing blank line; 52 parser tests, compilation-only and email race checks passed. Removed that blank line.
- New memory parser regressions before manifest registration: `node --test tests/scenarios/parse-run.test.mjs` exited 1 (50 passed, 12 failed): all ten missing memory outcomes were accepted; two reports had the old required count.
- `gofmt -w tests/e2e/chat_auth/security_urgent_test.go && node --test tests/scenarios/parse-run.test.mjs && go test -tags=e2e ./tests/e2e/chat_auth -run '^$' -count=1 && go test -race ./pkg/api -run '^TestConsolidationPreauthorizesAllParticipants$' -count=1 && go test -tags=e2e ./tests/e2eboot -run '^TestSecurityPreflight(Validation|BeforeBootstrap)$' -count=1 && git diff --check`: exit 0, 62 parser tests passed; E2E compilation only; existing memory preauthorization race tests and focused preflight tests passed.

The new memory E2E and prior channel E2E have NOT run on PostgreSQL or SQLite. User-deferred live acceptance stays deferred; no acknowledgement or destructive command supplied. Persistence-error rollback, embedding call counts, real-route injected failures, SMTP/MTA, browser/TUI and multi-replica acceptance remain unverified. Continue urgent authenticated first-run, full role/scope/masked-canary updates, duplicate-slug cross-org sessions, scoped MCP effects and memory/channel persistence-error coverage. D-01/D-03/D-04/D-05, driver and fleet schema gates remain open. Team apps/data remain collaborative within the team boundary, personal maintenance/private data supported, personal credentials not implicitly shared. ART live-reader obligations and containment remain unchanged.

HEAD still `a66f59d86e92596828273ed12b6a3f9db8e28c78`; prior working-tree modifications and unrelated slides retained. This slice touches only the five test/harness/docs paths above, this progress log and new `SECURITY_MEMORY_E2E_CONTINUATION.md`. Correction to the preceding checkpoint: the named `SECURITY_CHANNEL_E2E_CONTINUATION.md` standalone report was not present in the observed git status; channel evidence is preserved here, not claimed as an existing separate artifact. No commit/release/deployment/production rotation/MTA action or overall completion.

## Organization provider E2E continuation — implemented, live acceptance deferred

`urgent-independent-fixes` remains running/incomplete; continuity is complete, sandbox remains D-05-blocked, and all four downstream phases remain pending. No approval or phase completion changed. The historical live preflight failure remains unsuperseded.

Added `TestE2E_SecurityUrgentOrgProviderMasks` (CHAT-105) in `tests/e2e/chat_auth/security_urgent_test.go`: 13 named cases cover redacted reads, member read/write denial, exact/stale/unknown/foreign/unknown-field/empty-stored masks, mixed-invalid updates, explicit replacement, omitted providers, deletion and clearing. Cases assert persisted org state, unchanged other-org/platform settings, no response canary leakage and preserve defaults. Synthetic settings restore through cleanup before harness teardown. This is organization-provider coverage, not the full platform/team/replacement-mode matrix. No production handler changed or new handler defect claimed.

`tests/scenarios/parse-run.mjs` now requires 52 parent/subtest entries. `tests/scenarios/parse-run.test.mjs` requires each new case and CHAT-105 report mapping; `tests/scenarios/chat.yaml` and `docs/architecture/testing.md` record implemented coverage without live-pass claims.

Observed verification:
- Before manifest registration, `node --test tests/scenarios/parse-run.test.mjs` exited 1: 60 passed, 15 failed. All 13 omitted provider cases were accepted; two reports retained the old required count.
- `gofmt -w tests/e2e/chat_auth/security_urgent_test.go && node --test tests/scenarios/parse-run.test.mjs && go test -tags=e2e ./tests/e2e/chat_auth -run '^$' -count=1 && go test -race ./pkg/api -run '^TestProvider(MaskedUpdateMatrix|SaveReadFailureDoesNotWrite|ResponseMaskingLegacyTypeAndCopy)$' -count=1 && go test -tags=e2e ./tests/e2eboot -run '^TestSecurityPreflight(Validation|BeforeBootstrap)$' -count=1 && git diff --check` exited 0: 75 parser tests; E2E compilation only; existing provider race and preflight tests passed.
- `git diff --stat && git status --short && git rev-parse HEAD` exited 0. HEAD remains `a66f59d86e92596828273ed12b6a3f9db8e28c78`; prior changes preserved. This slice modifies only security_urgent_test.go, the parser and parser tests, chat.yaml, testing.md and this log; a standalone provider continuation report is also written. Existing untracked memory/remaining/status/urgent reports and unrelated slides paths remain untouched.

Next: remaining platform/team authenticated mask matrices, authorized first-run acceptance, duplicate-slug cross-org session fixtures, scoped MCP side effects and real-route memory/channel persistence failures. New provider/channel/memory E2E scenarios have not run on PostgreSQL or SQLite; live acceptance remains deferred pending operator-approved disposable infrastructure. D-01/D-03/D-04/D-05, SQLite driver and possible fleet schema gates remain open. ART containment and full live-reader obligations, approved team collaboration/private personal maintenance, non-sharing of personal credentials and production incident/MTA duties remain unchanged. No commit, release, deployment, production rotation or mail action.

## Same-slug session E2E continuation — reconciled and locally checked

This checkpoint supersedes the previous next-step request to add duplicate-slug session coverage: that coverage is now implemented, but NOT live verified. `continuity-and-acceptance` remains complete; `urgent-independent-fixes` is running/incomplete; `sandbox-and-artifact-boundaries` remains failed/blocked at D-05; `tools-apps-and-sql`, `tenant-fleet-lifecycle`, `hardening-and-incident-rehearsal` and `closure-matrix-and-handoff` remain pending. Historical preflight exit 2 remains unsuperseded. No completion verification was attempted without approved infrastructure.

### Reconciled implementation

- `tests/e2eboot/seed.go`: opt-in `SeedSameSlugTeams` provisions distinct team IDs with the identical `collision` slug in Alice's and Dave's separate organizations; validates persisted identities and owner roles, rejects shared inspector mode, reuses seeded authentication, and leaves ordinary Seed layout unchanged.
- `tests/e2e/chat_auth/security_urgent_test.go`: `TestE2E_SecurityUrgentSameSlugCrossOrgSessions` / CHAT-106 creates two private sessions. For each of `alice@acme.test` and `dave@globex.test`, requires `read-list-denied`, `delete-denied`, `injection-denied` and `owner-resume`; separately requires `owner-delete/alice@acme.test` and `owner-delete/dave@globex.test`. Denials preserve both owner-readable snapshots. Resume retains session identity and private history without foreign/injected markers. Owner deletion removes that session from read/list while preserving the other surviving session. Idempotent unauthorized DELETE may return 204 only with unchanged persisted snapshots.
- `tests/scenarios/parse-run.mjs` and `tests/scenarios/parse-run.test.mjs`: require all 13 added parent/actor/outcome entries, for 65 required entries total. Omission of each collision entry independently fails strict parsing; the complete synthetic report maps CHAT-106 to pass. Synthetic events are parser evidence, not executed server evidence.
- `tests/scenarios/chat.yaml` and `docs/architecture/testing.md`: document implemented coverage and explicitly separate it from live acceptance and sandbox filesystem collision isolation.
- No additional production changes or newly reproduced production defect in this continuation. A failing-before result for these specific new collision parser regressions was not available in the compacted evidence and is not claimed.

### Commands observed after resumption

All following completed with exit 0:

1. `node --test tests/scenarios/parse-run.test.mjs && go test -tags=e2e ./tests/e2e/chat_auth -run '^$' -count=1 && go test -tags=e2e ./tests/e2eboot -run '^TestSecurityPreflight(Validation|BeforeBootstrap)$' -count=1 && git diff --check` — 88 parser tests passed, zero skipped; E2E compilation explicitly reported no tests to run; focused preflight unit tests passed.
2. `git diff -- tests/scenarios/parse-run.test.mjs && gofmt -d tests/e2eboot/seed.go tests/e2e/chat_auth/security_urgent_test.go && go test -race ./pkg/api ./pkg/store/entstore -run 'Test.*Session' -count=1 && git diff --check` — no formatting differences; session-matching tests passed in both packages with race detection. These existing local tests do not execute the new two-org authenticated E2E.
3. `git rev-parse HEAD && git diff -- tests/scenarios/parse-run.mjs && git status --short` — HEAD remains `a66f59d86e92596828273ed12b6a3f9db8e28c78`.

### Durable remaining work and boundaries

Continue `urgent-independent-fixes`: authenticated platform/team mask matrices, authorized first-run acceptance, scoped MCP no-global-secret effects and real-route memory/channel persistence-error coverage remain outstanding. Run all urgent scenarios on BOTH PostgreSQL and SQLite only after explicit operator approval of actual disposable database, cluster/context, namespaces/PVCs and provider prerequisites. User-deferred live acceptance is not waived. New same-slug, provider, memory and channel E2Es are compiled, not live-passed. No destructive infrastructure command or automatic acknowledgement was used.

D-01 memory, D-03 fleet, D-04 credential consent, D-05 runtime, SQLite feasibility/dependency and possible fleet schema gates remain open. Preserve full ART confined-reader positive/negative acceptance and current containment. Team app maintenance/data/DDL remains collaborative within the team; personal maintenance/private data remains supported; personal credentials are not implicitly shared. Mandatory incident response, production rotation and MTA proof remain outstanding and separately authorized.

Working-tree inventory observed: Makefile; SECURITY_REMEDIATION_ANALYSIS.md; SECURITY_REMEDIATION_PROGRESS.md; docs/architecture/{channels,configuration,mcp,testing}.md; pkg/api/{provider_settings_handlers.go,provider_settings_handlers_test.go,settings_handlers.go,standard_servers_handler.go,standard_servers_handler_test.go,user_channel_handlers_test.go}; tests/e2e/chat_auth/security_urgent_test.go; tests/e2eboot/{security_preflight.go,security_preflight_test.go,seed.go}; tests/scenarios/{chat.yaml,parse-run.mjs,parse-run.test.mjs}. Existing untracked reports: SECURITY_MEMORY_E2E_CONTINUATION.md, SECURITY_PROVIDER_E2E_CONTINUATION.md, SECURITY_REMEDIATION_REMAINING.md, SECURITY_REMEDIATION_STATUS_CURRENT.md, SECURITY_URGENT_CONTINUATION.md. Unrelated untracked slides-diag, slides-import-fidelity-plan.md and slides_improve/ remain untouched. This resumption adds only this checkpoint and SECURITY_SESSION_E2E_CONTINUATION.md after inspecting/checking the six preceding coverage files. No commit, release, deployment, production rotation, MTA change or overall completion.

## Team provider E2E continuation — final local checks recorded

This additive checkpoint supersedes the previous request to implement team-provider coverage, not the historical evidence or gates. `continuity-and-acceptance` remains complete; `urgent-independent-fixes` remains running/incomplete; `sandbox-and-artifact-boundaries` remains failed/blocked at D-05; tools/apps/SQL, fleet, operations and closure remain pending. The historical exit-2 live preflight failure is not superseded by local checks.

### Reviewed implementation

- `tests/e2e/chat_auth/security_urgent_test.go`: `TestE2E_SecurityUrgentTeamProviderMasks` / CHAT-107 contains 13 cases in each of patch and provider-list replacement modes. Covers redacted reads, member write denial, exact/stale/unknown/inherited masks, missing fields, empty stored values, mixed-invalid updates, explicit replacement, omission, empty-map semantics and clearing. Whole persisted target settings are compared to exact expected state; org, foreign-org team and platform settings must remain unchanged. Synthetic target and inherited settings restore through cleanup.
- `tests/scenarios/parse-run.mjs`: urgent now requires 94 parent/subtest entries, including the 26 team cases and their parent/mode entries. `tests/scenarios/parse-run.test.mjs` enforces missing-entry rejection and CHAT-107 mapping. `tests/scenarios/chat.yaml` and `docs/architecture/testing.md` document implemented coverage, not a live pass.
- No new production change was made in this resumption. Prior context reports failing-before parser registration; this turn independently confirms only the current passing checks and does not claim to have rerun that baseline.

### Actual verification after resumption

All commands below exited 0:

1. `node --test --test-reporter=dot tests/scenarios/parse-run.test.mjs && go test -tags=e2e ./tests/e2e/chat_auth -run '^$' -count=1 && go test -race ./pkg/api -run '^TestProviderMaskedUpdateMatrix$' -count=1 && go test -tags=e2e ./tests/e2eboot -run '^TestSecurityPreflight(Validation|BeforeBootstrap)$' -count=1 && git diff --check && git rev-parse HEAD && git status --short` — 117 parser tests passed; authenticated E2E package compiled with explicitly no tests run; existing provider race and preflight unit tests passed.
2. `go test ./pkg/api -list 'Test.*(Mask|Settings)' && gofmt -d tests/e2e/chat_auth/security_urgent_test.go` — test names confirmed; no formatting differences. Listing is not test execution.
3. `go test -race ./pkg/api -run '^TestSecurityUrgent(SettingsMasksResponseOnly|MaskedUpdatesSameTarget|SettingsRejectInvalidWithoutMutation|SettingsOmitAndClear)$' -count=1` — existing team settings regressions passed with race detection.

The new authenticated team scenarios have NOT executed on PostgreSQL or SQLite. Store-failure injection, a complete role matrix and first-run setup positives are not established by these checks. No live server, browser/TUI, sandbox discovery, MTA or deployment acceptance was performed. The earlier preflight failure reflects missing operator approval, not a reason to weaken the gate; no destructive command or acknowledgement was supplied.

### Next exact work and preservation

Continue urgent authenticated platform-provider/admin coverage, authorized first-run setup, scoped MCP no-global-secret effects and real-route memory/channel persistence failures. Run both-storage urgent acceptance only after explicit operator approval of the actual disposable database, cluster/context, namespace pair, eligible PVCs and provider prerequisites. Live verification remains deferred by the user, not waived.

D-01/D-03/D-04/D-05, SQLite authorizer/dependency and possible fleet-schema gates remain open. Keep ART containment and full confined live-reader obligations. Team app maintenance/data/DDL remains collaborative within the team boundary; personal maintenance/private state stays supported; personal credentials are not implicitly shared. Mandatory incident response, production rotation and mail-ingress proof remain separately authorized and outstanding.

HEAD observed unchanged: `a66f59d86e92596828273ed12b6a3f9db8e28c78`. The working-tree inventory from the preceding checkpoint remains, with `SECURITY_SESSION_E2E_CONTINUATION.md` additionally present as untracked. This resumption writes only this progress checkpoint and `SECURITY_TEAM_SETTINGS_E2E_CONTINUATION.md`; existing test/harness/docs changes and unrelated slides paths are retained. No commit, release, deployment, production rotation, MTA action or overall completion.

## Consolidated setup/channel/MCP continuation

Latest detailed evidence is saved in **SECURITY_SETUP_CHANNEL_VERIFICATION.md**; it supersedes the preceding next-work statements, not historical results. Continuity remains complete, urgent-independent-fixes running/incomplete, sandbox-and-artifact-boundaries D-05-blocked, all downstream phases pending. HEAD unchanged: a66f59d86e92596828273ed12b6a3f9db8e28c78.

Recovered broad tests exited 0 (process 2bd66b75): normal Go suite plus 109 frontend files/828 tests. Focused API/store/race and real HTTP SQLite setup passed. New first-run absent-directory assertion failed before removing harness pre-creation, then passed in both auth modes; repeated three times also passed. Added stale-owner write-time channel proof regression (SQLite/mock), preserving unchanged-row/replay/fresh-proof behavior. Temporary-SQLite registered-route channel and memory destination-write failure tests exist and pass; statements that all such route tests are absent are obsolete. Login is not exercised by those test-issued-JWT fixtures.

Further review reproduced SQLite setup raw-error leakage; fixed operation-level responses and added pkg/api/platform_setup_errors_test.go (seven storage fault cases, two request cases, structural remaining-error guard). Team-membership failure is structurally covered, not directly injected. Final production check 39dae30c exited 0: full API/store, focused race, real HTTP first-run setup, make lint (zero issues), Go build to /tmp/astonish-security-verified and diff check. Frontend typecheck/build exited 0; Vite warnings remain. Broad make test preceded final setup-error/MCP additions; later focused checks cover them.

MCP install coverage adds six independent-principal cases: team-admin default/team success, org/platform denial, sibling-team denial and platform-admin platform success with expected scoped global-secret effects. Strict manifest now requires 147 entries; 171 parser tests pass. Tagged chat_auth compilation exits 0 with explicitly no tests run. MCP cases have NOT run against infrastructure. No full matrix/browser/TUI/actual daemon-process restart or PostgreSQL setup positive claimed.

Current continuation paths: pkg/api/{platform_setup_handlers.go,platform_setup_errors_test.go,user_channel_handlers_test.go}; tests/e2eboot/platform_core.go; tests/e2e/chat_auth/{chat_auth_scoping_test.go,chat_mcp_standard_install_test.go}; tests/scenarios/{parse-run.mjs,parse-run.test.mjs,chat.yaml}; docs/architecture/{configuration,channels,testing}.md; this progress file and SECURITY_SETUP_CHANNEL_VERIFICATION.md. Retained prior modified paths: Makefile; SECURITY_REMEDIATION_ANALYSIS.md; docs/architecture/mcp.md; pkg/api/{platform_setup_handlers_test.go,provider_settings_handlers.go,provider_settings_handlers_test.go,settings_handlers.go,standard_servers_handler.go,standard_servers_handler_test.go,user_channel_handlers.go}; pkg/store/{store.go,entstore/user_channels.go}; tests/e2e/chat_auth/security_urgent_test.go; tests/e2eboot/{security_preflight.go,security_preflight_test.go,seed.go}. New untracked test/report named above; existing untracked security reports and unrelated slides-diag, slides-import-fidelity-plan.md, slides_improve/ retained. No generated source/dependency changes, commit, release, deployment, key rotation or MTA action.

Next exact phase: urgent-independent-fixes. Remaining implementation includes distinct org-admin settings/MCP positives, independent team-admin nonempty provider mutation, platform-admin full-config write success, broader MCP write/legacy/inline-install/discovery matrices with no-start-effect assertions and absent-global-key variants. Do not recreate existing platform-provider, SQLite setup or local memory/channel destination-write failure coverage. PostgreSQL first-run, actual new-process restart/encryption continuity, key write/sync/close fault injection and later-stage memory deletion-failure behavior remain unverified. Both-storage acceptance still needs operator-approved actual disposable DB/cluster/context/namespaces/PVCs/provider prerequisites. Preserve D-01/D-03/D-04/D-05, SQL driver and fleet-schema gates, full ART live-reader obligations/containment, team collaboration/personal privacy policy, and separately authorized production incident/rotation/MTA duties. No phase completion or gate bypass attempted.

## Autonomous review and legacy MCP repair — 2026-10-20

This checkpoint supersedes the preceding next-work assertions, not historical evidence. Recovered atomic SQLite key publication and static error guard repairs pass independent focused race review. Key write/sync/close/concurrent publication tests and actual new-process SQLite continuity now exist; PostgreSQL first-run remains unverified. Memory deletion failures are covered through local registered HTTP/SQLite and injected handler tests; cleanup stops at first error, returns bounded failure, preserves the durable destination, and explicitly does not promise transaction rollback.

New production defect reproduced in 22 local subtests: legacy `UpdateMCPServerHandler` used team authority plus merged read scope, permitting platform mutation by an org admin and wrong omitted-scope routing. `RefreshMCPServerHandler` read storage before write authorization and could fall back to global file discovery. Both now use `mcpStoreForWrite` before lookup/body decoding. Omitted scope is team. File refresh requires explicit platform scope/superadmin, an available platform store and a nil-row/no-error result; missing stores and storage errors never fall back. Read-only resolution, local Code tools, SSE and team-app policy unchanged. Concrete newly confirmed urgent call-site blast radius: `pkg/api/handlers.go`, `pkg/api/tools_cache.go`; matching tests in `mcp_handlers_test.go`, `tools_cache_file_servers_test.go`, and `tests/e2e/chat_auth/chat_mcp_standard_install_test.go`; documented in `docs/architecture/mcp.md`.

Added CHAT-112 `TestE2E_SecurityUrgentLegacyMCPLocalAuthorization`: real HTTP/JWT/tenant middleware with isolated SQLite, 47 subcases, exact rejection codes, unchanged scoped/secret/file snapshots and four persisted toggle positives. Tokens are test-issued, not login proof. Registered parent plus all subcases in `tests/scenarios/parse-run.mjs`, `.test.mjs`, `chat.yaml`, `docs/architecture/testing.md`: 204 required entries, 232 parser tests. New omission regressions failed before registration (48 cases), then passed. File refresh has a 12-case local handler selection/cache matrix with discovery deliberately suppressed; not live discovery proof.

Independent review repairs retained: private key temp-file publication rather than exposing incomplete final key; static guard examines actual private implementation with nonzero minimum checks; local HTTP fixture disables inherited chat prewarming before config/environment setup and teardown rather than weakening secret snapshots; file-refresh fixture clears SUDO_USER, explicitly owns XDG_CONFIG_HOME/HOME, checks resolved path and creates/removes only its own file. Initial combined run `607a5a38` passed normal suites/parser/API+store race but failed the legacy HTTP secret snapshot due to inherited prewarming. Combined four-test sequence then passed three times and with race detection (delegated 104.083s). File-fixture correction initially missed MkdirAll (process `29ccad7e` exit 1); repaired and focused matrix repeated with race passed (`f8894f11`). Earlier `7b3e6779` failure was compile-time unused imports during concurrent test editing; no longer reproduces.

Residual runtime obligations: global ChatManager.Reset and name-only discovery deduplication are unchanged and do not constitute tenant-isolated cache/invalidation proof. SQLite key directory entries are not fsynced (atomic publication, not power-loss durability); temporary cleanup best-effort; hardlink-unsupported filesystems fail safely. Full urgent both-storage/Kubernetes acceptance remains unexecuted without explicit operator approval. Starting `tools-apps-and-sql` was attempted again and rejected `serial_blocked`; no downstream implementation or approval bypass. D-01/03/04/05, SQLite authorizer and potential fleet schema gates, ART containment/live-reader obligations, production incident response and MTA proof remain outstanding. No release, commit, production rotation, generated/dependency edit or unrelated slides change.

Latest checks in flight at checkpoint: `f8894f11` (232 parser tests, tagged all-package compile, preflight unit tests, lint already passed; build/version/diff finishing), `5980a622` (final normal Go/frontend suites already passed, 109 frontend files/828 tests; API/store race and four-test HTTP/setup/memory sequence finishing). `npm run typecheck` exit 0. Final exits will be appended below; starting jobs is not completion. HEAD remains `a66f59d86e92596828273ed12b6a3f9db8e28c78`.

## Consolidated memory, session and masked-secret batch

This additive checkpoint supersedes obsolete next-work statements above. Full prior MCP evidence is retained in `SECURITY_MCP_BATCH_VERIFICATION.md` (380 required entries and 408 parser tests at that checkpoint). The phase remains `urgent-independent-fixes`, running/incomplete; runtime again rejected `tools-apps-and-sql` with `serial_blocked`. Sandbox remains failed at D-05; downstream phases remain pending. No acknowledgement, architecture approval, release, commit, production rotation or mail-server change was inferred from autonomous-continuation instructions.

### Recovered and new corrections

- Recovered scoped memory cleanup changes in `pkg/api/memory_consolidation.go` / `memory_authorization_test.go` preserve the destination by request-resolved scope plus ID, not ID alone. Cross-scope sources/duplicates are cleaned and failures reported; deletion stops at first error. No cross-store rollback is promised. Existing transcript/artifact containment remains unchanged.
- New `TestConsolidationTransientPlanningFailureIsClosed` failed before the fix: a failed destination lookup was treated as no match, retried into HTTP 200/applied, one write and one source deletion. The planning adapter now retains the lookup error and apply stops before persistence. Focused normal/race tests pass.
- `tests/e2e/chat_auth/chat_auth_memory_test.go` adds an audit-draining local HTTP fixture and `TestE2E_Memory_LocalFixtureDrainsAuditBeforeDatabaseCleanup`. Before the fixture fix a delayed real audit write failed with `sql: database is closed`; afterward the server closes and pending audit writes join before seeded database cleanup. This is fixture lifecycle repair, not a production audit-lifecycle claim.
- Recovered session regressions in `pkg/api/{session_handlers_test.go,session_owner_gaps_test.go}` and `tests/e2e/chat_auth/chat_auth_same_team_session_test.go` use real scoped-store positive counterparts and exact denial codes. The older `TestE2E_Session_CrossOrgDenied` in `chat_auth_scoping_test.go` now requires completed chat, a real session ID, owner read/content, foreign 403/404, no leaks and an unchanged owner snapshot. That infrastructure test was compiled, not executed.
- `GetMCPPlatformServerHandler` nil-row panic reproduced in six read-scope cases. `pkg/api/mcp_platform_handlers.go` now returns the existing bounded 404 for nil rows. `TestGetMCPPlatformServerHandlerLookup` in `mcp_handlers_test.go` covers 30 missing/error/populated/unavailable cases, including redaction and unchanged secrets. Existing read-scope semantics and Toggle/Refresh fixes remain unchanged.

### Authenticated local coverage and strict manifest

- Existing MCP loopback/JWT/SQLite fixture now has 376 leaves: all previous 223 plus 153 `masked_update` cases across team/org/platform and modern/legacy/inline routes. Covers masked readback, exact roundtrip preservation, absent/inherited/wrong target/key, executable/destination changes, omission/null/empty, literal replacement/clear and invalid batch no-effects. Discovery remains stopped by the existing local barrier: these are not successful remote MCP-execution tests.
- New `TestE2E_SecurityUrgentFullConfigMasksLocal` / CHAT-114 adds 40 leaves (52 entries including actual parents) for browser proxy/CDP and embedding masks. Independent platform authority succeeds; lower roles and anonymous requests get exact 401/403, malformed/invalid masks exact 400, unchanged configuration bytes on rejection and redacted persisted readback. This fixture also drains audit work before database teardown.
- `tests/scenarios/{parse-run.mjs,parse-run.test.mjs,chat.yaml}` and `docs/architecture/testing.md` require **594 entries**. Scoped-memory additions had increased the earlier 380 baseline to 389; this batch adds 205 entries (153 MCP + 52 full-config). All 205 omission tests failed before registration and pass after. Normal parser suite: **633** tests; with actual local event evidence: **634**. Observed local JSON has 429 runs/passes matching both mask subsets exactly; partial evidence still cannot pass the full acceptance manifest.
- Four modern-team missing/inherited masked-target PUTs return bounded 500 because `teamMCPServerStore.Get` returns an error for absent rows, unlike org/platform nil/no-error. Tests explicitly record unchanged state and the storage-failure status, not authorization-denial success. Store contract normalization was NOT implemented; it needs caller-aware follow-up. No broad acceptance weakening is justified by these cases.

### Verification observed so far

Recovered process `4f78ffce` exited 0: normal Go/frontend tests, previous parser suite, focused API race, preflight units, lint and diff check. Recovered `328c0824` scoped-memory race run exited 0; earlier interrupted `79c986d1` and `64889826` exited -1 and are not passes.

Delegated commands completed with exit 0: memory API race; local memory HTTP normal/race including audit lifecycle; MCP masked HTTP `-count=3` and race; full-config HTTP normal/race; combined local MCP/full-config/memory `-count=2` normally and with race (434.649s); GET nil-row handler normal/race; strict parser normal and actual-event hierarchy validation; scoped diff checks. The new production regressions have explicit before-fix failures, not inferred failures from newly added passing coverage.

Broad post-batch checks started as `ff0d0c06` (normal suite, 633 parser tests, full API/store race, tagged all-package compile, preflight units, lint/diff) and `c3761602` (build/version plus local HTTP/process-continuity acceptance). Their final exits are not claimed until observed below.

### Remaining gates and exact next work

Do not recreate the now-present mask matrices, SQLite restart/encryption or memory deletion-failure tests. PostgreSQL authorized first-run positive acceptance is still **missing implementation**, not merely unexecuted; add it only through explicit safe disposable-target bootstrap/cleanup, then execute with operator approval. Full urgent both-storage/Kubernetes acceptance remains required, and the older cross-org session test still needs live execution. Missing-target MCP status normalization and production audit shutdown remain separate bounded follow-ups; no new vulnerability severity assigned.

D-01/03/04/05, SQLite authorizer/dependency and possible fleet-schema gates remain open. Full ART confined live-reader positives, authorized sandbox/MCP execution, browser/TUI, migration and incident/MTA obligations remain outstanding. Team collaboration/private personal apps and non-sharing of personal credentials remain the approved policy. No downstream implementation was attempted after the serial-block response.

Batch-touched paths: `pkg/api/{memory_consolidation.go,memory_authorization_test.go,mcp_platform_handlers.go,mcp_handlers_test.go}`; `tests/e2e/chat_auth/{chat_auth_memory_test.go,chat_auth_scoping_test.go,chat_mcp_standard_install_test.go}`; `tests/scenarios/{parse-run.mjs,parse-run.test.mjs,chat.yaml}`; `docs/architecture/{memory,mcp,testing}.md`; this progress record and analysis/report updates. Recovered session-test paths above and all prior 42 modified paths remain retained. Existing untracked security reports, `pkg/api/platform_setup_errors_test.go`, `SESSION_6f06908a_REGRESSION_ANALYSIS.md` and unrelated `slides-diag`, `slides-import-fidelity-plan.md`, `slides_improve/` remain untouched. HEAD is still `a66f59d86e92596828273ed12b6a3f9db8e28c78`.

## PostgreSQL first-run wiring and safety verification checkpoint

This additive checkpoint supersedes the assertion above that PostgreSQL first-run implementation is missing. CHAT-115 is implemented and wired only into the PostgreSQL acceptance leg (597 required entries versus SQLite's 594). Its real PostgreSQL positive setup/restart remains unexecuted. Detailed evidence and limitations: `SECURITY_POSTGRES_FIRST_RUN_VERIFICATION.md`.

This resumption recovered MCP/local HTTP race process `b6a5ee5c`, exit 0 (277.865s), and independently reviewed first-run instance approval, subprocess isolation and HTTP/audit/store shutdown. Added eight symlink regressions to `tests/e2e/chat_auth/security_urgent_test.go`; each failed before the fixture guard and passed after it. No production sandbox-confinement claim is implied by fixture path checks. Whole-instance explicit approval remains mandatory; no role/database cleanup sweep or automatic approval.

`tests/e2eboot/security_preflight_test.go` now tests the actual preflight entrypoint and both actual Make targets using isolated environments, real validation and trapped downstream effects. Missing/mismatched first-run authority blocks before Kubernetes or acceptance execution. A mutation test removing the validator callback failed all four rejection cases and the callback was restored. `tests/scenarios/parse-run.test.mjs` adds six actual-Make recipe tests using synthetic Go/Kubernetes responses for both backend selectors, reports and failure propagation. Corrected PostgreSQL synthetic command metadata. A new documentation-selector regression failed before fixing the typo that omitted IsolationBeforeEffects; now includes symlink isolation too. `docs/architecture/testing.md` documents commands and limits.

Observed final commands, all exit 0:
- `env -u KUBECONFIG CI=1 make test`: normal Go suites and 109 frontend files / 828 tests. Log `/tmp/astonish-security-local-suite.log`.
- Process `2fdc0d0a`: full Node parser suite (643 dots); targeted e2eboot race including actual Make gate and entrypoint; PostgreSQL local gate/isolation/symlink/HTTP rejection race; diff check. No live PostgreSQL setup performed.
- Focused Node actual-Make/report/documentation checks: 9/9, zero skips. Separate local PostgreSQL-safety race rerun: exit 0, 11.942s.
- Process `e975bf5f`: all E2E packages/e2eboot compiled with `-run '^$'` (no tests executed); `make lint` zero issues; Go build `/tmp/astonish-security-verified`; executable `--version` reports dev; diff check.
- Process `375383d9`: `go test -race ./pkg/api ./pkg/store/entstore -count=1` (68.662s / 19.810s) and `npm --prefix web run typecheck` passed.

Phase disposition: continuity remains complete; urgent-independent-fixes explicitly marked failed/blocked on live acceptance; sandbox-and-artifact-boundaries still D-05-blocked. A fresh attempt to start tools-apps-and-sql was rejected `serial_blocked` by the runtime. Tools/apps/SQL, fleet, operations and closure remain pending; no overall completion. The previous exit-2 live preflight remains historical and unsuperseded. No live command was launched against unapproved targets. This is an actual execution barrier, not a request to confirm another small local task.

Next permitted action: operator supplies approved disposable actual PostgreSQL instance and ordinary DB/cluster/context/namespaces/PVCs/provider prerequisites, then resume urgent live acceptance. Preserve D-01/03/04/05, SQL authorizer/dependency and fleet-schema gates, mandatory full ART positive/negative confinement/browser/terminal acceptance, migrations, production rotation and receiving-MTA obligations. Team collaborative app maintenance/data/DDL and personal-private apps remain approved; personal secrets are not shared by that decision.

This turn changes only `tests/e2e/chat_auth/security_urgent_test.go`, `tests/e2eboot/security_preflight_test.go`, `tests/scenarios/parse-run.test.mjs`, `docs/architecture/testing.md`, this progress file and new `SECURITY_POSTGRES_FIRST_RUN_VERIFICATION.md`. Recovered Makefile/parser/preflight/catalog implementation and all prior modified/untracked security paths remain retained; unrelated slides unchanged. HEAD unchanged. No release, commit, production operation, generated or dependency edit.
