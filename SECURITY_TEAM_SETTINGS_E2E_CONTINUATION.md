# Team settings security coverage — continuation checkpoint

## Current result

Reviewed the pending authenticated team-provider coverage: 13 cases in each of field-patch and provider-list replacement modes. Cases assert exact current-team mask preservation, rejection of stale/unknown/inherited masks, member write denial, explicit secret replacement/clearing, omission and empty-map behavior. Persisted target settings must match the expected whole object; organization, foreign-team and platform settings must remain unchanged.

The strict acceptance manifest requires these cases (94 required parent/subtest entries overall). No production handler changed during this resumption.

## Verified locally this turn

- 117 acceptance-parser tests passed.
- Tagged chat-auth E2E package compiled; no E2E tests executed.
- Existing provider mask matrix and team settings regressions passed with race detection.
- Focused preflight unit tests passed.
- Formatting and whitespace checks passed before this checkpoint was written.
- HEAD: `a66f59d86e92596828273ed12b6a3f9db8e28c78`.

Exact commands and additive evidence are recorded in `SECURITY_REMEDIATION_PROGRESS.md`, under “Team provider E2E continuation”. Earlier evidence and unrelated working-tree files are retained.

## Still missing

The new cases have not run against live PostgreSQL or SQLite. Urgent work still includes platform-provider/admin coverage, authorized first-run setup, scoped MCP side-effect assertions and real-route memory/channel persistence-failure coverage. Live acceptance remains deferred pending explicit disposable-infrastructure approval; its prior preflight failure is not superseded.

`urgent-independent-fixes` remains running/incomplete. Sandbox remediation remains blocked at D-05; downstream phases remain pending. Other policy, SQL-driver, potential fleet-schema and operational gates remain unchanged. Team apps/data remain collaborative within the team, personal maintenance/private data remains supported, and personal credentials are not implicitly shared. ART containment is not a completed confined-reader implementation.

No commit, deployment, production key rotation, MTA change or overall plan completion was performed.
