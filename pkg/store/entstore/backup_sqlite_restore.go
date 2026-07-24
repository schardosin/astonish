package entstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SAP/astonish/pkg/backup"
)

func (s *Store) sqliteRestoreTargetEmpty(ctx context.Context) (bool, error) {
	if s.dialect != DialectSQLite {
		return true, nil
	}
	for _, table := range []string{"users", "organizations", "org_memberships", "oidc_providers", "platform_settings", "platform_secrets"} {
		exists, err := sqliteTableExists(ctx, s.platformDB, table)
		if err != nil {
			return false, err
		}
		if !exists {
			continue
		}
		var count int
		if err := s.platformDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteSQLiteIdent(table)).Scan(&count); err != nil {
			return false, err
		}
		if count > 0 {
			return false, nil
		}
	}
	return true, nil
}

func (s *Store) resetSQLiteRestoreTarget(ctx context.Context) error {
	if s.dialect != DialectSQLite {
		return fmt.Errorf("reset-target restore requires sqlite backend, got %s", s.dialect)
	}
	dataDir := s.dataDir
	if dataDir == "" {
		dataDir = sqliteDataDirFromDSN(s.platformDSN)
	}
	if dataDir == "" {
		return fmt.Errorf("sqlite data directory is required for reset-target restore")
	}

	embedFunc := s.embedFunc
	cfg := Config{
		DSN:             s.platformDSN,
		DataDir:         dataDir,
		MaxOpenConns:    s.maxOpenConns,
		MaxIdleConns:    s.maxIdleConns,
		ConnMaxLifetime: s.connMaxLifetime,
	}
	s.orgClients.Range(func(key, value any) bool {
		if ds, ok := value.(*orgDataStore); ok {
			_ = ds.Close()
		}
		s.orgClients.Delete(key)
		return true
	})
	if err := s.Close(); err != nil {
		return fmt.Errorf("close sqlite target before reset: %w", err)
	}

	for _, path := range sqliteRestoreResetPaths(dataDir) {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return fmt.Errorf("create sqlite data directory: %w", err)
	}
	if err := BootstrapPlatform(ctx, cfg, nil); err != nil {
		return fmt.Errorf("bootstrap reset sqlite target: %w", err)
	}

	s.platformClient = nil
	s.platformDB = nil
	s.platformDSN = cfg.DSN
	s.dataDir = dataDir
	s.maxOpenConns = cfg.MaxOpenConns
	s.maxIdleConns = cfg.MaxIdleConns
	s.connMaxLifetime = cfg.ConnMaxLifetime
	s.embedFunc = embedFunc
	if err := s.openSQLite(ctx, cfg.DSN); err != nil {
		return fmt.Errorf("reopen reset sqlite target: %w", err)
	}
	s.sandboxTemplates = &sandboxTemplateStore{client: s.platformClient}
	return nil
}

func sqliteRestoreResetPaths(dataDir string) []string {
	return []string{
		filepath.Join(dataDir, "platform.db"),
		filepath.Join(dataDir, "platform.db-wal"),
		filepath.Join(dataDir, "platform.db-shm"),
		filepath.Join(dataDir, "platform.db-journal"),
		filepath.Join(dataDir, "orgs"),
		filepath.Join(dataDir, "apps"),
		filepath.Join(dataDir, "fleet_state"),
		filepath.Join(dataDir, "thread_index.json"),
		filepath.Join(dataDir, "sandbox_sessions.json"),
		filepath.Join(dataDir, "sessions.json"),
	}
}

