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
- `formatVersion`: archive format version, starting at `1`; this versions the tar/gzip layout and manifest semantics independently from database schema versions.
- `createdAt`: UTC timestamp.
- `backend`: source backend, for example `sqlite` or `postgres`.
- `mode`: `logical` for portable entity JSONL.
- `compression`: `gzip` by default, or `none` for explicitly uncompressed archives.
- `scopes`: exported platform/org/team/personal scopes.
- `schemaVersions`: applied migration state per scope when available.
- `entries`: files that belong to the archive payload.

`checksums.json` contains SHA-256 digests and sizes for every non-metadata logical payload file. Checksums are calculated over the uncompressed payload bytes so integrity semantics do not depend on the archive compression wrapper. `backup.Verify` rejects missing files, checksum mismatches, duplicate tar entries, unchecked payload files, unsafe archive paths, and malformed compressed streams.

When `--passphrase` is used, Astonish first creates the complete tar/gzip archive and then encrypts that entire byte stream. This means `manifest.json`, `checksums.json`, and all payload files are inside the AES-256-GCM ciphertext. The encryption metadata stores a versioned cipher/KDF description, including Argon2id memory, time, thread, and key-length parameters, and is passed as AES-GCM additional authenticated data.

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

Credential payloads are encrypted at rest and must remain encrypted in archives. Logical export must never serialize plaintext secrets, OAuth access tokens, refresh tokens, or provider keys. The default restore behavior preserves encrypted blobs only when the target has compatible key material.

Credentials use envelope encryption: each org has a `credential_key` row in `org_encryption_keys`, encrypted by the installation master key (`ASTONISH_MASTER_KEY` or `~/.config/astonish/.store_key`), and personal/team credential rows are encrypted with that org key. Restore planning inspects encrypted credential material before writing. If a backup contains credential rows and the current master key is missing or cannot decrypt the restored org credential key, restore reports a blocker instead of importing credentials that would appear in lists but fail at use time.

Portable non-secret exports use the explicit `--redact-secrets` option. Archive-level encryption is explicit via a passphrase and uses AES-256-GCM with an Argon2id-derived key. Archive passphrases protect the backup file in transit/storage; they do not replace the installation master key needed to decrypt restored credentials at runtime. Any future key export or re-key flow must be explicit, audited, and documented separately from normal backup creation.

## Consistency Guarantees

Logical backups are the portability path. Each source scope should be read in a consistent transaction where the backend supports it:

- SQLite logical export reads each database file through its normal connection with a read transaction.
- PostgreSQL logical export walks the platform database, each org database, and team/personal schemas through entstore-owned SQL connections.

A full PostgreSQL platform backup spans multiple databases, so there is no single cluster-wide snapshot in the first design. The archive should record per-scope snapshot times when logical export is implemented. Operators that require strict full-platform consistency should pause writes or run in a maintenance window until a platform-wide maintenance gate exists.

Physical backup mode is future work. It must use safe backend mechanisms such as SQLite's online backup API or PostgreSQL `pg_dump`; it must not copy live WAL-backed SQLite files naïvely.

## Restore Safety

Restore is a fresh-target recovery workflow first. The target must be empty, or the restore command fails before writing. SQLite targets may opt into a destructive reset path with `--reset-target`; this closes the target store, removes the SQLite platform database, tenant database tree, and legacy file-backed runtime directories under the configured data directory, bootstraps a new empty platform schema, and then imports the archive. Merge-style restore into an existing active platform is future work because it requires explicit conflict policy for users, org slugs, team slugs, app IDs, sessions, scheduler jobs, and credentials.

Restore must validate before writing:

1. Archive format and version are supported.
2. Manifest and checksums verify.
3. Target schema versions are compatible with the archive.
4. Target is empty, unless SQLite destructive reset is explicitly requested.
5. Encrypted credential payloads are usable with the current installation master key, or the backup is intentionally redacted/non-recovery.

Credential validation is a preflight safety gate. It checks restored `org_encryption_keys.credential_key` rows and representative personal/team `credentials.encrypted` rows. Missing or wrong key material is a blocker with an operator-facing remediation: copy the source `~/.config/astonish/.store_key` or set the same `ASTONISH_MASTER_KEY` before restore. Legacy plaintext credential rows are allowed with a warning.

