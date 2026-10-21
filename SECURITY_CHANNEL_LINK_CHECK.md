# Email-link creation: bounded persistence errors

## Change

Within the unfinished urgent remediation phase, removed raw storage-error text from the new email-link creation failure response. The API now returns HTTP 500 with `failed to link email`, without exposing database error details. Authentication, proof consumption, ownership, success response and channel enablement semantics are unchanged.

Changed files:
- `pkg/api/user_channel_handlers.go`
- `pkg/api/user_channel_handlers_test.go`
- `docs/architecture/channels.md`

## Regression evidence

Added `TestVerifyEmailCode_NewLinkPersistenceAndReplay`, covering failed creation and authorized successful creation with synthetic fixtures. It asserts bounded error content, no persisted link after the simulated failed write, correct owner/address/verified/enabled state after success, consumed proof, rejected replay, and exactly one creation attempt.

Before the production change, the new test failed because the response contained `database password=synthetic-secret internal-host:5432` from the synthetic storage error. After the change:

- `go test ./pkg/api -run '^TestVerifyEmailCode' -count=1` passed.
- `go test -race ./pkg/api -run '^TestVerifyEmailCode' -count=1` passed.
- `env -u KUBECONFIG go test ./pkg/api -count=1` passed.
- `git diff --check` passed.

## Limits and execution state

These are handler tests with injected identity and fake persistence, not live authenticated deployment acceptance. The urgent phase remains incomplete: independently confined sandbox artifact reads and authorized disposable-environment acceptance are still missing. The serial controller continues to block later phases. No phase completion, deployment, production rotation, runtime-design approval or team-policy decision is claimed. Existing changes in the three files were preserved; no blocked later-phase work was relabeled as urgent work.
