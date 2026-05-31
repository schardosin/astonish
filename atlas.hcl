// atlas.hcl — Atlas project configuration for Astonish migrations.
//
// Atlas uses a "dev database" as scratch space to compute schema diffs.
// It needs a real Postgres it can create/drop tables in freely.
//
// Configuration (pick one, in priority order):
//   1. ATLAS_PG_DEV_URL env var (explicit Atlas scratch DB)
//   2. Derived from ASTONISH_TEST_DSN by Makefile (replaces DB name with atlas_dev)
//   3. Falls back to localhost:5432/atlas_dev
//
// One-time setup on your dev Postgres:
//   createdb atlas_dev
//
// SQLite environments use in-memory (no setup needed).

locals {
  pg_dev_url   = getenv("ATLAS_PG_DEV_URL") != "" ? getenv("ATLAS_PG_DEV_URL") : "postgres://localhost:5432/atlas_dev?sslmode=disable"
  lite_dev_url = "sqlite://dev?mode=memory"
}

// ============================================================================
// Platform
// ============================================================================

env "platform_pg" {
  src = "file://schema/platform/schema.pg.atlas.sql"
  dev = local.pg_dev_url
  migration {
    dir = "file://pkg/store/pgstore/migrations/platform"
  }
}

env "platform_lite" {
  src = "file://schema/platform/schema.sqlite.sql"
  dev = local.lite_dev_url
  migration {
    dir = "file://pkg/store/sqlitestore/migrations/platform"
  }
}

// ============================================================================
// Org
// ============================================================================

env "org_pg" {
  src = "file://schema/org/schema.pg.atlas.sql"
  dev = local.pg_dev_url
  migration {
    dir = "file://pkg/store/pgstore/migrations/org"
  }
}

env "org_lite" {
  src = "file://schema/org/schema.sqlite.sql"
  dev = local.lite_dev_url
  migration {
    dir = "file://pkg/store/sqlitestore/migrations/org"
  }
}

// ============================================================================
// Team (PG uses {{schema}} placeholder — preprocessed by Makefile)
// ============================================================================

env "team_pg" {
  src = "file://schema/team/schema.pg.resolved.atlas.sql"
  dev = local.pg_dev_url
  migration {
    dir = "file://pkg/store/pgstore/migrations/team/.resolved"
  }
}

env "team_lite" {
  src = "file://schema/team/schema.sqlite.sql"
  dev = local.lite_dev_url
  migration {
    dir = "file://pkg/store/sqlitestore/migrations/team"
  }
}

// ============================================================================
// Personal (PG uses {{schema}} placeholder — preprocessed by Makefile)
// ============================================================================

env "personal_pg" {
  src = "file://schema/personal/schema.pg.resolved.atlas.sql"
  dev = local.pg_dev_url
  migration {
    dir = "file://pkg/store/pgstore/migrations/personal/.resolved"
  }
}

env "personal_lite" {
  src = "file://schema/personal/schema.sqlite.sql"
  dev = local.lite_dev_url
  migration {
    dir = "file://pkg/store/sqlitestore/migrations/personal"
  }
}
