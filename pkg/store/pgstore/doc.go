// Package pgstore implements the store interfaces for PostgreSQL-backed
// multi-tenant storage. It provides:
//
//   - Database-per-org isolation (each organization gets its own PostgreSQL database)
//   - Schema-per-team isolation (each team gets a schema within the org database)
//   - Personal schemas for user-private data
//   - Row-level security (RLS) as defense-in-depth on shared tables
//   - Automatic provisioning of databases, schemas, roles, and grants
//   - Forward-only versioned migrations
//   - Per-org connection pooling with search_path switching
//
// # Architecture
//
// Three database levels:
//
//   - Platform DB (astonish_platform): cross-org data — users, organizations,
//     OIDC providers, login sessions.
//   - Per-org DB (astonish_org_{slug}): all data for one organization.
//     Contains a public schema for org-wide shared data, plus team_{slug}
//     and personal_{user_id} schemas.
//   - Schemas within org DB: public (org shared), team_{slug} (team data),
//     personal_{user_id} (user-private data).
//
// # Request Flow
//
// Each HTTP request carries a JWT with user_id and org_id. Middleware:
//  1. Extracts org_id → obtains connection from org-specific pool
//  2. Sets search_path TO personal_{uid}, team_{slug}, public
//  3. Sets app.current_user and app.current_team session variables (for RLS)
//  4. Passes connection to handler via request context
//  5. Returns connection to pool on completion (search_path is reset)
//
// # Migration Strategy
//
// Migrations are forward-only SQL files stored in migrations/{platform,org,team,personal}/.
// Each file is named with a numeric prefix (e.g., 001_init.sql) and applied
// in order. A schema_migrations table tracks which migrations have been applied.
package pgstore
