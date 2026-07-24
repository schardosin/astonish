package entstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/SAP/astonish/pkg/backup"
)

func backupScopeKey(scope backup.Scope) string {
	switch scope.Kind {
	case "platform":
		return "platform"
	case "org":
		return "org:" + scope.OrgSlug
	case "team":
		return "team:" + scope.OrgSlug + ":" + scope.TeamSlug
	case "personal":
		return "personal:" + scope.OrgSlug + ":" + scope.UserID
	default:
		return scope.Kind
	}
}

func schemaVersionForScope(ctx context.Context, backend string, db *sql.DB, scope backup.Scope) (backup.SchemaVersion, error) {
	dialect := Dialect(backend)
	schema := ""
	if dialect == DialectPostgres {
		schema = postgresSchemaForScope(scope)
	}
	hash, err := schemaHash(ctx, db, dialect, schema)
	if err != nil {
		return backup.SchemaVersion{}, err
	}
	return backup.SchemaVersion{
		Scope:   backupScopeKey(scope),
		Version: backend + ":" + backup.ArchiveFormat,
		Hash:    hash,
	}, nil
}

func schemaHash(ctx context.Context, db *sql.DB, dialect Dialect, schema string) (string, error) {
	if dialect == DialectSQLite {
		return sqliteSchemaHash(ctx, db)
	}
	return logicalSchemaHash(ctx, db, dialect, schema)
}

func sqliteSchemaHash(ctx context.Context, db *sql.DB) (string, error) {
	tables, err := sqliteUserTables(ctx, db)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, table := range tables {
		columns, err := sqliteTableSchema(ctx, db, table)
		if err != nil {
			return "", err
		}
		parts = append(parts, table+"("+strings.Join(columns, ",")+")")
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:]), nil
}

func sqliteTableSchema(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+quoteSQLiteIdent(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns = append(columns, fmt.Sprintf("%03d:%s:%s:%d:%d", cid, name, strings.ToUpper(typ), notNull, pk))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}

func logicalSchemaHash(ctx context.Context, db *sql.DB, dialect Dialect, schema string) (string, error) {
	tables, err := userTables(ctx, db, dialect, schema)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, table := range tables {
		columns, err := tableColumns(ctx, db, dialect, schema, table)
		if err != nil {
			return "", err
		}
		parts = append(parts, table+"("+strings.Join(columns, ",")+")")
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:]), nil
}

func postgresSchemaForScope(scope backup.Scope) string {
	switch scope.Kind {
	case "team":
		return teamSchemaName(scope.TeamSlug)
	case "personal":
		return personalSchemaName(scope.UserID)
	default:
		return "public"
	}
}

func checkSchemaCompatible(archive, target backup.SchemaVersion) error {
	if archive.Hash == "" || target.Hash == "" {
		return nil
	}
	archiveBackend := strings.SplitN(archive.Version, ":", 2)[0]
	targetBackend := strings.SplitN(target.Version, ":", 2)[0]
	if archiveBackend != "" && targetBackend != "" && archiveBackend != targetBackend {
		return nil
	}
	if archive.Hash != target.Hash {
		return fmt.Errorf("target schema for scope %q does not match archive schema", archive.Scope)
	}
	return nil
}
