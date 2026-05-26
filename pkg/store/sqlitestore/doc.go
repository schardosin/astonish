// Package sqlitestore implements the store interfaces using SQLite as the
// persistence layer. It provides the same multi-tenant architecture as pgstore
// (platform → org → team → personal) but uses separate SQLite database files
// instead of PostgreSQL databases and schemas.
//
// This package requires no external dependencies beyond the Go binary itself.
// It uses modernc.org/sqlite (pure Go, CGO-free) for SQLite access, FTS5 for
// full-text search, and application-level brute-force cosine similarity for
// vector search (embeddings stored as BLOBs).
//
// File layout:
//
//	{dataDir}/
//	├── platform.db                    # Users, orgs, login sessions, OIDC, settings
//	├── orgs/
//	│   └── {org_slug}/
//	│       ├── org.db                 # Teams, memberships, org memories/skills/apps
//	│       ├── teams/
//	│       │   └── {team_slug}.db     # Sessions, memories, credentials, apps, flows
//	│       └── personal/
//	│           └── {user_id}.db       # Personal memories, apps, sessions, flows
package sqlitestore
