package entstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SAP/astonish/pkg/backup"
)

type sqliteLogicalDB struct {
	Path        string
	ArchiveDir  string
	Scope       backup.Scope
	ScopeName   string
	Description string
}

func (s *Store) ExportSQLiteLogicalBackup(ctx context.Context, archivePath string, opts PlatformBackupExportOptions) error {
	if s.dataDir == "" {
		s.dataDir = sqliteDataDirFromDSN(s.platformDSN)
	}
	if s.dialect != DialectSQLite {
		return fmt.Errorf("sqlite logical backup requires sqlite backend, got %s", s.dialect)
	}
	manifest := backup.NewManifest("sqlite", backupModeLogical, []backup.Scope{{Kind: "platform"}})
	manifest.Features = append(manifest.Features, "sqlite-logical-row-export")
	if opts.RedactSecrets {
		manifest.Features = append(manifest.Features, "redacted-secrets")
	}
	manifest.SchemaVersions = map[string]backup.SchemaVersion{
		"platform": {Scope: "platform"},
	}

	writer, err := backup.Create(archivePath, backup.WriterOptions{Compression: opts.Compression})
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = writer.Close()
		}
	}()

	dbs, err := s.discoverSQLiteLogicalDBs(ctx)
	if err != nil {
		return err
	}
	for _, db := range dbs {
		if !scopeInManifest(manifest.Scopes, db.Scope) {
			manifest.Scopes = append(manifest.Scopes, db.Scope)
		}
		if err := exportSQLiteDBRows(ctx, writer, &manifest, db, opts); err != nil {
			return err
		}
	}

	if err := writer.CloseWithManifest(manifest); err != nil {
		return err
	}
	closed = true
	return nil
}

func sqliteDataDirFromDSN(dsn string) string {
	path := strings.TrimPrefix(dsn, "file:")
	if u, err := url.Parse(dsn); err == nil && u.Scheme == "file" && u.Path != "" {
		path = u.Path
	}
	if path == "" || path == dsn {
		return ""
	}
	return filepath.Dir(path)
}

func (s *Store) discoverSQLiteLogicalDBs(ctx context.Context) ([]sqliteLogicalDB, error) {
	var dbs []sqliteLogicalDB
	dbs = append(dbs, sqliteLogicalDB{
		Path:        filepath.Join(s.dataDir, "platform.db"),
		ArchiveDir:  "platform",
		Scope:       backup.Scope{Kind: "platform"},
		ScopeName:   "platform",
		Description: "platform",
	})

	orgs, err := s.Organizations().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list organizations for backup: %w", err)
	}
	for _, org := range orgs {
		orgSlug := org.Slug
		orgDB := sqliteLogicalDB{
			Path:        filepath.Join(s.dataDir, "orgs", orgSlug, "org.db"),
			ArchiveDir:  filepath.ToSlash(filepath.Join("orgs", orgSlug, "org")),
			Scope:       backup.Scope{Kind: "org", OrgSlug: orgSlug},
			ScopeName:   "org:" + orgSlug,
			Description: "org " + orgSlug,
		}
		dbs = append(dbs, orgDB)

		orgStore, err := s.ForOrg(orgSlug)
		if err != nil {
			return nil, fmt.Errorf("open org %s for backup discovery: %w", orgSlug, err)
		}
		teams, err := orgStore.Teams().ListTeams(ctx)
		if err != nil {
			return nil, fmt.Errorf("list teams for org %s backup: %w", orgSlug, err)
		}
		for _, team := range teams {
			dbs = append(dbs, sqliteLogicalDB{
				Path:        filepath.Join(s.dataDir, "orgs", orgSlug, "teams", team.Slug+".db"),
				ArchiveDir:  filepath.ToSlash(filepath.Join("orgs", orgSlug, "teams", team.Slug)),
				Scope:       backup.Scope{Kind: "team", OrgSlug: orgSlug, TeamSlug: team.Slug},
				ScopeName:   "team:" + orgSlug + ":" + team.Slug,
				Description: "team " + orgSlug + "/" + team.Slug,
			})
		}

		users, err := s.Users().ListByOrg(ctx, org.ID)
		if err != nil {
			return nil, fmt.Errorf("list users for org %s backup: %w", orgSlug, err)
		}
		for _, user := range users {
			dbs = append(dbs, sqliteLogicalDB{
				Path:        filepath.Join(s.dataDir, "orgs", orgSlug, "personal", user.ID+".db"),
				ArchiveDir:  filepath.ToSlash(filepath.Join("orgs", orgSlug, "personal", user.ID)),
				Scope:       backup.Scope{Kind: "personal", OrgSlug: orgSlug, UserID: user.ID},
				ScopeName:   "personal:" + orgSlug + ":" + user.ID,
				Description: "personal " + orgSlug + "/" + user.ID,
			})
		}
	}
	return dbs, nil
}

