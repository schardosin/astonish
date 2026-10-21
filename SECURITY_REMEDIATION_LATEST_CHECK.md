# Security remediation: latest verification and blockers

## Status

The remediation program is **incomplete**. The urgent phase remains blocked, and subsequent serial phases have not been completed. This report records recovered test results and a fresh safety-gate check; it is not a release or deployment certificate.

## Artifact changes already implemented

- The download handler uses an explicit passive-image MIME allowlist rather than treating every `image/*` type as passive. SVG, HTML, JavaScript and other documents are attachments. Existing `nosniff` and sandbox CSP headers remain in place; content previews remain inert text.
- Studio artifact downloads in `ArtifactCard`, `EmbeddedFileViewer` and `FilePanel` use authenticated, team-aware blob requests instead of direct URL navigation.
- PDF exports use `fetchArtifactPDFBlob`, which uses the same team-aware transport. Download/export failures are visible in the UI.
- Coverage includes artifact consumer behavior and API transport. `ArtifactDelivery.test.tsx` is currently an untracked file and must be included when committing the changes.

Relevant files: `pkg/api/session_handlers.go`, `pkg/api/artifact_content_test.go`, `web/src/api/studioChat.ts`, `web/src/api/__tests__/studioChat.test.ts`, `web/src/components/chat/ArtifactCard.tsx`, `web/src/components/chat/ArtifactDelivery.test.tsx`, `web/src/components/chat/EmbeddedFileViewer.tsx`, `web/src/components/chat/EmbeddedFileViewer.test.tsx`, `web/src/components/chat/FilePanel.tsx`, and `docs/architecture/sessions.md`.

This corrects the previous version of this report, which described only the SVG work and incorrectly omitted the subsequent frontend changes.

## Verification evidence

### Completed runs confirmed in this continuation

| Check | Observed result |
|---|---|
| `env -u KUBECONFIG CI=1 make test` | Process `7f6f6f2c` exited 0. The saved log ends with “All unit tests passed!”; frontend totals are 109 files and 828 tests passed. |
| Focused frontend artifact, embedded viewer, API transport, download and harness-panel tests | Process `556e16b5` exited 0: 5 files, 54 tests passed. |
| `npm run typecheck` following those focused tests | Same command chain exited 0. |
| Focused Go artifact tests and focused race tests | Recorded process `02d4319e` exited 0. |
| `env -u KUBECONFIG go test ./pkg/api -count=1` | Recorded process `5c63efa2` exited 0. |
| `git diff --check` | Executed again in this continuation; exited 0. |

The earlier pre-fix SVG regression failed for `.svg` and `.SVG` because disposition was inline. The subsequent focused artifact tests passed. The HTTP disposition test uses a loopback test server and synthetic media bytes, not actual media decoding/playback. Platform transcript tests inject identity and a test store; they are not deployed authenticated acceptance.

### Fresh preflight investigation

Executed:

```sh
go test -tags=e2e ./tests/e2eboot -run '^TestSecurityPreflight(Validation|BeforeBootstrap)$' -count=1
```

Result: passed. These tests use synthetic environment values and verify unsafe bootstrap rejection for PostgreSQL and SQLite without inheriting runtime/database credentials.

Then executed only the non-destructive environment validator, explicitly removing acknowledgement:

```sh
env -u ASTONISH_E2E_DEDICATED ASTONISH_SECURITY_PREFLIGHT=1 go test -tags=e2e ./tests/e2eboot -run '^TestSecurityPreflightEnvironment$' -count=1
```

Result: expected rejection, exit 1:

> BLOCKED before cleanup: ASTONISH_E2E_DEDICATED=1 required: acknowledge dedicated disposable DB, namespaces, and PVCs

This check does not call Bootstrap or cleanup. It proves rejection without acknowledgement, not acceptance readiness. The full destructive `make test-security SECURITY_TRACK=urgent` was not retried. Its previous exit-2 result remains unresolved.

## Required inputs and remaining work

1. **Disposable acceptance environment:** explicit operator authorization identifying the Kubernetes context/kubeconfig, disposable PostgreSQL database target, control-plane and sandbox namespaces, and PVCs eligible for test cleanup. Merely having infrastructure or setting a flag is not authorization. No secret values should be pasted into this report or chat.
2. **Confined artifact reader:** approve and implement a race-resistant reader enforced independently of the untrusted workload. Platform sandbox file reads remain disabled. Workload-executed `cat`/`tar` plus path checks do not satisfy this boundary. ART-01/ART-02 remain open; transcript delivery is not equivalent to restoring sandbox artifact reads.
3. **Later implementation tracks:** tools/apps/SQL, sandbox isolation/publication, tenant-bound fleet lifecycle, hardening/incident rehearsal, and final acceptance remain incomplete under the serial plan.
4. **Design and policy gates:** approve supported multi-user runtime design, credential consent and destination grants, shared-app data/DDL permissions, fleet execution identity and active-edit approval, and intended team-memory visibility. SQLite execution also requires the driver-backed authorizer feasibility and dependency-review gate. These are not decisions inferred from a request to finish implementation.
5. **Operational evidence:** trusted mail-receiver header stripping, approved incident-response rehearsal and any separately authorized production rotation remain outstanding. Reviewer severity/link details for TOOL-02..07 must not be guessed.

## Not verified or performed

No deployed authenticated security matrix, browser download/rendering smoke, real media playback, remote TUI smoke, sandbox reader success, runtime isolation acceptance, SQL isolation acceptance, production key rotation, or mail-server change was performed in this continuation. Unit results do not substitute for these checks. No overall completion, PR release, or deployment is claimed.