Each logical scope import is wrapped in a database transaction and the SQLite path runs foreign-key checks before commit. PostgreSQL full-platform restore is not one global transaction because it provisions and writes multiple databases and schemas; failures can leave already-committed earlier scopes in place. This is acceptable only for the first fresh-target recovery workflow and is why PostgreSQL destructive reset remains outside the tool.

The implemented restore command supports:

- `--dry-run`: plan only, no writes.
- `--confirm`: execute restore; required for writes.
- `--reset-target`: delete and recreate a non-empty SQLite target before restore.
- `--enable-scheduled-jobs`: keep scheduled jobs active; default restores them paused.
- `--include-transient`: include login/runtime transient state; default skips it.
- `--passphrase`: decrypt an encrypted archive for inspect, verify, or restore.
- `--map-org old:new`: restore an organization under a new slug.
- `--map-team oldorg/oldteam:neworg/newteam`: restore a team under a new org/team slug.
- `--map-user oldorg/olduser:neworg/newuser`: restore a personal scope under a new org/user ID.

Mapping rewrites manifest scopes at restore time, provisions the mapped target scopes, and rewrites common scope-carrying values in logical rows before insert. Organization mappings clear `organizations.db_name` so PostgreSQL targets use the destination deployment's derived database name. Team mappings rewrite `teams.slug` and `teams.schema_name` to the destination schema convention. User mappings rewrite common user reference columns and string values, but they are still a portability tool rather than a merge/conflict resolver.

Future flags can relax fresh-target behavior further:

- `--skip-existing`: leave target records untouched.
- `--overwrite`: replace updateable records while preserving append-only audit semantics.

Scheduled jobs restore paused by default to avoid duplicate automations after staging restores or migrations. Login sessions, device sessions, link codes, OAuth caches, and sandbox runtime sessions are transient/security state and are skipped by default.

## Current Implementation Slice

The current implementation provides the safe archive foundation, SQLite/PostgreSQL logical export, and fresh-target logical restore:

- `pkg/backup.Manifest` and validation.
- `pkg/backup.Writer` for gzip-compressed tar-compatible archive creation.
- `pkg/backup.RecordWriter` for logical JSONL payloads.
- `pkg/backup.Inspect` for manifest/checksum metadata.
- `pkg/backup.OpenReader` and `Reader.ForEachFile` for streaming tar payload traversal. Encrypted archives still decrypt to memory because AES-GCM requires authenticated plaintext before tar parsing.
- `pkg/backup.Verify` for streaming checksum validation.
- `astonish platform backup create --output <archive> [--compression gzip|none] [--passphrase <secret>]` for logical row export across platform, org, team, and personal databases.
- Scoped export flags: `--org`, `--team`, and `--user`.
- `astonish platform backup inspect` and `astonish platform backup verify`, with `--passphrase` for encrypted archives.
- `astonish platform restore <archive> --dry-run` for restore planning, including encrypted credential key preflight.
- `astonish platform restore <archive> --confirm` for SQLite or PostgreSQL fresh-target logical restore.
- `astonish platform restore <archive> --confirm --reset-target` for destructive SQLite target reset followed by restore.
- Restore-time scope mapping with `--map-org`, `--map-team`, and `--map-user`.
- PostgreSQL integration scaffolding in `pkg/store/entstore/backup_postgres_restore_integration_test.go`, gated by `-tags=integration` and `ASTONISH_TEST_DSN`.

For SQLite and PostgreSQL, `backup create` discovers platform/org/team/personal scopes through the tenant metadata and exports every user table as JSONL records. This includes durable rows for flows, apps, app state, fleets, drills, sessions, memories, settings, MCP servers, credentials, and scheduled jobs when those rows exist. It does not include physical `.db` files. Recovery backups preserve stored values by default, including password hashes and encrypted credential blobs, because those values are required for full system recovery. Use `--redact-secrets` only for portable/support exports that intentionally cannot restore protected values. PostgreSQL destructive reset is intentionally blocked; operators should restore into a fresh PostgreSQL platform database or reset databases externally with site-specific safeguards. App-created SQL schemas and merge-style restore remain planned follow-up work.

## References

- `docs/architecture/multi-tenant-platform.md` — PostgreSQL tenant topology and isolation guarantees.
- `docs/architecture/sqlite-backend.md` — SQLite file topology and backup implications.
- `pkg/store/entstore/AGENTS.md` — tenant router invariants.
- `docs/architecture/credentials.md` — encrypted credential storage and redaction rules.