func exportSQLiteDBRows(ctx context.Context, writer *backup.Writer, manifest *backup.Manifest, dbInfo sqliteLogicalDB, opts PlatformBackupExportOptions) error {
	if _, err := os.Stat(dbInfo.Path); os.IsNotExist(err) {
		return nil
	}
	db, err := sql.Open("sqlite", dbInfo.Path)
	if err != nil {
		return fmt.Errorf("open %s: %w", dbInfo.Description, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		return fmt.Errorf("set %s query_only: %w", dbInfo.Description, err)
	}

	tables, err := sqliteUserTables(ctx, db)
	if err != nil {
		return fmt.Errorf("list %s tables: %w", dbInfo.Description, err)
	}
	for _, table := range tables {
		data, records, redacted, err := exportSQLiteTable(ctx, db, table, opts.RedactSecrets)
		if err != nil {
			return fmt.Errorf("export %s table %s: %w", dbInfo.Description, table, err)
		}
		archivePath := filepath.ToSlash(filepath.Join(dbInfo.ArchiveDir, table+".jsonl"))
		if _, err := writer.AddFile(archivePath, bytes.NewReader(data)); err != nil {
			return err
		}
		manifest.Entries = append(manifest.Entries, backup.Entry{
			Path:     archivePath,
			Kind:     "jsonl",
			Scope:    dbInfo.Scope,
			Entity:   table,
			Records:  records,
			Redacted: redacted,
		})
	}
	return nil
}

func sqliteUserTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '%_fts%' AND name NOT LIKE '%_fts_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		if table == "schema_migrations" {
			continue
		}
		tables = append(tables, table)
	}
	return tables, rows.Err()
}

func exportSQLiteTable(ctx context.Context, db *sql.DB, table string, redactSecrets bool) ([]byte, int64, bool, error) {
	columns, err := sqliteTableColumns(ctx, db, table)
	if err != nil {
		return nil, 0, false, err
	}
	quotedCols := make([]string, len(columns))
	for i, col := range columns {
		quotedCols[i] = quoteSQLiteIdent(col)
	}
	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(quotedCols, ", "), quoteSQLiteIdent(table)) //nolint:gosec // table and column names come from sqlite_master/table_info and are quoted as identifiers.
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, 0, false, err
	}
	defer rows.Close()

	var buf bytes.Buffer
	writer := backup.NewRecordWriter(&buf, table)
	redactedAny := false
	for rows.Next() {
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, 0, false, err
		}
		record := make(map[string]any, len(columns))
		for i, col := range columns {
			value := normalizeSQLiteValue(values[i])
			if redactSecrets {
				redacted, didRedact := redactBackupValue(table, col, value)
				if didRedact {
					redactedAny = true
				}
				record[col] = redacted
				continue
			}
			record[col] = value
		}
		id := backupRecordID(record)
		if err := writer.Write(id, record); err != nil {
			return nil, 0, false, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, false, err
	}
	return buf.Bytes(), writer.Records(), redactedAny, nil
}

func sqliteTableColumns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
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
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

func quoteSQLiteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func normalizeSQLiteValue(value any) any {
	switch v := value.(type) {
	case []byte:
		if json.Valid(v) {
			var decoded any
			if err := json.Unmarshal(v, &decoded); err == nil {
				return decoded
			}
		}
		if isPrintableUTF8(v) {
			return string(v)
		}
		return map[string]string{"encoding": "base64", "value": hex.EncodeToString(v)}
	default:
		return v
	}
}

func isPrintableUTF8(data []byte) bool {
	for _, b := range data {
		if b == '\n' || b == '\r' || b == '\t' {
			continue
		}
		if b < 0x20 || b == 0x7f {
			return false
		}
	}
	return true
}

func redactBackupValue(table, column string, value any) (any, bool) {
	name := strings.ToLower(table + "." + column)
	if isSensitiveBackupKey(name) && value != nil && value != "" {
		return "[REDACTED]", true
	}
	redacted, changed := redactNestedBackupValue(value)
	return redacted, changed
}

func redactNestedBackupValue(value any) (any, bool) {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		changed := false
		for key, child := range v {
			if isSensitiveBackupKey(strings.ToLower(key)) && child != nil && child != "" {
				out[key] = "[REDACTED]"
				changed = true
				continue
			}
			redacted, childChanged := redactNestedBackupValue(child)
			out[key] = redacted
			changed = changed || childChanged
		}
		return out, changed
	case []any:
		out := make([]any, len(v))
		changed := false
		for i, child := range v {
			redacted, childChanged := redactNestedBackupValue(child)
			out[i] = redacted
			changed = changed || childChanged
		}
		return out, changed
	default:
		return value, false
	}
}

func isSensitiveBackupKey(name string) bool {
	sensitiveFragments := []string{
		"password",
		"passwd",
		"secret",
		"token",
		"api_key",
		"apikey",
		"access_key",
		"refresh_token",
		"client_secret",
		"private_key",
		"jwt",
		"value_enc",
		"encrypted_value",
	}
	for _, fragment := range sensitiveFragments {
		if strings.Contains(name, fragment) {
			return true
		}
	}
	return false
}

func backupRecordID(record map[string]any) string {
	for _, key := range []string{"id", "name", "slug", "key"} {
		if value, ok := record[key]; ok && value != nil {
			return fmt.Sprint(value)
		}
	}
	keys := make([]string, 0, len(record))
	for key := range record {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprint(record[key]))
		if len(parts) == 3 {
			break
		}
	}
	return strings.Join(parts, ":")
}

func scopeInManifest(scopes []backup.Scope, scope backup.Scope) bool {
	for _, existing := range scopes {
		if existing == scope {
			return true
		}
	}
	return false
}
