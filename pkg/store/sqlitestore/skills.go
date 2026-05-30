package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/schardosin/astonish/pkg/skills"
	"github.com/schardosin/astonish/pkg/store"
)

// sqliteSkillStore implements store.SkillStore.
type sqliteSkillStore struct {
	db         *sql.DB
	table      string // "skills" or "org_skills"
	filesTable string // "skill_files" or "org_skill_files"
}

func (s *sqliteSkillStore) LoadAll(ctx context.Context) ([]store.Skill, error) {
	return s.List(ctx)
}

func (s *sqliteSkillStore) Get(ctx context.Context, name string) (*store.Skill, error) {
	row := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT name, content, COALESCE(validation_status, 'unknown'), COALESCE(validation_meta, '') FROM %s WHERE name = ?`, s.table), name)

	var dbName, rawContent, valStatus, valMeta string
	err := row.Scan(&dbName, &rawContent, &valStatus, &valMeta)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	skill := parseStoredSkillSQLite(dbName, rawContent)
	skill.ValidationStatus = valStatus
	skill.ValidationMeta = valMeta
	return skill, nil
}

func (s *sqliteSkillStore) Save(ctx context.Context, skill *store.Skill) error {
	valStatus := skill.ValidationStatus
	if valStatus == "" {
		valStatus = "unknown"
	}
	valMeta := skill.ValidationMeta
	if valMeta == "" {
		valMeta = ""
	}
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (id, name, content, frontmatter, validation_status, validation_meta, created_by, created_at, updated_at)
		 VALUES (hex(randomblob(16)), ?, ?, '{}', ?, ?, ?, datetime('now'), datetime('now'))
		 ON CONFLICT(name) DO UPDATE SET content = excluded.content, validation_status = excluded.validation_status, validation_meta = excluded.validation_meta, updated_at = datetime('now')`, s.table),
		skill.Name, skill.Content, valStatus, nilStr(valMeta), nilStr(skill.CreatedBy))
	return err
}

func (s *sqliteSkillStore) Delete(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE name = ?`, s.table), name)
	return err
}

func (s *sqliteSkillStore) UpdateValidationStatus(ctx context.Context, name, status, meta string) error {
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET validation_status = ?, validation_meta = ?, updated_at = datetime('now') WHERE name = ?`, s.table),
		status, nilStr(meta), name)
	return err
}

func (s *sqliteSkillStore) List(ctx context.Context) ([]store.Skill, error) {
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT name, content, COALESCE(validation_status, 'unknown') FROM %s ORDER BY name`, s.table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.Skill
	for rows.Next() {
		var dbName, rawContent, valStatus string
		if err := rows.Scan(&dbName, &rawContent, &valStatus); err != nil {
			return nil, err
		}
		skill := parseStoredSkillSQLite(dbName, rawContent)
		skill.ValidationStatus = valStatus
		result = append(result, *skill)
	}
	return result, rows.Err()
}

// parseStoredSkillSQLite re-parses the raw SKILL.md content stored in SQLite
// to populate all structured fields. Falls back to a minimal Skill
// with just the name and raw content if parsing fails.
func parseStoredSkillSQLite(name, rawContent string) *store.Skill {
	parsed, err := skills.ParseSkillFile("sqlite:"+name, []byte(rawContent))
	if err != nil {
		return &store.Skill{
			Name:    name,
			Content: rawContent,
			Source:  "custom",
		}
	}
	return &store.Skill{
		Name:        parsed.Name,
		Description: parsed.Description,
		OS:          parsed.OS,
		RequireBins: parsed.RequireBins,
		RequireEnv:  parsed.RequireEnv,
		Metadata:    parsed.Metadata,
		Content:     rawContent,
		Source:      "custom",
	}
}

// --- Multi-file support methods (skill_files table) ---

func (s *sqliteSkillStore) ListFiles(ctx context.Context, skillName string) ([]store.SkillFile, error) {
	// First resolve skill ID from name
	var skillID string
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT id FROM %s WHERE name = ?`, s.table), skillName).Scan(&skillID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, skill_id, path, filename, content, is_executable, size_bytes, created_at, updated_at
		FROM %s
		WHERE skill_id = ?
		ORDER BY path, filename`, s.filesTable), skillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []store.SkillFile
	for rows.Next() {
		var f store.SkillFile
		if err := rows.Scan(&f.ID, &f.SkillID, &f.Path, &f.Filename, &f.Content, &f.IsExecutable, &f.SizeBytes, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (s *sqliteSkillStore) GetFile(ctx context.Context, skillName, path, filename string) (*store.SkillFile, error) {
	var skillID string
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT id FROM %s WHERE name = ?`, s.table), skillName).Scan(&skillID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	row := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id, skill_id, path, filename, content, is_executable, size_bytes, created_at, updated_at
		FROM %s
		WHERE skill_id = ? AND path = ? AND filename = ?`, s.filesTable),
		skillID, path, filename)

	var f store.SkillFile
	if err := row.Scan(&f.ID, &f.SkillID, &f.Path, &f.Filename, &f.Content, &f.IsExecutable, &f.SizeBytes, &f.CreatedAt, &f.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &f, nil
}

func (s *sqliteSkillStore) SaveFile(ctx context.Context, skillName string, file *store.SkillFile) error {
	var skillID string
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT id FROM %s WHERE name = ?`, s.table), skillName).Scan(&skillID)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (id, skill_id, path, filename, content, is_executable, size_bytes, created_at, updated_at)
		VALUES (hex(randomblob(16)), ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
		ON CONFLICT(skill_id, path, filename) DO UPDATE SET
			content = excluded.content,
			is_executable = excluded.is_executable,
			size_bytes = excluded.size_bytes,
			updated_at = datetime('now')`, s.filesTable),
		skillID, file.Path, file.Filename, file.Content, file.IsExecutable, file.SizeBytes)
	return err
}

func (s *sqliteSkillStore) DeleteFile(ctx context.Context, skillName, path, filename string) error {
	var skillID string
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT id FROM %s WHERE name = ?`, s.table), skillName).Scan(&skillID)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM %s
		WHERE skill_id = ? AND path = ? AND filename = ?`, s.filesTable),
		skillID, path, filename)
	return err
}
