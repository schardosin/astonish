# Migration Management (Atlas)

Astonish uses [Atlas](https://atlasgo.io/) as a dev-time schema diff tool to generate SQL migration files. At runtime, the existing `migrate()` functions in sqlitestore/pgstore apply migrations unchanged — Atlas is never imported into the binary.

## Architecture

```
schema/                         ← Desired state (source of truth)
├── platform/
│   ├── schema.pg.sql
│   └── schema.sqlite.sql
├── org/
│   ├── schema.pg.sql
│   └── schema.sqlite.sql
├── team/
│   ├── schema.pg.sql
│   └── schema.sqlite.sql
└── personal/
    ├── schema.pg.sql
    └── schema.sqlite.sql

atlas.hcl                       ← Atlas project config (8 environments)

pkg/store/pgstore/migrations/   ← Generated migration files (committed)
pkg/store/sqlitestore/migrations/
```

## Workflow: Adding a Schema Change

1. **Edit the schema file** (single source of truth):
   ```bash
   # e.g., add a new table to team scope (both dialects)
   vim schema/team/schema.pg.sql
   vim schema/team/schema.sqlite.sql
   ```

2. **Generate the migration** (Atlas diffs desired vs. current state):
   ```bash
   # PG migrations (primary use case — Atlas handles these cleanly)
   make migrate-diff ENV=team_pg NAME=add_notifications

   # SQLite migrations: write manually (see Limitations below)
   ```

3. **Review** the generated SQL in `pkg/store/pgstore/migrations/team/`

4. **Commit** both the schema change and the migration together.

## Environments

| Environment | Schema Source | Migration Dir | Dev URL |
|---|---|---|---|
| `platform_pg` | `schema/platform/schema.pg.sql` | `pkg/store/pgstore/migrations/platform/` | `docker://postgres/16/dev` |
| `platform_lite` | `schema/platform/schema.sqlite.sql` | `pkg/store/sqlitestore/migrations/platform/` | `sqlite://dev?mode=memory` |
| `org_pg` | `schema/org/schema.pg.sql` | `pkg/store/pgstore/migrations/org/` | `docker://postgres/16/dev` |
| `org_lite` | `schema/org/schema.sqlite.sql` | `pkg/store/sqlitestore/migrations/org/` | `sqlite://dev?mode=memory` |
| `team_pg` | `schema/team/schema.pg.sql` | `pkg/store/pgstore/migrations/team/` | `docker://postgres/16/dev` |
| `team_lite` | `schema/team/schema.sqlite.sql` | `pkg/store/sqlitestore/migrations/team/` | `sqlite://dev?mode=memory` |
| `personal_pg` | `schema/personal/schema.pg.sql` | `pkg/store/pgstore/migrations/personal/` | `docker://postgres/16/dev` |
| `personal_lite` | `schema/personal/schema.sqlite.sql` | `pkg/store/sqlitestore/migrations/personal/` | `sqlite://dev?mode=memory` |

## `{{schema}}` Placeholder Handling

Team and personal PG schemas use `{{schema}}` for multi-tenant isolation (replaced at runtime with e.g., `team_general` or `personal_abc123`). Atlas can't parse this directly, so:

- `make migrate-diff` preprocesses: `{{schema}}` → `public` (for Atlas to parse)
- After generation: `public.` → `{{schema}}.` in the output migration
- The resolved files (`schema.pg.resolved.sql`) are gitignored

## Makefile Targets

```bash
make migrate-diff ENV=<env> NAME=<name>  # Generate migration for one env
make migrate-diff-all NAME=<name>         # Generate for all 8 envs
make migrate-verify                       # Validate atlas.sum integrity
make migrate-status                       # Show migration status per env
```

## Pre-commit Hook

The hook enforces two rules:

1. If `schema/*.sql` files are staged without corresponding migration files → **blocks commit** (reminds you to run `make migrate-diff`)
2. If migration files are staged → validates `atlas.sum` integrity (no tampered or missing hash entries)

## CI (lint.yml)

The `migration-drift-check` job runs `atlas migrate validate` on all 8 directories. Fails if any `atlas.sum` is inconsistent with its migration files.

## Baselining (One-Time Setup)

After initial Atlas setup, run `scripts/atlas-baseline.sh` to generate the initial `atlas.sum` files for existing migrations. This has already been done — the sum files are committed.

## Limitations

### SQLite (Community Edition)

Atlas community edition has two limitations affecting SQLite:

1. **Triggers/FTS5 require Atlas Pro** — The `org`, `team`, and `personal` SQLite schemas use FTS5 virtual tables with content-sync triggers. Atlas community edition cannot parse these. Migrations for these scopes must be written manually.

2. **Inline UNIQUE normalization** — SQLite `UNIQUE` column constraints create implicit indexes with auto-generated names. Atlas sees these differently from explicit `CREATE UNIQUE INDEX` statements, producing cosmetic drop/recreate diffs. The `platform_lite` environment (no triggers) works but may emit cosmetic no-op index changes on initial diff.

**Recommendation**: Use Atlas primarily for **PostgreSQL migrations** (`*_pg` environments). For SQLite, write migrations manually and update the schema file + `atlas.sum` together. The schema files still serve as the canonical reference for what the database should look like.

### Updating atlas.sum for manual migrations

After writing a SQLite migration manually:
```bash
# Add your migration file, then re-hash
atlas migrate hash --env team_lite
```

## Prerequisites

- **Atlas CLI**: `curl -sSf https://atlasgo.sh | sh`
- **Docker** (for PG environments): Atlas uses `docker://postgres/16/dev` to spin up a temporary PG instance for schema diffing

## Key Design Decisions

- **Dev-time only**: Atlas never appears in the Go binary or runtime. Zero runtime risk.
- **Existing `migrate()` unchanged**: sqlitestore and pgstore continue to embed and apply SQL files via `schema_migrations` at startup.
- **Two SQL files per scope**: `.pg.sql` + `.sqlite.sql` — no HCL, familiar format.
- **`atlas.sum` committed**: Provides integrity verification without needing Atlas at runtime.
- **PG primary, SQLite manual**: Atlas diff generation works cleanly for PG; SQLite migrations are written manually due to trigger/FTS5 limitations.
