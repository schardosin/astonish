# Platform Backup and Recovery

## Overview

Astonish platform backups capture control-plane data so an operator can recover a deployment or port data between locations. The backup system is intentionally backend-aware but not backend-locked: logical archives are designed to move data between SQLite and PostgreSQL deployments, while later physical backups can optimize same-backend disaster recovery.

The design follows the existing tenant topology:

- PostgreSQL: a platform database, one database per organization, and team/personal schemas inside each org database.
- SQLite: `platform.db`, `orgs/{org}/org.db`, `orgs/{org}/teams/{team}.db`, and `orgs/{org}/personal/{user}.db`.

All logical export/import code must go through `pkg/store/entstore`. Backup code may define archive primitives in `pkg/backup`, but it must not open raw Ent clients or bypass the tenant router.

## Archive Format

The archive is a single user-facing `.astonish-backup` file. Internally it is a tar-compatible multi-file logical archive, gzip-compressed by default. Readers detect gzip by magic bytes and continue to accept uncompressed tar archives for compatibility. The archive contains these reserved metadata entries:

```text
manifest.json
checksums.json
platform/*.jsonl
orgs/{org_slug}/org/*.jsonl
orgs/{org_slug}/teams/{team_slug}/*.jsonl
orgs/{org_slug}/personal/{user_id}/*.jsonl
```

`manifest.json` identifies the archive:

- `format`: always `astonish.backup`.
- `formatVersion`: archive format version, starting at `1`.
- `createdAt`: UTC timestamp.
- `backend`: source backend, for example `sqlite` or `postgres`.
- `mode`: `logical` for portable entity JSONL.
- `compression`: `gzip` by default, or `none` for explicitly uncompressed archives.
- `scopes`: exported platform/org/team/personal scopes.
- `schemaVersions`: applied migration state per scope when available.
- `entries`: files that belong to the archive payload.

`checksums.json` contains SHA-256 digests and sizes for every non-metadata logical payload file. Checksums are calculated over the uncompressed payload bytes so integrity semantics do not depend on the archive compression wrapper. `backup.Verify` rejects missing files, checksum mismatches, duplicate tar entries, unchecked payload files, unsafe archive paths, and malformed compressed streams.

## Scope Model

Backups are scoped to one of four boundaries:

| Scope | Required identifiers | Intended use |
| --- | --- | --- |
| `platform` | none | Full control-plane metadata and all selected tenant scopes. |
| `org` | `orgSlug` | Move or recover one organization. |
| `team` | `orgSlug`, `teamSlug` | Move or recover one team workspace. |
| `personal` | `orgSlug`, `userId` | Move or recover one user's private workspace. |

The scope identifiers in the manifest are security-relevant. Exporters must resolve them through the tenant router and must not widen a narrower request. A team export cannot include another team, and a personal export cannot include team data unless an explicit higher-level scope requested it.

## Credential Handling

Credential payloads are encrypted at rest and must remain encrypted in archives. Logical export must never serialize plaintext secrets, OAuth access tokens, refresh tokens, or provider keys. The default restore behavior should preserve encrypted blobs only when the target has compatible key material.

Portable non-secret exports use the explicit `--redact-secrets` option. Any future key export, archive encryption, or re-key flow must be explicit, audited, and documented separately from normal backup creation.

## Consistency Guarantees

Logical backups are the portability path. Each source scope should be read in a consistent transaction where the backend supports it:

- SQLite logical export reads each database file through its normal connection with a read transaction.
- PostgreSQL logical export uses repeatable-read transactions per platform or org database.

A full PostgreSQL platform backup spans multiple databases, so there is no single cluster-wide snapshot in the first design. The archive should record per-scope snapshot times when logical export is implemented. Operators that require strict full-platform consistency should pause writes or run in a maintenance window until a platform-wide maintenance gate exists.

Physical backup mode is future work. It must use safe backend mechanisms such as SQLite's online backup API or PostgreSQL `pg_dump`; it must not copy live WAL-backed SQLite files naïvely.

## Restore Safety

Restore is a fresh-target recovery workflow first. The target must be empty, or the restore command fails before writing. SQLite targets may opt into a destructive reset path with `--reset-target --yes`; this closes the target store, removes the SQLite platform database, tenant database tree, and legacy file-backed runtime directories under the configured data directory, bootstraps a new empty platform schema, and then imports the archive. Merge-style restore into an existing active platform is future work because it requires explicit conflict policy for users, org slugs, team slugs, app IDs, sessions, scheduler jobs, and credentials.

Restore must validate before writing:

1. Archive format and version are supported.
2. Manifest and checksums verify.
3. Target schema versions are compatible with the archive.
4. Target is empty, unless SQLite destructive reset is explicitly requested.
5. Secret payloads are usable in the target or intentionally redacted.

The implemented restore command supports:

- `--dry-run`: plan only, no writes.
- `--confirm`: execute restore; required for writes.
- `--reset-target --yes`: delete and recreate a non-empty SQLite target before restore.
- `--enable-scheduled-jobs`: keep scheduled jobs active; default restores them paused.
- `--include-transient`: include login/runtime transient state; default skips it.

Future flags can relax fresh-target behavior further:

- `--skip-existing`: leave target records untouched.
- `--overwrite`: replace updateable records while preserving append-only audit semantics.
- `--map-org`, `--map-team`, `--map-user`: port data into renamed scopes.

Scheduled jobs restore paused by default to avoid duplicate automations after staging restores or migrations. Login sessions, device sessions, link codes, OAuth caches, and sandbox runtime sessions are transient/security state and are skipped by default.

## Current Implementation Slice

The current implementation provides the safe archive foundation and SQLite full logical export:

- `pkg/backup.Manifest` and validation.
- `pkg/backup.Writer` for gzip-compressed tar-compatible archive creation.
- `pkg/backup.RecordWriter` for logical JSONL payloads.
- `pkg/backup.Inspect` for manifest/checksum metadata.
- `pkg/backup.Verify` for checksum validation.
- `astonish platform backup create --output <archive> [--compression gzip|none]` for logical SQLite row export across platform, org, team, and personal databases.
- `astonish platform backup inspect` and `astonish platform backup verify`.
- `astonish platform restore <archive> --dry-run` for restore planning.
- `astonish platform restore <archive> --confirm` for SQLite fresh-target logical restore.
- `astonish platform restore <archive> --confirm --reset-target --yes` for destructive SQLite target reset followed by restore.

For SQLite, `backup create` discovers platform/org/team/personal scopes through the tenant metadata and exports every user table as JSONL records. This includes durable rows for flows, apps, app state, fleets, drills, sessions, memories, settings, MCP servers, credentials, and scheduled jobs when those rows exist. It does not include physical `.db` files. Recovery backups preserve stored values by default, including password hashes and encrypted credential blobs, because those values are required for full system recovery. Use `--redact-secrets` only for portable/support exports that intentionally cannot restore protected values. PostgreSQL full logical export and restore/import remain planned follow-up work. Archive-level encryption is not implemented yet; operators must protect recovery archives as sensitive files.

## References

- `docs/architecture/multi-tenant-platform.md` — PostgreSQL tenant topology and isolation guarantees.
- `docs/architecture/sqlite-backend.md` — SQLite file topology and backup implications.
- `pkg/store/entstore/AGENTS.md` — tenant router invariants.
- `docs/architecture/credentials.md` — encrypted credential storage and redaction rules.
