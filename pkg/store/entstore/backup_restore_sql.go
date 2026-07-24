package entstore

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/SAP/astonish/pkg/backup"
)

func restoreLogicalEntries(ctx context.Context, db *sql.DB, dialect Dialect, schema string, files map[string][]byte, entries []backup.Entry, opts PlatformRestoreOptions, result *backup.RestoreResult) error {
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
		records, err := restoreLogicalEntry(ctx, tx, dialect, schema, entry, data, action)
		if err != nil {
			return err
		}
		result.RestoredEntries++
		result.RestoredRecords += records
	}
	if dialect == DialectSQLite {
		if err := sqliteForeignKeyCheck(ctx, tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func restoreLogicalEntry(ctx context.Context, tx *sql.Tx, dialect Dialect, schema string, entry backup.Entry, data []byte, action string) (int64, error) {
	scanner := backup.NewRecordScanner(strings.NewReader(string(data)), entry.Entity)
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
		if err := insertLogicalRow(ctx, tx, dialect, schema, entry.Entity, row); err != nil {
			return 0, fmt.Errorf("insert %s record %s: %w", entry.Entity, record.ID, err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

func insertLogicalRow(ctx context.Context, tx *sql.Tx, dialect Dialect, schema, table string, row map[string]any) error {
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
		placeholders[i] = placeholder(dialect, i+1)
		args[i] = normalizeRestoreValue(row[col])
	}
	query, err := insertLogicalQuery(dialect, schema, table, columns, placeholders)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, query, args...)
	return err
}

func insertLogicalQuery(dialect Dialect, schema, table string, columns, placeholders []string) (string, error) {
	quotedColumns := make([]string, len(columns))
	for i, col := range columns {
		quotedColumns[i] = quoteIdent(dialect, col)
	}
	target := quoteIdent(dialect, table)
	if dialect == DialectPostgres {
		if schema == "" {
			schema = "public"
		}
		target = quoteIdent(dialect, schema) + "." + quoteIdent(dialect, table)
	}
	switch dialect {
	case DialectSQLite:
		return fmt.Sprintf("INSERT OR REPLACE INTO %s (%s) VALUES (%s)", target, strings.Join(quotedColumns, ", "), strings.Join(placeholders, ", ")), nil //nolint:gosec // identifiers come from verified archive metadata and are quoted; values are bound parameters.
	case DialectPostgres:
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING", target, strings.Join(quotedColumns, ", "), strings.Join(placeholders, ", ")), nil //nolint:gosec // identifiers come from verified archive metadata and are quoted; values are bound parameters.
	default:
		return "", fmt.Errorf("unsupported dialect %s", dialect)
	}
}

func placeholder(dialect Dialect, index int) string {
	if dialect == DialectPostgres {
		return fmt.Sprintf("$%d", index)
	}
	return "?"
}

func normalizeRestoreValue(value any) any {
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