func (s *Store) restoreSQLiteLogicalBackup(ctx context.Context, archivePath string, opts PlatformRestoreOptions, plan backup.RestorePlan) (*backup.RestoreResult, error) {
	files, err := backup.ReadArchiveFiles(archivePath)
	if err != nil {
		return nil, err
	}
	result := &backup.RestoreResult{Plan: plan, Warnings: append([]string(nil), plan.Warnings...)}

	if err := restoreSQLiteEntries(ctx, s.platformDB, files, entriesForScope(plan.Archive.Manifest.Entries, backup.Scope{Kind: "platform"}), opts, result); err != nil {
		return nil, fmt.Errorf("restore platform entries: %w", err)
	}

	orgScopes := scopesOfKind(plan.Archive.Manifest.Scopes, "org")
	for _, scope := range orgScopes {
		if err := s.ProvisionOrg(ctx, restoredOrgID(files, scope.OrgSlug), scope.OrgSlug); err != nil {
			return nil, fmt.Errorf("provision org %s: %w", scope.OrgSlug, err)
		}
		orgDB, err := openRestoreSQLiteDB(filepath.Join(s.dataDir, "orgs", scope.OrgSlug, "org.db"))
		if err != nil {
			return nil, err
		}
		if err := restoreSQLiteEntries(ctx, orgDB, files, entriesForScope(plan.Archive.Manifest.Entries, scope), opts, result); err != nil {
			_ = orgDB.Close()
			return nil, fmt.Errorf("restore org %s entries: %w", scope.OrgSlug, err)
		}
		if err := orgDB.Close(); err != nil {
			return nil, err
		}
	}

	for _, scope := range scopesOfKind(plan.Archive.Manifest.Scopes, "team") {
		orgStore, err := s.ForOrg(scope.OrgSlug)
		if err != nil {
			return nil, fmt.Errorf("open org %s: %w", scope.OrgSlug, err)
		}
		if err := orgStore.ProvisionTeam(ctx, scope.TeamSlug); err != nil {
			return nil, fmt.Errorf("provision team %s/%s: %w", scope.OrgSlug, scope.TeamSlug, err)
		}
		teamDB, err := openRestoreSQLiteDB(filepath.Join(s.dataDir, "orgs", scope.OrgSlug, "teams", scope.TeamSlug+".db"))
		if err != nil {
			return nil, err
		}
		if err := restoreSQLiteEntries(ctx, teamDB, files, entriesForScope(plan.Archive.Manifest.Entries, scope), opts, result); err != nil {
			_ = teamDB.Close()
			return nil, fmt.Errorf("restore team %s/%s entries: %w", scope.OrgSlug, scope.TeamSlug, err)
		}
		if err := teamDB.Close(); err != nil {
			return nil, err
		}
	}

	for _, scope := range scopesOfKind(plan.Archive.Manifest.Scopes, "personal") {
		orgStore, err := s.ForOrg(scope.OrgSlug)
		if err != nil {
			return nil, fmt.Errorf("open org %s: %w", scope.OrgSlug, err)
		}
		if err := orgStore.ProvisionPersonalSchema(ctx, scope.UserID); err != nil {
			return nil, fmt.Errorf("provision personal %s/%s: %w", scope.OrgSlug, scope.UserID, err)
		}
		personalDB, err := openRestoreSQLiteDB(filepath.Join(s.dataDir, "orgs", scope.OrgSlug, "personal", scope.UserID+".db"))
		if err != nil {
			return nil, err
		}
		if err := restoreSQLiteEntries(ctx, personalDB, files, entriesForScope(plan.Archive.Manifest.Entries, scope), opts, result); err != nil {
			_ = personalDB.Close()
			return nil, fmt.Errorf("restore personal %s/%s entries: %w", scope.OrgSlug, scope.UserID, err)
		}
		if err := personalDB.Close(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func openRestoreSQLiteDB(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=10000"} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return db, nil
}

func restoreSQLiteEntries(ctx context.Context, db *sql.DB, files map[string][]byte, entries []backup.Entry, opts PlatformRestoreOptions, result *backup.RestoreResult) error {
	ordered := append([]backup.Entry(nil), entries...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return restoreTablePriority(ordered[i].Entity) < restoreTablePriority(ordered[j].Entity)
	})
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, entry := range ordered {
		action, _ := restoreActionForEntry(entry, opts)
		if action == "skip" {
			result.SkippedEntries++
			continue
		}
		data, ok := files[entry.Path]
		if !ok {
			return fmt.Errorf("archive missing %s", entry.Path)
		}
		records, err := restoreSQLiteEntry(ctx, tx, entry, data, action)
		if err != nil {
			return err
		}
		result.RestoredEntries++
		result.RestoredRecords += records
	}
	if err := sqliteForeignKeyCheck(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func restoreSQLiteEntry(ctx context.Context, tx *sql.Tx, entry backup.Entry, data []byte, action string) (int64, error) {
	scanner := backup.NewRecordScanner(bytes.NewReader(data), entry.Entity)
	var count int64
	for scanner.Next() {
		record, err := scanner.Record()
		if err != nil {
			return 0, err
		}
		row, err := backup.DecodeRecordData(record)
		if err != nil {
			return 0, err
		}
		if action == "restore_disabled" && entry.Entity == "scheduled_jobs" {
			row["status"] = "paused"
		}
		if err := insertSQLiteRow(ctx, tx, entry.Entity, row); err != nil {
			return 0, fmt.Errorf("insert %s record %s: %w", entry.Entity, record.ID, err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

func insertSQLiteRow(ctx context.Context, tx *sql.Tx, table string, row map[string]any) error {
	if len(row) == 0 {
		return nil
	}
	columns := make([]string, 0, len(row))
	for col := range row {
		columns = append(columns, col)
	}
	sort.Strings(columns)
	placeholders := make([]string, len(columns))
	args := make([]any, len(columns))
	for i, col := range columns {
		placeholders[i] = "?"
		args[i] = normalizeRestoreSQLiteValue(row[col])
	}
	query := fmt.Sprintf("INSERT OR REPLACE INTO %s (%s) VALUES (%s)", quoteSQLiteIdent(table), quoteSQLiteIdentList(columns), strings.Join(placeholders, ", ")) //nolint:gosec // table and column names come from verified backup entries and are quoted as identifiers; row values are bound parameters.
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

func normalizeRestoreSQLiteValue(value any) any {
	switch v := value.(type) {
	case nil, string, bool:
		return v
	case json.Number:
		if strings.Contains(v.String(), ".") {
			if f, err := v.Float64(); err == nil {
				return f
			}
		}
		if i, err := v.Int64(); err == nil {
			return i
		}
		return v.String()
	case map[string]any:
		if encoding, ok := v["encoding"].(string); ok && encoding == "base64" {
			if encoded, ok := v["value"].(string); ok {
				if decoded, err := hex.DecodeString(encoded); err == nil {
					return decoded
				}
			}
		}
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	case []any:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	default:
		return v
	}
}

func quoteSQLiteIdentList(columns []string) string {
	quoted := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = quoteSQLiteIdent(col)
	}
	return strings.Join(quoted, ", ")
}

func sqliteForeignKeyCheck(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("sqlite foreign key check failed")
	}
	return rows.Err()
}

func sqliteTableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func entriesForScope(entries []backup.Entry, scope backup.Scope) []backup.Entry {
	var out []backup.Entry
	for _, entry := range entries {
		if entry.Scope == scope {
			out = append(out, entry)
		}
	}
	return out
}

func scopesOfKind(scopes []backup.Scope, kind string) []backup.Scope {
	var out []backup.Scope
	for _, scope := range scopes {
		if scope.Kind == kind {
			out = append(out, scope)
		}
	}
	return out
}

func restoreTablePriority(entity string) int {
	switch entity {
	case "users", "organizations", "teams":
		return 0
	case "org_memberships", "team_memberships":
		return 1
	default:
		return 10
	}
}

func restoredOrgID(files map[string][]byte, orgSlug string) string {
	data, ok := files["platform/organizations.jsonl"]
	if !ok {
		return ""
	}
	scanner := backup.NewRecordScanner(bytes.NewReader(data), "organizations")
	for scanner.Next() {
		record, err := scanner.Record()
		if err != nil {
			continue
		}
		row, err := backup.DecodeRecordData(record)
		if err != nil {
			continue
		}
		if fmt.Sprint(row["slug"]) == orgSlug {
			return record.ID
		}
	}
	return ""
}
