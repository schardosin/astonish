# Urgent security — organization provider coverage

Added `TestE2E_SecurityUrgentOrgProviderMasks` (CHAT-105), with 13 authenticated HTTP cases covering redacted reads, member denial, exact/stale/unknown/foreign masks, unknown fields, empty stored values, mixed-invalid changes, explicit replacement, omission, deletion and clearing. Tests inspect persisted organization settings and verify foreign-organization/platform settings remain unchanged. Synthetic settings are restored during cleanup. No production handler changed in this slice.

The acceptance manifest now requires 52 parent/subtest entries. Thirteen new parser regressions ensure none of these cases can be omitted. Scenario catalog and testing architecture documentation were updated.

## Observed local verification

- Before manifest registration: parser tests exited 1 (60 passed, 15 failed), demonstrating missing-case acceptance and obsolete required counts.
- After changes: 75 parser tests passed, E2E package compiled (`-run '^$'`, no E2E execution), focused existing provider race tests passed, focused preflight tests passed, and `git diff --check` passed in the combined command (exit 0).
- HEAD observed: `a66f59d86e92596828273ed12b6a3f9db8e28c78`.

## Still open

The new provider, memory and channel E2E scenarios have not executed on PostgreSQL or SQLite. Live acceptance remains deferred, not waived; no disposable-infrastructure acknowledgement was supplied. Full platform/team configuration matrices, first-run acceptance, duplicate-slug cross-org sessions, scoped MCP side effects, and memory/channel persistence-failure coverage remain. Urgent phase stays running/incomplete; sandbox stays D-05-blocked; downstream phases remain pending. All policy, driver/schema and operational gates remain unchanged. No deployment, production rotation, MTA change or overall completion is claimed.

Detailed commands and continuation state are recorded in `SECURITY_REMEDIATION_PROGRESS.md` under “Organization provider E2E continuation”.
